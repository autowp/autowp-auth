package auth

import (
	"log"
	"time"
)

// TokenStoreOption is the configuration options type for token store
type TokenStoreOption func(s *TokenStore)

// WithTokenStoreGCInterval returns option that sets token store garbage collection interval
func WithTokenStoreGCInterval(gcInterval time.Duration) TokenStoreOption {
	return func(s *TokenStore) {
		s.gcInterval = gcInterval
	}
}

// WithTokenStoreLogger returns option that sets token store logger implementation
func WithTokenStoreLogger(logger *log.Logger) TokenStoreOption {
	return func(s *TokenStore) {
		s.logger = logger
	}
}

// WithTokenStoreGCDisabled returns option that disables token store garbage collection
func WithTokenStoreGCDisabled() TokenStoreOption {
	return func(s *TokenStore) {
		s.gcDisabled = true
	}
}
