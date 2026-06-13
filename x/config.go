package x

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the resolved runtime configuration for a command. It is built from
// defaults, then the config file, then the environment, then flags. There are
// no developer-API credentials here by design (spec §0.2): the only secrets x
// holds are the user's own browser session cookies (Tier 2).
type Config struct {
	// Session and access.
	AuthToken  string // the user's own auth_token cookie (Tier 2)
	CT0        string // the user's own ct0 cookie / CSRF token (Tier 2)
	AllowGuest bool   // enable the opt-in free guest-GraphQL tier (Tier 1)
	Tier       string // forced tier: ""|syndication|guest|session

	// Behavior.
	Rate     time.Duration
	Retries  int
	Timeout  time.Duration
	NoCache  bool
	DataDir  string
	CacheDir string
	Store    string // path to the SQLite store (--db)

	// GraphQL overrides (durability against query-id rotation; see spec §13).
	QueryIDs map[string]string // OperationName -> queryId
	Features string            // override features JSON
}

// DefaultConfig returns the built-in defaults before any file/env/flag overlay.
func DefaultConfig() Config {
	dir := dataDir()
	return Config{
		Rate:     time.Second,
		Retries:  3,
		Timeout:  30 * time.Second,
		DataDir:  dir,
		CacheDir: filepath.Join(dir, "cache"),
		QueryIDs: map[string]string{},
	}
}

// FromEnv overlays environment variables onto a config.
func (c *Config) FromEnv() {
	if v := os.Getenv("X_AUTH_TOKEN"); v != "" {
		c.AuthToken = v
	}
	if v := os.Getenv("X_CT0"); v != "" {
		c.CT0 = v
	}
	if v := os.Getenv("X_ALLOW_GUEST"); v == "1" || strings.EqualFold(v, "true") {
		c.AllowGuest = true
	}
	if v := os.Getenv("X_DATA_DIR"); v != "" {
		c.DataDir = v
		c.CacheDir = filepath.Join(v, "cache")
	}
	if v := os.Getenv("X_NO_CACHE"); v == "1" || strings.EqualFold(v, "true") {
		c.NoCache = true
	}
	// A session may also be loaded from the credentials file written by
	// `x auth import`; the env vars take precedence when both are present.
	if c.AuthToken == "" || c.CT0 == "" {
		if s, ok := LoadSession(); ok {
			if c.AuthToken == "" {
				c.AuthToken = s.AuthToken
			}
			if c.CT0 == "" {
				c.CT0 = s.CT0
			}
		}
	}
}

// HasSession reports whether the user's own session cookies are available (Tier 2).
func (c Config) HasSession() bool { return c.AuthToken != "" && c.CT0 != "" }

// dataDir returns the per-user data root (XDG-aware, with a sane fallback).
func dataDir() string {
	if v := os.Getenv("X_DATA_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "x")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "x")
	}
	return filepath.Join(home, ".local", "share", "x")
}

// ConfigPath returns the path to the TOML config file.
func ConfigPath() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "x", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "x", "config.toml")
	}
	return filepath.Join(home, ".config", "x", "config.toml")
}

// SessionStorePath returns where the imported session is persisted (a file
// fallback for platforms without a keychain integration).
func SessionStorePath() string {
	return filepath.Join(dataDir(), "session.json")
}

// GuestStorePath returns where a minted guest token is cached between runs.
// Reusing it keeps repeated invocations from re-minting (and tripping X's
// per-IP guest-activation rate limit).
func GuestStorePath() string {
	return filepath.Join(dataDir(), "guest.json")
}
