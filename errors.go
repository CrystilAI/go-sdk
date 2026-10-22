package crystil

import "errors"

var (
	// ErrMissingAPIKey indicates no API key was provided.
	ErrMissingAPIKey = errors.New("API key required")

	// ErrInvalidAttribution indicates attribution validation failed. Every error
	// SetAttribution returns matches it with errors.Is.
	ErrInvalidAttribution = errors.New("crystil: invalid attribution")
)
