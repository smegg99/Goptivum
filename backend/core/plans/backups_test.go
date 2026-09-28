// core/plans/backups_test.go

package plans_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/migrations"
)

// upload adds the next version of a plan with its own object, the way a save
// does, and returns it.
func upload(t *testing.T, repo *plans.Repository, planID, parent int64) plans.Version {
	t.Helper()
	version, err := repo.AddVersion(context.Background(), plans.Upload{
		PlanID: planID, Parent: parent, CreatedBy: "identity-1",
		ObjectKey: fmt.Sprintf("plans/%d/v%d", planID, parent+1), SizeBytes: 10,
	})
	if err != nil {
		t.Fatalf("upload after %d: %v", parent, err)
	}
	return version
}

// numbers is a plan's timeline as version numbers, oldest first.
func numbers(t *testing.T, repo *plans.Repository, planID int64) []int64 {
	t.Helper()
	timeline, err := repo.Versions(context.Background(), planID)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	out := make([]int64, 0, len(timeline))
	for _, version := range timeline {
		out = append(out, version.Version)
	}
	return out
}

// garbage is every key waiting for the sweep, sorted.
func garbage(t *testing.T, conn *sql.DB) []string {
	t.Helper()
	rows, err := conn.Query(`select object_key from object_garbage order by object_key`)
	if err != nil {
		t.Fatalf("read garbage: %v", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			t.Fatalf("read garbage: %v", err)
		}
		out = append(out, key)
	}
	return out
}

func setKeep(t *testing.T, repo *plans.Repository, planID int64, keep int) plans.Plan {
	t.Helper()
	plan, err := repo.Update(context.Background(), planID, plans.Change{BackupsKeep: &keep})
	if err != nil {
		t.Fatalf("set backups cap %d: %v", keep, err)
	}
	return plan
}

// The cap counts only plain versions below the newest: the main file and
// every checkpoint, live or in Trash, stay whatever the cap is.
func TestTheCapKeepsTheNewestBackupsAndEveryCheckpoint(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	if kept := setKeep(t, repo, plan.ID, 2); kept.BackupsKeep != 2 {
		t.Fatalf("backups cap = %d, want 2", kept.BackupsKeep)
	}

	upload(t, repo, plan.ID, 1)
	upload(t, repo, plan.ID, 2)
	if _, err := repo.NameCheckpoint(ctx, plan.ID, 2, "tydzień i klasy"); err != nil {
		t.Fatalf("name checkpoint: %v", err)
	}
	if _, err := repo.NameCheckpoint(ctx, plan.ID, 3, "przed zmianą"); err != nil {
		t.Fatalf("name checkpoint: %v", err)
	}
	if err := repo.DeleteCheckpoint(ctx, plan.ID, 3); err != nil {
		t.Fatalf("delete checkpoint: %v", err)
	}
	for parent := int64(3); parent < 8; parent++ {
		upload(t, repo, plan.ID, parent)
	}

	// 8 is the main file, 7 and 6 the two backups, 2 and 3 the checkpoints.
	if got, want := numbers(t, repo, plan.ID), []int64{2, 3, 6, 7, 8}; !reflect.DeepEqual(got, want) {
		t.Errorf("timeline = %v, want %v", got, want)
	}
	want := []string{"plans/ab/cd/abcd", fmt.Sprintf("plans/%d/v4", plan.ID), fmt.Sprintf("plans/%d/v5", plan.ID)}
	sort.Strings(want)
	if got := garbage(t, conn); !reflect.DeepEqual(got, want) {
		t.Errorf("garbage = %v, want the keys of versions 1, 4 and 5: %v", got, want)
	}
	if latest, err := repo.Latest(ctx, plan.ID); err != nil || latest.Version != 8 {
		t.Errorf("latest = %+v, %v", latest, err)
	}
}

