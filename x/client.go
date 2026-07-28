package x

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// UserAgent is the truthful identifier x sends on every request.
const UserAgent = "x-cli/" + "dev" + " (+https://github.com/tamnd/x-cli)"

// Client is the shared HTTP client for every tier: one rate limiter, retry
// policy, disk cache, and per-endpoint rate-limit accounting (the nitter
// lesson, spec §13.2, minus the account pool).
type Client struct {
	cfg   Config
	hc    *http.Client
	cache *Cache

	mu     sync.Mutex
	nextOK time.Time            // global rate-limit gate
	limits map[string]rateLimit // endpoint -> last seen rate-limit window
}

type rateLimit struct {
	remaining int
	reset     time.Time
	limit     int
}

// NewClient builds a Client from a config.
func NewClient(cfg Config) *Client {
	return &Client{
		cfg:    cfg,
		hc:     &http.Client{Timeout: cfg.Timeout},
		cache:  NewCache(cfg.CacheDir, !cfg.NoCache),
		limits: map[string]rateLimit{},
	}
}

// Cache exposes the underlying disk cache (for `x cache`).
func (c *Client) Cache() *Cache { return c.cache }

// Config returns the client's resolved config.
func (c *Client) Config() Config { return c.cfg }

// maxWait caps every wait this client will ever sit through, whether that is a
// retry backoff or a pre-emptive cooldown.
//
// X answers an exhausted window with a reset that can be hours out. Sleeping on
// that number turns the tool into a hang with no output, which reads as a bug
// even though the client is doing what it was told. Past this point the honest
// answer is "you are rate limited until HH:MM", printed now, exit code 5.
const maxWait = 30 * time.Second

// throttle blocks until the global minimum inter-request delay has elapsed and
// the named endpoint is not in a known rate-limit cooldown.
func (c *Client) throttle(ctx context.Context, endpoint string) error {
	c.mu.Lock()
	// The global gate is whatever the user asked for with --rate, so it is
	// honored however long it is. Clamping to zero matters: without it a
	// zero-value nextOK pushes the next slot decades into the past and the
	// configured delay is never applied again.
	wait := time.Until(c.nextOK)
	if wait < 0 {
		wait = 0
	}
	// Pre-emptive per-endpoint backoff: if we know we are nearly out and the
	// window has not reset, wait for the reset instead of provoking a 429.
	if rl, ok := c.limits[endpoint]; ok && rl.remaining <= 2 && time.Now().Before(rl.reset) {
		cool := time.Until(rl.reset)
		if cool > maxWait {
			c.mu.Unlock()
			return rateLimited(endpoint, rl.reset)
		}
		if cool > wait {
			wait = cool
		}
	}
	c.nextOK = time.Now().Add(wait).Add(c.cfg.Rate)
	c.mu.Unlock()
	if wait <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) noteRateLimit(endpoint string, h http.Header) {
	rem := h.Get("x-rate-limit-remaining")
	if rem == "" {
		return
	}
	rl := rateLimit{}
	rl.remaining, _ = strconv.Atoi(rem)
	if s := h.Get("x-rate-limit-reset"); s != "" {
		if u, err := strconv.ParseInt(s, 10, 64); err == nil {
			rl.reset = time.Unix(u, 0)
		}
	}
	rl.limit, _ = strconv.Atoi(h.Get("x-rate-limit-limit"))
	c.mu.Lock()
	c.limits[endpoint] = rl
	c.mu.Unlock()
}

// Req is one HTTP request to make through the shared policy.
type Req struct {
	Method   string
	URL      string
	Endpoint string // a stable label for rate-limit accounting and cache keys
	Header   http.Header
	Body     []byte
	CacheTTL time.Duration // 0 = do not cache
}

// Do executes a request through the rate limiter, retry policy, and cache,
// returning the (decompressed) body. Non-2xx returns an *HTTPError.
func (c *Client) Do(ctx context.Context, r Req) ([]byte, error) {
	key := r.Method + " " + r.URL
	if r.Method == "" || r.Method == http.MethodGet {
		if b, ok := c.cache.Get(key, r.CacheTTL); ok {
			return b, nil
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if err := c.throttle(ctx, r.Endpoint); err != nil {
			return nil, err
		}
		b, retry, err := c.do1(ctx, r)
		if err == nil {
			if r.Method == "" || r.Method == http.MethodGet {
				c.cache.Put(key, b)
			}
			return b, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
		// Exponential backoff honoring the rate-limit reset where known.
		back := time.Duration(1<<attempt) * 500 * time.Millisecond
		if he, ok := err.(*HTTPError); ok && he.RetryAfter > 0 {
			back = he.RetryAfter
		}
		if back > maxWait {
			return nil, c.limitErr(r.Endpoint, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(back):
		}
	}
	if he, ok := lastErr.(*HTTPError); ok && he.Status == 429 {
		return nil, c.limitErr(r.Endpoint, lastErr)
	}
	// A transport failure gets its own exit code, because nothing about the
	// request was wrong and the answer is to try again rather than to change it.
	if ne := AsNetwork(lastErr); ne != nil {
		return nil, ne
	}
	return nil, lastErr
}

// limitErr turns an exhausted window into the typed error the CLI maps to exit
// code 5, naming the endpoint and when it comes back.
func (c *Client) limitErr(endpoint string, err error) error {
	c.mu.Lock()
	reset := c.limits[endpoint].reset
	c.mu.Unlock()
	if he, ok := err.(*HTTPError); ok && he.Status != 429 {
		return err
	}
	return rateLimited(endpoint, reset)
}

func rateLimited(endpoint string, reset time.Time) error {
	msg := "rate limited by X on " + endpoint
	if !reset.IsZero() && time.Now().Before(reset) {
		msg += "; the window resets at " + reset.Local().Format("15:04:05") +
			" (in " + time.Until(reset).Truncate(time.Second).String() + ")"
	}
	return &RateLimitedError{
		Msg:      msg + "; slow down with --rate, or try again then",
		Endpoint: endpoint,
	}
}

func (c *Client) do1(ctx context.Context, r Req) ([]byte, bool, error) {
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if r.Body != nil {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.URL, body)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept-Encoding", "gzip")
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	// A caller may pass a browser-faithful User-Agent (the GraphQL tiers do, so
	// the request matches the transaction-id header X expects); otherwise send
	// our own truthful identifier.
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, true, err // network error: retry
	}
	defer func() { _ = resp.Body.Close() }()
	c.noteRateLimit(r.Endpoint, resp.Header)

	reader := resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, gerr := gzip.NewReader(resp.Body)
		if gerr == nil {
			defer func() { _ = gz.Close() }()
			reader = gz
		}
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, true, err
	}

	if resp.StatusCode/100 == 2 {
		return data, false, nil
	}
	he := &HTTPError{Status: resp.StatusCode, Body: string(data), URL: r.URL}
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		if s, e := strconv.Atoi(ra); e == nil {
			he.RetryAfter = time.Duration(s) * time.Second
		}
	}
	if resp.StatusCode == 429 && he.RetryAfter == 0 {
		if reset := resp.Header.Get("x-rate-limit-reset"); reset != "" {
			if u, e := strconv.ParseInt(reset, 10, 64); e == nil {
				he.RetryAfter = time.Until(time.Unix(u, 0))
			}
		}
	}
	retry := resp.StatusCode == 429 || resp.StatusCode >= 500
	return nil, retry, he
}

// HTTPError carries a non-2xx upstream response.
type HTTPError struct {
	Status     int
	Body       string
	URL        string
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d from %s: %s", e.Status, e.URL, truncate(e.Body, 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
