package festival

import (
	"longheng.io/server/internal/modules/dnf/pvf"
	"testing"
)

func cycleTestCatalog(repeatFrom uint32, repeat Reward) *Catalog {
	tiers := map[uint32]Reward{}
	for at := uint32(2); at <= 4; at++ {
		tiers[at] = Reward{Item: at + 1000, Count: 1}
	}
	return &Catalog{
		Periods: map[uint32]Period{201801: {ID: 201801, Name: "2018", RepeatFrom: repeatFrom, Repeat: repeat, Tiers: tiers}},
	}
}

func TestCycleModeReplaysTiersBySetNumber(t *testing.T) {
	c := cycleTestCatalog(5, Reward{}) // cycle=4: sets 1..4 template, 5 replays 1, 6 replays 2
	p := c.Periods[201801]
	if !p.Cycle() {
		t.Fatalf("cycle mode not detected: %+v", p)
	}
	st := State{Counts: map[uint32]uint32{}}
	if e := Apply(c, &st, map[uint32]uint32{201801: 8}, "hero", "r1", "d1"); e != nil {
		t.Fatal(e)
	}
	if st.Counts[201801] != 8 {
		t.Fatalf("count=%d", st.Counts[201801])
	}
	// set2->1002, set3->1003, set4->1004 (set1 has no tier)
	// set5->set1(no tier), set6->set2(1002), set7->set3(1003), set8->set4(1004)
	want := map[uint32]uint32{2: 1002, 3: 1003, 4: 1004, 6: 1002, 7: 1003, 8: 1004}
	if len(st.Pending) != len(want) {
		t.Fatalf("pending=%d want %d: %+v", len(st.Pending), len(want), st.Pending)
	}
	for _, a := range st.Pending {
		if w, ok := want[a.At]; !ok || w != a.Reward.Item {
			t.Fatalf("set %d got item %d want %v", a.At, a.Reward.Item, w)
		}
	}
}

func TestCycleModeCompletionKeyUniqueAcrossRounds(t *testing.T) {
	c := cycleTestCatalog(3, Reward{}) // cycle=2: set3 replays set1, set4 replays set2
	st := State{Counts: map[uint32]uint32{}}
	if e := Apply(c, &st, map[uint32]uint32{201801: 4}, "hero", "r1", "d1"); e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, a := range st.Pending {
		k := CompletionKey(a)
		if seen[k] {
			t.Fatalf("duplicate completion key %s", k)
		}
		seen[k] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected multiple distinct keys, got %d", len(seen))
	}
}

func TestLegacyRepeatModeUnchanged(t *testing.T) {
	c := cycleTestCatalog(5, Reward{Item: 9999, Count: 1})
	st := State{Counts: map[uint32]uint32{}}
	if e := Apply(c, &st, map[uint32]uint32{201801: 6}, "hero", "r1", "d1"); e != nil {
		t.Fatal(e)
	}
	got := 0
	for _, a := range st.Pending {
		if a.At >= 5 && a.Reward.Item == 9999 {
			got++
		}
	}
	if got != 2 {
		t.Fatalf("legacy repeat awards=%d want 2: %+v", got, st.Pending)
	}
}

func TestParseAcceptsCycleModePeriod(t *testing.T) {
	// tail>0 & item=0 & count=0 must pass Parse (cycle marker).
	raw := "[private festival periods]\n201801 `2018` 25 0 0\n[/private festival periods]\n" +
		"[private festival commodities]\n900076 201801\n[/private festival commodities]\n" +
		"[private festival rewards]\n201801 2 490701782 1\n[/private festival rewards]\n"
	d, e := pvf.Parse(PVFPath, raw)
	if e != nil {
		t.Fatal(e)
	}
	c, e := Parse(d)
	if e != nil {
		t.Fatal(e)
	}
	if p, ok := c.Periods[201801]; !ok || !p.Cycle() {
		t.Fatalf("cycle period missing: %+v", c.Periods)
	}
}
