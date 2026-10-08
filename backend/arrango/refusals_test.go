// arrango/refusals_test.go

package arrango_test

import (
	"testing"

	"github.com/smegg99/goptivum/solveclient"

	"github.com/smegg99/goptivum/backend/common/messages"
)

// The server's catalogue words every key a solve client refusal carries, in
// both languages, with the parameters the refusal sends: the module names the
// keys and cannot check them itself.
func TestTheCatalogueWordsEverySolveClientRefusal(t *testing.T) {
	for _, key := range solveclient.RefusalKeys() {
		for _, lang := range []string{"pl", "en"} {
			if said := messages.Localize(messages.Localizer(lang), key); said == key {
				t.Errorf("%s has no %s text", key, lang)
			}
		}
	}
	for _, refusal := range []solveclient.Refusal{
		&solveclient.ErrOutdated{Node: "n", Capability: "check"},
		&solveclient.ErrModelInvalid{},
		&solveclient.ErrIncompleteBaseline{Lessons: 1272, Unplaced: 14},
	} {
		for _, lang := range []string{"pl", "en"} {
			said := messages.Say(messages.Localizer(lang), refusal)
			if said == refusal.Key() || said == refusal.Error() {
				t.Errorf("%T in %s = %q, want the catalogue's words", refusal, lang, said)
			}
		}
	}
}
