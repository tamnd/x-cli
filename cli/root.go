package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/fang"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/tamnd/x-cli/x"
)

// Build metadata, stamped via -ldflags (spec §7).
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// App carries the global flag values and builds the engine/output per command.
type App struct {
	output   string
	fields   string
	template string
	noHeader bool
	limit    int
	tier     string
	guest    bool
	rate     time.Duration
	retries  int
	timeout  time.Duration
	dataDir  string
	noCache  bool
	quiet    bool
	verbose  bool
	color    string
	dryRun   bool
	db       string
	queryIDs []string
}

// Root builds the full command tree.
func Root() *cobra.Command {
	a := &App{}
	root := &cobra.Command{
		Use:           "x",
		Short:         "A fast, read-only command line for X (Twitter)",
		Long:          "x reads X's free public surfaces (syndication and the web-client GraphQL) and crawls them into a local store. Read-only: it never writes to your account. No paid API.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVarP(&a.output, "output", "o", "", "table|json|jsonl|csv|tsv|url|raw (default auto)")
	pf.StringVar(&a.fields, "fields", "", "comma-separated columns to project")
	pf.StringVar(&a.template, "template", "", "Go text/template per row")
	pf.BoolVar(&a.noHeader, "no-header", false, "omit the header row")
	pf.IntVarP(&a.limit, "limit", "n", 0, "max rows (0 = unlimited)")
	pf.StringVar(&a.tier, "tier", "", "force a tier: syndication|guest|session")
	pf.BoolVar(&a.guest, "guest", false, "enable the opt-in free guest-GraphQL tier")
	pf.DurationVar(&a.rate, "rate", time.Second, "min delay between requests")
	pf.IntVar(&a.retries, "retries", 3, "retries on 429/5xx")
	pf.DurationVar(&a.timeout, "timeout", 30*time.Second, "per-request timeout")
	pf.StringVar(&a.dataDir, "data-dir", "", "cache + store root")
	pf.BoolVar(&a.noCache, "no-cache", false, "bypass the cache")
	pf.BoolVarP(&a.quiet, "quiet", "q", false, "suppress progress on stderr")
	pf.BoolVarP(&a.verbose, "verbose", "v", false, "show tier/endpoint and timing")
	pf.StringVar(&a.color, "color", "auto", "auto|always|never")
	pf.BoolVar(&a.dryRun, "dry-run", false, "print the target instead of acting (e.g. open)")
	pf.StringVar(&a.db, "db", "", "path to the local SQLite store")
	pf.StringArrayVar(&a.queryIDs, "query-id", nil, "override a GraphQL query id (Op=hash)")

	root.AddGroup(
		&cobra.Group{ID: "read", Title: "Reads:"},
		&cobra.Group{ID: "data", Title: "Local store:"},
		&cobra.Group{ID: "meta", Title: "Meta:"},
	)
	addReadCommands(root, a)
	addEntityCommands(root, a)
	addDataCommands(root, a)
	addMetaCommands(root, a)
	return root
}

// Execute runs the CLI with fang and maps errors to exit codes (spec §6).
func Execute() {
	root := Root()
	err := fang.Execute(context.Background(), root,
		fang.WithVersion(Version),
		fang.WithNotifySignal(os.Interrupt),
	)
	if err != nil {
		os.Exit(exitCode(err))
	}
}

// config builds the resolved x.Config from defaults + env + flags.
func (a *App) config() x.Config {
	cfg := x.DefaultConfig()
	cfg.FromEnv()
	cfg.Rate = a.rate
	cfg.Retries = a.retries
	cfg.Timeout = a.timeout
	if a.noCache {
		cfg.NoCache = true
	}
	if a.dataDir != "" {
		cfg.DataDir = a.dataDir
	}
	if a.guest {
		cfg.AllowGuest = true
	}
	if a.tier != "" {
		cfg.Tier = a.tier
	}
	cfg.Store = a.db
	for _, kv := range a.queryIDs {
		if k, v, ok := strings.Cut(kv, "="); ok {
			if cfg.QueryIDs == nil {
				cfg.QueryIDs = map[string]string{}
			}
			cfg.QueryIDs[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return cfg
}

// engine builds the tier-resolving engine.
func (a *App) engine() *x.Engine { return x.NewEngine(a.config()) }

// out builds the Output, auto-detecting table vs jsonl when --output is unset.
func (a *App) out() (*Output, error) {
	format := a.output
	if format == "" {
		if isatty.IsTerminal(os.Stdout.Fd()) {
			format = "table"
		} else {
			format = "jsonl"
		}
	}
	return NewOutput(os.Stdout, format, a.fields, a.template, a.noHeader)
}

// ctx returns a base context (room for future cancellation wiring).
func (a *App) ctx() context.Context { return context.Background() }

// logf prints to stderr unless quiet.
func (a *App) logf(format string, args ...any) {
	if !a.quiet {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// exitCode maps a library error to the documented exit code (spec §6).
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var na *x.NeedAuthError
	if errors.As(err, &na) {
		fmt.Fprintln(os.Stderr, "x: "+na.Error())
		return 4
	}
	var rl *x.RateLimitedError
	if errors.As(err, &rl) {
		fmt.Fprintln(os.Stderr, "x: "+rl.Error())
		return 5
	}
	var nf *x.NotFoundError
	if errors.As(err, &nf) {
		fmt.Fprintln(os.Stderr, "x: "+nf.Error())
		return 6
	}
	if errors.Is(err, errNoResults) {
		fmt.Fprintln(os.Stderr, "x: no results")
		return 3
	}
	fmt.Fprintln(os.Stderr, "x: "+err.Error())
	return 1
}

var errNoResults = errors.New("no results")
