// core/auth/auth.go
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"smegg.me/goptivum/common/config"

	"github.com/spf13/viper"
	"golang.org/x/crypto/argon2"
)

type HashPrefix string

const (
	HashPrefixPassword HashPrefix = "password\x00"
)

func HashWithPepper(prefix HashPrefix, pepper []byte, data []byte) []byte {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(prefix))
	mac.Write(data)
	return mac.Sum(nil)
}

func CtEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func argon2Params() (time uint32, memory uint32, threads uint8, keyLen uint32) {
	c := config.GetConfig().Auth.PasswordHashing
	return c.Time, c.Memory, c.Threads, c.KeyLen
}

func encodeArgon2id(salt, hash []byte, t, m uint32, p uint8) string {
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", m, t, p, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash))
}

func parseArgon2id(encoded string) (t uint32, m uint32, p uint8, salt, sum []byte, err error) {
	if !strings.HasPrefix(encoded, "$argon2id$") {
		err = errors.New("unsupported hash format")
		return
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		err = errors.New("invalid argon2 hash parts")
		return
	}
	paramPart := parts[3]
	for _, kv := range strings.Split(paramPart, ",") {
		pair := strings.SplitN(kv, "=", 2)
		if len(pair) != 2 {
			continue
		}
		switch pair[0] {
		case "m":
			if u, e := strconv.ParseUint(pair[1], 10, 32); e == nil {
				m = uint32(u)
			}
		case "t":
			if u, e := strconv.ParseUint(pair[1], 10, 32); e == nil {
				t = uint32(u)
			}
		case "p":
			if u, e := strconv.ParseUint(pair[1], 10, 8); e == nil {
				p = uint8(u)
			}
		}
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return
	}
	sum, err = base64.RawStdEncoding.DecodeString(parts[5])
	return
}

func validatePasswordRequirements(password string) error {
	var Config = config.GetConfig()

	rules := Config.Auth.Password.Rules
	if len(password) < rules.MinLength {
		return fmt.Errorf("password must be at least %d characters long, got %d", rules.MinLength, len(password))
	}

	if rules.MaxLength > 0 && len(password) > rules.MaxLength {
		return fmt.Errorf("password must be at most %d characters long, got %d", rules.MaxLength, len(password))
	}

	if rules.RequireUppercase && !strings.ContainsAny(password, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return fmt.Errorf("password must contain at least one uppercase letter")
	}

	if rules.RequireLowercase && !strings.ContainsAny(password, "abcdefghijklmnopqrstuvwxyz") {
		return fmt.Errorf("password must contain at least one lowercase letter")
	}

	if rules.RequireNumbers && !strings.ContainsAny(password, "0123456789") {
		return fmt.Errorf("password must contain at least one number")
	}

	if rules.RequireSpecialChars && !strings.ContainsAny(password, "!@#$%^&*()_+-=[]{}|;:,.<>?") {
		return fmt.Errorf("password must contain at least one special character")
	}

	return nil
}

func ValidatePassword(password, encoded string) bool {
	pepper := []byte(viper.GetString("PEPPER"))
	prehash := HashWithPepper(HashPrefixPassword, pepper, []byte(password))
	t, m, p, salt, sum, err := parseArgon2id(encoded)
	if err != nil {
		return false
	}
	if uint64(len(sum)) > uint64(^uint32(0)) {
		return false
	}
	// #nosec G115
	computed := argon2.IDKey(prehash, salt, t, m, p, uint32(len(sum)))
	return CtEqual(computed, sum)
}

func ValidateLogin(login string) error {
	var Config = config.GetConfig()

	if len(login) < Config.Auth.Login.Rules.MinLength {
		return fmt.Errorf("login must be at least %d characters long, got %d", Config.Auth.Login.Rules.MinLength, len(login))
	}

	if Config.Auth.Login.Rules.MaxLength > 0 && len(login) > Config.Auth.Login.Rules.MaxLength {
		return fmt.Errorf("login must be at most %d characters long, got %d", Config.Auth.Login.Rules.MaxLength, len(login))
	}

	if Config.Auth.Login.Rules.NoSpaces && strings.ContainsAny(login, " \t\n\r") {
		return fmt.Errorf("login must not contain spaces")
	}

	return nil
}

func HashPassword(password string) (string, error) {
	if err := validatePasswordRequirements(password); err != nil {
		return "", err
	}

	pepper := []byte(viper.GetString("PEPPER"))
	prehash := HashWithPepper(HashPrefixPassword, pepper, []byte(password))

	t, m, p, keyLen := argon2Params()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey(prehash, salt, t, m, p, keyLen)
	return encodeArgon2id(salt, hash, t, m, p), nil
}
