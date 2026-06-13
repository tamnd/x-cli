package x

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

// x-client-transaction-id (the "TID") is an anti-automation header the x.com
// web client computes per request and X increasingly requires on GraphQL calls.
// Without it, otherwise-valid guest reads (search, followers) come back 404.
//
// The scheme is public and keyless: the browser ships an "animation key" /
// "verification" pair, mixes it with the request path and a coarse timestamp,
// SHA-256s the result, and base64s an obfuscated byte string. We source the
// pair dictionary from the community-maintained fa0311 dict, exactly as the
// reference web clients do, and regenerate a fresh TID per request.
const (
	tidKeyword     = "obfiowerehiring"
	tidPairsURL    = "https://raw.githubusercontent.com/fa0311/x-client-transaction-id-pair-dict/refs/heads/main/pair.json"
	tidEpochOffset = 1682924400 // X's own epoch, 2023-05-01T07:00:00Z
	tidTTL         = time.Hour
)

// tidPair is one (animationKey, verification) entry from the dictionary.
type tidPair struct {
	AnimationKey string `json:"animationKey"`
	Verification string `json:"verification"`
}

// tidProvider caches the fetched pair dictionary for an hour and falls back to
// the last good set if a refresh fails (the header is best-effort).
type tidProvider struct {
	mu      sync.Mutex
	pairs   []tidPair
	fetched time.Time
}

var tidShared = &tidProvider{}

// load returns the cached pairs, refreshing through the HTTP client (which adds
// its own caching) when the window has elapsed.
func (p *tidProvider) load(ctx context.Context, c *Client) ([]tidPair, error) {
	p.mu.Lock()
	if len(p.pairs) > 0 && time.Since(p.fetched) < tidTTL {
		ps := p.pairs
		p.mu.Unlock()
		return ps, nil
	}
	p.mu.Unlock()

	b, err := c.Do(ctx, Req{URL: tidPairsURL, Endpoint: "tid.pairs", CacheTTL: tidTTL})
	if err != nil {
		if ps := p.cached(); len(ps) > 0 {
			return ps, nil
		}
		return nil, err
	}
	var pairs []tidPair
	if err := json.Unmarshal(b, &pairs); err != nil || len(pairs) == 0 {
		if ps := p.cached(); len(ps) > 0 {
			return ps, nil
		}
		return nil, fmt.Errorf("tid: empty pair dictionary")
	}
	p.mu.Lock()
	p.pairs = pairs
	p.fetched = time.Now()
	p.mu.Unlock()
	return pairs, nil
}

func (p *tidProvider) cached() []tidPair {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pairs
}

// transactionID generates a header value for one request, or "" if generation
// is not possible (no dictionary, network down). A missing header is never
// fatal: many endpoints still answer without it.
func (g *GraphQL) transactionID(ctx context.Context, method, path string) string {
	pairs, err := g.s.transactionPairs(ctx, g.c)
	if err != nil || len(pairs) == 0 {
		return ""
	}
	pair := pairs[rand.Intn(len(pairs))]
	tid, err := generateTID(method, path, pair)
	if err != nil {
		return ""
	}
	return tid
}

// transactionPairs exposes the cached dictionary through the session so the
// guest/user header builders share one fetch.
func (s *Session) transactionPairs(ctx context.Context, c *Client) ([]tidPair, error) {
	return tidShared.load(ctx, c)
}

// generateTID computes the header value for "<METHOD> <path>" with one pair.
func generateTID(method, path string, pair tidPair) (string, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(pad64(pair.Verification))
	if err != nil {
		return "", err
	}
	timeNow := time.Now().Unix() - tidEpochOffset
	timeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(timeBytes, uint32(timeNow))

	data := method + "!" + path + "!" + strconv.FormatInt(timeNow, 10) + tidKeyword + pair.AnimationKey
	hash := sha256.Sum256([]byte(data))

	buf := make([]byte, 0, len(keyBytes)+4+16+1)
	buf = append(buf, keyBytes...)
	buf = append(buf, timeBytes...)
	buf = append(buf, hash[:16]...)
	buf = append(buf, 0x03)

	r := byte(rand.Intn(256))
	out := make([]byte, len(buf)+1)
	out[0] = r
	for i, b := range buf {
		out[i+1] = b ^ r
	}
	return strings.TrimRight(base64.StdEncoding.EncodeToString(out), "="), nil
}

// pad64 restores the padding base64 strings in the dictionary drop.
func pad64(s string) string {
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	return s
}
