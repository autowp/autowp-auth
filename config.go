package auth

import (
	"fmt"

	"github.com/spf13/viper"
	"gopkg.in/oauth2.v3"
	"gopkg.in/oauth2.v3/models"
)

// MigrationsConfig MigrationsConfig
type MigrationsConfig struct {
	DSN string `yaml:"dsn"`
	Dir string `yaml:"dir"`
}

// SentryConfig SentryConfig
type SentryConfig struct {
	DSN         string `yaml:"dsn"`
	Environment string `yaml:"environment"`
}

// OAuthConfig OAuthConfig
type OAuthConfig struct {
	Driver                string                `yaml:"driver"                     mapstructure:"driver"`
	DSN                   string                `yaml:"dsn"                        mapstructure:"dsn"`
	TokenType             string                `yaml:"token_type"                 mapstructure:"token_type"`
	AllowedResponseTypes  []oauth2.ResponseType `yaml:"allowed_response_types"     mapstructure:"allowed_response_types"`
	AllowedGrantTypes     []oauth2.GrantType    `yaml:"allowed_grant_types"        mapstructure:"allowed_grant_types"`
	AllowGetAccessRequest bool                  `yaml:"allowed_get_access_request" mapstructure:"allowed_get_access_request"`
	Secret                string                `yaml:"secret"                     mapstructure:"secret"`
	UserStore             UserStoreConfig       `yaml:"user_store"                 mapstructure:"user_store"`
	Clients               []models.Client       `yaml:"clients"                    mapstructure:"clients"`
	AccessTokenExpiresIn  uint                  `yaml:"access_token_expires_in"    mapstructure:"access_token_expires_in"`
	RefreshTokenExpiresIn uint                  `yaml:"refresh_token_expires_in"   mapstructure:"refresh_token_expires_in"`
}

// Config Application config definition
type Config struct {
	Sentry     SentryConfig     `yaml:"sentry"`
	Listen     string           `yaml:"listen"`
	Migrations MigrationsConfig `yaml:"migrations"`
	OAuth      OAuthConfig      `yaml:"oauth"`
}

// LoadConfig LoadConfig
func LoadConfig() Config {

	config := Config{}

	viper.SetConfigName("defaults")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}

	viper.SetConfigName("config")
	err = viper.MergeInConfig()
	if err != nil {
		panic(err)
	}

	err = viper.Unmarshal(&config)
	if err != nil {
		panic(fmt.Errorf("fatal error unmarshal config: %s", err))
	}

	return config
}

// ValidateConfig ValidateConfig
func ValidateConfig(config Config) {
}
