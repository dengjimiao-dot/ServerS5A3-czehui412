package dnfbridge

import (
	"fmt"
	"sync/atomic"
)

// riftEpicRateOverride is the GM-controlled epic drop rate for 时空裂缝.
// -1 = follow PVF exactly; 0..1000 = per-mille chance to apply the PVF rift
// epic-count table (316) or to roll the epic rate (other particular-hell maps).
var riftEpicRateOverride atomic.Int32

func init() { riftEpicRateOverride.Store(-1) }

func setRiftEpicRateOverride(rate int) error {
	if rate < -1 || rate > 1000 {
		return fmt.Errorf("rift epic rate must be -1..1000 per-mille")
	}
	// Rate tiers (including the -1 clear) replace count tiers, never stack.
	riftPieceCountOverride.Store(-1)
	riftEpicRateOverride.Store(int32(rate))
	return nil
}

// riftEpicRate returns the effective per-mille rate and whether overridden.
func riftEpicRate(pvfRate float64) (int, bool) {
	o := int(riftEpicRateOverride.Load())
	if o >= 0 {
		return o, true
	}
	return int(pvfRate * 1000), false
}
