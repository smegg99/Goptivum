// common/config/settings_test.go

package config_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/smegg99/goptivum/backend/common/config"
)

func openIn(t *testing.T, dir string) *config.SettingsStore {
	t.Helper()
	store, err := config.OpenSettings(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("open settings: %v", err)
	}
	return store
}

// The first start writes the defaults beside config.yaml, owner-only, so the
// file exists for the first change to patch.
func TestSettingsStartFromTheSchemaDefaults(t *testing.T) {
	dir := t.TempDir()
	store := openIn(t, dir)

	want := filepath.Join(dir, config.SettingsFile)
	if store.Path() != want {
		t.Errorf("path = %q, want %q beside config.yaml", store.Path(), want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("the defaults were not written: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %v, want 0600", mode)
	}
	got := store.Current().Lockout
	if got.AccountFailures != 5 || got.AddressFailures != 20 || got.BlockMinutes != 15 {
		t.Errorf("lockout = %+v, want the schema defaults 5, 20, 15", got)
	}
}

func TestALockoutChangeIsWrittenAndReadBack(t *testing.T) {
	dir := t.TempDir()
	store := openIn(t, dir)

	saved, err := store.SetLockout(config.Lockout{AccountFailures: 3, AddressFailures: 50, BlockMinutes: 30})
	if err != nil {
		t.Fatalf("set lockout: %v", err)
	}
	if saved.Lockout.AccountFailures != 3 || store.Current().Lockout.BlockMinutes != 30 {
		t.Errorf("after the change: saved %+v, current %+v", saved.Lockout, store.Current().Lockout)
	}

	reopened := openIn(t, dir).Current().Lockout
	if reopened.AccountFailures != 3 || reopened.AddressFailures != 50 || reopened.BlockMinutes != 30 {
		t.Errorf("reopened = %+v, want the change kept", reopened)
	}
}

// The schema checks a change before it is written, the same way it checks
// config.yaml, so a bad value never reaches the file.
func TestALockoutOutsideTheSchemaIsRefusedAndNothingChanges(t *testing.T) {
	dir := t.TempDir()
	store := openIn(t, dir)
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	for _, bad := range []config.Lockout{
		{AccountFailures: 0, AddressFailures: 20, BlockMinutes: 15},
		{AccountFailures: 5, AddressFailures: 1001, BlockMinutes: 15},
		{AccountFailures: 5, AddressFailures: 20, BlockMinutes: 1441},
	} {
		if _, err := store.SetLockout(bad); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
	after, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("a refused change rewrote the file:\n%s", after)
	}
	if got := store.Current().Lockout; got.AccountFailures != 5 {
		t.Errorf("a refused change moved the settings in memory: %+v", got)
	}
}

// A file edited by hand is checked at start like config.yaml is: an unknown
// key or a value out of range stops the server rather than being ignored.
func TestAHandEditedSettingsFileIsChecked(t *testing.T) {
	for name, body := range map[string]string{
		"out of range": "lockout:\n  account_failures: 0\n",
		"unknown key":  "lockout:\n  account_failures: 5\n  forever: true\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, config.SettingsFile)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			_, err := config.OpenSettings(filepath.Join(dir, "config.yaml"))
			if err == nil {
				t.Fatal("the file was accepted")
			}
			if !strings.Contains(err.Error(), config.SettingsFile) {
				t.Errorf("error %q does not name the file", err)
			}
		})
	}
}

func TestTheServerSettingsStartFromTheSchemaDefaults(t *testing.T) {
	current := openIn(t, t.TempDir()).Current()
	server, projects := current.Server, current.Projects
	if server.PublicAddresses == nil || len(server.PublicAddresses) != 0 {
		t.Errorf("public addresses = %#v, want an empty list", server.PublicAddresses)
	}
	if server.MaximumUploadBytes != 16<<20 || server.RateLimitPerMinute != 600 {
		t.Errorf("server = %+v, want 16 MiB and 600 a minute", server)
	}
	if projects.TrashDays != 30 || projects.BackupsKeep != 100 || projects.ResultsKeep != 20 {
		t.Errorf("projects = %+v, want 30, 100, 20", projects)
	}
}

