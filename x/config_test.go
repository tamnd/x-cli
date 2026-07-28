package x

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The defect from spec 3003 doc 06 section 4. The old tool read
// ~/.local/share/x/session.json whatever --data-dir said, so a run the caller
// believed was anonymous used their cookies and reported capabilities they did
// not have. This is that run: a session in the home store, --data-dir pointing
// somewhere empty, and the answer has to be tier 1.
func TestDataDirMovesTheSessionStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("X_DATA_DIR", "")
	t.Setenv("X_AUTH_TOKEN", "")
	t.Setenv("X_CT0", "")

	real := DefaultPaths()
	if err := real.SaveSession(Creds{AuthToken: "tok", CT0: "csrf", Handle: "someone"}); err != nil {
		t.Fatal(err)
	}
	if cfg := Resolve(NoOverrides()); !cfg.HasSession() {
		t.Fatal("with no --data-dir the home session should be found, and was not")
	}

	elsewhere := t.TempDir()
	o := NoOverrides()
	o.DataDir = elsewhere
	cfg := Resolve(o)
	if cfg.HasSession() {
		t.Errorf("--data-dir %s still read the session from %s", elsewhere, real.Session())
	}
	if cfg.Paths.Session() != filepath.Join(elsewhere, "session.json") {
		t.Errorf("session store = %s, want it under the data dir", cfg.Paths.Session())
	}
	if cfg.Paths.Cache != filepath.Join(elsewhere, "cache") {
		t.Errorf("cache = %s, want it under the data dir", cfg.Paths.Cache)
	}
	if cfg.StorePath() != filepath.Join(elsewhere, "x.db") {
		t.Errorf("store = %s, want it under the data dir", cfg.StorePath())
	}
}

// The guest token is credential-shaped too, and it caches beside the session it
// belongs to. A run pointed at an empty data dir mints its own rather than
// picking up one from a directory it was told to ignore.
func TestDataDirMovesTheGuestToken(t *testing.T) {
	dir := t.TempDir()
	o := NoOverrides()
	o.DataDir = dir
	s := NewSession(Resolve(o))
	if got, want := s.paths.Guest(), filepath.Join(dir, "guest.json"); got != want {
		t.Fatalf("guest store = %s, want %s", got, want)
	}
	s.saveGuestToken("abc", time.Now())
	if _, err := os.Stat(filepath.Join(dir, "guest.json")); err != nil {
		t.Fatalf("guest token was not written under the data dir: %v", err)
	}
	tok, _, ok := s.loadGuestToken()
	if !ok || tok != "abc" {
		t.Errorf("read back %q, %v; want abc, true", tok, ok)
	}
}

// The environment still wins over the store, because a caller who exports
// X_AUTH_TOKEN has said which session they mean.
func TestEnvBeatsTheSessionStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("X_DATA_DIR", "")
	if err := DefaultPaths().SaveSession(Creds{AuthToken: "stored", CT0: "stored-ct0"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("X_AUTH_TOKEN", "from-env")
	t.Setenv("X_CT0", "from-env-ct0")
	cfg := Resolve(NoOverrides())
	if cfg.AuthToken != "from-env" || cfg.CT0 != "from-env-ct0" {
		t.Errorf("got %s/%s, want the environment's pair", cfg.AuthToken, cfg.CT0)
	}
}

// The config file does not follow --data-dir. It is where the user wrote it.
func TestConfigFileStaysPut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("X_DATA_DIR", "")
	o := NoOverrides()
	o.DataDir = t.TempDir()
	want := filepath.Join(home, ".config", "x", "config.toml")
	if got := Resolve(o).Paths.File(); got != want {
		t.Errorf("config file = %s, want %s", got, want)
	}
}
