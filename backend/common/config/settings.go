// common/config/settings.go

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/smegg99/s99config"
)

// SettingsFile is the settings file's name. It sits beside config.yaml.
const SettingsFile = "settings.yaml"

// SettingsStore is the settings file: what an administrator changes while the
// server runs, checked against settings.cue before every write.
type SettingsStore struct {
	loader *s99config.Loader
	path   string

	mu      sync.RWMutex
	current Settings
}

// OpenSettings reads the settings file beside configPath, writing the schema's
// defaults the first time.
func OpenSettings(configPath string) (*SettingsStore, error) {
	path := filepath.Join(filepath.Dir(configPath), SettingsFile)
	loader, err := s99config.New(settingsDefinition, s99config.WithDefinition("#Settings"))
	if err != nil {
		return nil, fmt.Errorf("compile the settings schema: %w", err)
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := loader.WriteDefaults(path); err != nil {
			return nil, fmt.Errorf("write the default settings to %s: %w", path, err)
		}
	}
	if err := loader.Load(path); err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	store := &SettingsStore{loader: loader, path: path}
	if err := store.reload(); err != nil {
		return nil, err
	}
	return store, nil
}

// Path is the settings file.
func (s *SettingsStore) Path() string { return s.path }

// Current is the settings as last read or written.
func (s *SettingsStore) Current() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// SetLockout replaces the lockout limits. The schema checks the whole file
// before it is written, so a refused value leaves the file and Current alone.
func (s *SettingsStore) SetLockout(lockout Lockout) (Settings, error) {
	if err := s.loader.Patch(map[string]any{"lockout": lockout}); err != nil {
		return Settings{}, fmt.Errorf("change %s: %w", s.path, err)
	}
	if err := s.reload(); err != nil {
		return Settings{}, err
	}
	return s.Current(), nil
}

// SetServer replaces the server and project settings together, checked and
// written like SetLockout. No addresses is an empty list, which the schema
// takes and a null is not.
func (s *SettingsStore) SetServer(server ServerSettings, projects Projects) (Settings, error) {
	if server.PublicAddresses == nil {
		server.PublicAddresses = []string{}
	}
	if err := s.loader.Patch(map[string]any{"server": server, "projects": projects}); err != nil {
		return Settings{}, fmt.Errorf("change %s: %w", s.path, err)
	}
	if err := s.reload(); err != nil {
		return Settings{}, err
	}
	return s.Current(), nil
}

func (s *SettingsStore) reload() error {
	var decoded Settings
	if err := s.loader.Decode(&decoded); err != nil {
		return fmt.Errorf("decode %s: %w", s.path, err)
	}
	s.mu.Lock()
	s.current = decoded
	s.mu.Unlock()
	return nil
}
