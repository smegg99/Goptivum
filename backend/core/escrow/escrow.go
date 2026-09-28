// core/escrow/escrow.go

// Package escrow holds the school's own key for .gpf Identity mode.
//
// One key for the whole school, not one per person: anyone signed into the
// school opens any of its files. The threat this answers is a leaked
// attachment and a lost laptop, never a hostile school server, because the
// server is where the key lives.
//
// The private half is sealed with age before it touches the database, so a
// dump of the database alone opens nothing. The passphrase that seals it lives
// in configuration, which travels separately: a .sql backup goes on a USB stick
// while the configuration stays on the server. It is not protection against a
// compromised server, which holds both halves by definition.
package escrow

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"filippo.io/age"
	"github.com/smegg99/s99ab/data/db"
)

// ErrNoKey means this school has no escrowed key yet.
var ErrNoKey = errors.New("escrow: this school has no key")

// ErrSealed means the stored key exists and this passphrase does not open it,
// which is a changed or mistyped passphrase rather than a missing key.
var ErrSealed = errors.New("escrow: the stored key does not open with this passphrase")

// ErrDisabled means no passphrase is configured, so nothing is stored at all.
//
// Deliberately not "store it in the clear": a key in a database dump is every
// file the school ever exported, and a feature that is off is easier to explain
// than one that is quietly insecure.
var ErrDisabled = errors.New("escrow: no passphrase configured, so no key is held")

// DefaultWorkFactor is what sealing costs, as a power of two. Higher than a
// person's own password, because this is unsealed once at boot rather than on
// every sign-in, and the thing it protects is every file the school ever
// exported. About two seconds on a modern machine.
const DefaultWorkFactor = 19

// now is replaced in tests.
var now = func() time.Time { return time.Now().UTC() }

// Store is the school_key table.
type Store struct {
	conn       *sql.DB
	driver     string
	passphrase string
	// workFactor is the scrypt cost, so a test can run in milliseconds without
	// the production cost becoming a test's opinion.
	workFactor int
}

// NewStore binds the table to one pool. An empty passphrase leaves escrow off.
func NewStore(conn *sql.DB, driver, passphrase string) *Store {
	return &Store{conn: conn, driver: driver, passphrase: passphrase,
		workFactor: DefaultWorkFactor}
}

// WithWorkFactor lowers the scrypt cost. For tests: two seconds per seal is
// right for a school's key and wrong for a suite that runs on every save.
func (s *Store) WithWorkFactor(logN int) *Store {
	if logN > 0 {
		s.workFactor = logN
	}
	return s
}

// Enabled reports whether this deployment holds a key at all.
func (s *Store) Enabled() bool { return s != nil && s.conn != nil && s.passphrase != "" }

func (s *Store) rebind(query string) string { return db.Rebind(s.driver, query) }

// Key is the school's key, unsealed.
type Key struct {
	// Identity opens files. It never leaves this process except through a
	// deliberate release to an authenticated session.
	Identity *age.X25519Identity
	// Recipient is what files are encrypted to, and is not a secret.
	Recipient string
	CreatedAt time.Time
}

// Ensure returns the school's key, creating one the first time.
//
// Idempotent by the table's own shape: one row, checked at id = 1, so two
// servers racing at first boot cannot end up with two keys and half the
// school's files unreadable by the other half.
func (s *Store) Ensure(ctx context.Context) (*Key, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	key, err := s.Get(ctx)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, ErrNoKey) {
		return nil, err
	}

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("escrow: generate: %w", err)
	}
	sealed, err := seal(identity.String(), s.passphrase, s.workFactor)
	if err != nil {
		return nil, err
	}
	created := now()

	query := s.rebind(`insert into school_key (id, recipient, sealed, created_at)
		values (1, ?, ?, ?)`)
	if _, err := s.conn.ExecContext(ctx, query,
		identity.Recipient().String(), sealed, created); err != nil {
		// Somebody else inserted between the read and this write. Theirs is the
		// school's key, not this one: a second key would leave half the files
		// unreadable.
		if existing, readErr := s.Get(ctx); readErr == nil {
			return existing, nil
		}
		return nil, fmt.Errorf("escrow: store key: %w", err)
	}
	return &Key{Identity: identity, Recipient: identity.Recipient().String(), CreatedAt: created}, nil
}

