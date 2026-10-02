// common/messages/fault.go

package messages

import (
	"errors"

	"github.com/nicksnyder/go-i18n/v2/i18n"
)

// Fault is the error this server returns when a person will read it.
//
// It carries a message key and its parameters rather than a sentence, so the
// text is resolved in the language the reader chose, by the edge that knows
// which language that is. An English sentence built with fmt.Errorf cannot be
// translated once it exists, and a hand-rolled error type per package makes
// every caller render it by hand and differently.
//
// An internal diagnostic is not a Fault. Wrapping a driver error on its way to
// a log is a developer's sentence for a developer, and dressing it as a
// translatable message would put a hundred keys in the catalog that no school
// ever sees.
type Fault struct {
	key   string
	data  map[string]any
	cause error
}

// NewFault is a message a person will read.
func NewFault(key string, data map[string]any) *Fault {
	return &Fault{key: key, data: data}
}

// WrapFault keeps the cause for the log and the key for the reader.
func WrapFault(cause error, key string, data map[string]any) *Fault {
	return &Fault{key: key, data: data, cause: cause}
}

// Key is the catalog entry this fault renders from.
func (f *Fault) Key() string { return f.key }

// Data is the parameters the message needs, for a caller that has to carry them
// somewhere the catalog is not, such as an HTTP envelope the desktop renders.
func (f *Fault) Data() map[string]any { return f.data }

// Error is the key, never a sentence. A log line wants something stable to
// grep for, and the reader's language is not known here.
func (f *Fault) Error() string {
	if f.cause != nil {
		return f.key + ": " + f.cause.Error()
	}
	return f.key
}

// Unwrap keeps errors.Is and errors.As working through the chain.
func (f *Fault) Unwrap() error { return f.cause }

// Keyed is an error a person reads: a catalog key and its parameters. A
// *Fault is one; so is a refusal from the document module, which cannot import
// this package and names the keys this catalog words.
type Keyed interface {
	error
	Key() string
	Data() map[string]any
}

// Localize renders this fault in the localizer's language.
func (f *Fault) Localize(l *i18n.Localizer) string { return render(l, f) }

// render words a keyed error. A parameter that is itself an error is rendered
// too, so a message that says "cannot load configuration: X" resolves X in the
// same language rather than pasting in whatever English the inner error
// happened to carry.
func render(l *i18n.Localizer, keyed Keyed) string {
	if len(keyed.Data()) == 0 {
		return Localize(l, keyed.Key())
	}
	data := make(map[string]any, len(keyed.Data()))
	for name, value := range keyed.Data() {
		if inner, ok := value.(error); ok {
			data[name] = Say(l, inner)
			continue
		}
		data[name] = value
	}
	return withData(l, keyed.Key(), data)
}

// Say renders any error for a person.
//
// A keyed error anywhere in the chain is rendered from the catalog; anything
// else falls back to its own text, because an error with no key is a
// diagnostic that was never meant for this audience and hiding it would be
// worse than showing it untranslated.
func Say(l *i18n.Localizer, err error) string {
	if err == nil {
		return ""
	}
	var keyed Keyed
	if errors.As(err, &keyed) {
		return render(l, keyed)
	}
	return err.Error()
}
