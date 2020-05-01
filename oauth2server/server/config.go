package server

import (
	"net/http"
	"time"

	"github.com/autowp/auth/oauth2server"
)

// Config configuration parameters
type Config struct {
}

// NewConfig create to configuration instance
func NewConfig() *Config {
	return &Config{}
}

// AuthorizeRequest authorization request
type AuthorizeRequest struct {
	ResponseType   oauth2server.ResponseType
	ClientID       string
	Scope          string
	State          string
	UserID         string
	AccessTokenExp time.Duration
	Request        *http.Request
}
