// common/config/defaults.go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

func setEnvDefaultsCore() {
	// Logging configuration
	viper.SetDefault("LOGS_DIR", "./logs")
	viper.SetDefault("ENABLE_LOG_FILES", false)
	viper.SetDefault("NO_COLOR", false)
	viper.SetDefault("LOG_PREFIX", "")
	viper.SetDefault("LOG_LEVEL", "INFO")

	// Application configuration
	viper.SetDefault("CONFIG_PATH", "./")
	viper.SetDefault("CONFIG_TYPE", "json")
	viper.SetDefault("USE_DEBUG_CONFIG", false)
	viper.SetDefault("GIN_MODE", "release")
	viper.SetDefault("API_PORT", "3001")
	viper.SetDefault("DIST_PATH", "./dist")
	viper.SetDefault("ENABLE_SWAGGER", false)
	viper.SetDefault("DEBUG", false)

	// Database configuration
	viper.SetDefault("DB_FILE_PATH", "./badger")

	// Security
	viper.SetDefault("PEPPER", "")
	viper.SetDefault("JWT_SECRET", "")

	// Admin account defaults
	viper.SetDefault("ADMIN_ACCOUNT_LOGIN", "admin")
	viper.SetDefault("ADMIN_ACCOUNT_PASSWORD", "AsdAsd123!")

	// Default account defaults
	viper.SetDefault("DEFAULT_ACCOUNT_LOGIN", "default")
	viper.SetDefault("DEFAULT_ACCOUNT_PASSWORD", "DsaDsa321!")
}

func setConfigDefaults() {
	// API configuration
	viper.SetDefault("api.allow_origins", []string{"http://localhost:3000", "http://localhost:3001"})
	viper.SetDefault("api.max_request_size", int64(1048576)) // 1 MB
	viper.SetDefault("api.rate_limit_per_minute", int64(60))

	// Password hashing (Argon2id)
	viper.SetDefault("auth.password_hashing.time", 3)
	viper.SetDefault("auth.password_hashing.memory", 64*1024) // 64 MiB
	viper.SetDefault("auth.password_hashing.threads", 1)
	viper.SetDefault("auth.password_hashing.key_len", 32)

	// JWT configuration
	viper.SetDefault("auth.jwt.access_token_expiry", 900)     // 15 minutes
	viper.SetDefault("auth.jwt.refresh_token_expiry", 604800) // 7 days

	// Session configuration
	viper.SetDefault("auth.session.ttl", 7*24*time.Hour) // 7 days
	viper.SetDefault("auth.session.max_active_sessions", 1)
	viper.SetDefault("auth.session.session_cookie", "session_id")
	viper.SetDefault("auth.session.csrf_cookie", "csrf_token")
	viper.SetDefault("auth.session.cookie_domain", "localhost")
	viper.SetDefault("auth.session.cookie_secure", false)
}

func bindEnvVars() {
	keys := []string{
		// Application settings
		"GIN_MODE",
		"LOG_LEVEL",
		"ENABLE_SWAGGER",
		"API_PORT",
		"DEBUG",
		"DIST_PATH",
		"CONFIG_PATH",
		"CONFIG_TYPE",
		"USE_DEBUG_CONFIG",

		// Database settings
		"DB_FILE_PATH",

		// Security
		"PEPPER",
		"JWT_SECRET",

		// Admin account
		"ADMIN_ACCOUNT_LOGIN",
		"ADMIN_ACCOUNT_PASSWORD",

		// Default account
		"DEFAULT_ACCOUNT_LOGIN",
		"DEFAULT_ACCOUNT_PASSWORD",

		// Additional overrides
		"ALLOWED_ORIGINS",
	}

	for _, k := range keys {
		_ = viper.BindEnv(k)
	}
}

func writeDefaultConfigFile(cfg GlobalConfig) error {
	dir := viper.GetString("CONFIG_PATH")
	if dir == "" {
		dir = "."
	}

	name := "config"
	if viper.GetBool("USE_DEBUG_CONFIG") {
		name = "debug_config"
	}

	ext := viper.GetString("CONFIG_TYPE")
	if ext == "" {
		ext = "json"
	}

	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("%s.%s", name, ext))
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
