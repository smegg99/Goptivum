// core/audit/audit_test.go

package audit_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/migrations"
)

// clock hands out one time per call, a second apart, so the order is known.
func clock() func() time.Time {
	at := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	return func() time.Time {
		at = at.Add(time.Second)
		return at
	}
}

func newLog(t *testing.T) *audit.Log {
	t.Helper()
	return audit.New(db.TestDB(t, migrations.FS), db.SQLite).WithClock(clock())
}

var anna = audit.Actor{ID: "identity-1", Name: "Anna Kowalska"}

func record(t *testing.T, log *audit.Log, action audit.Action, target audit.Target, details map[string]string) {
	t.Helper()
	if err := log.Record(context.Background(), anna, action, target, details); err != nil {
		t.Fatalf("record %s: %v", action, err)
	}
}

// An entry reads back as it was written, names and all, newest first.
func TestEntriesReadBackNewestFirst(t *testing.T) {
	log := newLog(t)
	plan := audit.Target{Kind: audit.Project, ID: "42", Name: "Plan 2027"}
	record(t, log, audit.ProjectRenamed, plan, map[string]string{"from": "Plan 2027", "to": "Plan 2027 test"})
	record(t, log, audit.ProjectRenamed, audit.Target{Kind: audit.Project, ID: "42", Name: "Plan 2027 test"},
		map[string]string{"from": "Plan 2027 test", "to": "Plan 2027"})
	record(t, log, audit.UserCreated, audit.Target{Kind: audit.User, ID: "identity-2", Name: "planner-test"},
		map[string]string{"role": "planner"})

	entries, total, err := log.Page(context.Background(), "", 1, 25)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if total != 3 || len(entries) != 3 {
		t.Fatalf("total %d, entries %+v", total, entries)
	}
	if entries[0].Action != audit.UserCreated || entries[2].Details["to"] != "Plan 2027 test" {
		t.Errorf("order = %s, %s, %s; want newest first", entries[0].Action, entries[1].Action, entries[2].Action)
	}
	oldest := entries[2]
	if oldest.ID == 0 || oldest.Actor != anna || oldest.Target != plan ||
		!reflect.DeepEqual(oldest.Details, map[string]string{"from": "Plan 2027", "to": "Plan 2027 test"}) {
		t.Errorf("oldest = %+v", oldest)
	}
	if !oldest.At.Equal(time.Date(2026, 9, 27, 8, 0, 1, 0, time.UTC)) || oldest.At.Location() != time.UTC {
		t.Errorf("at = %v, want the clock's first second in UTC", oldest.At)
	}
}

func TestAPageNarrowsByKindAndCountsEveryMatch(t *testing.T) {
	log := newLog(t)
	for i := 0; i < 3; i++ {
		record(t, log, audit.ProjectDeleted, audit.Target{Kind: audit.Project, ID: "1"}, nil)
		record(t, log, audit.NodeRevoked, audit.Target{Kind: audit.Node, ID: "7", Name: "Serwer szkolny"}, nil)
	}

	nodes, total, err := log.Page(context.Background(), audit.Node, 1, 2)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if total != 3 || len(nodes) != 2 {
		t.Fatalf("nodes: total %d, %d entries; want 3 and a page of 2", total, len(nodes))
	}
	for _, entry := range nodes {
		if entry.Target.Kind != audit.Node || entry.Details == nil || len(entry.Details) != 0 {
			t.Errorf("entry = %+v, want a node entry with empty details", entry)
		}
	}
	last, _, err := log.Page(context.Background(), audit.Node, 2, 2)
	if err != nil || len(last) != 1 || !last[0].At.Before(nodes[1].At) {
		t.Errorf("second page = %+v, %v; want the oldest node entry", last, err)
	}
	if beyond, total, err := log.Page(context.Background(), "", 9, 25); err != nil || len(beyond) != 0 || total != 6 {
		t.Errorf("past the end = %+v, %d, %v", beyond, total, err)
	}
}
