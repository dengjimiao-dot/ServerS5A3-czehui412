package dnfbridge

import "testing"

func TestNativeHellRateOverride(t *testing.T) {
	t.Cleanup(func() { nativeHellRateOverride.Store(-1) })
	nativeHellRateOverride.Store(-1)
	if got, over := nativeHellRate(19); got != 19 || over {
		t.Fatalf("default: got %d over=%v", got, over)
	}
	if err := setNativeHellRateOverride(125); err != nil {
		t.Fatal(err)
	}
	if got, over := nativeHellRate(19); got != 125 || !over {
		t.Fatalf("override: got %d over=%v", got, over)
	}
	if err := setNativeHellRateOverride(-1); err != nil {
		t.Fatal(err)
	}
	if got, over := nativeHellRate(19); got != 19 || over {
		t.Fatalf("cleared: got %d over=%v", got, over)
	}
}

func TestNativeHellRateOverrideValidation(t *testing.T) {
	for _, r := range []int{-2, 1001, -99} {
		if err := setNativeHellRateOverride(r); err == nil {
			t.Fatalf("rate %d should be rejected", r)
		}
	}
	for _, r := range []int{-1, 0, 1, 1000} {
		if err := setNativeHellRateOverride(r); err != nil {
			t.Fatalf("rate %d should be accepted: %v", r, err)
		}
	}
	nativeHellRateOverride.Store(-1)
}
