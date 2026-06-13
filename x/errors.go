package x

// Typed errors that the CLI maps to stable exit codes (spec §6). The library
// returns these; cli/root.go maps them to codes 4/5/6.

// NeedAuthError marks a capability that needs an API token or user session that
// is not present (exit code 4).
type NeedAuthError struct {
	Msg  string
	User bool // true when a user-context session (not just a bearer) is required
}

func (e *NeedAuthError) Error() string { return e.Msg }

// ErrNeedAuth builds a need-auth error (a bearer token unlocks it).
func ErrNeedAuth(msg string) error { return &NeedAuthError{Msg: msg} }

// ErrNeedUser builds a need-auth error that specifically requires a user session.
func ErrNeedUser(msg string) error { return &NeedAuthError{Msg: msg, User: true} }

// RateLimitedError marks an exhausted upstream after retries (exit code 5).
type RateLimitedError struct{ Msg string }

func (e *RateLimitedError) Error() string { return e.Msg }
