package dnfbridge

import "testing"

func TestRiftCountTierExcludesRateTier(t *testing.T) {
	t.Cleanup(func() {
		riftPieceCountOverride.Store(-1)
		riftEpicRateOverride.Store(-1)
	})
	if err := setRiftEpicRateOverride(500); err != nil {
		t.Fatal(err)
	}
	if got := riftPieceCount(); got != -1 {
		t.Fatalf("rate tier left count tier active: %d", got)
	}
	if err := setRiftPieceCountOverride(4); err != nil {
		t.Fatal(err)
	}
	if got := int(riftEpicRateOverride.Load()); got != -1 {
		t.Fatalf("count tier left rate tier active: %d", got)
	}
	if err := setRiftEpicRateOverride(-1); err != nil {
		t.Fatal(err)
	}
	if got := riftPieceCount(); got != -1 {
		t.Fatalf("clear rate did not clear count tier: %d", got)
	}
	if err := setRiftEpicRateOverride(500); err != nil {
		t.Fatal(err)
	}
	if err := setRiftPieceCountOverride(-1); err != nil {
		t.Fatal(err)
	}
	if got := int(riftEpicRateOverride.Load()); got != -1 {
		t.Fatalf("clear count did not clear rate tier: %d", got)
	}
}

func TestRiftCountTierValidation(t *testing.T) {
	t.Cleanup(func() { riftPieceCountOverride.Store(-1) })
	for _, c := range []int{-2, 1, 6, 99} {
		if err := setRiftPieceCountOverride(c); err == nil {
			t.Fatalf("count %d should be rejected", c)
		}
	}
	for _, c := range []int{-1, 0, 2, 3, 4, 5} {
		if err := setRiftPieceCountOverride(c); err != nil {
			t.Fatalf("count %d should be accepted: %v", c, err)
		}
	}
}