// A file written before these settings existed holds only the lockout; the
// rest come from the schema's defaults and the first change writes them.
func TestASettingsFileWithOnlyTheLockoutOpens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.SettingsFile)
	if err := os.WriteFile(path, []byte("lockout:\n  account_failures: 7\n  address_failures: 20\n  block_minutes: 15\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store := openIn(t, dir)
	if got := store.Current(); got.Lockout.AccountFailures != 7 || got.Projects.TrashDays != 30 {
		t.Fatalf("current = %+v, want the file's lockout and the default projects", got)
	}
	projects := store.Current().Projects
	projects.TrashDays = 7
	if _, err := store.SetServer(store.Current().Server, projects); err != nil {
		t.Fatalf("set server: %v", err)
	}
	if got := openIn(t, dir).Current(); got.Lockout.AccountFailures != 7 || got.Projects.TrashDays != 7 {
		t.Errorf("reopened = %+v, want the lockout kept and 7 Trash days", got)
	}
}

func TestServerSettingsAreWrittenAndReadBack(t *testing.T) {
	dir := t.TempDir()
	store := openIn(t, dir)

	server := config.ServerSettings{
		PublicAddresses:    []string{"http://192.0.2.10:8833", "https://plan.szkola.example/"},
		MaximumUploadBytes: 32 << 20, RateLimitPerMinute: 0,
	}
	projects := config.Projects{TrashDays: 7, BackupsKeep: 50, ResultsKeep: 5}
	saved, err := store.SetServer(server, projects)
	if err != nil {
		t.Fatalf("set server: %v", err)
	}
	if len(saved.Server.PublicAddresses) != 2 || saved.Projects.BackupsKeep != 50 {
		t.Errorf("saved = %+v", saved)
	}

	reopened := openIn(t, dir).Current()
	if len(reopened.Server.PublicAddresses) != 2 || reopened.Server.PublicAddresses[0] != "http://192.0.2.10:8833" ||
		reopened.Server.MaximumUploadBytes != 32<<20 || reopened.Server.RateLimitPerMinute != 0 {
		t.Errorf("reopened server = %+v", reopened.Server)
	}
	if reopened.Projects != projects {
		t.Errorf("reopened projects = %+v, want %+v", reopened.Projects, projects)
	}
	if reopened.Lockout.AccountFailures != 5 {
		t.Errorf("the lockout moved: %+v", reopened.Lockout)
	}
}

// The last address removed is an empty list in the file, not a missing one
// and not a null the schema refuses.
func TestTheLastPublicAddressCanBeRemoved(t *testing.T) {
	dir := t.TempDir()
	store := openIn(t, dir)
	projects := store.Current().Projects

	with := store.Current().Server
	with.PublicAddresses = []string{"http://192.0.2.10:8833"}
	if _, err := store.SetServer(with, projects); err != nil {
		t.Fatalf("add: %v", err)
	}
	for name, empty := range map[string][]string{"empty": {}, "nil": nil} {
		without := store.Current().Server
		without.PublicAddresses = empty
		if _, err := store.SetServer(without, projects); err != nil {
			t.Fatalf("%s: remove: %v", name, err)
		}
		got := openIn(t, dir).Current().Server.PublicAddresses
		if got == nil || len(got) != 0 {
			t.Errorf("%s: reopened addresses = %#v, want an empty list", name, got)
		}
		with.PublicAddresses = []string{"http://192.0.2.10:8833"}
		if _, err := store.SetServer(with, projects); err != nil {
			t.Fatalf("re-add: %v", err)
		}
	}
}

func TestServerSettingsOutsideTheSchemaAreRefusedAndNothingChanges(t *testing.T) {
	dir := t.TempDir()
	store := openIn(t, dir)
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	good := store.Current()

	tooMany := make([]string, 17)
	for i := range tooMany {
		tooMany[i] = "http://192.0.2.10:" + strconv.Itoa(8000+i)
	}
	for name, change := range map[string]func(*config.ServerSettings, *config.Projects){
		"ftp address": func(s *config.ServerSettings, _ *config.Projects) { s.PublicAddresses = []string{"ftp://192.0.2.10"} },
		"bare host":   func(s *config.ServerSettings, _ *config.Projects) { s.PublicAddresses = []string{"192.0.2.10:8833"} },
		"address with user": func(s *config.ServerSettings, _ *config.Projects) {
			s.PublicAddresses = []string{"http://me@192.0.2.10"}
		},
		"17 addresses":  func(s *config.ServerSettings, _ *config.Projects) { s.PublicAddresses = tooMany },
		"small upload":  func(s *config.ServerSettings, _ *config.Projects) { s.MaximumUploadBytes = 1023 },
		"negative rate": func(s *config.ServerSettings, _ *config.Projects) { s.RateLimitPerMinute = -1 },
		"no Trash days": func(_ *config.ServerSettings, p *config.Projects) { p.TrashDays = 0 },
		"backups over":  func(_ *config.ServerSettings, p *config.Projects) { p.BackupsKeep = 10001 },
		"no results":    func(_ *config.ServerSettings, p *config.Projects) { p.ResultsKeep = 0 },
	} {
		server, projects := good.Server, good.Projects
		change(&server, &projects)
		if _, err := store.SetServer(server, projects); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	after, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("a refused change rewrote the file:\n%s", after)
	}
	if got := store.Current(); got.Projects != good.Projects || got.Server.MaximumUploadBytes != good.Server.MaximumUploadBytes {
		t.Errorf("a refused change moved the settings in memory: %+v", got)
	}
}
