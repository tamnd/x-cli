package x

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The defect from doc 06 section 4, as a test. A 404 with an empty body is the
// tier wall and has to say so; a 404 with a JSON error body is a missing object.
// Reporting the first as the second is what sent readers hunting for operation
// ids that would not have helped.
func TestEmptyBody404IsTheTierWallNotAMissingObject(t *testing.T) {
	wall := gqlError("TweetDetail", &HTTPError{Status: 404, Body: ""})
	var na *NeedAuthError
	if !errors.As(wall, &na) {
		t.Fatalf("an empty-body 404 gave %T (%v), want the tier wall", wall, wall)
	}
	if na.Tier != 2 {
		t.Errorf("need_tier = %d, want 2", na.Tier)
	}
	if f := FailureOf(wall, "20", 1); f.Code != 4 || f.NeedTier == nil || *f.NeedTier != 2 {
		t.Errorf("failure = %+v, want code 4 with need_tier 2", f)
	}

	missing := gqlError("TweetResultByRestId", &HTTPError{
		Status: 404,
		Body:   `{"errors":[{"message":"_Missing: No status found with that ID."}]}`,
	})
	var nf *NotFoundError
	if !errors.As(missing, &nf) {
		t.Fatalf("a 404 with an error body gave %T (%v), want not-found", missing, missing)
	}
	if f := FailureOf(missing, "999", 1); f.Code != 6 {
		t.Errorf("failure code = %d, want 6", f.Code)
	}
}

// Whitespace is not a body. A 404 whose body is a newline is still the wall.
func TestBlank404BodyIsStillTheWall(t *testing.T) {
	var na *NeedAuthError
	if err := gqlError("UserTweets", &HTTPError{Status: 404, Body: "\n  \n"}); !errors.As(err, &na) {
		t.Errorf("got %T, want the tier wall", err)
	}
}

func TestNeedTierNamesTheWayOut(t *testing.T) {
	one := NeedTier("trends", 1).Error()
	if !strings.Contains(one, "--guest") {
		t.Errorf("tier 1 message does not say how to get there: %q", one)
	}
	two := NeedTier("search", 2).Error()
	if !strings.Contains(two, "x auth import") {
		t.Errorf("tier 2 message does not say how to get there: %q", two)
	}
	// The old message guessed at three causes and was wrong on all three.
	for _, bad := range []string{"deleted", "suspended", "protected"} {
		if strings.Contains(two, bad) {
			t.Errorf("the tier wall message still guesses at %q: %q", bad, two)
		}
	}
}

// 4 and 7 are different, and the difference is the whole point.
func TestUnsupportedIsNotNeedAuth(t *testing.T) {
	err := Unsupported("direct messages", "they are not public at any tier")
	var na *NeedAuthError
	if errors.As(err, &na) {
		t.Fatal("an unsupported capability must not read as a tier wall")
	}
	if f := FailureOf(err, "dm", 2); f.Code != 7 || f.NeedTier != nil {
		t.Errorf("failure = %+v, want code 7 and no need_tier", f)
	}
}

func TestAsNetworkClassifiesTransportFailures(t *testing.T) {
	for name, err := range map[string]error{
		"dns":     &net.DNSError{Name: "x.com", Err: "no such host"},
		"timeout": &url.Error{Op: "Get", URL: "https://x.com/", Err: context.DeadlineExceeded},
		"reset":   &url.Error{Op: "Get", URL: "https://x.com/", Err: errors.New("connection reset by peer")},
	} {
		if ne := AsNetwork(err); ne == nil {
			t.Errorf("%s: AsNetwork returned nil, want a network error", name)
		} else if f := FailureOf(ne, "x.com", 0); f.Code != 8 {
			t.Errorf("%s: failure code = %d, want 8", name, f.Code)
		}
	}
}

// A server that answered is not a transport failure, whatever it answered with,
// and a cancelled context is the caller giving up rather than the network.
func TestAsNetworkLeavesTheRestAlone(t *testing.T) {
	for name, err := range map[string]error{
		"http":      &HTTPError{Status: 500, Body: "boom", URL: "https://x.com/"},
		"cancelled": context.Canceled,
		"plain":     errors.New("something else"),
	} {
		if ne := AsNetwork(err); ne != nil {
			t.Errorf("%s: AsNetwork returned %v, want nil", name, ne)
		}
	}
}

// The rate-limit error names the endpoint, so a JSONL consumer can see which
// bucket is empty without reading the prose.
func TestRateLimitFailureCarriesTheSurface(t *testing.T) {
	f := FailureOf(rateLimited("syndication.tweet", time.Time{}), "20", 0)
	if f.Code != 5 {
		t.Errorf("code = %d, want 5", f.Code)
	}
	if f.Surface != "syndication.tweet" {
		t.Errorf("surface = %q, want the endpoint", f.Surface)
	}
}

// A 404 is the record not existing, not an unclassified failure. Before
// asNotFound existed, `x tweet <bad id>` came back with exit code 1 and a page
// of X's HTML as the reason, because the tombstone check only catches the case
// where X answers 200 with {}.
func TestAsNotFoundClassifies404(t *testing.T) {
	body := "<!DOCTYPE html><html lang=\"en\" class=\"dog\">"
	got := asNotFound(&HTTPError{Status: 404, Body: body, URL: "https://cdn.syndication.twimg.com/tweet-result"}, "tweet", "999")
	if got == nil {
		t.Fatal("a 404 should be a not-found error")
	}
	var nf *NotFoundError
	if !errors.As(got, &nf) || nf.Kind != "tweet" || nf.Ref != "999" {
		t.Fatalf("got %v, want a tweet/999 not-found", got)
	}
	if f := FailureOf(got, "999", 0); f.Code != 6 {
		t.Errorf("failure code = %d, want 6", f.Code)
	}
	if strings.Contains(got.Error(), "DOCTYPE") {
		t.Error("the reason should describe the tweet, not hand back X's error page")
	}
	// Everything else stays alone, so a 403 does not get to claim the id is
	// missing when the real answer is that the account is protected.
	for _, err := range []error{
		&HTTPError{Status: 403, URL: "https://x.com"},
		&HTTPError{Status: 500, URL: "https://x.com"},
		&NetworkError{Msg: "cannot resolve x.com"},
		errors.New("something else"),
	} {
		if got := asNotFound(err, "tweet", "999"); got != nil {
			t.Errorf("asNotFound(%v) = %v, want nil", err, got)
		}
	}
}