// Without its own cap a plan follows the server's, and dropping its own cap
// goes back to the server's.
func TestAPlanWithoutItsOwnCapFollowsTheServer(t *testing.T) {
	ctx := context.Background()
	repo := plans.NewRepository(db.TestDB(t, migrations.FS), db.SQLite, plans.Caps{BackupsKeep: 1, ResultsKeep: 20})
	if repo.DefaultBackupsKeep() != 1 {
		t.Fatalf("default cap = %d, want 1", repo.DefaultBackupsKeep())
	}
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	upload(t, repo, plan.ID, 1)
	upload(t, repo, plan.ID, 2)
	if got, want := numbers(t, repo, plan.ID), []int64{2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("timeline = %v, want %v", got, want)
	}

	setKeep(t, repo, plan.ID, 3)
	upload(t, repo, plan.ID, 3)
	upload(t, repo, plan.ID, 4)
	if got, want := numbers(t, repo, plan.ID), []int64{2, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("with its own cap of 3, timeline = %v, want %v", got, want)
	}

	// A lower cap removes nothing until the next version.
	if dropped := setKeep(t, repo, plan.ID, 0); dropped.BackupsKeep != 0 {
		t.Fatalf("backups cap = %d after dropping it", dropped.BackupsKeep)
	}
	if got := numbers(t, repo, plan.ID); len(got) != 4 {
		t.Errorf("dropping the cap removed versions at once: %v", got)
	}
	upload(t, repo, plan.ID, 5)
	if got, want := numbers(t, repo, plan.ID), []int64{5, 6}; !reflect.DeepEqual(got, want) {
		t.Errorf("back on the server's cap, timeline = %v, want %v", got, want)
	}
	if read, err := repo.Get(ctx, plan.ID); err != nil || read.BackupsKeep != 0 {
		t.Errorf("read back %+v, %v", read, err)
	}
}

func TestBackupsKeepOutsideItsRangeIsRefused(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	for _, keep := range []int{-1, plans.MaxBackupsKeep + 1} {
		_, err := repo.Update(ctx, plan.ID, plans.Change{BackupsKeep: &keep})
		var refusal *plans.Invalid
		if !errors.As(err, &refusal) || refusal.Field != "backups_keep" || refusal.Code != "invalid" {
			t.Errorf("cap %d: error = %v, want backups_keep: invalid", keep, err)
		}
	}
	if read, err := repo.Get(ctx, plan.ID); err != nil || read.BackupsKeep != 0 {
		t.Errorf("a refused cap was stored: %+v, %v", read, err)
	}
}

// A retry of an upload whose answer was lost must get that version back, not a
// conflict, even after the plan moved on.
func TestAnUploadKeyAlreadyStoredAnswersWithItsVersion(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	sent := plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/12/34/1234", SizeBytes: 50,
		CreatedBy: "identity-1", UploadKey: "k-0123456789abcdef",
	}
	first, err := repo.AddVersion(ctx, sent)
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if first.Version != 2 || first.UploadKey != sent.UploadKey {
		t.Fatalf("first send stored %+v", first)
	}

	again, err := repo.AddVersion(ctx, sent)
	if err != nil || again.Version != 2 || !again.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("second send = %+v, %v; want version 2 as stored", again, err)
	}

	upload(t, repo, plan.ID, 2)
	late, err := repo.AddVersion(ctx, sent)
	if err != nil || late.Version != 2 {
		t.Errorf("a retry after the plan moved on = %+v, %v; want version 2", late, err)
	}
	if got, want := numbers(t, repo, plan.ID), []int64{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("timeline = %v, want %v", got, want)
	}

	// The key is the plan's: another plan may use the same one.
	other := newPlan(t, repo, "identity-1", "Plan 2028")
	sent.PlanID = other.ID
	if theirs, err := repo.AddVersion(ctx, sent); err != nil || theirs.Version != 2 || theirs.PlanID != other.ID {
		t.Errorf("the same key on another plan = %+v, %v", theirs, err)
	}
}

func TestAnUploadKeyIsCheckedAndNotReused(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	sent := plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/12/34/1234",
		CreatedBy: "identity-1", UploadKey: "k-0123456789abcdef",
	}
	if _, err := repo.AddVersion(ctx, sent); err != nil {
		t.Fatalf("first send: %v", err)
	}

	for _, test := range []struct {
		name   string
		change func(plans.Upload) plans.Upload
		code   string
	}{
		{"another document", func(u plans.Upload) plans.Upload { u.ObjectKey = "plans/56/78/5678"; return u }, "reused"},
		{"another parent", func(u plans.Upload) plans.Upload { u.Parent = 2; return u }, "reused"},
		{"a short key", func(u plans.Upload) plans.Upload { u.UploadKey = "short"; return u }, "invalid"},
		{"a key with a space", func(u plans.Upload) plans.Upload { u.UploadKey = "k 0123456789abcdef"; return u }, "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := repo.AddVersion(ctx, test.change(sent))
			var refusal *plans.Invalid
			if !errors.As(err, &refusal) || refusal.Field != "upload_key" || refusal.Code != test.code {
				t.Errorf("error = %v, want upload_key: %s", err, test.code)
			}
		})
	}
	if got := numbers(t, repo, plan.ID); len(got) != 2 {
		t.Errorf("a refused upload landed: %v", got)
	}
}

