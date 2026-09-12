// cmd/cmd_test.go

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/version"
)

// run drives the tree the way a shell does, with nothing attached to a
// terminal and a configuration file of its own: loading one writes the
// defaults when it is missing, and a test must not leave that in the tree.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, _, err := runWith(t, args...)
	return out, err
}

func runWith(t *testing.T, args ...string) (string, *server, error) {
	t.Helper()

	root, s := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--config", filepath.Join(t.TempDir(), "config.yaml")}, args...))
	err := root.ExecuteContext(context.Background())
	return out.String(), s, err
}

func TestHelpListsTheCommands(t *testing.T) {
	out, err := run(t, "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, want := range []string{"serve", "status", "--addr", "--config", "--verbose"} {
		if !strings.Contains(out, want) {
			t.Errorf("help does not mention %q", want)
		}
	}
}

func TestVersionSaysWhatItSpeaks(t *testing.T) {
	out, err := run(t, "--version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	for _, want := range []string{version.Release, version.API, version.MinClient} {
		if !strings.Contains(out, want) {
			t.Errorf("version line %q does not carry %q", strings.TrimSpace(out), want)
		}
	}
}

// A misuse and a failure of the work itself are different exit codes, so a
// script can tell them apart.
func TestMisuseIsAUsageError(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unknown command", []string{"frobnicate"}},
		{"unknown flag", []string{"serve", "--wat"}},
		{"an argument serve does not take", []string{"serve", "extra"}},
		{"an argument status does not take", []string{"status", "extra"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := run(t, test.args...)
			if !errors.Is(err, errUsage) {
				t.Errorf("error = %v, want a usage error so the process exits 2", err)
			}
		})
	}
}

// The configuration file owns the settings, and a flag overrides one for a
// single run. A flag that was not given must not quietly replace a value the
// school wrote down.
func TestTheConfigurationFileOwnsTheSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	written := "server:\n  address: 127.0.0.1:19111\nlogging:\n  level: WARN\n"
	if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	root, s := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	// status fails because nothing is listening there; the settings are what
	// this test reads.
	root.SetArgs([]string{"status", "--config", path})
	_ = root.ExecuteContext(context.Background())

	if s.settings.Server.Address != "127.0.0.1:19111" {
		t.Errorf("address = %q, want the file's", s.settings.Server.Address)
	}
	if s.settings.Logging.Level != "WARN" {
		t.Errorf("level = %q, want the file's", s.settings.Logging.Level)
	}
	if s.settings.Storage.Documents.Dir != "./documents" {
		t.Errorf("documents dir = %q, want the schema default", s.settings.Storage.Documents.Dir)
	}

	root, s = newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"status", "--config", path, "--addr", "127.0.0.1:19222"})
	_ = root.ExecuteContext(context.Background())

	if s.settings.Server.Address != "127.0.0.1:19222" {
		t.Errorf("address = %q, want the flag to win for one run", s.settings.Server.Address)
	}
}

// A first run writes the defaults, so a fresh install has a file to edit
// rather than an error to read.
func TestAMissingConfigurationIsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	root, s := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"status", "--config", path, "--addr", "127.0.0.1:1"})
	_ = root.ExecuteContext(context.Background())

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the defaults were not written: %v", err)
	}
	if s.settings.Application.Name != "goptivum-server" {
		t.Errorf("application name = %q", s.settings.Application.Name)
	}
}

// Asking for help must not create a configuration file as a side effect.
func TestHelpWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	root, _ := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--help", "--config", path})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("help: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("asking for help wrote a configuration file")
	}
}

func TestStatusReadsARunningServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/info":
			json.NewEncoder(w).Encode(gen.Info{
				Product: gen.Goptivum, ApiVersion: version.API,
				MinClientVersion: "0.1.0", ServerVersion: "9.9.9",
			})
		case "/api/" + version.API + "/ready":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	out, err := run(t, "status", "--addr", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"goptivum", "[*] " + version.API, "9.9.9", "[*] ready"} {
		if !strings.Contains(out, want) {
			t.Errorf("status does not report %q:\n%s", want, out)
		}
	}
}

// Something else answering on that address is a different problem from nothing
// answering, and the two have different fixes.
func TestStatusSaysWhenItReachedSomethingElse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/info" {
			w.Write([]byte(`{"product":"something-else","api_version":"v4","min_client_version":"1","server_version":"1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	out, err := run(t, "status", "--addr", strings.TrimPrefix(server.URL, "http://"))
	if err == nil {
		t.Fatal("status accepted a server that is not a Goptivum Server")
	}
	if !strings.Contains(out, "[x] something-else") {
		t.Errorf("status does not name what it reached:\n%s", out)
	}
	if !strings.Contains(out, "[!] v4") {
		t.Errorf("status does not say the version is one this build does not know:\n%s", out)
	}
}

// The command line is one program's voice, so it speaks the language the
// configuration names, the same one the log does.
func TestStatusSpeaksTheConfiguredLanguage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/info":
			json.NewEncoder(w).Encode(gen.Info{
				Product: gen.Goptivum, ApiVersion: version.API,
				MinClientVersion: "0.1.0", ServerVersion: "1.0.0",
			})
		case "/api/" + version.API + "/ready":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("application:\n  language: pl\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	root, _ := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status", "--config", path,
		"--addr", strings.TrimPrefix(server.URL, "http://")})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("status: %v", err)
	}

	for _, want := range []string{"adres", "produkt", "wersja api", "gotowość", "[*] gotowy"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status did not render %q in Polish:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "readiness") {
		t.Error("an English label survived into the Polish rendering")
	}
}

func TestStatusSaysWhenNothingAnswers(t *testing.T) {
	out, err := run(t, "status", "--addr", "127.0.0.1:1")
	if err == nil {
		t.Fatal("status reported a server nothing is listening for")
	}
	if !strings.Contains(out, "[x] no") {
		t.Errorf("status does not say it reached nothing:\n%s", out)
	}
}

// A readiness probe that says no is a running server with a stopped
// dependency, which is not the same as an absent one.
func TestStatusSeparatesReadinessFromReachability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/info" {
			json.NewEncoder(w).Encode(gen.Info{
				Product: gen.Goptivum, ApiVersion: version.API,
				MinClientVersion: "0.1.0", ServerVersion: "1.0.0",
			})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	out, err := run(t, "status", "--addr", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "[!] a dependency is down") {
		t.Errorf("status does not report readiness:\n%s", out)
	}
}
