package auth

import (
	"fmt"

	"github.com/autowp/auth/oauth2server/models"
	"github.com/spf13/viper"
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
	Driver                string          `yaml:"driver"                     mapstructure:"driver"`
	DSN                   string          `yaml:"dsn"                        mapstructure:"dsn"`
	Secret                string          `yaml:"secret"                     mapstructure:"secret"`
	UserStore             UserStoreConfig `yaml:"user_store"                 mapstructure:"user_store"`
	Clients               []models.Client `yaml:"clients"                    mapstructure:"clients"`
	AccessTokenExpiresIn  uint            `yaml:"access_token_expires_in"    mapstructure:"access_token_expires_in"`
	RefreshTokenExpiresIn uint            `yaml:"refresh_token_expires_in"   mapstructure:"refresh_token_expires_in"`
}

// ServiceConfig ServiceConfig
type ServiceConfig struct {
	ClientID     string   `yaml:"client_id"     mapstructure:"client_id"`
	ClientSecret string   `yaml:"client_secret" mapstructure:"client_secret"`
	Scopes       []string `yaml:"scopes"        mapstructure:"scopes"`
}

// ServicesConfig ...
type ServicesConfig struct {
	RedirectURI string        `yaml:"redirect_uri" mapstructure:"redirect_uri"`
	Google      ServiceConfig `yaml:"google"       mapstructure:"google"`
	Facebook    ServiceConfig `yaml:"facebook"     mapstructure:"facebook"`
	VK          ServiceConfig `yaml:"vk"           mapstructure:"vk"`
}

// Config Application config definition
type Config struct {
	Sentry     SentryConfig     `yaml:"sentry"`
	Listen     string           `yaml:"listen"`
	Migrations MigrationsConfig `yaml:"migrations"`
	OAuth      OAuthConfig      `yaml:"oauth"`
	Hosts      []Host           `yaml:"hosts"`
	Services   ServicesConfig   `yaml:"services"`
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
