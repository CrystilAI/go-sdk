package crystil

import (
	"errors"
	"time"
)

// config holds client configuration.
type config struct {
	apiKey            string
	collectorURL      string
	apiURL            string
	timeout           time.Duration
	raiseFinalAttempt bool
	version           string
}

// Option configures a Client.
type Option func(*config) error

// WithAPIKey sets the Crystil API key.
func WithAPIKey(key string) Option {
	return func(c *config) error {
		if key == "" {
			return errors.New("crystil: empty API key")
		}
		c.apiKey = key
		return nil
	}
}

// WithTimeout sets the HTTP timeout for analytics submission.
func WithTimeout(d time.Duration) Option {
	return func(c *config) error {
		if d <= 0 {
			return errors.New("crystil: timeout must be positive")
		}
		c.timeout = d
		return nil
	}
}

// WithCollectorURL sets a custom collector endpoint (for testing).
func WithCollectorURL(url string) Option {
	return func(c *config) error {
		if url == "" {
			return errors.New("crystil: empty collector URL")
		}
		c.collectorURL = url
		return nil
	}
}

// WithAPIURL sets a custom API endpoint (for testing).
func WithAPIURL(url string) Option {
	return func(c *config) error {
		if url == "" {
			return errors.New("crystil: empty API URL")
		}
		c.apiURL = url
		return nil
	}
}

// APIKey returns the API key.
func (c *config) APIKey() string {
	return c.apiKey
}

// Version returns the SDK version.
func (c *config) Version() string {
	return c.version
}
