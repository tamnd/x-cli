package x

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// ops.go is the read surface as operations: one registration each, served over
// HTTP by `x serve` and to an agent by `x mcp` (spec 3003 doc 05 section 6, doc
// 06 section 2).
//
// The command line is not built from these. It is hand-written in cli/, because
// a curated table beats a reflected dump of thirty fields, and OpMeta.NoCLI is
// what lets both exist: the op serves, the hand-written command is what a person
// types. A host such as ant registers the same set with NoCLI off, and there the
// ops are the command line.
//
// The handlers are thin on purpose. Every decision about what a surface says
// lives in the engine next to the data it decides about; what is left here is
// resolving a reference and making one call.

// OpOptions says how a caller wants the operations registered.
type OpOptions struct {
	// NoCLI keeps the operations off the command line, for a binary that ships
	// its own commands for the same reads and only wants the serve and MCP
	// surfaces filled in.
	NoCLI bool
}

// RegisterOps installs every read operation onto app.
func RegisterOps(app *kit.App, o OpOptions) {
	registerTweetOps(app, o)
	registerUserOps(app, o)
	registerSearchOps(app, o)
	registerGraphOps(app, o)
	registerPlaceOps(app, o)
}

// handle registers one operation, applying the caller's surface choice. It is a
// thin wrapper on kit.Handle so no registration below has to remember it.
func handle[In, Out any](app *kit.App, o OpOptions, m kit.OpMeta, fn func(context.Context, In, func(Out) error) error) {
	m.NoCLI = o.NoCLI
	kit.Handle(app, m, fn)
}

// --- inputs ---
//
// One struct per argument shape rather than one per operation, because a dozen
// reads take a tweet and nothing else and a dozen near-identical structs would
// be a dozen places to get the help text wrong.

type tweetRef struct {
	Ref    string  `kit:"arg" help:"tweet id or status URL"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Engine *Engine `kit:"inject"`
}

type userRef struct {
	Ref    string  `kit:"arg" help:"@handle, user id, or profile URL"`
	ByID   bool    `kit:"flag" help:"treat the reference as a numeric user id"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Engine *Engine `kit:"inject"`
}

type queryRef struct {
	Query   []string `kit:"arg,variadic" help:"search terms"`
	Product string   `kit:"flag" help:"Top|Latest|People|Photos|Videos" default:"Latest"`
	Limit   int      `kit:"flag,inherit" help:"max results"`
	Engine  *Engine  `kit:"inject"`
}

type noRef struct {
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Engine *Engine `kit:"inject"`
}

// --- tweets ---

