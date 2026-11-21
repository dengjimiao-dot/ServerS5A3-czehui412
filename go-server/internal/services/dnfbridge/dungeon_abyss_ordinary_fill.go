package dnfbridge

// 深渊/裂缝"补普通装备"档：一局装备总数（史诗 + 白 + 蓝）封顶到
// abyssOrdinaryFillTarget 件。史诗保持现有掉落逻辑一字不动，缺口用
// 白装/蓝装补齐，按 3:2 分配（白多蓝少）。紫装(rarity 2)和神器(rarity 3)
// 明确不补——selectOrdinaryMonsterEquipment 只传 0/1，天然排除它们。
//
// 独立 RNG：补齐用的随机流从房间 seed 派生，与史诗/碎片的既有流分离，
// 因此不改变任何现有史诗件数、稀有度、史诗碎片序列。
const (
	abyssOrdinaryFillTarget = 8
	abyssOrdinaryWhiteShare = 3 // 白:蓝 = 3:2
	abyssOrdinaryBlueShare  = 2
	// 补齐装备的等级窗口（相对副本 basis/min 等级的上下浮动）。
	abyssOrdinaryLevelDown = 12
	abyssOrdinaryLevelUp   = 8
)

// fillAbyssOrdinaryEquipment 按当前史诗件数把装备总数补到 8 件。
// epicCount 是本局已经落地的史诗件数；返回的普通装备不含紫/神器。
// catalog 或 rng 为空、等级非法时返回 nil，绝不阻断副本结算。
func fillAbyssOrdinaryEquipment(rng *currentDungeonDropLCG, catalog *pvfDungeonDropCatalog, basisLevel, minimumLevel int64, epicCount int) []hellPartyRewardItem {
	if rng == nil || catalog == nil || epicCount >= abyssOrdinaryFillTarget {
		return nil
	}
	// 用传入流的当前 seed 派生一条**独立**子流：补齐的抽取只推进子流，
	// 调用方持有的 rng.seed 保持不动，史诗/碎片/材料序列逐字节不受影响。
	rng = newCurrentDungeonDropLCG(rng.Seed() ^ 0x5bd1e995)
	need := abyssOrdinaryFillTarget - epicCount
	if need <= 0 {
		return nil
	}
	level := basisLevel
	if level <= 0 {
		level = minimumLevel
	}
	if level <= 0 || level > 200 {
		return nil
	}
	// 3:2 分配：先算白装数，余数给蓝装。need=5 -> 白3蓝2；need=1 -> 白1。
	white := need * abyssOrdinaryWhiteShare / (abyssOrdinaryWhiteShare + abyssOrdinaryBlueShare)
	blue := need - white
	out := make([]hellPartyRewardItem, 0, need)
	appendRarity := func(rarity int64, count int) {
		for i := 0; i < count; i++ {
			itemID, selected, err := catalog.selectOrdinaryMonsterEquipment(
				rng, int(level), abyssOrdinaryLevelDown, abyssOrdinaryLevelUp, rarity,
			)
			if err != nil || !selected || itemID == 0 {
				continue
			}
			out = append(out, hellPartyRewardItem{ItemID: itemID, Amount: 1})
		}
	}
	appendRarity(0, white) // 白装
	appendRarity(1, blue)  // 蓝装
	return out
}
