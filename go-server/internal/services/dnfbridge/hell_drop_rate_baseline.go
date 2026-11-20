package dnfbridge

// currentHellDropRateBaseline reads the PVF reference rate shown in the GM
// panel. It uses the first configured level row and the first difficulty
// column, which is the value operators tune in etc/itemdropinfo_monster_hell.etc.
func (s *Service) currentHellDropRateBaseline() (int, bool) {
	c, err := s.currentPVFItemCatalog()
	if err != nil || c == nil {
		return 0, false
	}
	info, err := c.hellPartyRewardDropInfo()
	if err != nil || len(info.Probabilities) == 0 {
		return 0, false
	}
	row := info.Probabilities[0]
	if len(row.Rates) == 0 {
		return 0, false
	}
	return int(row.Rates[0]), true
}
