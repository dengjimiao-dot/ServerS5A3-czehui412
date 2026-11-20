package dnfbridge

import "testing"

func TestRiftEpicRateOverride(t *testing.T) {
	t.Cleanup(func() { riftEpicRateOverride.Store(-1) })
	riftEpicRateOverride.Store(-1)
	if got, over := riftEpicRate(1); got != 1000 || over {
		t.Fatalf("default: got %d over=%v", got, over)
	}
	if err := setRiftEpicRateOverride(250); err != nil {
		t.Fatal(err)
	}
	if got, over := riftEpicRate(1); got != 250 || !over {
		t.Fatalf("override: got %d over=%v", got, over)
	}
	if err := setRiftEpicRateOverride(-1); err != nil {
		t.Fatal(err)
	}
	if got, over := riftEpicRate(1); got != 1000 || over {
		t.Fatalf("cleared: got %d over=%v", got, over)
	}
}

func TestRiftEpicRateOverrideValidation(t *testing.T) {
	t.Cleanup(func() { riftEpicRateOverride.Store(-1) })
	for _, r := range []int{-2, 1001, -99} {
		if err := setRiftEpicRateOverride(r); err == nil {
			t.Fatalf("rate %d should be rejected", r)
		}
	}
	for _, r := range []int{-1, 0, 1, 1000} {
		if err := setRiftEpicRateOverride(r); err != nil {
			t.Fatalf("rate %d should be accepted: %v", r, err)
		}
	}
}
