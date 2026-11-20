package dnfbridge

import (
	"fmt"
	"sync/atomic"
)

// Runtime override for native (official-24.9+) hell epic drop rate.
// Stored as chance-per-1000 in the range 0..1000; -1 means "no override,
// follow the PVF baseline". Written by the private game admin API and read
// at the point of each settlement, so a change takes effect without restart.
var nativeHellRateOverride atomic.Int32

func init() { nativeHellRateOverride.Store(-1) }

// setNativeHellRateOverride validates and installs a runtime rate override.
// Pass -1 to clear the override and fall back to PVF.
func setNativeHellRateOverride(rate int) error {
	if rate < -1 || rate > 1000 {
		return fmt.Errorf("hell epic rate must be -1 (PVF) or 0..1000, got %d", rate)
	}
	nativeHellRateOverride.Store(int32(rate))
	return nil
}

// nativeHellRate returns the effective chance and whether an override is active.
func nativeHellRate(pvfRate int) (int, bool) {
	o := nativeHellRateOverride.Load()
	if o < 0 {
		return pvfRate, false
	}
	return int(o), true
}

// hellDropRateSnapshot is the JSON shape shared with the GM panel via adminapi.
type hellDropRateSnapshot struct {
	Rate        int  `json:"rate"`
	Override    bool `json:"override"`
	PVFBaseline int  `json:"pvfBaseline"`
	Scale       int  `json:"scale"`
}
