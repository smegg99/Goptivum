// cmd/locale_test.go

package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocaleBeforeCommandExecution(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"root help", []string{"--lang", "pl", "--help"}, "Dostępne polecenia:"},
		{"help before language", []string{"--help", "--lang=pl"}, "Dostępne polecenia:"},
		{"command help", []string{"status", "--lang=pl", "--help"}, "Opcje globalne:"},
		{"help command", []string{"help", "status", "--lang=pl"}, "Odczytaj informacje"},
		{"completion help", []string{"completion", "zsh", "--lang=pl", "--help"}, "Wygeneruj uzupełnianie dla zsh"},
		{"version", []string{"--lang=pl", "--version"}, "klienci >="},
		{"language reset", []string{"--lang=pl", "--lang=en", "--help"}, "Available Commands:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOPTIVUM_LANG", "en")
			path := filepath.Join(t.TempDir(), "missing.yaml")
			root, _ := newRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(append([]string{"--config", path}, test.args...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), test.want) {
				t.Fatalf("missing %q:\n%s", test.want, &out)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("help/version touched configuration: %v", err)
			}
		})
	}
}

func TestHelpDetectsShellLanguage(t *testing.T) {
	for _, name := range []string{"GOPTIVUM_LANG", "LC_ALL", "LC_MESSAGES"} {
		t.Setenv(name, "")
	}
	t.Setenv("LANG", "pl_PL.UTF-8")
	out, err := run(t, "--help")
	if err != nil || !strings.Contains(out, "Dostępne polecenia:") {
		t.Fatalf("shell locale ignored: %v\n%s", err, out)
	}
	out, err = run(t, "--lang=en", "--help")
	if err != nil || !strings.Contains(out, "Available Commands:") {
		t.Fatalf("explicit language ignored: %v\n%s", err, out)
	}
}

func TestLocalizedUsageErrors(t *testing.T) {
	t.Setenv("GOPTIVUM_LANG", "pl")
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"missing"}, "Nieznane polecenie: missing"},
		{[]string{"help", "missing"}, "Nieznane polecenie: missing"},
		{[]string{"serve", "extra"}, "nie przyjmuje argumentów"},
		{[]string{"status", "--unknown"}, "Nieprawidłowa opcja:"},
		{[]string{"--verbose=invalid"}, "Nieprawidłowa opcja:"},
		{[]string{"--lang=de"}, "Wybierz en lub pl"},
	} {
		_, err := run(t, test.args...)
		if !errors.Is(err, errUsage) || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%v: %v, want usage error containing %q", test.args, err, test.want)
		}
	}
}

func TestLanguageFlagOverridesConfiguration(t *testing.T) {
	t.Setenv("GOPTIVUM_LANG", "en")
	path := filepath.Join(t.TempDir(), "config.yaml")
	const source = "application:\n  language: en\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	root, s := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status", "--config", path, "--addr", "127.0.0.1:1", "--lang=pl"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "nie odpowiedział") {
		t.Fatalf("status error not localized: %v", err)
	}
	if s.settings.Application.Language != "pl" || !strings.Contains(out.String(), "adres") {
		t.Fatalf("language flag ignored: %s\n%s", s.settings.Application.Language, &out)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != source {
		t.Fatalf("one-run override changed config: %v\n%s", err, contents)
	}
}
