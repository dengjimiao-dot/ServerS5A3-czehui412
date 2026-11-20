package dnfbridge

import (
	"fmt"
	"sync/atomic"
)

// riftPieceCountOverride is the GM-controlled "guaranteed pieces" mode for
// rift boss settlements.
//
//	 -1 = no count tier (PVF/rate mode)
//	  0 = disable rift epic equipment drops
//	2..5 = final count is 1..N, uniformly distributed by the existing boss RNG
var riftPieceCountOverride atomic.Int32

func init() { riftPieceCountOverride.Store(-1) }

func setRiftPieceCountOverride(count int) error {
	if count != -1 && count != 0 && (count < 2 || count > 5) {
		return fmt.Errorf("rift epic piece count must be -1, 0, or 2..5, got %d", count)
	}
	// Count tiers (including the -1 clear) replace the rate path, never stack.
	riftEpicRateOverride.Store(-1)
	riftPieceCountOverride.Store(int32(count))
	return nil
}

func riftPieceCount() int { return int(riftPieceCountOverride.Load()) }
