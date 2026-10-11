// core/escrow/escrow_test.go

package escrow_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"filippo.io/age"
	"github.com/smegg99/s99ab/data/db"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/core/escrow"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/goptivum"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/document/gpf"
)

const passphrase = "the school's own escrow passphrase"

func newStore(t *testing.T, secret string) *escrow.Store {
	t.Helper()
	// A low work factor: the production cost is two seconds per seal, which is
	// right for a school's key and wrong for a suite.
	return escrow.NewStore(db.TestDB(t, migrations.FS), db.SQLite, secret).WithWorkFactor(10)
}

// The school gets one key, once. Asking twice returns the same key: a second
// one would leave half the school's files unreadable by the other half.
func TestEnsureIsIdempotent(t *testing.T) {
	store := newStore(t, passphrase)

	first, err := store.Ensure(t.Context())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	second, err := store.Ensure(t.Context())
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if first.Recipient != second.Recipient {
		t.Errorf("two calls made two keys: %s and %s", first.Recipient, second.Recipient)
	}
	if first.Identity.String() != second.Identity.String() {
		t.Error("the private half changed between calls")
	}
}

// Two servers starting together must not end up with two keys.
func TestConcurrentFirstBootMakesOneKey(t *testing.T) {
	store := newStore(t, passphrase)

	var wait sync.WaitGroup
	keys := make([]string, 8)
	for i := range keys {
		wait.Add(1)
		go func() {
			defer wait.Done()
			key, err := store.Ensure(t.Context())
			if err != nil {
				t.Errorf("ensure: %v", err)
				return
			}
			keys[i] = key.Recipient
		}()
	}
	wait.Wait()

	for i, key := range keys {
		if key != keys[0] {
			t.Fatalf("caller %d got %s and caller 0 got %s", i, key, keys[0])
		}
	}
}

// The private half is never in the database in the clear. A dump on its own
// opens nothing, which is the whole reason the passphrase exists.
func TestThePrivateKeyIsNeverStoredInTheClear(t *testing.T) {
	conn := db.TestDB(t, migrations.FS)
	store := escrow.NewStore(conn, db.SQLite, passphrase).WithWorkFactor(10)

	key, err := store.Ensure(t.Context())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	var recipient string
	var sealed []byte
	if err := conn.QueryRowContext(t.Context(),
		"select recipient, sealed from school_key where id = 1").Scan(&recipient, &sealed); err != nil {
		t.Fatalf("read row: %v", err)
	}
	// The private half is an AGE-SECRET-KEY- string. It must appear nowhere.
	if secret := key.Identity.String(); len(sealed) == 0 || bytes.Contains(sealed, []byte(secret)) {
		t.Error("the stored row carries the private key in the clear")
	}
	// The public half is not a secret: it is what files are encrypted to.
	if recipient != key.Recipient {
		t.Errorf("recipient = %q, want %q", recipient, key.Recipient)
	}
}

// A changed or mistyped passphrase is told apart from a missing key. They need
// different answers: one is "you configured this wrong" and the other is
// "first boot".
func TestAWrongPassphraseIsNotAMissingKey(t *testing.T) {
	conn := db.TestDB(t, migrations.FS)
	key, err := escrow.NewStore(conn, db.SQLite, passphrase).WithWorkFactor(10).Ensure(t.Context())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	_, err = escrow.NewStore(conn, db.SQLite, "a different passphrase").WithWorkFactor(10).Get(t.Context())
	if !errors.Is(err, escrow.ErrSealed) {
		t.Fatalf("err = %v, want ErrSealed", err)
	}
	// And the public half still reads, with its date, so a server that cannot
	// open its own key can still say what files are encrypted to. This is
	// also what shows it is read without unlocking: this passphrase cannot.
	public, err := escrow.NewStore(conn, db.SQLite, "a different passphrase").WithWorkFactor(10).Public(t.Context())
	if err != nil {
		t.Fatalf("public: %v", err)
	}
	if public.Recipient != key.Recipient || !public.CreatedAt.Equal(key.CreatedAt) {
		t.Errorf("public = %+v, want recipient %s made %s", public, key.Recipient, key.CreatedAt)
	}
}

func TestThePublicHalfSaysWhenThereIsNoKey(t *testing.T) {
	if _, err := newStore(t, passphrase).Public(t.Context()); !errors.Is(err, escrow.ErrNoKey) {
		t.Errorf("before first boot: err = %v, want ErrNoKey", err)
	}
	if _, err := newStore(t, "").Public(t.Context()); !errors.Is(err, escrow.ErrDisabled) {
		t.Errorf("without a passphrase: err = %v, want ErrDisabled", err)
	}
}

// No passphrase means no key, not a key in the clear. A feature that is off is
// easier to explain than one that is quietly insecure.
func TestWithoutAPassphraseNothingIsStored(t *testing.T) {
	store := newStore(t, "")
	if store.Enabled() {
		t.Error("escrow reports enabled with no passphrase")
	}
	for _, err := range []error{
		second(store.Ensure(t.Context())),
		second(store.Get(t.Context())),
	} {
		if !errors.Is(err, escrow.ErrDisabled) {
			t.Errorf("err = %v, want ErrDisabled", err)
		}
	}
}

