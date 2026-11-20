package activeness

import (
	"errors"
	"longheng.io/server/internal/modules/dnf/pvf"
	repo "longheng.io/server/internal/modules/dnf/repository"
	"strings"
	"testing"
)

func rewards200Catalog(t *testing.T) *Catalog {
	t.Helper()
	s := strings.Replace(config, "1 1 20 2 1 20 3 1 20 4 1 100 5 1 20 6 100 20 7 1 20 8 1 20", "1 1 10 2 1 10 3 1 50 4 1 100 5 1 10 6 100 20 7 1 10 8 1 10", 1)
	s = strings.Replace(s, "2 50 2500", "2 50 500", 1)
	s = strings.Replace(s, "3 100 3500", "3 100 1000", 1)
	s = strings.Replace(s, "1 30 500 990027401 1 0 0", "1 30 200 990027401 1 3330 500", 1)
	s = strings.Replace(s, "2 50 500 990027402 1 0 0", "2 50 500 990027402 1 3330 500", 1)
	s = strings.Replace(s, "3 100 1000 990027403 1 0 0", "3 100 1000 990027403 1 3330 500", 1)
	d, e := pvf.Parse(PVFPath, s)
	if e != nil {
		t.Fatal(e)
	}
	c, e := ParseCatalog(d)
	if e != nil || c.Fingerprint != rewards200Fingerprint {
		t.Fatal(c, e)
	}
	return c
}

func TestActivityRewards200MigratesRewardOnlyPreservingProgress(t *testing.T) {
	old := rewards006Catalog(t)
	next := rewards200Catalog(t)
	if old.Tasks != next.Tasks {
		t.Fatal("tasks changed; reward-only migration is not valid")
	}
	_, _, cmd := fixture(t)
	ctx := cmd.Now

	ch := repo.CharacterRecord{Stats: map[string]int64{}}
	if e := initState(&ch, old, ctx); e != nil {
		t.Fatal(e)
	}
	for i := 1; i <= 8; i++ {
		if _, e := Apply(&ch, old, uint16(i), 100, ctx, ctx); e != nil {
			t.Fatal(e)
		}
	}
	beforePoints := ch.Stats[prefix+"points"]
	beforeDone := ch.Stats[prefix+"done"]
	beforeProgress6 := ch.Stats[progressKey(6)]

	if e := initState(&ch, next, ctx); e != nil {
		t.Fatal("reward-only migration rejected:", e)
	}
	if ch.Stats[prefix+"fingerprint"] != rewards200Fingerprint {
		t.Fatal("fingerprint not advanced")
	}
	if ch.Stats[prefix+"points"] != beforePoints || ch.Stats[prefix+"done"] != beforeDone || ch.Stats[progressKey(6)] != beforeProgress6 {
		t.Fatal("progress lost across migration")
	}
	// A later fatigue event must be recorded normally under the new catalog.
	if _, e := Apply(&ch, next, 6, 1, ctx, ctx); e != nil {
		t.Fatal("apply under new fingerprint failed:", e)
	}
}

func TestActivityRewards200RejectsUnknownFingerprint(t *testing.T) {
	next := rewards200Catalog(t)
	_, _, cmd := fixture(t)
	ch := repo.CharacterRecord{Stats: map[string]int64{
		prefix + "day":         Day(cmd.Now),
		prefix + "fingerprint": next.Fingerprint + 1,
	}}
	if e := initState(&ch, next, cmd.Now); !errors.Is(e, ErrCatalogChanged) {
		t.Fatal("unknown fingerprint accepted", e)
	}
}
