package dnfbridge

import (
	"fmt"
	pvf "longheng.io/server/internal/modules/dnf/pvf"
	repo "longheng.io/server/internal/modules/dnf/repository"
	"math"
	"time"
)

type currentCeraDiscountCoupon struct{ Group, Item, Base, Discount, Percent uint32 }

func parseCurrentCeraDiscountCoupons(d *pvf.Document, products map[uint32]currentCeraShopProduct) (map[uint32]currentCeraDiscountCoupon, error) {
	out := map[uint32]currentCeraDiscountCoupon{}
	ts, found := d.Section("private coupon discounts")
	if !found {
		return out, nil
	}
	v := currentCeraShopPVFValues(ts)
	if len(v)%5 != 0 {
		return nil, fmt.Errorf("coupon discount row width")
	}
	for i := 0; i < len(v); i += 5 {
		var n [5]uint32
		for j := range n {
			var ok bool
			n[j], ok = currentCeraShopPVFUint32(v[i+j])
			if !ok || n[j] == 0 {
				return nil, fmt.Errorf("invalid coupon discount row %d", i/5)
			}
		}
		r := currentCeraDiscountCoupon{n[0], n[1], n[2], n[3], n[4]}
		base, bok := products[r.Base]
		p, pok := products[r.Discount]
		if _, duplicate := out[r.Discount]; duplicate {
			return nil, fmt.Errorf("duplicate coupon discount binding %d", r.Discount)
		}
		if !bok || !pok {
			// 绑定引用的商品已从 cerashop 下架（运营删除商城条目后折扣行仍
			// 引用）：商品买不到，绑定随之失效，跳过而不是拒绝整个 catalog。
			continue
		}
		if r.Base == r.Discount || r.Percent >= 100 {
			// 结构性错误：自身绑定或比例非法，仍然 fail-closed。
			return nil, fmt.Errorf("invalid coupon discount binding %d", r.Discount)
		}
		if base.Section != "package" || p.Section != base.Section || p.ItemID != base.ItemID || p.Count != base.Count ||
			p.CeraPrice == 0 || uint64(p.CeraPrice) != uint64(base.CeraPrice)*uint64(r.Percent)/100 {
			// 数据不一致（定制 PVF 折后价与折扣比例不符等）：降级为跳过——
			// 该行不入折扣表，玩家用不了这张券、无法利用异常价，catalog 其余
			// 部分不受影响。2026-11-17：上游代码未变但 1500374（39900×90%
			// ≠31410）开始触发整 catalog 拒绝，定制档脏数据不应全商城连坐。
			continue
		}
		out[r.Discount] = r
	}
	for _, r := range out {
		if _, nested := out[r.Base]; nested {
			return nil, fmt.Errorf("nested coupon discount %d", r.Base)
		}
	}
	return out, nil
}

func (c *pvfCeraShopCatalog) resolveDiscountCoupon(q currentCeraShopPurchaseRequestItem, payment byte, now time.Time) (currentCeraDiscountCoupon, error) {
	rule, discount := c.discountCoupons[q.CommodityID]
	if !discount {
		if q.DiscountCouponItemID != 0 {
			return rule, fmt.Errorf("coupon supplied for undiscounted commodity")
		}
		return rule, nil
	}
	if payment != currentCeraShopPaymentModeCera || q.DiscountCouponItemID != rule.Item || q.DiscountCouponSlot < 0 || repo.IsAccountSharedInventorySlot(repo.MainInventoryListType, q.DiscountCouponSlot) {
		return rule, fmt.Errorf("required coupon missing or wrong")
	}
	def, e := c.items.ResolveItem(rule.Item)
	if e != nil {
		return rule, e
	}
	if def.Kind != dungeonDropItemStackable || def.StackableType != "[coupon]" || (!def.ExpirationDate.IsZero() && !now.Before(def.ExpirationDate)) {
		return rule, fmt.Errorf("invalid or expired coupon %d", rule.Item)
	}
	doc, e := parseDungeonCardPVFDocument(c.source, def.PVFPath)
	if e != nil {
		return rule, e
	}
	ts, ok := doc.Section("stackable type")
	if !ok {
		return rule, fmt.Errorf("coupon definition missing")
	}
	v := currentCeraShopPVFValues(ts)
	if len(v) < 2 {
		return rule, fmt.Errorf("coupon group missing")
	}
	group, ok := currentCeraShopPVFUint32(v[1])
	if !ok || group != rule.Group {
		return rule, fmt.Errorf("coupon group mismatch")
	}
	return rule, nil
}

func consumeCurrentCeraDiscountCoupon(inventory *repo.InventoryRecord, id uint32, slot int16, now time.Time) (currentItemListEntry, error) {
	if inventory == nil || slot < 0 || repo.IsAccountSharedInventorySlot(repo.MainInventoryListType, slot) {
		return currentItemListEntry{}, errCurrentCeraShopOwnerUnavailable
	}
	key := fmt.Sprintf("0:%d", slot)
	stack, ok := inventory.Slots[key]
	if !ok || stack.ItemID != int64(id) || stack.Count < 1 || (!stack.ExpireAt.IsZero() && !now.Before(stack.ExpireAt)) {
		return currentItemListEntry{}, fmt.Errorf("coupon slot unavailable or expired")
	}
	stack.Count--
	if stack.Count == 0 {
		delete(inventory.Slots, key)
		var removed currentItemListEntry
		removed.patchCore(slot, math.MaxUint32, 0)
		return removed, nil
	}
	entry := currentItemListEntryFromStack(repo.MainInventoryListType, slot, stack)
	stack.RawEntry = append([]byte(nil), entry.data[:]...)
	inventory.Slots[key] = stack
	return entry, nil
}

func (c *pvfCeraShopCatalog) couponBaseCommodity(id uint32) uint32 {
	if r, ok := c.discountCoupons[id]; ok {
		return r.Base
	}
	return id
}
