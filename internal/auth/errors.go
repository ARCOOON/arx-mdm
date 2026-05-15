package auth

import "errors"

// Sentinel errors for enrollment and token handling. Callers map these to HTTP status codes.
var (
	ErrEnrollmentTokenInvalid = errors.New("auth: invalid, expired, or already used enrollment token")
	ErrEnrollmentCSRInvalid   = errors.New("auth: invalid certificate signing request")
	ErrEnrollmentSignEmpty    = errors.New("auth: empty certificate sign response")
)
