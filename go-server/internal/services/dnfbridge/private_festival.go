package dnfbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/festival"
	"longheng.io/server/internal/modules/dnf/pvf"
	repo "longheng.io/server/internal/modules/dnf/repository"
)

func parsePrivateFestivalCatalog(source pvf.Source, products map[uint32]currentCeraShopProduct) (*festival.Catalog, error) {
	text, e := source.ReadText(festival.PVFPath)
	if e != nil {
		return nil, nil
	} // optional on upstream PVFs
	d, e := pvf.Parse(festival.PVFPath, text)
	if e != nil {
		return nil, e
	}
	c, e := festival.Parse(d)
	if e != nil || c == nil {
		return c, e
	}
	for id := range c.Commodities {
		p, ok := products[id]
		if !ok {
			// 商品已从 cerashop 下架（运营删除商城条目后 multibonus 仍引用）：
			// 它永远不会出现在购物车里，festival 计数也不会触发，跳过即可。
			// "存在但价格/数量异常"仍然 fail-closed。
			continue
		}
		if p.Count != 1 || p.CeraPrice == 0 {
			return nil, fmt.Errorf("invalid festival paid commodity %d", id)
		}
	}
	return c, nil
}
func festivalCartUnits(c *pvfCeraShopCatalog, q currentCeraShopPurchaseRequest) map[uint32]uint32 {
	out := map[uint32]uint32{}
	if c == nil || c.festivals == nil {
		return out
	}
	for _, r := range q.Items {
		if p := c.festivals.Commodities[c.couponBaseCommodity(r.CommodityID)]; p != 0 {
			out[p]++
		}
	}
	return out
}
func (s *Service) preparePrivateFestivalPurchase(session *gameSession, c *pvfCeraShopCatalog, q *currentCeraShopPurchaseRequest, body []byte, seqs []*uint64) error {
	if len(festivalCartUnits(c, *q)) == 0 {
		return nil
	}
	if session == nil || session.loginRevoked.Load() || session.selectedCharacterID == 0 || q.PaymentMode != currentCeraShopPaymentModeCera || len(seqs) == 0 || seqs[0] == nil {
		return festival.ErrInvalid
	}
	serial := *seqs[0]
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if !bound {
		return festival.ErrInvalid
	}
	q.FestivalOwner = identity
	q.FestivalAccount = s.accountIDForSession(session)
	if q.FestivalAccount == "" {
		return festival.ErrInvalid
	}
	high, ok := session.guild.purchaseSequence.currentHigh()
	if !ok || high != serial {
		return festival.ErrReplay
	}
	id, e := session.guild.purchaseSequence.identity(serial)
	if e != nil {
		return e
	}
	h := sha256.Sum256(body)
	q.FestivalRequestID = id
	q.FestivalDigest = hex.EncodeToString(h[:])
	q.FestivalSerial = serial
	return nil
}
func (s *Service) privateFestivalPreflight(session *gameSession, a *repo.AccountRecord, q currentCeraShopPurchaseRequest) error {
	if q.FestivalRequestID == "" {
		return nil
	}
	if session == nil || a == nil || session.loginRevoked.Load() {
		return festival.ErrInvalid
	}
	if q.FestivalOwner.session != session || !s.boundGameSessionCharacterCurrent(q.FestivalOwner) || q.FestivalAccount != a.AccountID || q.FestivalAccount != s.accountIDForSession(session) {
		return festival.ErrInvalid
	}
	s.mu.Lock()
	last := session.privateFestivalCommittedSerial
	s.mu.Unlock()
	if q.FestivalSerial <= last {
		return festival.ErrReplay
	}
	st, e := festival.Load(a.Metadata[festival.StateKey(strconv.Itoa(int(q.FestivalOwner.character)))])
	if e != nil {
		return e
	}
	return festival.CheckReceipt(st, q.FestivalRequestID, q.FestivalDigest)
}
func projectPrivateFestivalPurchase(c *pvfCeraShopCatalog, a *repo.AccountRecord, character string, q currentCeraShopPurchaseRequest, purchases []currentCeraShopResolvedPurchase) ([]festival.Award, error) {
	units := festivalCartUnits(c, q)
	if len(units) == 0 {
		return nil, nil
	}
	if q.FestivalRequestID == "" || len(purchases) != len(q.Items) {
		return nil, festival.ErrInvalid
	}
	// Resolved checkout products must agree with the whitelist and paid units.
	for _, p := range purchases {
		if c.festivals.Commodities[c.couponBaseCommodity(p.Product.CommodityID)] != 0 && (p.Product.Count != 1 || p.Product.CeraPrice == 0) {
			return nil, festival.ErrInvalid
		}
	}
	st, e := festival.Load(a.Metadata[festival.StateKey(character)])
	if e != nil {
		return nil, e
	}
	minted, e := festival.Apply(c.festivals, &st, units, character, q.FestivalRequestID, q.FestivalDigest)
	if e != nil {
		return nil, e
	}
	raw, e := festival.Save(st)
	if e != nil {
		return nil, e
	}
	if a.Metadata == nil {
		a.Metadata = map[string]string{}
	}
	a.Metadata[festival.StateKey(character)] = raw
	return minted, nil
}

