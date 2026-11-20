package dnfbridge

import "testing"

func TestHellCountTierExcludesRateTier(t *testing.T) {
	t.Cleanup(func() {
		hellPieceCountOverride.Store(-1)
		nativeHellRateOverride.Store(-1)
	})
	if err := setNativeHellRateOverride(125); err != nil {
		t.Fatal(err)
	}
	if got := hellPieceCount(); got != -1 {
		t.Fatalf("rate tier left count tier active: %d", got)
	}
	if err := setHellPieceCountOverride(3); err != nil {
		t.Fatal(err)
	}
	if got := int(nativeHellRateOverride.Load()); got != -1 {
		t.Fatalf("count tier left rate tier active: %d", got)
	}
	if err := setNativeHellRateOverride(-1); err != nil {
		t.Fatal(err)
	}
	if got := hellPieceCount(); got != -1 {
		t.Fatalf("clear rate did not clear count tier: %d", got)
	}
	if err := setNativeHellRateOverride(125); err != nil {
		t.Fatal(err)
	}
	if err := setHellPieceCountOverride(-1); err != nil {
		t.Fatal(err)
	}
	if got := int(nativeHellRateOverride.Load()); got != -1 {
		t.Fatalf("clear count did not clear rate tier: %d", got)
	}
}

func TestHellCountTierValidation(t *testing.T) {
	t.Cleanup(func() { hellPieceCountOverride.Store(-1) })
	for _, c := range []int{-2, 1, 6, 99} {
		if err := setHellPieceCountOverride(c); err == nil {
			t.Fatalf("count %d should be rejected", c)
		}
	}
	for _, c := range []int{-1, 0, 2, 3, 4, 5} {
		if err := setHellPieceCountOverride(c); err != nil {
			t.Fatalf("count %d should be accepted: %v", c, err)
		}
	}
}