// A save that does not mention the validity keeps the parent's; an empty
// value clears it.
func TestAnUploadKeepsTheParentsValidityUnlessItSaysOtherwise(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 6, 26, 0, 0, 0, 0, time.UTC)
	first := firstUpload("identity-1")
	first.ValidFrom, first.ValidTo = &from, &to
	plan, _, err := repo.Create(ctx, plans.Plan{OwnerIdentityID: "identity-1", Name: "Plan 2027"}, first)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	kept, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/a", CreatedBy: "identity-1",
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if !kept.ValidFrom.Equal(from) || !kept.ValidTo.Equal(to) {
		t.Errorf("kept validity = %v..%v, want %v..%v", kept.ValidFrom, kept.ValidTo, from, to)
	}

	later := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
	changed, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: 2, ObjectKey: "plans/b", CreatedBy: "identity-1",
		ValidFrom: &later, ValidTo: &time.Time{},
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if !changed.ValidFrom.Equal(later) || !changed.ValidTo.IsZero() {
		t.Errorf("changed validity = %v..%v, want %v..none", changed.ValidFrom, changed.ValidTo, later)
	}

	stored, err := repo.Version(ctx, plan.ID, 2)
	if err != nil || !stored.ValidFrom.Equal(from) || !stored.ValidTo.Equal(to) {
		t.Errorf("stored version 2 = %+v, %v", stored, err)
	}
}

func TestASummaryRoundTripsAndIsChecked(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	summary := plans.Summary{"added": {"day": 5, "period": 6}, "removed": {"class": 1}}
	stored, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/a", CreatedBy: "identity-1", Summary: summary,
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if !reflect.DeepEqual(stored.Summary, summary) {
		t.Errorf("stored summary = %v, want %v", stored.Summary, summary)
	}
	read, err := repo.Version(ctx, plan.ID, 2)
	if err != nil || !reflect.DeepEqual(read.Summary, summary) {
		t.Errorf("read back %v, %v; want %v", read.Summary, err, summary)
	}
	if first, err := repo.Version(ctx, plan.ID, 1); err != nil || first.Summary != nil {
		t.Errorf("a version without a summary reads %v, %v", first.Summary, err)
	}

	_, err = repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: 2, ObjectKey: "plans/b", CreatedBy: "identity-1",
		Summary: plans.Summary{"added": {"teacher": 0}},
	})
	var refusal *plans.Invalid
	if !errors.As(err, &refusal) || refusal.Field != "summary" {
		t.Errorf("a zero count = %v, want summary: invalid", err)
	}
}

func TestParseSummary(t *testing.T) {
	for _, test := range []struct {
		name string
		json string
		want plans.Summary
		ok   bool
	}{
		{"counts", `{"moved":{"lesson":2},"added":{"teacher":3}}`,
			plans.Summary{"moved": {"lesson": 2}, "added": {"teacher": 3}}, true},
		{"nothing", ``, nil, true},
		{"an empty object", `{}`, nil, true},
		{"an empty kind", `{"added":{}}`, nil, true},
		{"prose", `"added 3 teachers"`, nil, false},
		{"a fraction", `{"added":{"teacher":1.5}}`, nil, false},
		{"a negative count", `{"added":{"teacher":-1}}`, nil, false},
		{"a count too large", `{"added":{"teacher":1000001}}`, nil, false},
		{"a name with a space", `{"added":{"school day":1}}`, nil, false},
		{"a name in capitals", `{"Added":{"teacher":1}}`, nil, false},
		{"a flat count", `{"added":3}`, nil, false},
		{"trailing data", `{"added":{"teacher":1}} {}`, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := plans.ParseSummary([]byte(test.json))
			if (err == nil) != test.ok {
				t.Fatalf("error = %v, want ok %v", err, test.ok)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("summary = %v, want %v", got, test.want)
			}
		})
	}
}

// Two sends of one upload racing, as when a retry leaves before the first
// answer arrives: one version lands and both get it.
func TestConcurrentSendsOfOneUploadLandOnce(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	sent := plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/12/34/1234",
		CreatedBy: "identity-1", UploadKey: "k-0123456789abcdef",
	}

	const senders = 6
	results := make(chan error, senders)
	versions := make(chan int64, senders)
	start := make(chan struct{})
	for i := 0; i < senders; i++ {
		go func() {
			<-start
			version, err := repo.AddVersion(ctx, sent)
			versions <- version.Version
			results <- err
		}()
	}
	close(start)
	for i := 0; i < senders; i++ {
		if err := <-results; err != nil {
			t.Errorf("a send failed: %v", err)
		}
		if version := <-versions; version != 2 {
			t.Errorf("a send answered version %d, want 2", version)
		}
	}
	if got, want := numbers(t, repo, plan.ID), []int64{1, 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("timeline = %v, want %v", got, want)
	}
}
