package x

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Paths is every file the tool owns, resolved once. It exists because of the
// defect in spec 3003 doc 06 section 4: the old tool read the session store from
// the home directory no matter what --data-dir said, so a run the caller
// believed was anonymous quietly used their cookies and reported capabilities
// they did not have. That produced a wrong tier-1 capability table during the
// reconnaissance for the spec, and it was only caught by isolating HOME.
//
// One struct, resolved once, and every store opens through it.
type Paths struct {
	Data   string // the data root: session, guest token, local store
	Cache  string // the HTTP cache
	Config string // the config directory, which does not follow --data-dir
}

// DefaultPaths resolves the XDG locations, with a sane fallback when there is no
// home directory to speak of.
func DefaultPaths() Paths {
	d := dataDir()
	return Paths{Data: d, Cache: filepath.Join(d, "cache"), Config: configDir()}
}

// At moves the data root, and the cache with it. The config file stays put,
// because --data-dir is about where the run's state lives and not about which
// config the user wrote.
func (p Paths) At(dir string) Paths {
	if dir == "" {
		return p
	}
	return Paths{Data: dir, Cache: filepath.Join(dir, "cache"), Config: p.Config}
}

// WithCache moves the cache on its own, for a caller who wants a shared cache
// under a private data dir.
func (p Paths) WithCache(dir string) Paths {
	if dir == "" {
		return p
	}
	p.Cache = dir
	return p
}

// Session is where `x auth import` writes the user's own cookies.
func (p Paths) Session() string { return filepath.Join(p.Data, "session.json") }

// Guest is where a minted guest token is cached between runs.
func (p Paths) Guest() string { return filepath.Join(p.Data, "guest.json") }

// DB is the typed local store the crawl and export commands use.
func (p Paths) DB() string { return filepath.Join(p.Data, "x.db") }

// File is the TOML config file.
func (p Paths) File() string { return filepath.Join(p.Config, "config.toml") }

// Config is the resolved runtime configuration for a command. It is built from
// defaults, then the environment, then flags, then the session store, in that
// order, by Resolve. There are no developer-API credentials here by design (doc
// 00 section 2): the only secrets x holds are the user's own browser session
// cookies, which the user hands over on purpose.
type Config struct {
	// Session and access.
	AuthToken  string // the user's own auth_token cookie (tier 2)
	CT0        string // the user's own ct0 cookie / CSRF token (tier 2)
	AllowGuest bool   // enable the opt-in free guest tier (tier 1)
	Tier       string // forced tier: ""|syndication|web|guest|session

	// Behavior.
	Rate    time.Duration
	Retries int
	Timeout time.Duration
	NoCache bool
	Verbose int // -v: how much the tool says about its own workings
	Paths   Paths
	Store   string // path to the SQLite store, when the caller names one

	// GraphQL overrides (durability against query-id rotation).
	QueryIDs map[string]string // OperationName -> queryId
	Features string            // override features JSON
}

// Overrides are what a caller knows that the defaults and the environment do
// not: the flags. Zero means unset for every field but Retries, where zero is a
// real answer and -1 means unset.
type Overrides struct {
	DataDir    string
	CacheDir   string
	NoCache    bool
	Rate       time.Duration
	Retries    int
	Timeout    time.Duration
	Verbose    int
	AllowGuest bool
	Tier       string
	QueryIDs   map[string]string
}

// NoOverrides is the zero set, with Retries spelled as unset.
func NoOverrides() Overrides { return Overrides{Retries: -1} }

// Resolve builds a run's config in one pass, in precedence order, and only then
// reads the session store. The order is the point: which session store there is
// is one of the things --data-dir decides, so reading it any earlier is the
// defect this function exists to close.
func Resolve(o Overrides) Config {
	c := DefaultConfig()
	c.FromEnv()
	c.Paths = c.Paths.At(o.DataDir).WithCache(o.CacheDir)
	if o.NoCache {
		c.NoCache = true
	}
	if o.Rate > 0 {
		c.Rate = o.Rate
	}
	if o.Retries >= 0 {
		c.Retries = o.Retries
	}
	if o.Timeout > 0 {
		c.Timeout = o.Timeout
	}
	if o.Verbose > 0 {
		c.Verbose = o.Verbose
	}
	if o.AllowGuest {
		c.AllowGuest = true
	}
	if o.Tier != "" {
		c.Tier = o.Tier
	}
	for k, v := range o.QueryIDs {
		if c.QueryIDs == nil {
			c.QueryIDs = map[string]string{}
		}
		c.QueryIDs[k] = v
	}
	c.LoadStoredSession()
	return c
}

// DefaultConfig returns the built-in defaults before any file, environment, or
// flag overlay. It touches no disk and carries no credentials.
func DefaultConfig() Config {
	return Config{
		Rate:     time.Second,
		Retries:  3,
		Timeout:  30 * time.Second,
		Paths:    DefaultPaths(),
		QueryIDs: map[string]string{},
	}
}

// FromEnv overlays environment variables onto a config. It deliberately does not
// read the session store: see Resolve.
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
		c.Paths = c.Paths.At(v)
	}
	if v := os.Getenv("X_NO_CACHE"); v == "1" || strings.EqualFold(v, "true") {
		c.NoCache = true
	}
}

// LoadStoredSession fills in credentials from the session store under the
// resolved data dir, for whichever of the two the environment did not already
// provide. The environment wins, because a caller who exports X_AUTH_TOKEN has
// said which session they mean.
func (c *Config) LoadStoredSession() {
	if c.AuthToken != "" && c.CT0 != "" {
		return
	}
	s, ok := c.Paths.LoadSession()
	if !ok {
		return
	}
	if c.AuthToken == "" {
		c.AuthToken = s.AuthToken
	}
	if c.CT0 == "" {
		c.CT0 = s.CT0
	}
}

// HasSession reports whether the user's own session cookies are available (tier 2).
func (c Config) HasSession() bool { return c.AuthToken != "" && c.CT0 != "" }

// StorePath is where the typed local store lives for this run: the caller's
// --db when they named one, and the data dir otherwise.
func (c Config) StorePath() string {
	if c.Store != "" {
		return c.Store
	}
	return c.Paths.DB()
}

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

// configDir returns the per-user config directory.
func configDir() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "x")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "x")
	}
	return filepath.Join(home, ".config", "x")
}

// ConfigPath returns the default config file path, for a caller with no
// resolved config in hand.
func ConfigPath() string { return DefaultPaths().File() }

// TierNum is the highest tier this run is allowed to use, which is the tier a
// failure gets reported against. A forced --tier wins, then a session, then the
// opt-in guest switch, and 0 otherwise.
//
// It is the tier that was tried, which is not the tier that would have worked:
// Failure keeps those apart, and need_tier is the one a reader acts on.
func (c Config) TierNum() int {
	switch c.Tier {
	case "session":
		return 2
	case "guest":
		return 1
	case "syndication", "web", "oembed":
		return 0
	}
	switch {
	case c.HasSession():
		return 2
	case c.AllowGuest:
		return 1
	}
	return 0
}
