package dnfbridge

import "fmt"

// currentFixedCountHellReward implements the GM "guaranteed 1..N pieces" tier.
// It first consumes the exact native PVF RNG stream so the separate epic-piece
// guarantee that runs later in the same room keeps its original sequence, then
// discards those probabilistic items and samples the fixed tier from a private
// RNG seeded by the room seed. Only real .equ candidates can be selected.
func currentFixedCountHellReward(request hellPartyRewardRequest, max int, roomSeed uint32) (hellPartyRewardResult, error) {
	native, err := currentNativePVFHellReward(request)
	if err != nil {
		return native, err
	}
	if max <= 0 {
		native.Selected = nil
		native.RateSource = "runtime_count_tier_off"
		native.RateOverridden = true
		return native, nil
	}

	pick := request
	pick.rng = newCurrentDungeonDropLCG(roomSeed)
	// The tier is a range, not a fixed grant: choose one target in 1..max
	// from a private stream, then force exactly that many pieces.
	target := 1 + pick.rng.Next(max)
	pick.RewardRollCount = int64(target * 3)
	forced := hellPartyRewardDropChanceDenominator - 1
	picked, err := currentNativePVFHellRewardWithChance(pick, &forced)
	if err != nil {
		return native, err
	}

	native.Selected = append([]hellPartyRewardItem(nil), picked.Selected...)
	if len(native.Selected) > target {
		native.Selected = native.Selected[:target]
	}
	for len(native.Selected) < target && len(picked.Selected) > 0 {
		native.Selected = append(native.Selected, picked.Selected[len(native.Selected)%len(picked.Selected)])
	}
	native.RateSource = fmt.Sprintf("runtime_count_tier:%d", target)
	native.RateOverridden = true
	return native, nil
}
