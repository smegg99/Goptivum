// core/library/library_test.go

package library_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/core/library"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/migrations"
)

func setup(t *testing.T) (*library.Repository, *plans.Repository) {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	index := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20})
	return library.NewRepository(conn, db.SQLite, index), index
}

func newPlan(t *testing.T, index *plans.Repository, name string) int64 {
	t.Helper()
	plan, _, err := index.Create(t.Context(), plans.Plan{OwnerIdentityID: "anna", Name: name, SchoolYear: "2026/2027"},
		plans.Upload{ObjectKey: "plans/ab/cd/abcd", SizeBytes: 100, CreatedBy: "anna"})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return plan.ID
}

func empty() library.Library {
	return library.Library{Folders: []library.Folder{}, Projects: []int64{}, Pinned: []int64{}, Recent: []int64{}}
}

func stale(t *testing.T, err error) library.Library {
	t.Helper()
	var refused *library.Stale
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want *library.Stale", err)
	}
	return refused.Current
}

func TestAnAccountThatNeverSavedReadsAnEmptyArrangement(t *testing.T) {
	repo, _ := setup(t)
	got, err := repo.Get(t.Context(), "anna")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(got, empty()) {
		t.Fatalf("got %+v, want version 0 with every list empty", got)
	}
}

// Folders nest and keep their order, a folder holds its projects in order, and
// the next save names the version the first one answered.
func TestASaveIsReadBackAtTheNextVersion(t *testing.T) {
	repo, index := setup(t)
	a, b, c := newPlan(t, index, "Plan 2027"), newPlan(t, index, "Plan 2028"), newPlan(t, index, "M0 weryfikacja")

	sent := library.Library{
		Folders: []library.Folder{
			{ID: "klasy", Name: "  Klasy 1  ", Accent: "violet", Pinned: true, Projects: []int64{b, a}},
			{ID: "stare", Name: "Stare", Parent: "klasy", Projects: []int64{}},
			{ID: "zima", Name: "Zima", Projects: []int64{}},
		},
		Projects: []int64{c},
		Pinned:   []int64{b},
		Recent:   []int64{c, a},
	}
	saved, err := repo.Save(t.Context(), "anna", sent)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	want := sent
	want.Version = 1
	want.Folders = append([]library.Folder(nil), sent.Folders...)
	want.Folders[0].Name = "Klasy 1"
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved = %+v\nwant    %+v", saved, want)
	}
	read, err := repo.Get(t.Context(), "anna")
	if err != nil || !reflect.DeepEqual(read, want) {
		t.Fatalf("read = %+v, %v\nwant   %+v", read, err, want)
	}

	next := read
	next.Recent = []int64{a, c}
	again, err := repo.Save(t.Context(), "anna", next)
	if err != nil || again.Version != 2 || !reflect.DeepEqual(again.Recent, []int64{a, c}) {
		t.Fatalf("second save = %+v, %v; want version 2", again, err)
	}
}

// Two windows of one account read version 1; the second to save is refused
// with what the first stored, and nothing it sent is kept.
func TestAStaleSaveIsRefusedWithTheStoredArrangement(t *testing.T) {
	repo, index := setup(t)
	a, b := newPlan(t, index, "Plan 2027"), newPlan(t, index, "Plan 2028")

	first := empty()
	first.Recent = []int64{a}
	stored, err := repo.Save(t.Context(), "anna", first)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	second := empty()
	second.Recent = []int64{b}
	if current := stale(t, mustFail(repo.Save(t.Context(), "anna", second))); !reflect.DeepEqual(current, stored) {
		t.Fatalf("conflict carries %+v, want %+v", current, stored)
	}
	ahead := second
	ahead.Version = 7
	if current := stale(t, mustFail(repo.Save(t.Context(), "anna", ahead))); current.Version != 1 {
		t.Fatalf("a version ahead of the stored one carries %+v, want version 1", current)
	}
	if read, _ := repo.Get(t.Context(), "anna"); !reflect.DeepEqual(read, stored) {
		t.Fatalf("after the refusals the account reads %+v, want %+v", read, stored)
	}

	merged := stored
	merged.Recent = []int64{b, a}
	if saved, err := repo.Save(t.Context(), "anna", merged); err != nil || saved.Version != 2 {
		t.Fatalf("a save at the stored version = %+v, %v", saved, err)
	}
}

