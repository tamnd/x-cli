package x

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// errors.go is the typed error taxonomy the CLI turns into stable exit codes
// (spec 3003 doc 03 section 11).
//
//	2 usage        bad flags, bad argument shape
//	3 no results   the query was valid and matched nothing
//	4 need auth    the tier wall, and need_tier says which tier clears it
//	5 rate limited a bucket is empty, with the reset time
//	6 not found    the id does not exist
//	7 unsupported  no surface serves this at any tier
//	8 network      DNS, TLS, timeout, connection reset
//
// The distinction that makes the whole thing worth having is 4 against 7.
// 4 means get a session and this works. 7 means nothing serves this anywhere,
// so do not go looking for an operation id that would fix it.

// NeedAuthError marks a capability that sits behind a tier the current run does
// not have (exit code 4). Tier is the tier that would answer it.
type NeedAuthError struct {
	Msg  string
	Tier int  // 1 (a guest token) or 2 (the user's own session)
	User bool // true when only a user-context session will do
}

func (e *NeedAuthError) Error() string { return e.Msg }

// NeedTier builds the tier-wall error, naming what was asked for, which tier
// serves it, and the command that gets there.
//
// This replaces the message the old tool gave for the same condition, "Object
// not found: (deleted, suspended, or protected)", which is wrong on all three
// guesses and sends the reader hunting for an operation id that does not exist.
// Doc 01 section 4.2 has the measurement: the same operation works with a
// cookie, and six vintages of id all fail without one.
func NeedTier(what string, tier int) error {
	e := &NeedAuthError{Tier: tier, User: tier >= 2}
	if tier == 1 {
		e.Msg = what + " needs tier 1, a guest token. run it again with --guest"
	} else {
		e.Msg = what + " needs tier 2, a session cookie. no anonymous surface serves it. " +
			"run: x auth import"
	}
	return e
}

// ErrNeedAuth builds a need-auth error that a guest token unlocks.
func ErrNeedAuth(msg string) error { return &NeedAuthError{Msg: msg, Tier: 1} }

// ErrNeedUser builds a need-auth error that only a session unlocks.
func ErrNeedUser(msg string) error { return &NeedAuthError{Msg: msg, Tier: 2, User: true} }

// asNotFound turns a 404 into the not-found error, and returns nil for anything
// else. Every surface that addresses a record by id needs this: the tombstone
// check further down only catches the case where X answers 200 with {}, and for
// an id that never existed at all X answers 404 with a page of HTML. Without
// this the caller gets an unclassified error carrying that HTML as its reason.
func asNotFound(err error, kind, ref string) error {
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusNotFound {
		return &NotFoundError{Kind: kind, Ref: ref}
	}
	return nil
}

// RateLimitedError marks an exhausted upstream after retries (exit code 5).
type RateLimitedError struct {
	Msg      string
	Endpoint string
}

func (e *RateLimitedError) Error() string { return e.Msg }

// PartialError marks a walk that delivered rows and then stopped on something
// other than the end of the data.
//
// A cursor walk can run for hundreds of records and hit the window halfway
// through. The rows it already streamed are real and are not withdrawn, but the
// walk did not finish, and a caller who asked for a thousand tweets and got
// seven hundred has to be able to tell that from an account with seven hundred
// tweets. It wraps its cause, so the exit code is whatever stopped it: 5 for a
// spent window, 8 for a dropped connection.
type PartialError struct {
	Got   int    // records delivered before the stop
	Kind  string // what they were, plural: "tweets", "accounts"
	Cause error
}

func (e *PartialError) Error() string {
	return fmt.Sprintf("stopped after %d %s: %s", e.Got, e.Kind, e.Cause.Error())
}

func (e *PartialError) Unwrap() error { return e.Cause }

// partial wraps a mid-walk failure, and returns nil when nothing was delivered
// so the caller returns the plain error instead: an empty result that failed is
// just a failure, and "stopped after 0 tweets" says less than the cause does.
func partial(n int, kind string, cause error) error {
	if n == 0 || cause == nil {
		return cause
	}
	return &PartialError{Got: n, Kind: kind, Cause: cause}
}

// UnsupportedError marks something no surface serves at any tier (exit code 7).
// It is deliberately not a NeedAuthError, because there is nothing to go and get.
type UnsupportedError struct{ Msg string }

func (e *UnsupportedError) Error() string { return e.Msg }

// Unsupported builds the exit-code-7 error. why is the reason, and it should
// say what X does rather than what x-cli does not.
func Unsupported(what, why string) error {
	msg := "no surface serves " + what + " at any tier"
	if why != "" {
		msg += ": " + why
	}
	return &UnsupportedError{Msg: msg}
}

// NetworkError marks a transport failure (exit code 8): DNS, TLS, a timeout, a
// reset connection. It is separate from the rest because the answer is
// different. Nothing about the request was wrong, so trying again is reasonable.
type NetworkError struct {
	Msg string
	Err error
}

func (e *NetworkError) Error() string { return e.Msg }
func (e *NetworkError) Unwrap() error { return e.Err }

// AsNetwork returns a *NetworkError when err is a transport failure and nil when
// it is anything else. A context cancellation is the caller giving up rather
// than the network failing, so it is left alone.
func AsNetwork(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return nil
	}
	var he *HTTPError
	if errors.As(err, &he) {
		// The server answered, so whatever went wrong is not the transport.
		return nil
	}
	var de *net.DNSError
	var ne net.Error
	var ue *url.Error
	switch {
	case errors.As(err, &de):
		return &NetworkError{Msg: "cannot resolve " + de.Name + ": " + de.Err, Err: err}
	case errors.Is(err, context.DeadlineExceeded):
		return &NetworkError{Msg: "the request timed out: raise --timeout or try again", Err: err}
	case errors.As(err, &ne) && ne.Timeout():
		return &NetworkError{Msg: "the request timed out: raise --timeout or try again", Err: err}
	case errors.As(err, &ue):
		return &NetworkError{Msg: "cannot reach " + hostOf(ue.URL) + ": " + ue.Err.Error(), Err: err}
	}
	return nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// Failure is an error as a record (spec 3003 doc 03 section 11). A caller
// streaming JSONL needs to see what did not work without parsing stderr, and
// need_tier is the field the whole thing exists for.
type Failure struct {
	Kind     string `json:"kind"` // always "error"
	Target   string `json:"target"`
	Code     int    `json:"code"`
	Reason   string `json:"reason"`
	Tier     int    `json:"tier"`
	NeedTier *int   `json:"need_tier,omitempty"`
	Surface  string `json:"surface,omitempty"`
}

// FailureOf renders an error as a Failure record. tier is the tier that was
// tried, which is not the same as the tier that would have worked.
func FailureOf(err error, target string, tier int) Failure {
	f := Failure{Kind: "error", Target: target, Code: 1, Reason: err.Error(), Tier: tier}
	var na *NeedAuthError
	var rl *RateLimitedError
	var nf *NotFoundError
	var un *UnsupportedError
	var ne *NetworkError
	switch {
	case errors.As(err, &na):
		f.Code = 4
		if na.Tier > 0 {
			need := na.Tier
			f.NeedTier = &need
		}
	case errors.As(err, &rl):
		f.Code, f.Surface = 5, rl.Endpoint
	case errors.As(err, &nf):
		f.Code = 6
	case errors.As(err, &un):
		f.Code = 7
	case errors.As(err, &ne):
		f.Code = 8
	}
	return f
}
