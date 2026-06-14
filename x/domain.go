package x

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes X as a kit Domain: a driver that a multi-domain host (ant)
// enables with a single blank import,
//
//	import _ "github.com/tamnd/x-cli/x"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// x:// URIs by routing to the operations Register installs. The standalone x
// binary does not use any of this, so the CLI is unchanged.
func init() { kit.Register(Domain{}) }

// Domain is the X driver. It carries no state; the per-run Engine is built by
// the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity a host reuses for help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme:  "x",
		Aliases: []string{"twitter"},
		Hosts:   []string{"x.com", "twitter.com"},
		Identity: kit.Identity{
			Binary: "x",
			Short:  "Read public X (Twitter) data",
			Site:   "x.com",
			Repo:   "https://github.com/tamnd/x-cli",
		},
	}
}

// Register installs the Engine factory and every X operation onto app. A resolver
// op (Single) names its own record type and answers `ant get`; a List op
// enumerates a parent resource's members and answers `ant ls`.
func (Domain) Register(app *kit.App) {
	app.SetClient(newEngine)

	// Resolver ops: one record per id, the home of `ant get x://<type>/<id>`.
	kit.Handle(app, kit.OpMeta{Name: "status", Group: "read", Single: true,
		Summary: "Fetch a tweet by id or status URL", URIType: "status", Resolver: true,
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, getStatus)
	kit.Handle(app, kit.OpMeta{Name: "user", Group: "read", Single: true,
		Summary: "Fetch a profile by @handle, id, or URL", URIType: "user", Resolver: true,
		Args: []kit.Arg{{Name: "ref", Help: "@handle, user id, or profile URL"}}}, getUser)

	// List ops: members of a parent resource, the home of `ant ls`.
	kit.Handle(app, kit.OpMeta{Name: "timeline", Group: "read", List: true,
		Summary: "List a user's recent tweets", URIType: "user",
		Args: []kit.Arg{{Name: "ref", Help: "@handle, user id, or profile URL"}}}, listTimeline)
	kit.Handle(app, kit.OpMeta{Name: "thread", Group: "read", List: true,
		Summary: "List a conversation thread", URIType: "status",
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, listThread)

	// Search rounds out the surface; URIType "status" keeps it from claiming a
	// second authority, since the status resolver already owns the Tweet type.
	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", URIType: "status",
		Summary: "Search recent tweets",
		Args:    []kit.Arg{{Name: "query", Help: "search terms", Variadic: true}}}, search)
}

// newEngine builds the X engine from the host-resolved config, reusing the same
// data dir and environment the standalone binary uses so a lent session (the
// user's own cookies) and the page cache are shared.
func newEngine(_ context.Context, cfg kit.Config) (any, error) {
	xcfg := DefaultConfig()
	xcfg.FromEnv()
	if cfg.DataDir != "" {
		xcfg.DataDir = cfg.DataDir
		xcfg.CacheDir = filepath.Join(cfg.DataDir, "cache")
	}
	if cfg.NoCache {
		xcfg.NoCache = true
	}
	return NewEngine(xcfg), nil
}

// --- inputs ---

type tweetRef struct {
	Ref    string  `kit:"arg" help:"tweet id or status URL"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Engine *Engine `kit:"inject"`
}

type userRef struct {
	Ref    string  `kit:"arg" help:"@handle, user id, or profile URL"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Engine *Engine `kit:"inject"`
}

type queryRef struct {
	Query  []string `kit:"arg,variadic" help:"search terms"`
	Limit  int      `kit:"flag,inherit" help:"max results"`
	Engine *Engine  `kit:"inject"`
}

// --- handlers ---

func getStatus(ctx context.Context, in tweetRef, emit func(*Tweet) error) error {
	id, err := ParseTweetRef(in.Ref)
	if err != nil {
		return errs.Usage("%s", err.Error())
	}
	t, err := in.Engine.Tweet(ctx, id)
	if err != nil {
		return mapErr(err)
	}
	return emit(t)
}

func getUser(ctx context.Context, in userRef, emit func(*User) error) error {
	ref, isID, err := ParseUserRef(in.Ref, true)
	if err != nil {
		return errs.Usage("%s", err.Error())
	}
	u, err := in.Engine.User(ctx, ref, isID)
	if err != nil {
		return mapErr(err)
	}
	return emit(u)
}

func listTimeline(ctx context.Context, in userRef, emit func(*Tweet) error) error {
	ref, isID, err := ParseUserRef(in.Ref, true)
	if err != nil {
		return errs.Usage("%s", err.Error())
	}
	return mapErr(in.Engine.Timeline(ctx, ref, isID, TimelineOpts{Limit: in.Limit}, emit))
}

func listThread(ctx context.Context, in tweetRef, emit func(*Tweet) error) error {
	id, err := ParseTweetRef(in.Ref)
	if err != nil {
		return errs.Usage("%s", err.Error())
	}
	return mapErr(in.Engine.Thread(ctx, id, in.Limit, emit))
}

func search(ctx context.Context, in queryRef, emit func(*Tweet) error) error {
	q := SearchQuery{Raw: strings.Join(in.Query, " "), Limit: in.Limit}
	return mapErr(in.Engine.Search(ctx, q, emit))
}

// --- Resolver: the URI-native string functions, reused from ref.go ---

// Classify turns any accepted input into the canonical (type, id), so `ant
// resolve` and `ant url` need no network. A bare numeric or a /status/ URL is a
// tweet; anything else is read as a profile reference.
func (Domain) Classify(input string) (uriType, id string, err error) {
	if tid, terr := ParseTweetRef(input); terr == nil {
		return "status", tid, nil
	}
	ref, _, uerr := ParseUserRef(input, false)
	if uerr != nil || ref == "" {
		return "", "", errs.Usage("unrecognized X reference: %q", input)
	}
	return "user", ref, nil
}

// Locate is the inverse: the live page URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "status":
		return TweetURL("", id), nil
	case "user":
		return UserURL(id), nil
	default:
		return "", errs.Usage("x has no resource type %q", uriType)
	}
}

// mapErr converts a library error into the kit error kind that carries the right
// exit code, so a host renders the same need-auth/rate-limited outcomes the
// standalone binary does.
func mapErr(err error) error {
	var na *NeedAuthError
	var rl *RateLimitedError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &na):
		return errs.NeedAuth("%s", na.Error())
	case errors.As(err, &rl):
		return errs.RateLimited("%s", rl.Error())
	default:
		return err
	}
}