// A school with no key yet says so, rather than inventing one on a read.
func TestGetBeforeFirstBootSaysThereIsNoKey(t *testing.T) {
	if _, err := newStore(t, passphrase).Get(t.Context()); !errors.Is(err, escrow.ErrNoKey) {
		t.Errorf("err = %v, want ErrNoKey", err)
	}
}

// The escrowed key actually opens a file encrypted to it. Everything above is
// bookkeeping if this does not hold.
func TestTheEscrowedKeyOpensWhatItIsEncryptedTo(t *testing.T) {
	store := newStore(t, passphrase)
	key, err := store.Ensure(t.Context())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	recipient, err := age.ParseX25519Recipient(key.Recipient)
	if err != nil {
		t.Fatalf("parse recipient: %v", err)
	}
	sealed := encryptTo(t, []byte("the school's timetable"), recipient)

	// Read back from the row, the way a later boot does, rather than reusing
	// the identity Ensure happened to return.
	reopened, err := store.Get(t.Context())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	reader, err := age.Decrypt(bytes.NewReader(sealed), reopened.Identity)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	opened, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(opened) != "the school's timetable" {
		t.Errorf("decrypted %q", opened)
	}
}

// encryptTo seals bytes to a recipient, the way a .gpf in Identity mode does.
func encryptTo(t *testing.T, plain []byte, recipient age.Recipient) []byte {
	t.Helper()
	var out bytes.Buffer
	writer, err := age.Encrypt(&out, recipient)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := writer.Write(plain); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return out.Bytes()
}

func second[T any](_ T, err error) error { return err }

// The whole point, end to end: a file this server exports is encrypted to the
// school, and this server opens it again from the key in its own database.
//
// This is what "a new laptop must not destroy a year of timetables" means in
// practice. Nobody typed a password and nobody carried a key.
func TestAnExportedFileOpensAgainWithTheSchoolsOwnKey(t *testing.T) {
	store := newStore(t, passphrase)
	if _, err := store.Ensure(t.Context()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	keys := store.ForFiles()

	school := &arrangov1.SchoolModel{
		Name:     "Zespol Szkol",
		Teachers: []*arrangov1.Teacher{{Id: 1, Name: "A. Kowalska"}},
	}
	exporter := goptivum.NewExporter("szkola.edu.pl", "goptivum-server").WithKeys(keys)
	file, err := exporter.Export(t.Context(), &formats.Result{Document: document.New(school)})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Really encrypted: the teacher's name is not in the bytes.
	if bytes.Contains(file, []byte("A. Kowalska")) {
		t.Error("the exported file carries its contents in the clear")
	}

	// And this server reads it back with no password and no key file.
	importer := goptivum.NewImporter("szkola.edu.pl").WithKeys(keys)
	result, err := importer.Import(t.Context(), formats.Open("wrzesien.gpf", file))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Model.GetName() != "Zespol Szkol" {
		t.Errorf("model = %q", result.Model.GetName())
	}

	// A server without the key cannot, which is what makes it worth holding.
	stranger := goptivum.NewImporter("szkola.edu.pl")
	if _, err := stranger.Import(t.Context(), formats.Open("wrzesien.gpf", file)); !errors.Is(err, gpf.ErrIdentityRequired) {
		t.Errorf("err = %v, want ErrIdentityRequired for a server with no key", err)
	}
}

// A deployment with no passphrase writes plain files rather than failing. Not
// configuring escrow is a real choice a small school makes.
func TestWithoutEscrowFilesAreWrittenPlain(t *testing.T) {
	keys := newStore(t, "").ForFiles()
	exporter := goptivum.NewExporter("szkola.edu.pl", "goptivum-server").WithKeys(keys)

	file, err := exporter.Export(t.Context(),
		&formats.Result{Document: document.New(&arrangov1.SchoolModel{Name: "Mala Szkola"})})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	read, err := gpf.Decode(file, gpf.ReadOptions{Realm: "szkola.edu.pl"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if read.Mode != gpf.ModePlain {
		t.Errorf("mode = %q, want plain with no escrow configured", read.Mode)
	}
}

func TestFileKeyIsOwnedForTheStoreLifetime(t *testing.T) {
	conn := db.TestDB(t, migrations.FS)
	store := escrow.NewStore(conn, db.SQLite, passphrase).WithWorkFactor(10)
	key, err := store.Ensure(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`update school_key set sealed = x'0000' where id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(t.Context()); err == nil {
		t.Fatal("direct storage read ignored changed ciphertext")
	}
	keys := store.ForFiles()
	identity, err := keys.Identity(t.Context())
	if err != nil || identity != key.Identity {
		t.Fatal("file codec lost its loaded key")
	}
	recipient, err := keys.Recipient(t.Context())
	if err != nil || fmt.Sprint(recipient) != key.Recipient {
		t.Fatal("file codec changed its variant")
	}
}
