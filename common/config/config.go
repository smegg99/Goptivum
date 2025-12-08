// common/config/config.go
package config

import (
	"fmt"

	"smegg.me/goptivum/common/logger"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

var Global GlobalConfig

func loadConfig(config *GlobalConfig) error {
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			logger.Warnf("config file not found, using defaults and environment")
			if err := viper.Unmarshal(config); err != nil {
				return fmt.Errorf("failed to unmarshal default config: %w", err)
			}
			if err := writeDefaultConfigFile(*config); err != nil {
				logger.Warnf("failed to create config file: %v", err)
			}
			return nil
		}
		return fmt.Errorf("error reading config file: %w", err)
	}

	logger.Infof("loaded config file: %s", viper.ConfigFileUsed())

	if err := viper.Unmarshal(config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	logger.Debugf("all settings snapshot: %#v", viper.AllSettings())

	return nil
}

func Initialize() error {
	if err := godotenv.Load(); err != nil {
		logger.Infof(".env file not found or failed to load: %v", err)
	} else {
		logger.Debug(".env loaded successfully")
	}

	setEnvDefaultsCore()
	setConfigDefaults()
	bindEnvVars()

	logger.Configure(logger.Config{
		Dir:         viper.GetString("LOGS_DIR"),
		EnableFiles: viper.GetBool("ENABLE_LOG_FILES"),
		NoColor:     viper.GetBool("NO_COLOR"),
		Prefix:      viper.GetString("LOG_PREFIX"),
		Level:       viper.GetString("LOG_LEVEL"),
	})

	viper.AddConfigPath(viper.GetString("CONFIG_PATH"))
	viper.SetConfigType(viper.GetString("CONFIG_TYPE"))

	if viper.GetBool("USE_DEBUG_CONFIG") {
		viper.SetConfigName("debug_config")
	} else {
		viper.SetConfigName("config")
	}

	if err := loadConfig(&Global); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := validateConfig(&Global); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	if err := validateEnv(); err != nil {
		return fmt.Errorf("environment validation failed: %w", err)
	}

	logger.Info("configuration loaded and validated successfully")

	return nil
}

func GetConfig() *GlobalConfig {
	return &Global
}
