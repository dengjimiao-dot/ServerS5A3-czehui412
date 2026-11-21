package dnfbridge

import (
	"os"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestAbyssOrdinaryFillAllocationRule(t *testing.T) {
	// 纯分配比例：白:蓝 = 3:2，总数补到 8；公式与 fillAbyssOrdinaryEquipment 一致。
	cases := []struct {
		epics, wantTotal, wantWhite, wantBlue int
	}{
		{0, 8, 4, 4},
		{1, 7, 4, 3},
		{3, 5, 3, 2},
		{5, 3, 1, 2},
		{8, 0, 0, 0},
		{10, 0, 0, 0},
	}
	for _, c := range cases {
		need := abyssOrdinaryFillTarget - c.epics
		if need <= 0 {
			if c.wantTotal != 0 || c.wantWhite != 0 || c.wantBlue != 0 {
				t.Fatalf("epics=%d expected zero fill", c.epics)
			}
			continue
		}
		white := need * abyssOrdinaryWhiteShare / (abyssOrdinaryWhiteShare + abyssOrdinaryBlueShare)
		blue := need - white
		if need != c.wantTotal || white != c.wantWhite || blue != c.wantBlue {
			t.Fatalf("epics=%d got need=%d white=%d blue=%d want %d/%d/%d", c.epics, need, white, blue, c.wantTotal, c.wantWhite, c.wantBlue)
		}
	}
}

func TestAbyssOrdinaryFillRealPVF(t *testing.T) {
	path := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if path == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to an explicit PVF")
	}
	archive, err := platformpvf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := newPVFDungeonDropCatalog(archive)
	if err != nil {
		t.Fatal(err)
	}
	// 史诗掉 3 件 -> 应补 5 件（白3蓝2），且全部是 rarity 0/1（不产紫/神器）。
	items := fillAbyssOrdinaryEquipment(newCurrentDungeonDropLCG(0x12345678), catalog, 90, 0, 3)
	if len(items) == 0 {
		t.Fatal("real PVF produced no ordinary equipment")
	}
	for _, item := range items {
		if item.Amount != 1 || item.ItemID == 0 {
			t.Fatalf("bad ordinary item: %+v", item)
		}
		definition, err := catalog.ResolveItem(item.ItemID)
		if err != nil {
			t.Fatalf("resolve %d: %v", item.ItemID, err)
		}
		if definition.Rarity < 0 || definition.Rarity > 1 {
			t.Fatalf("item %d rarity=%d, want white/blue only", item.ItemID, definition.Rarity)
		}
	}
	// 独立 RNG：传入的 rng 不被消耗（seed 不变）。
	seedRNG := newCurrentDungeonDropLCG(0xabcdef01)
	before := seedRNG.Seed()
	fillAbyssOrdinaryEquipment(seedRNG, catalog, 90, 0, 3)
	if seedRNG.Seed() != before {
		t.Fatal("fill consumed the caller RNG stream")
	}
}
