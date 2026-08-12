package crystil

import "errors"

var (
	// ErrInvalidClient indicates the provider client is not the expected type.
	ErrInvalidClient = errors.New("invalid client type")

	// ErrAlreadyRegistered indicates the client is already wrapped.
	ErrAlreadyRegistered = errors.New("client already registered")

	// ErrMissingAPIKey indicates no API key was provided.
	ErrMissingAPIKey = errors.New("API key required")

	// ErrInvalidAttribution indicates attribution validation failed.
	ErrInvalidAttribution = errors.New("invalid attribution")
)
