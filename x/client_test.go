package x

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T) *Client {
	t.Helper()
	return NewClient(Config{
		Timeout: 10 * time.Second,
		Retries: 3,
		NoCache: true,
		Paths:   Paths{Cache: t.TempDir()},
	})
}

// X answers an exhausted window with a reset that can be hours away. Sleeping
// on that number is what made `x user <handle>` sit there printing nothing
// until the whole command timed out, so the client has to give up and say so.
func TestRateLimitDoesNotSleepUntilTheReset(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		reset := time.Now().Add(2 * time.Hour).Unix()
		w.Header().Set("x-rate-limit-limit", "30")
		w.Header().Set("x-rate-limit-remaining", "0")
		w.Header().Set("x-rate-limit-reset", strconv.FormatInt(reset, 10))
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	start := time.Now()
	_, err := testClient(t).Do(context.Background(), Req{URL: srv.URL, Endpoint: "probe"})
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("the client waited %v on a two-hour reset", took)
	}
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want a RateLimitedError so the CLI exits 5", err)
	}
	if hits != 1 {
		t.Errorf("hit the server %d times, want 1: a spent window is not worth retrying", hits)
	}
}

// A short reset is still worth waiting for, and the retry has to actually
// happen, otherwise the cap turns every blip into a failure.
func TestShortRateLimitIsRetried(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	b, err := testClient(t).Do(context.Background(), Req{URL: srv.URL, Endpoint: "probe"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(b) != "ok" {
		t.Errorf("body = %q", b)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
}

// The pre-emptive cooldown has the same ceiling: knowing the window is spent
// should shorten the answer, not stretch it.
func TestPreemptiveCooldownGivesUpInsteadOfWaiting(t *testing.T) {
	c := testClient(t)
	c.limits["probe"] = rateLimit{remaining: 0, reset: time.Now().Add(time.Hour), limit: 30}

	start := time.Now()
	err := c.throttle(context.Background(), "probe")
	if took := time.Since(start); took > time.Second {
		t.Fatalf("throttle waited %v", took)
	}
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want a RateLimitedError", err)
	}
	if !strings.Contains(err.Error(), "probe") {
		t.Errorf("the message does not name the endpoint: %v", err)
	}
}

// The global gate is the user's own --rate, so it is honored however long it
// is. It also has to survive the first call: a zero nextOK used to push the
// next slot decades into the past and the delay was never applied again.
func TestGlobalRateGateSurvivesTheFirstCall(t *testing.T) {
	c := NewClient(Config{Timeout: time.Second, NoCache: true, Paths: Paths{Cache: t.TempDir()}, Rate: 40 * time.Millisecond})
	ctx := context.Background()
	if err := c.throttle(ctx, "probe"); err != nil {
		t.Fatalf("first throttle: %v", err)
	}
	start := time.Now()
	if err := c.throttle(ctx, "probe"); err != nil {
		t.Fatalf("second throttle: %v", err)
	}
	if took := time.Since(start); took < 20*time.Millisecond {
		t.Errorf("the second call waited %v, want about the configured 40ms", took)
	}
}
