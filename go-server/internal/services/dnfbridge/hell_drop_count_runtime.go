package dnfbridge

import (
	"fmt"
	"sync/atomic"
)

// hellPieceCountOverride is the GM-controlled "guaranteed pieces" mode for
// official-24.9 ordinary abyss rooms.
//
//	 -1 = no count tier (PVF baseline or the legacy per-mille rate override)
//	  0 = disable ordinary abyss epic equipment drops
//	2..5 = final room drop count is 1..N, uniformly distributed by the existing
//	       room RNG; the first piece is always granted.
var hellPieceCountOverride atomic.Int32

func init() { hellPieceCountOverride.Store(-1) }

func setHellPieceCountOverride(count int) error {
	if count != -1 && count != 0 && (count < 2 || count > 5) {
		return fmt.Errorf("hell epic piece count must be -1, 0, or 2..5, got %d", count)
	}
	// Count tiers (including the -1 clear) replace the per-mille path,
	// never stack with it. -1 means a full return to PVF.
	nativeHellRateOverride.Store(-1)
	hellPieceCountOverride.Store(int32(count))
	return nil
}

func hellPieceCount() int { return int(hellPieceCountOverride.Load()) }