// Get reads the school's key, unsealing it.
func (s *Store) Get(ctx context.Context) (*Key, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	query := s.rebind(`select recipient, sealed, created_at from school_key where id = 1`)
	var recipient string
	var sealed []byte
	var created time.Time
	err := s.conn.QueryRowContext(ctx, query).Scan(&recipient, &sealed, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoKey
	}
	if err != nil {
		return nil, fmt.Errorf("escrow: read key: %w", err)
	}

	opened, err := unseal(sealed, s.passphrase, s.workFactor)
	if err != nil {
		return nil, err
	}
	identity, err := age.ParseX25519Identity(opened)
	if err != nil {
		return nil, fmt.Errorf("escrow: the stored key is not an identity: %w", err)
	}
	// The stored recipient and the one the key derives must agree. They cannot
	// disagree by accident, so a disagreement is a database somebody edited.
	if derived := identity.Recipient().String(); derived != recipient {
		return nil, fmt.Errorf("escrow: the stored key does not match its recipient")
	}
	return &Key{Identity: identity, Recipient: recipient, CreatedAt: created.UTC()}, nil
}

// Public is the key's public half and when it was made.
type Public struct {
	Recipient string
	CreatedAt time.Time
}

// Public reads the public half without unsealing, so it costs no scrypt and a
// server that cannot open its own key can still say what files are encrypted
// to.
func (s *Store) Public(ctx context.Context) (Public, error) {
	if !s.Enabled() {
		return Public{}, ErrDisabled
	}
	query := s.rebind(`select recipient, created_at from school_key where id = 1`)
	var public Public
	err := s.conn.QueryRowContext(ctx, query).Scan(&public.Recipient, &public.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Public{}, ErrNoKey
	}
	if err != nil {
		return Public{}, fmt.Errorf("escrow: read the public half: %w", err)
	}
	public.CreatedAt = public.CreatedAt.UTC()
	return public, nil
}

// seal wraps the private key with the operator's passphrase.
func seal(secret, passphrase string, workFactor int) ([]byte, error) {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("escrow: passphrase: %w", err)
	}
	recipient.SetWorkFactor(workFactor)

	var out bytes.Buffer
	writer, err := age.Encrypt(&out, recipient)
	if err != nil {
		return nil, fmt.Errorf("escrow: seal: %w", err)
	}
	if _, err := io.WriteString(writer, secret); err != nil {
		return nil, fmt.Errorf("escrow: seal: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("escrow: seal: %w", err)
	}
	return out.Bytes(), nil
}

// unseal opens it again.
func unseal(sealed []byte, passphrase string, workFactor int) (string, error) {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return "", fmt.Errorf("escrow: passphrase: %w", err)
	}
	// Bounded, so a row somebody edited cannot choose how long this takes.
	identity.SetMaxWorkFactor(workFactor + 1)

	reader, err := age.Decrypt(bytes.NewReader(sealed), identity)
	if err != nil {
		if errors.Is(err, age.ErrIncorrectIdentity) {
			return "", ErrSealed
		}
		return "", fmt.Errorf("escrow: unseal: %w", err)
	}
	opened, err := io.ReadAll(io.LimitReader(reader, 4096))
	if err != nil {
		return "", fmt.Errorf("escrow: unseal: %w", err)
	}
	return string(opened), nil
}

// Keys adapts this store to what the .gpf codec asks for.
//
// A separate type so the codec depends on two small methods rather than on a
// store that needs a database: the desktop uses that codec and has neither.
type Keys struct{ store *Store }

// ForFiles is this store as the codec's key source. A nil store is a
// deployment with no escrow, and answers nil rather than an error, so a plain
// file still reads.
func (s *Store) ForFiles() *Keys { return &Keys{store: s} }

// Identity opens files encrypted to this school, and is nil when there is no
// key to open them with.
func (k *Keys) Identity(ctx context.Context) (age.Identity, error) {
	if k == nil || !k.store.Enabled() {
		return nil, nil
	}
	key, err := k.store.Get(ctx)
	if errors.Is(err, ErrNoKey) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return key.Identity, nil
}

// Recipient is what new files are encrypted to.
//
// It reads the public half without unsealing, so a server whose passphrase
// changed can still write files the school's other machines will open once the
// passphrase is put back.
func (k *Keys) Recipient(ctx context.Context) (age.Recipient, error) {
	if k == nil || !k.store.Enabled() {
		return nil, nil
	}
	stored, err := k.store.Public(ctx)
	if errors.Is(err, ErrNoKey) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	recipient, err := age.ParseX25519Recipient(stored.Recipient)
	if err != nil {
		return nil, fmt.Errorf("escrow: the stored recipient is not a key: %w", err)
	}
	return recipient, nil
}