func mustFail(_ library.Library, err error) error { return err }

// However many windows save at once from one version, one save lands.
func TestConcurrentSavesAtOneVersionStoreOne(t *testing.T) {
	repo, index := setup(t)
	a := newPlan(t, index, "Plan 2027")

	for version := int64(0); version < 2; version++ {
		var wg sync.WaitGroup
		var mu sync.Mutex
		landed, refused := 0, 0
		for window := range 8 {
			wg.Go(func() {
				next := empty()
				next.Version = version
				next.Folders = []library.Folder{{ID: fmt.Sprintf("okno-%d", window), Name: "Okno", Projects: []int64{}}}
				next.Recent = []int64{a}
				_, err := repo.Save(t.Context(), "anna", next)
				var conflict *library.Stale
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					landed++
				case errors.As(err, &conflict):
					refused++
				default:
					t.Errorf("save at %d: %v", version, err)
				}
			})
		}
		wg.Wait()
		if landed != 1 || refused != 7 {
			t.Fatalf("at version %d: %d saves landed and %d were refused, want 1 and 7", version, landed, refused)
		}
	}
	if read, _ := repo.Get(t.Context(), "anna"); read.Version != 2 {
		t.Fatalf("read version %d, want 2", read.Version)
	}
}

func TestEachAccountReadsAndSavesOnlyItsOwn(t *testing.T) {
	repo, index := setup(t)
	a, b := newPlan(t, index, "Plan 2027"), newPlan(t, index, "Plan 2028")

	annas := empty()
	annas.Folders = []library.Folder{{ID: "moje", Name: "Moje", Projects: []int64{a}}}
	storedAnna, err := repo.Save(t.Context(), "anna", annas)
	if err != nil {
		t.Fatalf("anna's save: %v", err)
	}
	if read, err := repo.Get(t.Context(), "piotr"); err != nil || !reflect.DeepEqual(read, empty()) {
		t.Fatalf("piotr reads %+v, %v; want his own empty arrangement", read, err)
	}

	piotrs := empty()
	piotrs.Pinned = []int64{b}
	storedPiotr, err := repo.Save(t.Context(), "piotr", piotrs)
	if err != nil || storedPiotr.Version != 1 {
		t.Fatalf("piotr's first save = %+v, %v; want his own version 1", storedPiotr, err)
	}
	if read, _ := repo.Get(t.Context(), "anna"); !reflect.DeepEqual(read, storedAnna) {
		t.Fatalf("anna reads %+v after piotr saved, want %+v", read, storedAnna)
	}

	// Both are at version 1 now; anna's next save changes only hers.
	annas = storedAnna
	annas.Recent = []int64{a}
	if _, err := repo.Save(t.Context(), "anna", annas); err != nil {
		t.Fatalf("anna's second save: %v", err)
	}
	if read, _ := repo.Get(t.Context(), "piotr"); !reflect.DeepEqual(read, storedPiotr) {
		t.Fatalf("piotr reads %+v after anna's second save, want %+v", read, storedPiotr)
	}
}

