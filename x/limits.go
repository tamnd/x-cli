package x

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// limits.go keeps the rate-limit buckets across runs.
//
// The bucket key is the operation, not the host (spec 3003 doc 01 section 10):
// UserByScreenName gets 150 per 15 minutes and UserTweets gets 50, on the same
// host, and spending one does not touch the other. The client already keyed them
// that way in memory, which was worth nothing to a command line tool, because
// every invocation is a fresh process that starts out knowing nothing and
// learns the window is empty by walking into a 429.
//
// So the buckets go to disk, under the run's data dir, and the next invocation
// starts from what the last one was told. That is also the only honest answer to
// "how many more of these can I do", which is what `x doctor` prints.

// RateBucket is one rate-limit window as X last reported it. It is not the
// Bucket in types.go, which is a bucket of tweet counts and got there first.
type RateBucket struct {
	Endpoint  string    `json:"endpoint"`
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"reset,omitempty"`
	Seen      time.Time `json:"seen"`
}

// Spent reports whether the window is empty and has not come back yet.
func (b RateBucket) Spent() bool {
	return b.Remaining <= 0 && !b.Reset.IsZero() && time.Now().Before(b.Reset)
}

// Limits is where the buckets are kept between runs.
func (p Paths) Limits() string { return filepath.Join(p.Data, "limits.json") }

// loadBuckets reads what the last run was told. A window whose reset has passed
// is dropped rather than reported as empty: it refilled while we were not
// looking, and how full is something only the next response can say.
func (p Paths) loadBuckets() map[string]rateLimit {
	out := map[string]rateLimit{}
	b, err := os.ReadFile(p.Limits())
	if err != nil {
		return out
	}
	var recs []RateBucket
	if err := json.Unmarshal(b, &recs); err != nil {
		return out
	}
	now := time.Now()
	for _, r := range recs {
		if !r.Reset.IsZero() && now.After(r.Reset) {
			continue
		}
		out[r.Endpoint] = rateLimit{limit: r.Limit, remaining: r.Remaining, reset: r.Reset, seen: r.Seen}
	}
	return out
}

// saveBuckets writes the buckets out, atomically, so a run killed mid-write
// leaves the last good file rather than half of this one. A failure here is not
// worth failing a read over: the worst case is the next run learns the window is
// empty the way it used to, from a 429.
func (p Paths) saveBuckets(m map[string]rateLimit) {
	if p.Data == "" {
		return
	}
	recs := bucketsOf(m)
	b, err := json.Marshal(recs)
	if err != nil {
		return
	}
	if err := os.MkdirAll(p.Data, 0o700); err != nil {
		return
	}
	tmp := p.Limits() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, p.Limits()); err != nil {
		_ = os.Remove(tmp)
	}
}

// bucketsOf turns the in-memory map into the sorted record list, which is both
// the on-disk shape and what a command prints.
func bucketsOf(m map[string]rateLimit) []RateBucket {
	out := make([]RateBucket, 0, len(m))
	for ep, rl := range m {
		out = append(out, RateBucket{
			Endpoint:  ep,
			Limit:     rl.limit,
			Remaining: rl.remaining,
			Reset:     rl.reset,
			Seen:      rl.seen,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Endpoint < out[j].Endpoint })
	return out
}

// Buckets returns every window this client knows about, including the ones it
// inherited from an earlier run.
func (c *Client) Buckets() []RateBucket {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bucketsOf(c.limits)
}
