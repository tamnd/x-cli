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

// A numeric --tier caps what a run may use, and the whole point is that it wins
// over a credential that is already there. Before this, --tier 0 set a string
// nothing compared against, so a machine with a session imported read at tier 2
// and reported tier 0, which is worse than not having the flag: it is the flag
// lying about the answer.
func TestANumericTierTakesTheCredentialAway(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("X_DATA_DIR", "")
	t.Setenv("X_AUTH_TOKEN", "")
	t.Setenv("X_CT0", "")
	if err := DefaultPaths().SaveSession(Creds{AuthToken: "tok", CT0: "csrf"}); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		tier    string
		guest   bool
		session bool
		num     int
	}{
		{tier: "", session: true, num: 2},
		{tier: "0", num: 0},
		{tier: "1", guest: true, num: 1},
		{tier: "2", session: true, num: 2},
	} {
		o := NoOverrides()
		o.Tier = c.tier
		cfg := Resolve(o)
		if cfg.HasSession() != c.session {
			t.Errorf("--tier %q: session %v, want %v", c.tier, cfg.HasSession(), c.session)
		}
		if cfg.AllowGuest != c.guest {
			t.Errorf("--tier %q: guest %v, want %v", c.tier, cfg.AllowGuest, c.guest)
		}
		if got := cfg.TierNum(); got != c.num {
			t.Errorf("--tier %q: reports tier %d, want %d", c.tier, got, c.num)
		}
	}
}

// --tier 1 turns the guest tier on rather than only capping it, because a cap
// that leaves you below the number you asked for is not the thing you asked for.
func TestTierOneTurnsGuestOn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("X_DATA_DIR", "")
	t.Setenv("X_AUTH_TOKEN", "")
	t.Setenv("X_CT0", "")
	o := NoOverrides()
	o.Tier = "1"
	if cfg := Resolve(o); !cfg.AllowGuest {
		t.Error("--tier 1 with no --guest did not enable the guest tier")
	}
}

// The named values still pin a surface, and a cap must not have quietly turned
// one of them into a no-op.
func TestNamedTiersStillPin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("X_DATA_DIR", "")
	t.Setenv("X_AUTH_TOKEN", "tok")
	t.Setenv("X_CT0", "csrf")
	for _, name := range []string{"syndication", "oembed", "web", "guest", "session"} {
		o := NoOverrides()
		o.Tier = name
		cfg := Resolve(o)
		if cfg.Tier != name {
			t.Errorf("--tier %s came back as %q", name, cfg.Tier)
		}
		if !cfg.HasSession() {
			t.Errorf("--tier %s dropped the session, and only a number should do that", name)
		}
	}
}

func TestValidTierKnowsEveryValueTheFlagAccepts(t *testing.T) {
	for _, v := range TierValues() {
		if !ValidTier(v) {
			t.Errorf("%q is offered and rejected", v)
		}
	}
	for _, v := range []string{"sindication", "3", "-1", "web ", "SESSION"} {
		if ValidTier(v) {
			t.Errorf("%q should not be a tier", v)
		}
	}
}
