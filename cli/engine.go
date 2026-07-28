package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/any-cli/kit/render"
	"github.com/tamnd/x-cli/x"
)

// Row is one output record: an ordered, curated column set (Cols/Vals) for the
// table, csv, tsv, and url views plus the full typed object as Value for json,
// jsonl, raw, and template. It is kit's render.Record, so every row builder
// feeds straight into the shared renderer with no per-format code of our own,
// the same way ytb-cli does. Snowflake IDs stay strings (the Value tags them).
type Row = render.Record

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
	st     *kit.State // the resolved run state; nil only on the defensive fallback
	cfg    x.Config
	eng    *x.Engine
	limit  int
	quiet  bool
	dryRun bool
	// target is the thing this run was asked for, set once a command has parsed
	// its argument. The failure record names it, so a caller reading a stream of
	// them can tell which request went wrong.
	target string
}

// appFromCtx assembles the run's App from the resolved kit State. It folds the
// framework globals (rate, retries, timeout, data dir, output settings) and the
// x-specific globals (tier, guest, query-id) into the x.Config the standalone
// binary has always built, so behavior matches the old cobra wiring exactly.
func appFromCtx(ctx context.Context) *App {
	st := kit.FromContext(ctx)
	a := &App{actx: ctx, st: st}
	if st == nil {
		// No PersistentPreRunE ran (should not happen in normal use); fall back
		// to plain defaults so the command still does something sane.
		a.cfg = xConfig(kit.Config{})
		return a
	}
	kc := st.Config
	a.cfg = xConfig(kc)
	a.limit = st.Globals.Limit
	a.quiet = kc.Quiet
	a.dryRun = kc.DryRun
	return a
}

// xConfig folds the kit globals into an x.Overrides and lets x.Resolve build the
// config. kit's defaults differ from x's (rate 0, retries -1, timeout 0 all mean
// "unset"), and x.Resolve reads them the same way, so the x defaults (1s, 3,
// 30s) stand unless the user said otherwise.
//
// The session store is read by Resolve, last, once --data-dir has settled where
// it is. That ordering is the fix for the defect in doc 06 section 4.
func xConfig(kc kit.Config) x.Config {
	o := x.Overrides{
		DataDir:    kc.DataDir,
		NoCache:    kc.NoCache,
		Rate:       kc.Rate,
		Retries:    kc.Retries,
		Timeout:    kc.Timeout,
		Verbose:    kc.Verbose,
		AllowGuest: flagGuest,
		Tier:       flagTier,
		QueryIDs:   map[string]string{},
	}
	for _, kv := range flagQueryIDs {
		if k, v, ok := strings.Cut(kv, "="); ok {
			o.QueryIDs[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return x.Resolve(o)
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

// out builds the shared kit renderer over stdout from the run's resolved output
// settings (the same --output/--fields/--no-header/--template every kit surface
// uses), so the standalone binary, an ant host, and serve/mcp all format records
// identically. It differs from kit's default in one place: with no -o, x prints
// the readable list/section view on a terminal instead of a table (the table is
// still a step away with `-o table`), and jsonl when piped so scripts stay
// machine-readable. An explicit -o or --template always wins. The run's terminal
// width still applies to any truncate columns.
func (a *App) out() (*render.Renderer, error) {
	if a.st == nil {
		return render.New(render.Options{Format: render.List, Writer: os.Stdout})
	}
	o := a.st.Output
	return render.New(render.Options{
		Format:   a.format(),
		IsTTY:    o.IsTTY,
		Color:    o.Color,
		Fields:   o.Fields,
		NoHeader: o.NoHeader,
		Template: o.Template,
		Width:    o.Width,
		Writer:   os.Stdout,
	})
}

// format is the output format this run resolved to. It differs from kit's
// default in one place: with no -o, x prints the readable list view on a
// terminal and jsonl when piped, so scripts stay machine-readable.
func (a *App) format() render.Format {
	if a.st == nil {
		return render.List
	}
	o := a.st.Output
	format := render.Format(o.Format)
	if o.Template == "" && (format == "" || format == render.Auto) {
		if o.IsTTY {
			return render.List
		}
		return render.JSONL
	}
	return format
}

// fail is the error path of a read: it writes the failure record from doc 03
// section 11 to stdout, then hands the error to mapErr for the exit code.
//
// Only on jsonl, which is deliberate rather than a shortcut. jsonl is the format
// a script gets when it pipes x, it is the one the spec names when it says a
// caller needs to see what did not work without parsing stderr, and appending
// one more line to it is always valid. json cannot take the record after its
// array has closed, csv cannot take a row with different columns, and the human
// formats already print a better message on stderr.
//
// The error still comes back mapped, so the exit code and the stderr message are
// unchanged. The record is additional, never a replacement.
func (a *App) fail(target string, err error) error {
	if err == nil {
		return nil
	}
	if a.format() == render.JSONL {
		if b, e := json.Marshal(a.failure(target, err)); e == nil {
			fmt.Fprintln(os.Stdout, string(b))
		}
	}
	return mapErr(err)
}

// failure builds the record. errNoResults is a cli sentinel rather than an x
// error, so x.FailureOf would call it unclassified; it gets code 3 here to match
// the exit code the same error produces.
func (a *App) failure(target string, err error) x.Failure {
	if errors.Is(err, errNoResults) {
		return x.Failure{Kind: "error", Target: target, Code: 3,
			Reason: "no results", Tier: a.cfg.TierNum()}
	}
	return x.FailureOf(err, target, a.cfg.TierNum())
}

// done is the tail of every read: nil passes through, and anything else becomes
// a failure record plus the mapped exit code, against whatever the command was
// asked for.
func (a *App) done(err error) error { return a.fail(a.target, err) }

// StorePath is where the typed local store lives for this run, under the data
// dir. The crawl, queue, db, and export commands read and write this rich
// schema. It is deliberately not kit's generic --db sink: that store carries a
// different schema and serves the (currently empty) record-op surface.
func (a *App) StorePath() string { return a.cfg.StorePath() }

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
// matching exit code (spec 3003 doc 03 section 11): no-results 3, need-auth 4,
// rate-limited 5, not-found 6, unsupported 7, network 8. Every escape-hatch Run
// wraps its returned error in this.
//
// 4 and 7 are the pair that matter. 4 means get a session and this works, and
// the message names which tier. 7 means nothing serves this at any tier, so
// there is no credential to go and find.
func mapErr(err error) error {
	var na *x.NeedAuthError
	var rl *x.RateLimitedError
	var nf *x.NotFoundError
	var un *x.UnsupportedError
	var ne *x.NetworkError
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
	case errors.As(err, &un):
		return errs.Unsupported("%s", un.Error())
	case errors.As(err, &ne):
		return errs.Network("%s", ne.Error())
	}
	// A transport failure that never went through the client, such as a direct
	// media download, still gets code 8.
	if n := x.AsNetwork(err); n != nil {
		return errs.Network("%s", n.Error())
	}
	return err
}