// A project in Trash keeps its folder, pin and place, so a restore brings it
// back there; one removed for good is left out, and reading changes nothing.
func TestAProjectInTrashStaysAndOneRemovedForGoodIsLeftOut(t *testing.T) {
	repo, index := setup(t)
	trashed, purged, live := newPlan(t, index, "Kosz"), newPlan(t, index, "Usunięty"), newPlan(t, index, "Plan 2027")

	sent := empty()
	sent.Folders = []library.Folder{{ID: "rok", Name: "Rok 2026", Projects: []int64{purged, trashed}}}
	sent.Projects = []int64{live}
	sent.Pinned = []int64{purged, trashed}
	sent.Recent = []int64{purged, live, trashed}
	if _, err := repo.Save(t.Context(), "anna", sent); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := index.Trash(t.Context(), purged); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if removed, err := index.Purge(t.Context(), time.Now().Add(48*time.Hour), 1); err != nil || removed != 1 {
		t.Fatalf("purge = %d, %v; want 1", removed, err)
	}
	if err := index.Trash(t.Context(), trashed); err != nil {
		t.Fatalf("trash: %v", err)
	}

	want := empty()
	want.Version = 1
	want.Folders = []library.Folder{{ID: "rok", Name: "Rok 2026", Projects: []int64{trashed}}}
	want.Projects = []int64{live}
	want.Pinned = []int64{trashed}
	want.Recent = []int64{live, trashed}
	for range 2 {
		if read, err := repo.Get(t.Context(), "anna"); err != nil || !reflect.DeepEqual(read, want) {
			t.Fatalf("read = %+v, %v\nwant   %+v", read, err, want)
		}
	}

	// A save naming a project that is gone by then lands, and its answer
	// leaves the project out like every read.
	gone := want
	gone.Projects = []int64{live, purged}
	saved, err := repo.Save(t.Context(), "anna", gone)
	want.Version = 2
	if err != nil || !reflect.DeepEqual(saved, want) {
		t.Fatalf("save naming a removed project = %+v, %v\nwant %+v", saved, err, want)
	}
}

// Deleting a folder is a save without it: its projects are not touched.
func TestDeletingAFolderKeepsItsProjects(t *testing.T) {
	repo, index := setup(t)
	a := newPlan(t, index, "Plan 2027")

	with := empty()
	with.Folders = []library.Folder{{ID: "rok", Name: "Rok 2026", Projects: []int64{a}}}
	stored, err := repo.Save(t.Context(), "anna", with)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	without := empty()
	without.Version = stored.Version
	if _, err := repo.Save(t.Context(), "anna", without); err != nil {
		t.Fatalf("save without the folder: %v", err)
	}
	if live, err := index.Live(t.Context(), a); err != nil || !live {
		t.Fatalf("the project after its folder went: live = %v, %v", live, err)
	}
}

