// identity/rules.go

package identity

import (
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

// PasswordRules are the password rules Kratos enforces. Kratos v1.3.1 takes
// them from its configuration only and offers no way to read them while it
// runs, so they are read from the file it was started with.
type PasswordRules struct {
	MinLength            int
	IdentifierSimilarity bool
	BreachCheck          bool
	MaxBreaches          int
}

// kratosFile is the part of kratos.yml the rules live in. A pointer is a key
// the file leaves out, which takes Kratos's default.
type kratosFile struct {
	Selfservice struct {
		Methods struct {
			Password struct {
				Config struct {
					MinPasswordLength                *int  `yaml:"min_password_length"`
					IdentifierSimilarityCheckEnabled *bool `yaml:"identifier_similarity_check_enabled"`
					HaveIBeenPwnedEnabled            *bool `yaml:"haveibeenpwned_enabled"`
					MaxBreaches                      *int  `yaml:"max_breaches"`
				} `yaml:"config"`
			} `yaml:"password"`
		} `yaml:"methods"`
	} `yaml:"selfservice"`
}

// ReadPasswordRules reads Kratos's configuration file. What it leaves out is
// Kratos v1.3.1's default from embedx/config.schema.json. Values Kratos takes
// from its environment are not seen.
func ReadPasswordRules(path string) (PasswordRules, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PasswordRules{}, fmt.Errorf("identity: read the Kratos configuration: %w", err)
	}
	var file kratosFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return PasswordRules{}, fmt.Errorf("identity: parse the Kratos configuration %s: %w", path, err)
	}
	config := file.Selfservice.Methods.Password.Config
	return PasswordRules{
		MinLength:            orValue(config.MinPasswordLength, 8),
		IdentifierSimilarity: orValue(config.IdentifierSimilarityCheckEnabled, true),
		BreachCheck:          orValue(config.HaveIBeenPwnedEnabled, true),
		MaxBreaches:          orValue(config.MaxBreaches, 0),
	}, nil
}

func orValue[T any](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}
