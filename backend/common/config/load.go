// common/config/load.go

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/smegg99/s99config"
)

// DefaultPath is where the server looks when nothing says otherwise.
const DefaultPath = "config.yaml"

// keyringService is where a secret lives when it is not in the environment.
const keyringService = "Goptivum Server"

// Load reads and validates the configuration, writing the defaults on first
// run so a fresh install starts with a file it can edit rather than an error.
func Load(path string) (Config, string, error) {
	// A .env beside the binary is how a development machine supplies secrets.
	_ = godotenv.Load()

	if path == "" {
		path = Path()
	}
	loader, err := s99config.New(definition, s99config.WithReferences(s99config.ReferenceOptions{
		ConfigDir:      filepath.Dir(path),
		DataDir:        filepath.Dir(path),
		KeyringService: keyringService,
	}))
	if err != nil {
		return Config{}, path, fmt.Errorf("compile the configuration schema: %w", err)
	}

	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := loader.WriteDefaults(path); err != nil {
			return Config{}, path, fmt.Errorf("write the default configuration to %s: %w", path, err)
		}
	}
	if err := loader.Load(path); err != nil {
		return Config{}, path, fmt.Errorf("load %s: %w", path, err)
	}

	var settings Config
	if err := loader.Decode(&settings); err != nil {
		return Config{}, path, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := validate(settings); err != nil {
		return Config{}, path, err
	}
	return settings, path, nil
}

// Path is the configuration file the environment names, or the default.
func Path() string {
	if value := strings.TrimSpace(os.Getenv("GOPTIVUM_CONFIG")); value != "" {
		return value
	}
	return DefaultPath
}

// validate catches what CUE cannot: a combination that compiles and still
// cannot serve.
func validate(settings Config) error {
	if settings.Storage.Database.Driver == "postgres" && !hasSecret(settings.Storage.Database.DSN) {
		return fmt.Errorf("a postgres database needs storage.database.dsn or GOPTIVUM_DATABASE_URL")
	}
	if settings.Storage.Documents.Endpoint != "" {
		if !hasSecret(settings.Storage.Documents.AccessKey) || !hasSecret(settings.Storage.Documents.SecretKey) {
			return fmt.Errorf("a document bucket needs storage.documents.access_key and secret_key, " +
				"or GOPTIVUM_STORAGE_ACCESS_KEY and GOPTIVUM_STORAGE_SECRET_KEY")
		}
	}
	return nil
}

func hasSecret(secret s99config.Secret) bool {
	return secret != nil && secret.IsSet() && strings.TrimSpace(secret.Reveal()) != ""
}

// DatabaseDSN is the connection string, which for sqlite is the configured
// file path when no DSN is set.
func (c Config) DatabaseDSN() string {
	if hasSecret(c.Storage.Database.DSN) {
		return c.Storage.Database.DSN.Reveal()
	}
	return c.Storage.Database.Path
}