func TestWhatAnArrangementCannotHoldIsRefused(t *testing.T) {
	repo, index := setup(t)
	a, b := newPlan(t, index, "Plan 2027"), newPlan(t, index, "Plan 2028")

	folder := func(id, parent string) library.Folder {
		return library.Folder{ID: id, Name: "Folder " + id, Parent: parent, Projects: []int64{}}
	}
	chain := func(depth int) []library.Folder {
		out := []library.Folder{folder("f0", "")}
		for level := 1; level < depth; level++ {
			out = append(out, folder(fmt.Sprintf("f%d", level), fmt.Sprintf("f%d", level-1)))
		}
		return out
	}
	many := func(count int, from int64) []int64 {
		out := make([]int64, count)
		for i := range out {
			out[i] = from + int64(i)
		}
		return out
	}
	folders := func(count int) []library.Folder {
		out := make([]library.Folder, count)
		for i := range out {
			out[i] = folder(fmt.Sprintf("f%d", i), "")
		}
		return out
	}

	cases := []struct {
		name        string
		change      func(*library.Library)
		field, code string
	}{
		{"a version below 0", func(l *library.Library) { l.Version = -1 }, "version", "invalid"},
		{"over 500 folders", func(l *library.Library) { l.Folders = folders(501) }, "folders", "too_many"},
		{"an empty folder id", func(l *library.Library) { l.Folders = []library.Folder{folder("", "")} }, "folders.id", "invalid"},
		{"a folder id with a space", func(l *library.Library) { l.Folders = []library.Folder{folder("a b", "")} }, "folders.id", "invalid"},
		{"a long folder id", func(l *library.Library) { l.Folders = []library.Folder{folder(strings.Repeat("a", 65), "")} }, "folders.id", "invalid"},
		{"one folder id twice", func(l *library.Library) { l.Folders = []library.Folder{folder("a", ""), folder("a", "")} }, "folders.id", "duplicate"},
		{"a blank name", func(l *library.Library) {
			l.Folders = []library.Folder{{ID: "a", Name: "   ", Projects: []int64{}}}
		}, "folders.name", "required"},
		{"a long name", func(l *library.Library) {
			l.Folders = []library.Folder{{ID: "a", Name: strings.Repeat("ż", 201), Projects: []int64{}}}
		}, "folders.name", "too_long"},
		{"an accent in capitals", func(l *library.Library) {
			l.Folders = []library.Folder{{ID: "a", Name: "A", Accent: "Violet", Projects: []int64{}}}
		}, "folders.accent", "invalid"},
		{"a parent that is not there", func(l *library.Library) { l.Folders = []library.Folder{folder("a", "b")} }, "folders.parent_id", "unknown"},
		{"a folder inside itself", func(l *library.Library) { l.Folders = []library.Folder{folder("a", "a")} }, "folders.parent_id", "cycle"},
		{"two folders inside each other", func(l *library.Library) {
			l.Folders = []library.Folder{folder("a", "b"), folder("b", "a"), folder("c", "")}
		}, "folders.parent_id", "cycle"},
		{"nine folders deep", func(l *library.Library) { l.Folders = chain(9) }, "folders.parent_id", "too_deep"},
		{"project id 0", func(l *library.Library) { l.Projects = []int64{0} }, "projects", "invalid"},
		{"a negative project id in a folder", func(l *library.Library) {
			l.Folders = []library.Folder{{ID: "a", Name: "A", Projects: []int64{-4}}}
		}, "folders.projects", "invalid"},
		{"project id 0 pinned", func(l *library.Library) { l.Pinned = []int64{0} }, "pinned", "invalid"},
		{"project id 0 in Recent", func(l *library.Library) { l.Recent = []int64{0} }, "recent", "invalid"},
		{"a project twice at the top", func(l *library.Library) { l.Projects = []int64{a, b, a} }, "projects", "duplicate"},
		{"a project in two folders", func(l *library.Library) {
			l.Folders = []library.Folder{{ID: "x", Name: "X", Projects: []int64{a}}, {ID: "y", Name: "Y", Projects: []int64{b, a}}}
		}, "projects", "duplicate"},
		{"a project in a folder and at the top", func(l *library.Library) {
			l.Folders = []library.Folder{{ID: "x", Name: "X", Projects: []int64{a}}}
			l.Projects = []int64{a}
		}, "projects", "duplicate"},
		{"a project pinned twice", func(l *library.Library) { l.Pinned = []int64{a, a} }, "pinned", "duplicate"},
		{"a project twice in Recent", func(l *library.Library) { l.Recent = []int64{b, a, b} }, "recent", "duplicate"},
		{"21 in Recent", func(l *library.Library) { l.Recent = many(21, 1) }, "recent", "too_many"},
		{"over 5000 projects named", func(l *library.Library) {
			l.Projects = many(3000, 1)
			l.Pinned = many(2001, 3001)
		}, "projects", "too_many"},
		{"over 256 KiB stored", func(l *library.Library) {
			l.Folders = folders(500)
			for i := range l.Folders {
				l.Folders[i].Name = strings.Repeat("ż", 200)
			}
			l.Projects = many(4000, 1_000_000_000_000_000_000)
		}, "body", "too_large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := empty()
			tc.change(&next)
			_, err := repo.Save(t.Context(), "anna", next)
			var invalid *library.Invalid
			if !errors.As(err, &invalid) || invalid.Field != tc.field || invalid.Code != tc.code {
				t.Fatalf("err = %v, want %s: %s", err, tc.field, tc.code)
			}
		})
	}
	if read, err := repo.Get(t.Context(), "anna"); err != nil || !reflect.DeepEqual(read, empty()) {
		t.Fatalf("after the refusals the account reads %+v, %v; want nothing stored", read, err)
	}

	// The limits themselves are allowed.
	edge := empty()
	edge.Folders = append(chain(8), library.Folder{ID: "dlugi", Name: strings.Repeat("ż", 200), Accent: "amber-2", Projects: []int64{}})
	edge.Recent = many(20, 1)
	edge.Projects = many(4980, 21)
	if _, err := repo.Save(t.Context(), "anna", edge); err != nil {
		t.Fatalf("a save at every limit: %v", err)
	}
}
