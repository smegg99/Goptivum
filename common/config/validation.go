// common/config/validation.go
package config

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"

	"github.com/go-playground/validator"
	"github.com/spf13/viper"
	"smegg.me/goptivum/common/logger"
)

func validateConfig(config *GlobalConfig) error {
	validate := validator.New()

	_ = validate.RegisterValidation("matches", func(fl validator.FieldLevel) bool {
		if fl.Field().Kind() != reflect.String {
			return false
		}
		pattern := fl.Param()
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false
		}
		return re.MatchString(fl.Field().String())
	})

	err := validate.Struct(config)
	if err != nil {
		var invalidValidationError *validator.InvalidValidationError
		if errors.As(err, &invalidValidationError) {
			logger.Errorf("invalid validation error: %v", err)
			return err
		}

		var validateErrs validator.ValidationErrors
		if errors.As(err, &validateErrs) {
			logger.Errorf("configuration validation failed:")
			for _, e := range validateErrs {
				logger.Errorf("field: %s, tag: %s, value: %v, error: failed '%s' validation",
					e.Field(), e.Tag(), e.Value(), e.Tag())
			}
		}
		return err
	}
	return nil
}

func validateEnv() error {
	required := map[string]string{
		"PEPPER": viper.GetString("PEPPER"),
	}

	missing := []string{}
	for k, v := range required {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		logger.Errorf("missing required environment variables: %v", missing)
		return fmt.Errorf("missing required env: %v", missing)
	}

	pepper := viper.GetString("PEPPER")
	if len(pepper) > 0 && len(pepper) < 32 {
		logger.Warnf("PEPPER should be at least 32 characters for security (current: %d)", len(pepper))
	}

	dbFilePath := viper.GetString("DB_FILE_PATH")
	if dbFilePath == "" {
		logger.Warn("DB_FILE_PATH not set, using default: ./badger")
	}

	apiPort := viper.GetString("API_PORT")
	if apiPort == "" {
		logger.Warn("API_PORT not set, using default: 3001")
	}

	adminLogin := viper.GetString("ADMIN_ACCOUNT_LOGIN")
	adminPassword := viper.GetString("ADMIN_ACCOUNT_PASSWORD")
	if adminLogin == "" {
		logger.Warn("ADMIN_ACCOUNT_LOGIN not set, using default")
	}
	if adminPassword == "" {
		logger.Warn("ADMIN_ACCOUNT_PASSWORD not set, using default")
	}

	defaultLogin := viper.GetString("DEFAULT_ACCOUNT_LOGIN")
	defaultPassword := viper.GetString("DEFAULT_ACCOUNT_PASSWORD")
	if defaultLogin == "" {
		logger.Warn("DEFAULT_ACCOUNT_LOGIN not set, using default")
	}
	if defaultPassword == "" {
		logger.Warn("DEFAULT_ACCOUNT_PASSWORD not set, using default")
	}

	return nil
}
