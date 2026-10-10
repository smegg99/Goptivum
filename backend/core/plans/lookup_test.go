// core/plans/lookup_test.go

package plans_test

import (
	"github.com/smegg99/goptivum/backend/core/plans"
	"testing"
)

// A node's history and the jobs of the school name the project of every job,
// whoever owns it and whether or not it is in Trash.
func TestLookupReadsEveryOwnersPlansTrashIncluded(t *testing.T) {
	ctx := t.Context()
	repo := repository(t)
	mine := newPlan(t, repo, "owner", "Plan 2027")
	theirs := newPlan(t, repo, "someone-else", "Plan liceum")
	trashed := newPlan(t, repo, "owner", "Stary plan")
	if err := repo.Trash(ctx, trashed.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}

	found, err := repo.Lookup(ctx, []int64{mine.ID, theirs.ID, trashed.ID, 424242})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(found) != 3 {
		t.Errorf("found %d plans, want 3: %+v", len(found), found)
	}
	if plan := found[mine.ID]; plan != mine {
		t.Errorf("mine = %+v, want %+v", plan, mine)
	}
	if plan := found[theirs.ID]; plan.Name != "Plan liceum" || plan.OwnerIdentityID != "someone-else" {
		t.Errorf("theirs = %+v", plan)
	}
	if plan := found[trashed.ID]; plan.Name != "Stary plan" || plan.DeletedAt.IsZero() {
		t.Errorf("trashed = %+v, want its deletion time", plan)
	}

	if empty, err := repo.Lookup(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("no ids = %v, %v, want an empty map", empty, err)
	}
}

func TestSummaryMustRejectTrailingDelimiters(t *testing.T) {
	for _, raw := range []string{`{"added":{"teacher":1}}]`, `{"added":{"teacher":1}}}`} {
		if summary, err := plans.ParseSummary([]byte(raw)); err == nil {
			t.Errorf("invalid JSON accepted: %s -> %#v", raw, summary)
		}
	}
}
