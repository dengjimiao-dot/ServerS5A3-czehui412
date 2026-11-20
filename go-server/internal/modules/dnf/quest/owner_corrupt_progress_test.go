package quest

import (
	"context"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestOwnerApplySetTriggerCollapsesImplausibleLegacyProgress(t *testing.T) {
	ctx := context.Background()
	repos := dnfrepomemory.NewMemoryGroup()
	if err := repos.Character.Save(ctx, dnfrepo.CharacterRecord{
		CharacterID: "47",
		AccountID:   "acc-corrupt-progress",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repos.Quest.Save(ctx, dnfrepo.QuestRecord{
		CharacterID: "47",
		States: map[int64]dnfrepo.QuestState{
			2900: {Status: "active", ProgressValue: 250343},
		},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := NewOwner(repos)
	if err != nil {
		t.Fatal(err)
	}
	cmd := NewTriggerCommand(alignedRequest("acc-corrupt-progress", 47), SetTriggerRequest{
		QuestID:     2900,
		TriggerType: 0,
		IsIncrement: false,
	})
	first, err := owner.ApplySetTrigger(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if !first.StateChanged || first.ProgressValue != 0 {
		t.Fatalf("first trigger: changed=%v progress=%d, want changed=true progress=0", first.StateChanged, first.ProgressValue)
	}
	second, err := owner.ApplySetTrigger(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if second.StateChanged || second.ProgressValue != 0 {
		t.Fatalf("replay: changed=%v progress=%d, want changed=false progress=0", second.StateChanged, second.ProgressValue)
	}
}
