// config/models.go
package config

type APIConfig struct {
	AllowOrigins       []string `mapstructure:"allow_origins" validate:"required,min=1,max=10,dive,required,url"`
	MaxRequestSize     int64    `mapstructure:"max_request_size" validate:"required,min=1024,max=104857600"` // 1KB to 100MB
	RateLimitPerMinute int64    `mapstructure:"rate_limit_per_minute" validate:"required,min=1,max=10000"`
}

type PasswordRules struct {
	MinLength           int  `mapstructure:"min_length" validate:"gte=0,max=64"`
	MaxLength           int  `mapstructure:"max_length" validate:"gte=0,max=128"`
	RequireUppercase    bool `mapstructure:"require_uppercase"`
	RequireLowercase    bool `mapstructure:"require_lowercase"`
	RequireNumbers      bool `mapstructure:"require_numbers"`
	RequireSpecialChars bool `mapstructure:"require_special_chars"`
}

type PasswordConfig struct {
	Rules PasswordRules `mapstructure:"rules" validate:"required"`
}

type LoginRules struct {
	MinLength int  `mapstructure:"min_length" validate:"gte=0,max=64"`
	MaxLength int  `mapstructure:"max_length" validate:"gte=0,max=128"`
	NoSpaces  bool `mapstructure:"no_spaces"`
}

type LoginConfig struct {
	Rules LoginRules `mapstructure:"rules" validate:"required"`
}

type JWTConfig struct {
	AccessTokenExpiry  int `mapstructure:"access_token_expiry" validate:"required,min=60,max=86400"`      // 1 minute to 24 hours in seconds
	RefreshTokenExpiry int `mapstructure:"refresh_token_expiry" validate:"required,min=3600,max=2592000"` // 1 hour to 30 days in seconds
}

type AuthConfig struct {
	Login           LoginConfig           `mapstructure:"login" validate:"required"`
	Password        PasswordConfig        `mapstructure:"password" validate:"required"`
	PasswordHashing PasswordHashingConfig `mapstructure:"password_hashing" validate:"required"`
	JWT             JWTConfig             `mapstructure:"jwt" validate:"required"`
}

type PasswordHashingConfig struct {
	Time    uint32 `mapstructure:"time" validate:"required,min=1,max=10"`
	Memory  uint32 `mapstructure:"memory" validate:"required,min=8192,max=1048576"` // 8 MiB - 1 GiB
	Threads uint8  `mapstructure:"threads" validate:"required,min=1,max=16"`
	KeyLen  uint32 `mapstructure:"key_len" validate:"required,min=16,max=64"`
}

type GlobalConfig struct {
	API  APIConfig  `mapstructure:"api" validate:"required"`
	Auth AuthConfig `mapstructure:"auth" validate:"required"`
}
