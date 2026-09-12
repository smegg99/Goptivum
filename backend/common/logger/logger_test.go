// common/logger/logger_test.go

package logger_test

import (
	"context"
	"strings"
	"testing"

	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
)

// Every event this server logs has a sentence in every language. The
// translator's own check is what the logger would fall back from at runtime,
// so it runs here instead.
func TestEveryEventTranslatesInEveryLanguage(t *testing.T) {
	translator := logger.Translator()
	if translator == nil {
		t.Fatal("no translator; the catalogs did not load")
	}
	verifier, ok := translator.(interface {
		Verify([]string, ...s99logger.MessageID) error
	})
	if !ok {
		t.Fatalf("the translator cannot verify: %T", translator)
	}
	if err := verifier.Verify([]string{"en", "pl"}, logger.All...); err != nil {
		t.Errorf("an event has no message: %v", err)
	}
}

func TestAnEventReadsAsASentence(t *testing.T) {
	translator := logger.Translator()
	if translator == nil {
		t.Fatal("no translator")
	}

	cases := []struct {
		language, want string
	}{
		{"en", "server started"},
		{"pl", "serwer uruchomiony"},
	}
	for _, test := range cases {
		t.Run(test.language, func(t *testing.T) {
			got, err := translator.Translate(context.Background(), test.language,
				s99logger.NewEvent(logger.EventServerStarted))
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if !strings.EqualFold(got, test.want) {
				t.Errorf("rendered %q, want %q", got, test.want)
			}
		})
	}
}

// An event id that is not a catalog key renders as the raw id, which is the
// one failure the runtime hides behind a fallback.
func TestEveryEventIdRendersAsSomethingOtherThanItself(t *testing.T) {
	localizer := messages.Localizer("en")
	for _, event := range logger.All {
		if !strings.HasPrefix(string(event), "logs.") {
			t.Errorf("event %q is not in the logs namespace", event)
		}
		if rendered := messages.Localize(localizer, string(event)); rendered == string(event) {
			t.Errorf("event %q has no message in the catalog", event)
		}
	}
}