// commitPrivateFestivalAwardsInTransaction grants minted tier awards into the
// buyer's inventory inside the checkout transaction. Awards that do not fit
// (backpack full) stay in Pending and fall back to mailbox delivery by
// settlePrivateFestival, so a full backpack never fails the purchase.
// Delivered awards are removed from Pending here; the account clone is
// persisted by the surrounding checkout transaction. Returns one changed slot
// per granted stack (same shape as the ordinary product grant loop) plus the
// op64 bonus display rows.
func commitPrivateFestivalAwardsInTransaction(
	a *repo.AccountRecord,
	inventory *repo.InventoryRecord,
	c *pvfCeraShopCatalog,
	awards []festival.Award,
	character string,
	now time.Time,
) ([]currentCeraShopChangedSlot, []currentCeraShopBonusItem, error) {
	if a == nil || inventory == nil || c == nil || len(awards) == 0 {
		return nil, nil, nil
	}
	delivered := make(map[int]struct{}, len(awards))
	bonuses := make([]currentCeraShopBonusItem, 0, len(awards))
	changed := make([]currentCeraShopChangedSlot, 0, len(awards))
	for i, award := range awards {
		definition, err := c.items.ResolveItem(award.Reward.Item)
		if err != nil {
			return changed, bonuses, fmt.Errorf("private festival award item=%d: %v", award.Reward.Item, err)
		}
		definition, err = currentPVFItemDefinitionForGrantAt(definition, now)
		if err != nil {
			return changed, bonuses, fmt.Errorf("private festival award item=%d expiration: %v", award.Reward.Item, err)
		}
		// Trial grant on a cloned slot map: grantCurrentCeraShopProductAt may
		// partially fill stacks before failing on a full backpack, so write
		// back only the touched slots after a full success.
		trial := *inventory
		trial.Slots = make(map[string]repo.ItemStack, len(inventory.Slots)+1)
		for key, value := range inventory.Slots {
			trial.Slots[key] = value
		}
		slots, err := grantCurrentCeraShopProductAt(&trial, definition, award.Reward.Count, now)
		if err != nil {
			continue // backpack full: leave in Pending for mailbox delivery
		}
		for _, slot := range slots {
			key := currentCeraShopInventorySlotKey(repo.MainInventoryListType, int16(slot))
			inventory.Slots[key] = trial.Slots[key]
			changed = append(changed, currentCeraShopChangedSlot{
				ListType: repo.MainInventoryListType,
				Slot:     int16(slot),
				Product:  currentCeraShopProduct{ItemID: award.Reward.Item, Count: award.Reward.Count},
				Festival: true,
			})
		}
		delivered[i] = struct{}{}
		bonuses = append(bonuses, currentCeraShopBonusItem{ItemID: award.Reward.Item, Count: award.Reward.Count})
	}
	if len(delivered) == 0 {
		return nil, nil, nil
	}
	st, e := festival.Load(a.Metadata[festival.StateKey(character)])
	if e != nil {
		return changed, bonuses, e
	}
	keep := make([]festival.Award, 0, len(st.Pending))
	for _, pending := range st.Pending {
		consumed := false
		for i := range awards {
			if _, ok := delivered[i]; !ok {
				continue
			}
			if pending.Period == awards[i].Period && pending.At == awards[i].At && pending.Character == awards[i].Character {
				consumed = true
				break
			}
		}
		if !consumed {
			keep = append(keep, pending)
		}
	}
	st.Pending = keep
	raw, e := festival.Save(st)
	if e != nil {
		return changed, bonuses, e
	}
	a.Metadata[festival.StateKey(character)] = raw
	return changed, bonuses, nil
}

