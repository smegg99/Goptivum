// common/messages/messages_test.go

package messages_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/smegg99/goptivum/backend/common/messages"
)

// Every message this server renders exists in every language it offers. A
// missing one is not a runtime fallback to hide, it is a translation nobody
// wrote.
func TestEveryMessageExistsInEveryLanguage(t *testing.T) {
	catalogs := map[string]map[string]any{}
	for _, name := range messages.Names {
		body, err := messages.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var catalog map[string]any
		if err := json.Unmarshal(body, &catalog); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		catalogs[name] = catalog
	}

	reference := catalogs[messages.Names[0]]
	if len(reference) == 0 {
		t.Fatal("the first catalog is empty")
	}
	for name, catalog := range catalogs {
		for key := range reference {
			if _, ok := catalog[key]; !ok {
				t.Errorf("%s has no message for %q", name, key)
			}
		}
		for key := range catalog {
			if _, ok := reference[key]; !ok {
				t.Errorf("%s carries %q, which %s does not", name, key, messages.Names[0])
			}
		}
	}
}

func TestLocalizerRendersTheLanguageItWasAsked(t *testing.T) {
	cases := []struct {
		name, language, want string
	}{
		{"english", "en", "configuration loaded"},
		{"polish", "pl", "wczytano konfigurację"},
		{"a language with no catalog", "de", "configuration loaded"},
		{"nothing", "", "configuration loaded"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := messages.LogsConfigLoaded(messages.Localizer(test.language)); got != test.want {
				t.Errorf("rendered %q, want %q", got, test.want)
			}
		})
	}
}

// A placeholder is a named parameter, never a sentence built before
// translation: word order differs between languages.
func TestPlaceholdersAreFilledInBothLanguages(t *testing.T) {
	english := messages.CliStatusDidNotAnswer(messages.Localizer("en"),
		messages.CliStatusDidNotAnswerParams{Address: "http://127.0.0.1:8833"})
	polish := messages.CliStatusDidNotAnswer(messages.Localizer("pl"),
		messages.CliStatusDidNotAnswerParams{Address: "http://127.0.0.1:8833"})

	for _, rendered := range []string{english, polish} {
		if !strings.Contains(rendered, "http://127.0.0.1:8833") {
			t.Errorf("the address did not reach the message: %q", rendered)
		}
		if strings.Contains(rendered, "{") {
			t.Errorf("a placeholder was left unfilled: %q", rendered)
		}
	}
	if english == polish {
		t.Error("both languages rendered the same sentence")
	}
}

func TestLanguageFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"nothing set", nil, "en"},
		{"our own variable", map[string]string{"GOPTIVUM_LANG": "pl"}, "pl"},
		{"a shell locale", map[string]string{"LANG": "pl_PL.UTF-8"}, "pl"},
		{"ours wins over the shell", map[string]string{"GOPTIVUM_LANG": "en", "LANG": "pl_PL.UTF-8"}, "en"},
		{"the C locale is not a language", map[string]string{"LANG": "C"}, "en"},
		{"LC_ALL wins", map[string]string{"LC_ALL": "en_US.UTF-8", "LC_MESSAGES": "pl_PL.UTF-8", "LANG": "pl_PL.UTF-8"}, "en"},
		{"message locale wins over LANG", map[string]string{"LC_MESSAGES": "pl_PL.UTF-8", "LANG": "en_US.UTF-8"}, "pl"},
		{"C overrides Polish LANG", map[string]string{"LC_ALL": "C", "LANG": "pl_PL.UTF-8"}, "en"},
		{"POSIX overrides Polish LANG", map[string]string{"LC_ALL": "POSIX", "LANG": "pl_PL.UTF-8"}, "en"},
		{"C UTF-8 overrides Polish LANG", map[string]string{"LC_ALL": "C.UTF-8", "LANG": "pl_PL.UTF-8"}, "en"},
		{"locale modifier", map[string]string{"LANG": "pl@euro"}, "pl"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for _, name := range []string{"GOPTIVUM_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
				t.Setenv(name, "")
			}
			for name, value := range test.env {
				t.Setenv(name, value)
			}
			if got := messages.LanguageFromEnv(); got != test.want {
				t.Errorf("language = %q, want %q", got, test.want)
			}
		})
	}
}
