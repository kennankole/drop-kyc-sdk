package verification

import "errors"

var (
	// ErrUnavailable means the provider could not answer: it was
	// unreachable, timed out, rejected the credentials, or replied in a
	// shape the client does not understand. It is the only error the Engine
	// falls back on.
	ErrUnavailable = errors.New("verification: provider unavailable")

	// ErrUnsupported means the provider does not implement the check.
	ErrUnsupported = errors.New("verification: check not supported by provider")

	// ErrInvalidSubject means the subject lacks a field the check needs.
	// Another provider would need it too, so the Engine does not fall back.
	ErrInvalidSubject = errors.New("verification: subject is missing data the check needs")
)
