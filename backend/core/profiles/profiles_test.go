// core/profiles/profiles_test.go

package profiles_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/smegg99/s99ab/data/db"
	"google.golang.org/protobuf/proto"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/core/profiles"
	"github.com/smegg99/goptivum/backend/migrations"
)

var (
	creator  = profiles.Editor{ID: "identity-anna"}
	stranger = profiles.Editor{ID: "identity-piotr"}
	admin    = profiles.Editor{ID: "identity-admin", Admin: true}
)

func newRepository(t *testing.T) *profiles.Repository {
	t.Helper()
	return profiles.NewRepository(db.TestDB(t, migrations.FS), db.SQLite)
}

func teacherGapsOff() []*arrangov1.RuleOverride {
	return []*arrangov1.RuleOverride{{Rule: "teacher_gaps", Mode: arrangov1.RuleMode_RULE_MODE_OFF}}
}

func create(t *testing.T, repo *profiles.Repository, name string) profiles.Profile {
	t.Helper()
	created, err := repo.Create(t.Context(), profiles.NewProfile{
		Name: name, Overrides: teacherGapsOff(), CreatedBy: creator.ID, CreatedByName: "Anna Kowalska",
	})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return created
}

func refusal(t *testing.T, err error) profiles.Invalid {
	t.Helper()
	var invalid *profiles.Invalid
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *profiles.Invalid", err)
	}
	return *invalid
}

// A profile is stored as its overrides, and a solve sends exactly those as the
// request's rules: no built-in name under them.
func TestAProfileKeepsItsOverridesAndSendsThemAsItsRules(t *testing.T) {
	repo := newRepository(t)
	created := create(t, repo, "  Nasza szkoła  ")
	if created.ID == 0 || created.Name != "Nasza szkoła" || created.CreatedBy != creator.ID ||
		created.CreatedByName != "Anna Kowalska" || created.CreatedAt.IsZero() || !created.UpdatedAt.Equal(created.CreatedAt) {
		t.Fatalf("created = %+v", created)
	}

	listed, err := repo.List(t.Context())
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	read := listed[0]
	if read.ID != created.ID || read.Name != created.Name || !read.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("read = %+v, want %+v", read, created)
	}
	want := &arrangov1.RuleConfig{Overrides: teacherGapsOff()}
	if got := read.Rules(); !proto.Equal(got, want) {
		t.Errorf("rules = %v, want %v", got, want)
	}
}

