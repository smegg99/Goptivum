// formats/goptivum/goptivum.go

// Package goptivum puts Goptivum's own .gpf in the format registry. The
// container itself, its reader, writer and encryption, is the document
// module's gpf package, which the desktop uses without the server.
package goptivum

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"filippo.io/age"

	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/document/gpf"
)

// ID is what the registry and the API call this format.
const ID = "goptivum.gpf"

// Keys is where the school's own key comes from, when it has one.
//
// An interface so this package does not depend on the escrow store, which
// depends on a database.
type Keys interface {
	// Identity opens files encrypted to this school. It is nil when the
	// deployment holds no key.
	Identity(ctx context.Context) (age.Identity, error)
	// Recipient is what new files are encrypted to.
	Recipient(ctx context.Context) (age.Recipient, error)
}

// Importer reads a .gpf through the same registry every other format goes
// through.
//
// A password-locked file opens only with the passphrase the person importing
// it types, handed over through ImportUnlocked after detection, never on
// formats.Source, which every detector in the registry reads. The school's own
// key is different: it belongs to this server and opening its own files with
// it is the point of holding it.
type Importer struct {
	// Realm is this server's own, for the WrongRealm refusal.
	Realm string
	// Keys is the school's escrowed key. Nil means Identity-mode files are
	// refused with their reason rather than opened.
	Keys Keys
}

// NewImporter builds the registry entry.
func NewImporter(realm string) *Importer { return &Importer{Realm: realm} }

// WithKeys lets this importer open files encrypted to the school.
func (i *Importer) WithKeys(keys Keys) *Importer {
	i.Keys = keys
	return i
}

// ID identifies this importer.
func (*Importer) ID() string { return ID }

// Detect is the cheapest detector in the registry: eight bytes and a
// comparison, no parsing and no allocation.
//
// Certain on a match, because nothing else on earth starts with these bytes. A
// file that is not a .gpf is No rather than an error: formats.Detect treats an
// error as a non-match anyway, so returning one would only hide a real failure.
func (*Importer) Detect(source formats.Source) (formats.Confidence, error) {
	names, err := formats.Files(source)
	if err != nil {
		return formats.No, nil
	}
	for _, name := range names {
		head, err := formats.Head(source.FS, name, len(gpf.Magic))
		if err != nil {
			continue
		}
		if gpf.Looks(head) {
			return formats.Certain, nil
		}
	}
	return formats.No, nil
}

// Import reads the file into the shape the rest of the server uses.
//
// A .gpf is Goptivum's own canonical format, so unlike Optivum there is no
// lossy mapping and no findings: it either is a valid document or it is not. A
// refusal is an error the caller can act on rather than a Finding buried in a
// result.
func (i *Importer) Import(ctx context.Context, source formats.Source) (*formats.Result, error) {
	return i.ImportUnlocked(ctx, source, "")
}

// ImportUnlocked is Import with the passphrase a password-locked file opens
// with; empty is none, and a file that is not locked ignores it.
func (i *Importer) ImportUnlocked(ctx context.Context, source formats.Source, passphrase string) (*formats.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := readOne(source)
	if err != nil {
		return nil, err
	}
	options := gpf.ReadOptions{Realm: i.Realm, Passphrase: passphrase}
	if i.Keys != nil {
		// A failure to reach the key is not a failure to read the file: a plain
		// one still opens, and an encrypted one refuses with its own reason.
		if identity, err := i.Keys.Identity(ctx); err == nil && identity != nil {
			options.Identities = []age.Identity{identity}
		}
	}
	file, err := gpf.Decode(data, options)
	if err != nil {
		return nil, err
	}
	return &formats.Result{
		Model:    file.Document.GetModel(),
		Schedule: file.Document.GetSchedule(),
		Document: file.Document,
	}, nil
}

// readOne finds the single file in the source and reads it whole.
func readOne(source formats.Source) ([]byte, error) {
	names, err := formats.Files(source)
	if err != nil {
		return nil, fmt.Errorf("gpf: read source: %w", err)
	}
	for _, name := range names {
		head, err := formats.Head(source.FS, name, len(gpf.Magic))
		if err != nil || !gpf.Looks(head) {
			continue
		}
		// Sized before it is read. The upload path caps this already, but a
		// zip entry is not an upload and fs.ReadFile allocates whatever it
		// finds.
		info, err := fs.Stat(source.FS, name)
		if err != nil {
			return nil, fmt.Errorf("gpf: stat %s: %w", name, err)
		}
		if info.Size() > gpf.MaxFile {
			return nil, fmt.Errorf("%w: %s is %d bytes, over the %d cap",
				gpf.ErrMalformed, name, info.Size(), gpf.MaxFile)
		}
		data, err := fs.ReadFile(source.FS, name)
		if err != nil {
			return nil, fmt.Errorf("gpf: read %s: %w", name, err)
		}
		return data, nil
	}
	return nil, gpf.ErrNotGPF
}

// Exporter writes a .gpf: encrypted to the school's own key when the server
// holds one (Identity mode), plain when it holds none.
type Exporter struct {
	// Realm and App go into the header of every file this server writes.
	Realm string
	App   string
	// Keys is the school's escrowed key. With one, every file this server
	// writes is encrypted to the school and opens on any of its machines.
	// Without one, files are written plain, which is a real choice a school
	// makes by not configuring a passphrase.
	Keys Keys
}

// NewExporter builds the registry entry.
func NewExporter(realm, app string) *Exporter { return &Exporter{Realm: realm, App: app} }

// WithKeys encrypts what this exporter writes to the school's own key.
func (e *Exporter) WithKeys(keys Keys) *Exporter {
	e.Keys = keys
	return e
}

// ID identifies this exporter.
func (*Exporter) ID() string { return ID }

// Variant names what Export encrypts to now: the school's recipient, or
// empty when files are written plain. A kept file with another variant is
// stale.
func (e *Exporter) Variant(ctx context.Context) (string, error) {
	recipient, err := e.recipient(ctx)
	if err != nil || recipient == nil {
		return "", err
	}
	named, ok := recipient.(fmt.Stringer)
	if !ok {
		return "", errors.New("gpf: the school's key has no printable recipient")
	}
	return named.String(), nil
}

// recipient is the school's key files are encrypted to, nil when plain.
func (e *Exporter) recipient(ctx context.Context) (age.Recipient, error) {
	if e.Keys == nil {
		return nil, nil
	}
	return e.Keys.Recipient(ctx)
}

// Export writes the result as a .gpf, encrypted to the school's key when it
// has one.
func (e *Exporter) Export(ctx context.Context, result *formats.Result) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("gpf: nothing to export")
	}
	document := result.Document
	if document == nil {
		return nil, errors.New("gpf: the result carries no document")
	}
	header := gpf.NewHeader().
		Set(gpf.FieldSchool, document.GetModel().GetName()).
		Set(gpf.FieldApp, e.App).
		Set(gpf.FieldRealm, e.Realm)

	options := gpf.WriteOptions{Header: header}
	recipient, err := e.recipient(ctx)
	if err != nil {
		// Deliberately an error rather than a plain file. A school that
		// configured a key and silently got an unencrypted export would learn
		// about it from whoever received the file.
		return nil, err
	}
	if recipient != nil {
		options.Recipients = []age.Recipient{recipient}
	}
	return gpf.Encode(document, options)
}
