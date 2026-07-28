package cli

import (
	"context"
	"errors"
	"time"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/x-cli/x"
)

// Build metadata, stamped via -ldflags (spec §7). goreleaser targets
// github.com/tamnd/x-cli/cli.{Version,Commit,Date}.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// errNoResults is the sentinel a read raises when it produced nothing. mapErr
// turns it into the no-results exit code (3).
var errNoResults = errors.New("no results")

// New builds the kit App: the identity, the x-specific global flags, the
// per-run config finalize, and every command as a kit escape hatch. cli only
// touches kit here; kit wraps cobra/fang internally.
func New() *kit.App {
	app := kit.New(kit.Identity{
		Binary:  "x",
		Short:   "A fast, read-only command line for X (Twitter)",
		Long:    "x reads X's free public surfaces (syndication and the web-client GraphQL) and crawls them into a local store. Read-only: it never writes to your account. No paid API.",
		Version: Version,
		Site:    "https://x.com",
		Repo:    "https://github.com/tamnd/x-cli",
	}, kit.WithDefaults(withXDefaults))

	app.GlobalFlags(bindXFlags)

	app.CommandGroup("auth", "Manage your X session (Tier 2)")
	app.CommandGroup("config", "Show config paths and resolved values")
	app.CommandGroup("cache", "Inspect or clear the HTTP cache")
	app.CommandGroup("db", "Query and inspect the local store")
	app.CommandGroup("queue", "Show or clear the crawl queue")

	for _, c := range readCommands() {
		app.AddCommand(c)
	}
	for _, c := range entityCommands() {
		app.AddCommand(c)
	}
	for _, c := range trendCommands() {
		app.AddCommand(c)
	}
	for _, c := range dataCommands() {
		app.AddCommand(c)
	}
	for _, c := range identityCommands() {
		app.AddCommand(c)
	}
	for _, c := range tableCommands() {
		app.AddCommand(c)
	}
	for _, c := range metaCommands() {
		app.AddCommand(c)
	}

	// The same reads again, as operations, which is what `x serve` and `x mcp`
	// answer with (issue #18: both surfaces were empty because everything above
	// is an escape hatch, and an escape hatch is a command and nothing else).
	//
	// NoCLI keeps them off the command line. They would otherwise shadow the
	// commands above, and the difference is not cosmetic: the hand-written ones
	// render a curated table of the fields a person wants, while a reflected
	// Tweet is thirty-odd columns with JSON in the cells. So the operation
	// serves and the command stays typed.
	app.SetClient(func(_ context.Context, kc kit.Config) (any, error) {
		return x.NewEngine(xConfig(kc)), nil
	})
	x.RegisterOps(app, x.OpOptions{NoCLI: true})
	return app
}

// withXDefaults overlays x's request defaults onto the kit baseline so help and
// the resolved config read the same whether or not the user passes flags.
func withXDefaults(c *kit.Config) {
	c.Rate = time.Second
	c.Retries = 3
	c.Timeout = 30 * time.Second
}

// bindXFlags registers the x-only persistent flags on the root. The framework
// already provides -o/--output, --fields, --template, --no-header, -n/--limit,
// --rate, --retries, --timeout, --data-dir, --no-cache, -q/--quiet, -v, --color,
// --dry-run, --db, and --profile, so x adds only these three.
func bindXFlags(f *kit.FlagSet) {
	f.StringVar(&flagTier, "tier", "", "force one surface: syndication|oembed|web|guest|session")
	f.BoolVar(&flagGuest, "guest", false, "enable the opt-in free guest-GraphQL tier")
	f.StringSliceVar(&flagQueryIDs, "query-id", nil, "override a GraphQL query id (Op=hash)")
}