// The school's profiles are listed oldest first, so a rename does not move a
// card.
func TestProfilesAreListedOldestFirst(t *testing.T) {
	repo := newRepository(t)
	first := create(t, repo, "Zima")
	second := create(t, repo, "Anatomia")
	renamed := "Aaa"
	if _, err := repo.Update(t.Context(), second.ID, creator, profiles.Change{Name: &renamed}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	listed, err := repo.List(t.Context())
	if err != nil || len(listed) != 2 || listed[0].ID != first.ID || listed[1].ID != second.ID {
		t.Fatalf("list = %+v, %v", listed, err)
	}
}

// Only the account that created a profile and administrators change or delete
// it; everybody else is refused and nothing changes.
func TestOnlyTheCreatorAndAdministratorsChangeAProfile(t *testing.T) {
	repo := newRepository(t)
	created := create(t, repo, "Nasza szkoła")
	name := "Cudza"

	if _, err := repo.Update(t.Context(), created.ID, stranger, profiles.Change{Name: &name}); !errors.Is(err, profiles.ErrNotYours) {
		t.Errorf("a stranger's rename: err = %v, want ErrNotYours", err)
	}
	if err := repo.Delete(t.Context(), created.ID, stranger); !errors.Is(err, profiles.ErrNotYours) {
		t.Errorf("a stranger's delete: err = %v, want ErrNotYours", err)
	}
	if read, err := repo.Get(t.Context(), created.ID); err != nil || read.Name != "Nasza szkoła" {
		t.Fatalf("after the refusals: %+v, %v", read, err)
	}
	if created.EditableBy(stranger) || !created.EditableBy(creator) || !created.EditableBy(admin) {
		t.Errorf("EditableBy: stranger %v, creator %v, admin %v",
			created.EditableBy(stranger), created.EditableBy(creator), created.EditableBy(admin))
	}

	softer := []*arrangov1.RuleOverride{{Rule: "student_gaps", Mode: arrangov1.RuleMode_RULE_MODE_SOFT, Weight: 5000}}
	changed, err := repo.Update(t.Context(), created.ID, admin, profiles.Change{Name: &name, Overrides: &softer})
	if err != nil {
		t.Fatalf("an administrator's change: %v", err)
	}
	if changed.Name != name || !proto.Equal(changed.Rules(), &arrangov1.RuleConfig{Overrides: softer}) ||
		changed.CreatedBy != creator.ID || !changed.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("changed = %+v", changed)
	}
	if err := repo.Delete(t.Context(), created.ID, creator); err != nil {
		t.Fatalf("the creator's delete: %v", err)
	}
	if _, err := repo.Get(t.Context(), created.ID); !errors.Is(err, profiles.ErrNotFound) {
		t.Errorf("after delete: err = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(t.Context(), created.ID, creator); !errors.Is(err, profiles.ErrNotFound) {
		t.Errorf("a second delete: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Update(t.Context(), 42, admin, profiles.Change{Name: &name}); !errors.Is(err, profiles.ErrNotFound) {
		t.Errorf("an unknown id: err = %v, want ErrNotFound", err)
	}
}

// Two profiles never share a name: a picker names them.
func TestAProfileNameIsTheSchoolsOnce(t *testing.T) {
	repo := newRepository(t)
	create(t, repo, "Nasza szkoła")
	other := create(t, repo, "Luźny")

	_, err := repo.Create(t.Context(), profiles.NewProfile{Name: " Nasza szkoła ", CreatedBy: creator.ID})
	if got := refusal(t, err); got.Field != "name" || got.Code != "taken" {
		t.Errorf("a second create: %+v", got)
	}
	taken := "Nasza szkoła"
	_, err = repo.Update(t.Context(), other.ID, creator, profiles.Change{Name: &taken})
	if got := refusal(t, err); got.Field != "name" || got.Code != "taken" {
		t.Errorf("a rename onto it: %+v", got)
	}
	same := "Luźny"
	if _, err := repo.Update(t.Context(), other.ID, creator, profiles.Change{Name: &same}); err != nil {
		t.Errorf("keeping its own name: %v", err)
	}
}

// What a profile holds is refused before it is stored: a name and a set of
// unscoped overrides, one per rule.
func TestAProfileIsRefusedWhatItCannotHold(t *testing.T) {
	repo := newRepository(t)
	tooMany := make([]*arrangov1.RuleOverride, profiles.MaxOverrides+1)
	for i := range tooMany {
		tooMany[i] = &arrangov1.RuleOverride{Rule: "rule_" + strings.Repeat("a", i+1)}
	}
	cases := []struct {
		name      string
		profile   profiles.NewProfile
		field     string
		code      string
		overrides []*arrangov1.RuleOverride
	}{
		{name: "blank name", profile: profiles.NewProfile{Name: "   "}, field: "name", code: "required"},
		{name: "long name", profile: profiles.NewProfile{Name: strings.Repeat("ż", profiles.MaxNameLength+1)}, field: "name", code: "too_long"},
		{name: "too many", profile: profiles.NewProfile{Name: "a", Overrides: tooMany}, field: "overrides", code: "too_many"},
		{name: "duplicate", profile: profiles.NewProfile{Name: "a", Overrides: []*arrangov1.RuleOverride{
			{Rule: "teacher_gaps", Mode: arrangov1.RuleMode_RULE_MODE_OFF},
			{Rule: "teacher_gaps", Weight: 3},
		}}, field: "overrides", code: "duplicate"},
		{name: "bad key", profile: profiles.NewProfile{Name: "a", Overrides: []*arrangov1.RuleOverride{{Rule: "Teacher Gaps"}}}, field: "overrides", code: "invalid"},
		{name: "long key", profile: profiles.NewProfile{Name: "a", Overrides: []*arrangov1.RuleOverride{{Rule: strings.Repeat("a", 65)}}}, field: "overrides", code: "invalid"},
		{name: "unknown mode", profile: profiles.NewProfile{Name: "a", Overrides: []*arrangov1.RuleOverride{{Rule: "teacher_gaps", Mode: 9}}}, field: "overrides", code: "invalid"},
		{name: "negative weight", profile: profiles.NewProfile{Name: "a", Overrides: []*arrangov1.RuleOverride{{Rule: "teacher_gaps", Weight: -1}}}, field: "overrides", code: "invalid"},
		{name: "scoped", profile: profiles.NewProfile{Name: "a", Overrides: []*arrangov1.RuleOverride{{Rule: "teacher_gaps", TeacherId: 3}}}, field: "overrides", code: "invalid"},
	}
	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			one.profile.CreatedBy = creator.ID
			_, err := repo.Create(t.Context(), one.profile)
			if got := refusal(t, err); got.Field != one.field || got.Code != one.code {
				t.Errorf("refusal = %+v, want %s: %s", got, one.field, one.code)
			}
		})
	}
	listed, err := repo.List(t.Context())
	if err != nil || len(listed) != 0 {
		t.Errorf("stored after refusals: %+v, %v", listed, err)
	}

	stored := create(t, repo, "Nasza szkoła")
	blank := " "
	_, err = repo.Update(t.Context(), stored.ID, creator, profiles.Change{Name: &blank})
	if got := refusal(t, err); got.Field != "name" || got.Code != "required" {
		t.Errorf("a blank rename: %+v", got)
	}
	scoped := []*arrangov1.RuleOverride{{Rule: "teacher_gaps", YearId: 1}}
	_, err = repo.Update(t.Context(), stored.ID, creator, profiles.Change{Overrides: &scoped})
	if got := refusal(t, err); got.Field != "overrides" || got.Code != "invalid" {
		t.Errorf("scoped overrides: %+v", got)
	}
}

// An empty profile is the solver's defaults, which a copy of `default` is.
func TestAProfileWithNoOverridesIsTheDefaults(t *testing.T) {
	repo := newRepository(t)
	created, err := repo.Create(t.Context(), profiles.NewProfile{Name: "Kopia default", CreatedBy: creator.ID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	read, err := repo.Get(t.Context(), created.ID)
	if err != nil || len(read.Overrides) != 0 || !proto.Equal(read.Rules(), &arrangov1.RuleConfig{}) {
		t.Errorf("read = %+v, %v", read, err)
	}
}