// A failure here does not roll back an already-paid order: its durable outbox
// remains pending. The next authenticated status request retries it. Each mail
// and removal of its pending award commit together; no independent receipt write.
func (s *Service) settlePrivateFestival(ctx context.Context, session *gameSession) (festival.State, *festival.Catalog, error) {
	if session == nil || session.loginRevoked.Load() || session.selectedCharacterID == 0 {
		return festival.State{}, nil, festival.ErrInvalid
	}
	identity, bound := s.boundGameSessionCharacterSnapshot(session)
	if !bound {
		return festival.State{}, nil, festival.ErrInvalid
	}
	account := s.accountIDForSession(session)
	character := strconv.Itoa(int(identity.character))
	c, e := s.currentCeraShopCatalog()
	if e != nil {
		return festival.State{}, nil, e
	}
	if c.festivals == nil {
		return festival.State{}, nil, festival.ErrInvalid
	}
	g, ok := s.repositoryGroup()
	if !ok {
		return festival.State{}, nil, festival.ErrInvalid
	}
	uow, ok := g.MailboxAssets.(repo.AccountMailboxUnitOfWork)
	if !ok {
		return festival.State{}, nil, festival.ErrInvalid
	}
	now := s.gameplayNow()
	var state festival.State
	e = uow.WithinAccountMailbox(ctx, account, character, func(accounts repo.AccountRepository, boxes repo.MailboxRepository) error {
		if !s.boundGameSessionCharacterCurrent(identity) || session.loginRevoked.Load() {
			return festival.ErrInvalid
		}
		a, found, e := accounts.Load(ctx, account)
		if e != nil {
			return e
		}
		if !found {
			return festival.ErrInvalid
		}
		state, e = festival.Load(a.Metadata[festival.StateKey(character)])
		if e != nil {
			return e
		}
		keep := make([]festival.Award, 0, len(state.Pending))
		delivered := 0
		for _, award := range state.Pending {
			if award.Character != character {
				keep = append(keep, award)
				continue
			}
			def, e := c.items.ResolveItem(award.Reward.Item)
			if e != nil {
				return e
			}
			def, e = currentPVFItemDefinitionForGrantAt(def, now)
			if e != nil {
				return e
			}
			if def.Kind != dungeonDropItemStackable || (!def.ExpirationDate.IsZero() && !def.ExpirationDate.After(now)) {
				return festival.ErrInvalid
			}
			name := c.festivals.Periods[award.Period].Name
			if name == "" {
				name = "节日礼包"
			}
			_, e = repo.AppendSystemMail(ctx, boxes, repo.SystemMailDelivery{RecipientCharacterID: character, Title: fmt.Sprintf("%s第%d套赠礼", name, award.At), Body: "本角色累计购买奖励，请查收附件。", Source: "private_festival", CompletionKey: festival.CompletionKey(award), CreatedAt: now, Attachments: []repo.MailAttachment{{ItemID: int64(award.Reward.Item), Count: int64(award.Reward.Count), ExpireAt: def.ExpirationDate}}})
			if e != nil {
				return e
			}
			delivered++
		}
		if delivered == 0 {
			return nil
		}
		state.Pending = keep
		raw, e := festival.Save(state)
		if e != nil {
			return e
		}
		if !s.boundGameSessionCharacterCurrent(identity) || session.loginRevoked.Load() {
			return festival.ErrInvalid
		}
		return repo.SaveAccountMetadataEntry(ctx, accounts, a, festival.StateKey(character), raw, now)
	})
	return state, c.festivals, e
}
