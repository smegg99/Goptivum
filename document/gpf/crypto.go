// document/gpf/crypto.go

package gpf

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"filippo.io/age"
)

// maxWorkFactor bounds the scrypt cost this reader will pay for a file somebody
// sent it.
//
// age defaults its reader to 2^22, about fifteen seconds, and its own comment
// says that "might not be suitable for systems processing untrusted files".
// Goptivum reads files other people sent. 2^20 is about four seconds, which is
// well above any cost a legitimate writer chooses and far below a denial of
// service.
const maxWorkFactor = 20

// ErrPasswordRequired is an encrypted file whose payload is wrapped to a
// passphrase, and no passphrase was supplied.
//
// Answered from the flags before any age call, so a caller learns what to ask
// for rather than watching a decryption fail.
var ErrPasswordRequired Refusal = &refusal{key: KeyPasswordRequired}

// ErrIdentityRequired is the same for a file wrapped to keys.
var ErrIdentityRequired Refusal = &refusal{key: KeyIdentityRequired}

// ErrWrongPassword is a passphrase that did not open the file.
var ErrWrongPassword Refusal = &refusal{key: KeyWrongPassword}

// WrongRealmError is a file that belongs to another school.
//
// The realm is plaintext and unsigned, so a hostile file can claim any value:
// this refusal exists to turn "cannot decrypt" into "this belongs to
// szkola.edu.pl and you are signed into liceum.pl", and never to decide whether
// the payload is safe. What a file cannot lie about is which key opens it.
type WrongRealmError struct {
	File  string
	Local string
}

func (e *WrongRealmError) Error() string { return KeyWrongRealm }
func (e *WrongRealmError) Key() string   { return KeyWrongRealm }
func (e *WrongRealmError) Data() map[string]any {
	return map[string]any{"File": e.File, "Local": e.Local}
}

// checkRealm refuses a file from another school, when both say which.
//
// A file with no realm is not refused: plenty of legitimate files are written
// by a deployment that never configured one, and refusing them would make the
// field mandatory by accident.
func checkRealm(header *Header, local string) error {
	if local == "" {
		return nil
	}
	stated := header.Get(FieldRealm)
	if stated == "" || stated == local {
		return nil
	}
	return &WrongRealmError{File: stated, Local: local}
}

// unwrap turns the payload chunk into the encoded document.
//
// The chunk is already an exact slice, which is what makes this safe: age
// rejects trailing bytes, and a .gpf carries a footer and a locator after the
// payload. Handing age the rest of the file returns a nil error and then fails
// the read with "may be corrupted or tampered with", which is a message that
// sends somebody hunting an attack that never happened.
func unwrap(payload []byte, flags uint16, options ReadOptions) ([]byte, error) {
	if flags&FlagEncrypted == 0 {
		return payload, nil
	}

	identities, err := identitiesFor(flags, options)
	if err != nil {
		return nil, err
	}
	reader, err := age.Decrypt(bytes.NewReader(payload), identities...)
	if err != nil {
		if errors.Is(err, age.ErrIncorrectIdentity) {
			if flags&FlagPassphrase != 0 {
				return nil, ErrWrongPassword
			}
			return nil, ErrIdentityRequired
		}
		return nil, fmt.Errorf("gpf: decrypt: %w", err)
	}

	// Bounded before it reaches the decompressor. age.Decrypt streams, so
	// without this a file could feed unbounded plaintext into zstd, and the
	// zstd memory limit does not cap output on the streaming path.
	plain, err := io.ReadAll(io.LimitReader(reader, MaxPayload+1))
	if err != nil {
		// A truncated age stream fails here rather than at Decrypt, because the
		// authentication tag is at the end of each chunk.
		return nil, fmt.Errorf("gpf: decrypt: %w", err)
	}
	if len(plain) > MaxPayload {
		return nil, fmt.Errorf("%w: the decrypted payload is over the %d cap",
			ErrMalformed, MaxPayload)
	}
	return plain, nil
}

// identitiesFor is what this reader has to open the file with, and the one
// place PasswordRequired is answered.
func identitiesFor(flags uint16, options ReadOptions) ([]age.Identity, error) {
	if flags&FlagPassphrase != 0 {
		if options.Passphrase == "" {
			return nil, ErrPasswordRequired
		}
		identity, err := age.NewScryptIdentity(options.Passphrase)
		if err != nil {
			return nil, fmt.Errorf("gpf: passphrase: %w", err)
		}
		// Bounded, or a crafted file chooses how long this reader works.
		identity.SetMaxWorkFactor(maxWorkFactor)
		return []age.Identity{identity}, nil
	}
	if len(options.Identities) == 0 {
		return nil, ErrIdentityRequired
	}
	return options.Identities, nil
}

// wrap encrypts the encoded document, and reports the flags that describe what
// it did. Plain is genuinely plain: no age, not even a no-op recipient.
func wrap(encoded []byte, options WriteOptions) ([]byte, uint16, error) {
	switch {
	case options.Passphrase != "" && len(options.Recipients) > 0:
		// age refuses this itself, with a message about stanzas that says
		// nothing about what the caller did wrong.
		return nil, 0, fmt.Errorf("gpf: a file is wrapped to a password or to keys, never both")

	case options.Passphrase != "":
		recipient, err := age.NewScryptRecipient(options.Passphrase)
		if err != nil {
			return nil, 0, fmt.Errorf("gpf: passphrase: %w", err)
		}
		if options.WorkFactor > 0 {
			recipient.SetWorkFactor(options.WorkFactor)
		}
		sealed, err := seal(encoded, recipient)
		if err != nil {
			return nil, 0, err
		}
		return sealed, FlagEncrypted | FlagPassphrase, nil

	case len(options.Recipients) > 0:
		sealed, err := seal(encoded, options.Recipients...)
		if err != nil {
			return nil, 0, err
		}
		return sealed, FlagEncrypted, nil

	default:
		return encoded, 0, nil
	}
}

// seal runs one age stream to completion. Close is what writes the final chunk,
// so a caller that forgets it produces a file that decrypts to nothing.
func seal(plain []byte, recipients ...age.Recipient) ([]byte, error) {
	var out bytes.Buffer
	writer, err := age.Encrypt(&out, recipients...)
	if err != nil {
		return nil, fmt.Errorf("gpf: encrypt: %w", err)
	}
	if _, err := writer.Write(plain); err != nil {
		return nil, fmt.Errorf("gpf: encrypt: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("gpf: encrypt: %w", err)
	}
	return out.Bytes(), nil
}
