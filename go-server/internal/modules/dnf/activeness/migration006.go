package activeness

import (
	repo "longheng.io/server/internal/modules/dnf/repository"
	"time"
)

// Exact 011 -> 006 reward-only transition. Preserve points/progress/claim receipts.
// No retroactive grant, reward reset, or wildcard acceptance of unknown catalogs.
const rewards006Fingerprint int64 = 7935271219541804260

// rewards200Fingerprint is the current PVF: same tasks, happy-token rewards
// changed from 500/500/5500 to 200/500/1000. The transition is reward-only, so
// points/progress/claim receipts are preserved exactly as the 011->006 step.
const rewards200Fingerprint int64 = 2829276166620466105

func migrateRewards006(ch *repo.CharacterRecord, c *Catalog, now time.Time) error {
	if c.Fingerprint == rewards200Fingerprint {
		stored := ch.Stats[prefix+"fingerprint"]
		if stored != rewards006Fingerprint && stored != rewards200Fingerprint {
			return ErrCatalogChanged
		}
		if stored == rewards200Fingerprint {
			return nil
		}
		// Rebuild the exact pre-change catalog and validate the persisted state
		// against it before adopting the new fingerprint. Tasks are untouched;
		// only the three HappyTokens values differ (200/500/1000 -> 500/500/5500).
		old := *c
		old.Fingerprint = rewards006Fingerprint
		old.Rewards[0].HappyTokens = 500
		old.Rewards[1].HappyTokens = 500
		old.Rewards[2].HappyTokens = 5500
		prior := repo.CloneCharacter(*ch)
		if err := initState(&prior, &old, now); err != nil {
			return err
		}
		prior.Stats[prefix+"fingerprint"] = c.Fingerprint
		for key, value := range prior.Stats {
			ch.Stats[key] = value
		}
		return nil
	}
	if c.Fingerprint != rewards006Fingerprint {
		return migrateRules011(ch, c, now)
	}
	if ch.Stats[prefix+"fingerprint"] != rules011NewFingerprint && ch.Stats[prefix+"fingerprint"] != rules011OldFingerprint {
		return ErrCatalogChanged
	}
	old := *c
	old.Fingerprint = rules011NewFingerprint
	old.Rewards[0].InvitationID = 0
	old.Rewards[0].InvitationCount = 0
	prior := repo.CloneCharacter(*ch)
	if err := initState(&prior, &old, now); err != nil {
		return err
	}
	prior.Stats[prefix+"fingerprint"] = c.Fingerprint
	// initState retains a reference to this map; commit validated values in place.
	for key, value := range prior.Stats {
		ch.Stats[key] = value
	}
	return nil
}