func registerTweetOps(app *kit.App, o OpOptions) {
	handle(app, o, kit.OpMeta{Name: "tweet", Group: "read", Single: true,
		Summary: "Read one tweet", URIType: KindTweet, Resolver: true,
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, getTweet)

	handle(app, o, kit.OpMeta{Name: "thread", Group: "read", List: true,
		Summary: "Read the conversation around a tweet", URIType: KindTweet,
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, listThread)

	handle(app, o, kit.OpMeta{Name: "replies", Group: "read",
		Summary: "Read the replies to a tweet", URIType: KindTweet,
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, listReplies)

	handle(app, o, kit.OpMeta{Name: "quotes", Group: "read",
		Summary: "Read the quote tweets of a tweet, through search", URIType: KindTweet,
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, listQuotes)

	handle(app, o, kit.OpMeta{Name: "media", Group: "read",
		Summary: "Read the media on a tweet, at a resolved URL", URIType: KindMedia,
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, listMedia)

	handle(app, o, kit.OpMeta{Name: "poll", Group: "read",
		Summary: "Read a tweet's poll options and tallies", URIType: KindPoll,
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, listPoll)

	handle(app, o, kit.OpMeta{Name: "likers", Group: "read",
		Summary: "Read the accounts that liked a tweet (session)", URIType: KindUser,
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, listLikers)

	handle(app, o, kit.OpMeta{Name: "retweeters", Group: "read",
		Summary: "Read the accounts that reposted a tweet (session)", URIType: KindUser,
		Args: []kit.Arg{{Name: "ref", Help: "tweet id or status URL"}}}, listRetweeters)

	handle(app, o, kit.OpMeta{Name: "space", Group: "read", Single: true,
		Summary: "Read one audio Space", URIType: KindSpace, Resolver: true,
		Args: []kit.Arg{{Name: "ref", Help: "Space id or i/spaces URL"}}}, getSpace)
}

func getTweet(ctx context.Context, in tweetRef, emit func(*Tweet) error) error {
	id, err := parseTweet(in.Ref)
	if err != nil {
		return err
	}
	t, err := in.Engine.Tweet(ctx, id)
	if err != nil {
		return mapErr(err)
	}
	return emit(t)
}

func listThread(ctx context.Context, in tweetRef, emit func(*Tweet) error) error {
	id, err := parseTweet(in.Ref)
	if err != nil {
		return err
	}
	return mapErr(in.Engine.Thread(ctx, id, in.Limit, emit))
}

// listReplies drops the reply total the engine hands back. The CLI turns it into
// a warning about how much of the conversation this tier could not see; over
// HTTP and MCP there is no stderr to say it on, and inventing a field for it
// would put a fact about the crawl in with the records.
func listReplies(ctx context.Context, in tweetRef, emit func(*Tweet) error) error {
	id, err := parseTweet(in.Ref)
	if err != nil {
		return err
	}
	_, err = in.Engine.Replies(ctx, id, in.Limit, emit)
	return mapErr(err)
}

func listQuotes(ctx context.Context, in tweetRef, emit func(*Tweet) error) error {
	id, err := parseTweet(in.Ref)
	if err != nil {
		return err
	}
	q := SearchQuery{Raw: "quoted_tweet_id:" + id, Product: "Latest", Limit: in.Limit}
	return mapErr(in.Engine.Search(ctx, q, emit))
}

// MediaItem is one picture or video with the URL a caller can actually fetch.
//
// The URL is resolved rather than copied, because a photo carries its size in
// the URL and a video has no single one, so the address on the record is an
// answer to a question nobody asked. Media rides inside a tweet in the model and
// is its own record here, since "the media on this post" is a list.
type MediaItem struct {
	Media
	URL string `json:"url"`
}

func listMedia(ctx context.Context, in tweetRef, emit func(*MediaItem) error) error {
	id, err := parseTweet(in.Ref)
	if err != nil {
		return err
	}
	t, err := in.Engine.Tweet(ctx, id)
	if err != nil {
		return mapErr(err)
	}
	for _, m := range t.Media {
		u, err := MediaURL(m, DefaultMediaSize, "")
		if err != nil {
			continue
		}
		if err := emit(&MediaItem{Media: m, URL: u}); err != nil {
			return err
		}
	}
	return nil
}

func listPoll(ctx context.Context, in tweetRef, emit func(*PollOption) error) error {
	id, err := parseTweet(in.Ref)
	if err != nil {
		return err
	}
	t, err := in.Engine.Tweet(ctx, id)
	if err != nil {
		return mapErr(err)
	}
	if t.Poll == nil || len(t.Poll.Options) == 0 {
		return errs.NoResults("tweet %s has no poll", id)
	}
	for _, opt := range t.Poll.Options {
		if err := emit(&opt); err != nil {
			return err
		}
	}
	return nil
}

func listLikers(ctx context.Context, in tweetRef, emit func(*User) error) error {
	id, err := parseTweet(in.Ref)
	if err != nil {
		return err
	}
	return mapErr(in.Engine.Likers(ctx, id, in.Limit, emit))
}

func listRetweeters(ctx context.Context, in tweetRef, emit func(*User) error) error {
	id, err := parseTweet(in.Ref)
	if err != nil {
		return err
	}
	return mapErr(in.Engine.Retweeters(ctx, id, in.Limit, emit))
}

func getSpace(ctx context.Context, in tweetRef, emit func(*Space) error) error {
	id, err := ParseSpaceRef(in.Ref)
	if err != nil {
		return errs.Usage("%s", err.Error())
	}
	s, err := in.Engine.Space(ctx, id)
	if err != nil {
		return mapErr(err)
	}
	return emit(s)
}

// --- users ---

func registerUserOps(app *kit.App, o OpOptions) {
	handle(app, o, kit.OpMeta{Name: "user", Group: "read", Single: true,
		Summary: "Read one profile", URIType: KindUser, Resolver: true,
		Args: []kit.Arg{{Name: "ref", Help: "@handle, user id, or profile URL"}}}, getUser)

	handle(app, o, kit.OpMeta{Name: "timeline", Group: "read", List: true,
		Summary: "Read a user's tweets", URIType: KindUser,
		Args: []kit.Arg{{Name: "ref", Help: "@handle, user id, or profile URL"}}}, listTimeline)

	handle(app, o, kit.OpMeta{Name: "mentions", Group: "read",
		Summary: "Read tweets mentioning a user, through search", URIType: KindTweet,
		Args: []kit.Arg{{Name: "ref", Help: "@handle, user id, or profile URL"}}}, listMentions)

	handle(app, o, kit.OpMeta{Name: "likes", Group: "read",
		Summary: "Read the tweets a user has liked (session)", URIType: KindTweet,
		Args: []kit.Arg{{Name: "ref", Help: "@handle, user id, or profile URL"}}}, listLikes)

	handle(app, o, kit.OpMeta{Name: "followers", Group: "read",
		Summary: "Read the accounts following a user (session)", URIType: KindUser,
		Args: []kit.Arg{{Name: "ref", Help: "@handle, user id, or profile URL"}}}, listFollowers)

	handle(app, o, kit.OpMeta{Name: "following", Group: "read",
		Summary: "Read the accounts a user follows (session)", URIType: KindUser,
		Args: []kit.Arg{{Name: "ref", Help: "@handle, user id, or profile URL"}}}, listFollowing)

	handle(app, o, kit.OpMeta{Name: "home", Group: "read",
		Summary: "Read your reverse-chronological home timeline (session)", URIType: KindTweet},
		listHome)

	handle(app, o, kit.OpMeta{Name: "bookmarks", Group: "read",
		Summary: "Read your bookmarks (session)", URIType: KindTweet}, listBookmarks)

	handle(app, o, kit.OpMeta{Name: "list", Group: "read", List: true,
		Summary: "Read the tweets in an X List (session)", URIType: KindList,
		Args: []kit.Arg{{Name: "ref", Help: "list id or i/lists URL"}}}, listTweets)
}

func getUser(ctx context.Context, in userRef, emit func(*User) error) error {
	ref, isID, err := parseUser(in.Ref, in.ByID)
	if err != nil {
		return err
	}
	u, err := in.Engine.User(ctx, ref, isID)
	if err != nil {
		return mapErr(err)
	}
	return emit(u)
}

func listTimeline(ctx context.Context, in userRef, emit func(*Tweet) error) error {
	ref, isID, err := parseUser(in.Ref, in.ByID)
	if err != nil {
		return err
	}
	return mapErr(in.Engine.Timeline(ctx, ref, isID, TimelineOpts{Limit: in.Limit}, emit))
}

func listMentions(ctx context.Context, in userRef, emit func(*Tweet) error) error {
	ref, _, err := parseUser(in.Ref, false)
	if err != nil {
		return err
	}
	q := SearchQuery{Raw: "@" + ref, Product: "Latest", Limit: in.Limit}
	return mapErr(in.Engine.Search(ctx, q, emit))
}

func listLikes(ctx context.Context, in userRef, emit func(*Tweet) error) error {
	ref, isID, err := parseUser(in.Ref, in.ByID)
	if err != nil {
		return err
	}
	return mapErr(in.Engine.Likes(ctx, ref, isID, in.Limit, emit))
}

func listFollowers(ctx context.Context, in userRef, emit func(*User) error) error {
	ref, isID, err := parseUser(in.Ref, in.ByID)
	if err != nil {
		return err
	}
	return mapErr(in.Engine.Followers(ctx, ref, isID, in.Limit, emit))
}

func listFollowing(ctx context.Context, in userRef, emit func(*User) error) error {
	ref, isID, err := parseUser(in.Ref, in.ByID)
	if err != nil {
		return err
	}
	return mapErr(in.Engine.Following(ctx, ref, isID, in.Limit, emit))
}

func listHome(ctx context.Context, in noRef, emit func(*Tweet) error) error {
	return mapErr(in.Engine.GraphQL().Home(ctx, in.Limit, emit))
}

func listBookmarks(ctx context.Context, in noRef, emit func(*Tweet) error) error {
	return mapErr(in.Engine.GraphQL().Bookmarks(ctx, in.Limit, emit))
}

func listTweets(ctx context.Context, in tweetRef, emit func(*Tweet) error) error {
	kind, id, err := Classify(in.Ref)
	if err != nil || kind != KindList {
		return errs.Usage("not a list id or i/lists URL: %q", in.Ref)
	}
	return mapErr(in.Engine.GraphQL().ListTweets(ctx, id, in.Limit, emit))
}

// --- search ---

func registerSearchOps(app *kit.App, o OpOptions) {
	handle(app, o, kit.OpMeta{Name: "search", Group: "read", URIType: KindTweet,
		Summary: "Search tweets (session)",
		Args:    []kit.Arg{{Name: "query", Help: "search terms", Variadic: true}}}, search)

	handle(app, o, kit.OpMeta{Name: "counts", Group: "read",
		Summary: "Per-day tweet counts for a search, bucketed here rather than by X",
		Args:    []kit.Arg{{Name: "query", Help: "search terms", Variadic: true}}}, counts)
}

func search(ctx context.Context, in queryRef, emit func(*Tweet) error) error {
	q := SearchQuery{Raw: strings.Join(in.Query, " "), Product: in.Product, Limit: in.Limit}
	return mapErr(in.Engine.Search(ctx, q, emit))
}

// counts buckets a search by day on this side of the wire. X's own counts
// endpoint is on the paid API, and this tool does not have one of those.
func counts(ctx context.Context, in queryRef, emit func(*Bucket) error) error {
	q := SearchQuery{Raw: strings.Join(in.Query, " "), Product: in.Product, Limit: in.Limit}
	days := map[string]int{}
	err := in.Engine.Search(ctx, q, func(t *Tweet) error {
		key := "undated"
		if !t.CreatedAt.IsZero() {
			key = t.CreatedAt.UTC().Format("2006-01-02")
		}
		days[key]++
		return nil
	})
	if err != nil {
		return mapErr(err)
	}
	keys := make([]string, 0, len(days))
	for k := range days {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		day, _ := time.Parse("2006-01-02", k)
		b := Bucket{Start: day, End: day.AddDate(0, 0, 1), Count: days[k]}
		if err := emit(&b); err != nil {
			return err
		}
	}
	return nil
}

// --- the graph ---

func registerGraphOps(app *kit.App, o OpOptions) {
	handle(app, o, kit.OpMeta{Name: "edges", Group: "graph",
		Summary: "Read the graph edges a record asserts, with the URL each came from",
		Args:    []kit.Arg{{Name: "ref", Help: "any x reference"}}}, emitEdges)

	handle(app, o, kit.OpMeta{Name: "graph", Group: "graph", Single: true,
		Summary: "Read the edges and the nodes they address, as one document",
		Args:    []kit.Arg{{Name: "ref", Help: "any x reference"}}}, getGraph)
}

// readRef fetches whatever a reference points at, for the two graph reads. It is
// the one place in this file that dispatches on kind, because the graph is the
// one thing here that does not care what kind it was handed.
func readRef(ctx context.Context, e *Engine, ref string) (any, error) {
	kind, id, err := Classify(ref)
	if err != nil {
		return nil, errs.Usage("%s", err.Error())
	}
	switch kind {
	case KindTweet:
		return e.Tweet(ctx, id)
	case KindUser:
		return e.User(ctx, id, false)
	case KindSpace:
		return e.Space(ctx, id)
	}
	return nil, errs.Unsupported("nothing reads a %s directly; it is a node other records point at", kind)
}

func emitEdges(ctx context.Context, in tweetRef, emit func(*Edge) error) error {
	rec, err := readRef(ctx, in.Engine, in.Ref)
	if err != nil {
		return mapErr(err)
	}
	for _, e := range MergeEdges(Edges(rec)) {
		if err := emit(&e); err != nil {
			return err
		}
	}
	return nil
}

func getGraph(ctx context.Context, in tweetRef, emit func(*Document) error) error {
	rec, err := readRef(ctx, in.Engine, in.Ref)
	if err != nil {
		return mapErr(err)
	}
	d := Graph(rec)
	return emit(&d)
}

// --- places ---

func registerPlaceOps(app *kit.App, o OpOptions) {
	handle(app, o, kit.OpMeta{Name: "trends", Group: "read",
		Summary: "Read what is trending in a place",
		Args:    []kit.Arg{{Name: "woeid", Help: "where-on-earth id (default worldwide)", Optional: true}}},
		listTrends)

	handle(app, o, kit.OpMeta{Name: "places", Group: "read",
		Summary: "Read the places X has trends for, and their woeids",
		Args:    []kit.Arg{{Name: "query", Help: "match a place name", Optional: true}}}, listPlaces)
}

type trendRef struct {
	WOEID  string  `kit:"arg" help:"where-on-earth id (default worldwide)"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Engine *Engine `kit:"inject"`
}

type placeRef struct {
	Query   string  `kit:"arg" help:"match a place name"`
	Country string  `kit:"flag" help:"only places in this country"`
	Type    string  `kit:"flag" help:"only places of this type (Town, Country, Supername)"`
	Limit   int     `kit:"flag,inherit" help:"max results"`
	Engine  *Engine `kit:"inject"`
}

func listTrends(ctx context.Context, in trendRef, emit func(*Trend) error) error {
	woeid, err := in.Engine.ResolveWOEID(ctx, in.WOEID)
	if err != nil {
		return mapErr(err)
	}
	trends, err := in.Engine.Trends(ctx, woeid, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, t := range trends {
		if err := emit(t); err != nil {
			return err
		}
	}
	return nil
}

func listPlaces(ctx context.Context, in placeRef, emit func(*Place) error) error {
	places, err := in.Engine.Places(ctx, in.Query, in.Country, in.Type, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, p := range places {
		if err := emit(p); err != nil {
			return err
		}
	}
	return nil
}

// --- shared ---

func parseTweet(ref string) (string, error) {
	id, err := ParseTweetRef(ref)
	if err != nil {
		return "", errs.Usage("%s", err.Error())
	}
	return id, nil
}

func parseUser(ref string, byID bool) (string, bool, error) {
	r, isID, err := ParseUserRef(ref, byID)
	if err != nil {
		return "", false, errs.Usage("%s", err.Error())
	}
	return r, isID, nil
}
