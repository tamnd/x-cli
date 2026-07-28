package x

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// A command line tool is a fresh process every time, so a bucket that only
// lives in memory is a bucket nobody ever reads. The second client here is the
// second invocation: it has to know the window is empty without asking.
func TestBucketsSurviveTheProcess(t *testing.T) {
	dir := t.TempDir()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("x-rate-limit-limit", "50")
		w.Header().Set("x-rate-limit-remaining", "0")
		w.Header().Set("x-rate-limit-reset", strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10))
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	cfg := Config{Timeout: 2 * time.Second, NoCache: true, Paths: Paths{Data: dir, Cache: t.TempDir()}}
	first := NewClient(cfg)
	if _, err := first.Do(context.Background(), Req{URL: srv.URL, Endpoint: "graphql.UserTweets"}); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if hits != 1 {
		t.Fatalf("server saw %d requests, want 1", hits)
	}

	second := NewClient(cfg)
	b := second.Buckets()
	if len(b) != 1 || b[0].Endpoint != "graphql.UserTweets" || b[0].Limit != 50 || !b[0].Spent() {
		t.Fatalf("the second run inherited %+v, want one spent UserTweets bucket", b)
	}
	_, err := second.Do(context.Background(), Req{URL: srv.URL, Endpoint: "graphql.UserTweets"})
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("got %v, want a rate-limited error", err)
	}
	if hits != 1 {
		t.Errorf("the second run spent a request on a window it knew was empty (%d hits)", hits)
	}
	if rl.Endpoint != "graphql.UserTweets" {
		t.Errorf("endpoint = %q, want the operation", rl.Endpoint)
	}
}

// Buckets are per operation, not per host. Spending UserTweets says nothing
// about TweetResultByRestId, which has ten times the budget on the same host.
func TestBucketsAreKeyedByOperation(t *testing.T) {
	dir := t.TempDir()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		rem := "0"
		if r.URL.Path == "/cheap" {
			rem = "480"
		}
		w.Header().Set("x-rate-limit-limit", "500")
		w.Header().Set("x-rate-limit-remaining", rem)
		w.Header().Set("x-rate-limit-reset", strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10))
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	cfg := Config{Timeout: 2 * time.Second, NoCache: true, Paths: Paths{Data: dir, Cache: t.TempDir()}}
	c := NewClient(cfg)
	ctx := context.Background()
	if _, err := c.Do(ctx, Req{URL: srv.URL + "/spent", Endpoint: "graphql.UserTweets"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(ctx, Req{URL: srv.URL + "/cheap", Endpoint: "graphql.TweetResultByRestId"}); err != nil {
		t.Fatalf("the second operation was refused on the first one's budget: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("server saw %v, want both operations through", seen)
	}
	buckets := NewClient(cfg).Buckets()
	if len(buckets) != 2 {
		t.Fatalf("kept %d buckets, want one per operation", len(buckets))
	}
	if buckets[0].Endpoint != "graphql.TweetResultByRestId" || buckets[0].Remaining != 480 {
		t.Errorf("first bucket = %+v", buckets[0])
	}
	if !buckets[1].Spent() {
		t.Errorf("second bucket = %+v, want it spent", buckets[1])
	}
}

// A window that has come back is not an empty window. The record is dropped
// rather than reported as zero, because how full it is now is something only the
// next response can say.
func TestAStaleBucketIsForgotten(t *testing.T) {
	p := Paths{Data: t.TempDir()}
	p.saveBuckets(map[string]rateLimit{
		"old": {limit: 50, remaining: 0, reset: time.Now().Add(-time.Minute), seen: time.Now().Add(-time.Hour)},
		"new": {limit: 50, remaining: 3, reset: time.Now().Add(time.Minute), seen: time.Now()},
	})
	got := p.loadBuckets()
	if _, ok := got["old"]; ok {
		t.Error("a window that already reset was carried forward as empty")
	}
	if rl, ok := got["new"]; !ok || rl.remaining != 3 {
		t.Errorf("the live window came back as %+v", rl)
	}
}
