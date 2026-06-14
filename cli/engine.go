package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/x-cli/x"
)

// x-specific global flags, bound on the root by the GlobalFlags hook in root.go.
// They are not part of the kit framework baseline, so x owns them: the forced
// tier, the opt-in guest-GraphQL switch, and per-operation query-id overrides.
// cobra parses them before any command's Run runs, so appFromCtx reads them
// directly when it resolves the per-run config.
var (
	flagTier     string
	flagGuest    bool
	flagQueryIDs []string
)

// App is the per-run state every command works through. The reads, the
// local-store workflow, and the meta commands are all kit.Command escape
// hatches: each one rebuilds this state from the run context with appFromCtx, so
// they share the resolved config, the engine, and the output settings kit
// resolved once for the run. The record operations also live in the x domain
// (x/domain.go), which an ant host drives; the standalone binary keeps its full
// command surface here, now on kit instead of cobra.
type App struct {
	actx   context.Context
	cfg    x.Config
	eng    *x.Engine
	limit  int
	format string
	fields string
	tmpl   string
	header bool // true means a header row is shown
	isTTY  bool
	quiet  bool
	dryRun bool
}

// appFromCtx assembles the run's App from the resolved kit State. It folds the
// framework globals (rate, retries, timeout, data dir, output settings) and the
// x-specific globals (tier, guest, query-id) into the x.Config the standalone
// binary has always built, so behavior matches the old cobra wiring exactly.
func appFromCtx(ctx context.Context) *App {
	st := kit.FromContext(ctx)
	a := &App{actx: ctx}
	if st == nil {
		// No PersistentPreRunE ran (should not happen in normal use); fall back
		// to plain defaults so the command still does something sane.
		a.cfg = xConfig(kit.Config{})
		a.format = "auto"
		a.header = true
		return a
	}
	kc := st.Config
	a.cfg = xConfig(kc)
	a.limit = st.Globals.Limit
	a.format = st.Output.Format
	a.fields = strings.Join(st.Output.Fields, ",")
	a.tmpl = st.Output.Template
	a.header = !st.Output.NoHeader
	a.isTTY = st.Output.IsTTY
	a.quiet = kc.Quiet
	a.dryRun = kc.DryRun
	return a
}

// xConfig builds the resolved x.Config from defaults, the environment, and the
// kit globals folded on top. kit's defaults differ from x's (rate 0, retries -1,
// timeout 0 all mean "unset"), so each is applied only when the user set it; the
// x defaults (1s, 3, 30s) otherwise stand.
func xConfig(kc kit.Config) x.Config {
	cfg := x.DefaultConfig()
	cfg.FromEnv()
	if kc.Rate > 0 {
		cfg.Rate = kc.Rate
	}
	if kc.Retries >= 0 {
		cfg.Retries = kc.Retries
	}
	if kc.Timeout > 0 {
		cfg.Timeout = kc.Timeout
	}
	if kc.NoCache {
		cfg.NoCache = true
	}
	if kc.DataDir != "" {
		cfg.DataDir = kc.DataDir
		cfg.CacheDir = filepath.Join(kc.DataDir, "cache")
	}
	if flagGuest {
		cfg.AllowGuest = true
	}
	if flagTier != "" {
		cfg.Tier = flagTier
	}
	for _, kv := range flagQueryIDs {
		if k, v, ok := strings.Cut(kv, "="); ok {
			if cfg.QueryIDs == nil {
				cfg.QueryIDs = map[string]string{}
			}
			cfg.QueryIDs[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return cfg
}

// ctx returns the run context (carries cancellation from the signal handler).
func (a *App) ctx() context.Context { return a.actx }

// config returns the resolved x configuration.
func (a *App) config() x.Config { return a.cfg }

// engine builds (once) the tier-resolving engine.
func (a *App) engine() *x.Engine {
	if a.eng == nil {
		a.eng = x.NewEngine(a.cfg)
	}
	return a.eng
}

// out builds the Output, auto-detecting table vs jsonl when --output is unset or
// "auto" (kit's default), matching the old cobra behavior.
func (a *App) out() (*Output, error) {
	format := a.format
	if format == "" || format == "auto" {
		if a.isTTY {
			format = "table"
		} else {
			format = "jsonl"
		}
	}
	return NewOutput(os.Stdout, format, a.fields, a.tmpl, !a.header)
}

// StorePath is the fixed location of the typed local store, under the data dir.
// The crawl, queue, db, and export commands read and write this rich schema. It
// is deliberately not kit's generic --db sink: that store carries a different
// schema and serves the (currently empty) record-op surface.
func (a *App) StorePath() string {
	dir := a.cfg.DataDir
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "x.db")
}

// openStore opens (creating the data dir) the typed local store at the fixed
// path. The data-group commands need it; it no longer depends on a --db flag.
func (a *App) openStore() (*x.Store, error) {
	path := a.StorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return x.OpenStore(path)
}

// logf prints progress to stderr unless --quiet.
func (a *App) logf(format string, args ...any) {
	if !a.quiet {
		_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// mapErr converts a library error into the kit error kind that carries the
// matching exit code, so kit prints and exits the same way the old hand-rolled
// exitCode map did: no-results 3, need-auth 4, rate-limited 5, not-found 6.
// Every escape-hatch Run wraps its returned error in this.
func mapErr(err error) error {
	var na *x.NeedAuthError
	var rl *x.RateLimitedError
	var nf *x.NotFoundError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errNoResults):
		return errs.NoResults("no results")
	case errors.As(err, &na):
		return errs.NeedAuth("%s", na.Error())
	case errors.As(err, &rl):
		return errs.RateLimited("%s", rl.Error())
	case errors.As(err, &nf):
		return errs.NotFound("%s", nf.Error())
	default:
		return err
	}
}
