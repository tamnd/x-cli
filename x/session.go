package x

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// publicWebBearer is the public, non-secret bearer token every x.com web client
// sends. It is not a developer credential and it is not anyone's account token;
// it is the same constant the browser ships, used only to mint a guest token
// (spec §13.1). It is hard-coded here exactly as nitter hard-codes it.
const publicWebBearer = "AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"

// browserUA is the desktop-Chrome identifier the GraphQL tiers send so the
// request matches the x-client-transaction-id header X expects from its client.
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"

// Creds are the persisted credential fields written by `x auth import`. A zero
// Creds means guest-only (Tier 1); one carrying the user's cookies is Tier 2.
type Creds struct {
	AuthToken string `json:"auth_token,omitempty"`
	CT0       string `json:"ct0,omitempty"`
	Handle    string `json:"handle,omitempty"`
}

// Session is the runtime authentication state shared by the GraphQL tiers. It
// carries the credentials (if any) plus the cached guest token.
type Session struct {
	AuthToken string
	CT0       string
	Handle    string

	mu         sync.Mutex
	guestToken string
	guestAt    time.Time
}

// NewSession builds a runtime session from a config's credentials.
func NewSession(cfg Config) *Session {
	return &Session{AuthToken: cfg.AuthToken, CT0: cfg.CT0}
}

// IsUser reports whether this session carries the user's own cookies (Tier 2).
func (s *Session) IsUser() bool { return s.AuthToken != "" && s.CT0 != "" }

// guestTTL is how long a minted guest token is reused before refreshing.
const guestTTL = 2 * time.Hour

// ensureGuest mints (or reuses) a guest token via the public web bearer. No
// account and no cost are involved (spec §13.1).
func (s *Session) ensureGuest(ctx context.Context, c *Client) (string, error) {
	s.mu.Lock()
	if s.guestToken != "" && time.Since(s.guestAt) < guestTTL {
		t := s.guestToken
		s.mu.Unlock()
		return t, nil
	}
	s.mu.Unlock()

	// Reuse a token a previous run cached on disk. Minting is what X rate-limits
	// per IP, so a single invocation storm must not re-mint on every call.
	if tok, at, ok := loadGuestToken(); ok && time.Since(at) < guestTTL {
		s.mu.Lock()
		s.guestToken, s.guestAt = tok, at
		s.mu.Unlock()
		return tok, nil
	}

	h := http.Header{}
	h.Set("Authorization", "Bearer "+bearerForHeader())
	b, err := c.Do(ctx, Req{
		Method:   http.MethodPost,
		URL:      "https://api.x.com/1.1/guest/activate.json",
		Endpoint: "guest.activate",
		Header:   h,
	})
	if err != nil {
		return "", fmt.Errorf("activate guest token: %w", err)
	}
	var out struct {
		GuestToken string `json:"guest_token"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.GuestToken == "" {
		return "", fmt.Errorf("activate guest token: empty response")
	}
	now := time.Now()
	s.mu.Lock()
	s.guestToken = out.GuestToken
	s.guestAt = now
	s.mu.Unlock()
	saveGuestToken(out.GuestToken, now)
	return out.GuestToken, nil
}

// guestRecord is the on-disk shape of the cached guest token.
type guestRecord struct {
	Token    string    `json:"guest_token"`
	MintedAt time.Time `json:"minted_at"`
}

func loadGuestToken() (string, time.Time, bool) {
	b, err := os.ReadFile(GuestStorePath())
	if err != nil {
		return "", time.Time{}, false
	}
	var r guestRecord
	if err := json.Unmarshal(b, &r); err != nil || r.Token == "" {
		return "", time.Time{}, false
	}
	return r.Token, r.MintedAt, true
}

func saveGuestToken(tok string, at time.Time) {
	p := GuestStorePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	b, err := json.Marshal(guestRecord{Token: tok, MintedAt: at})
	if err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o600)
}

// bearerForHeader returns the public web bearer with its trailing URL-encoded
// padding decoded for use in an Authorization header.
func bearerForHeader() string {
	return strings.ReplaceAll(publicWebBearer, "%3D", "=")
}

// authHeaders builds the header set the web client sends, in guest mode or with
// the user's own cookies (spec §13.1). The browser-faithful set matters because
// X rejects requests that do not look like its own client.
func (s *Session) authHeaders(ctx context.Context, c *Client) (http.Header, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+bearerForHeader())
	h.Set("x-twitter-active-user", "yes")
	h.Set("x-twitter-client-language", "en")
	h.Set("Accept", "*/*")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Referer", "https://x.com/")
	h.Set("Origin", "https://x.com")
	// Browser-faithful identity: the GraphQL paths attach an x-client-transaction-id
	// computed against this client, so the User-Agent and client hints must agree.
	h.Set("User-Agent", browserUA)
	h.Set("sec-ch-ua", `"Chromium";v="142", "Not_A Brand";v="24", "Google Chrome";v="142"`)
	h.Set("sec-ch-ua-mobile", "?0")
	h.Set("sec-ch-ua-platform", `"Windows"`)
	h.Set("priority", "u=1, i")
	if s.IsUser() {
		h.Set("Cookie", "auth_token="+s.AuthToken+"; ct0="+s.CT0)
		h.Set("x-csrf-token", s.CT0)
		h.Set("x-twitter-auth-type", "OAuth2Session")
		return h, nil
	}
	tok, err := s.ensureGuest(ctx, c)
	if err != nil {
		return nil, err
	}
	h.Set("x-guest-token", tok)
	return h, nil
}

// LoadSession reads the persisted credentials written by `x auth import`.
func LoadSession() (Creds, bool) {
	b, err := os.ReadFile(SessionStorePath())
	if err != nil {
		return Creds{}, false
	}
	var s Creds
	if err := json.Unmarshal(b, &s); err != nil || s.AuthToken == "" {
		return Creds{}, false
	}
	return s, true
}

// SaveSession persists the user's imported credentials (file fallback; 0600).
func SaveSession(s Creds) error {
	p := SessionStorePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// ForgetSession removes the persisted session.
func ForgetSession() error {
	err := os.Remove(SessionStorePath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
