package x

import "context"

// Engine wires the three tiers behind one capability surface (spec §4.2). It
// resolves the cheapest free surface that can answer each call: Tier 0
// (syndication, no auth) for single tweets / profiles / recent timelines, then
// the GraphQL tiers (1 guest, 2 the user's own session) for everything else.
type Engine struct {
	cfg Config
	c   *Client
	s   *Session
	g   *GraphQL
}

// NewEngine builds an Engine from a resolved config.
func NewEngine(cfg Config) *Engine {
	c := NewClient(cfg)
	s := NewSession(cfg)
	return &Engine{cfg: cfg, c: c, s: s, g: NewGraphQL(c, s, cfg)}
}

// Client exposes the shared HTTP client (for `x cache`, downloads).
func (e *Engine) Client() *Client { return e.c }

// Config returns the engine's config.
func (e *Engine) Config() Config { return e.cfg }

// GraphQL returns the GraphQL client (the deeper reads beyond syndication).
func (e *Engine) GraphQL() *GraphQL { return e.g }

// canGraphQL reports whether a GraphQL tier (1 or 2) is available.
func (e *Engine) canGraphQL() bool {
	return e.cfg.HasSession() || e.cfg.AllowGuest || e.cfg.Tier == "guest" || e.cfg.Tier == "session"
}

// needGraphQL returns the actionable need-auth error for a GraphQL-only call.
// A guest token clears it, so this is tier 1 rather than tier 2.
func needGraphQL(cap string) error {
	return &NeedAuthError{
		Msg:  cap + " needs the GraphQL tier: pass --guest, or run `x auth import` to use your own session",
		Tier: 1,
	}
}

// Tweet resolves one tweet.
//
// Tier 0 is two surfaces. Syndication gives the typed tweet with its entities
// and media; the x.com page gives the bookmark count, the views, and the
// author's follower counts. The tool reads both and merges, because neither
// one is a superset and picking a winner would drop real data.
func (e *Engine) Tweet(ctx context.Context, id string) (*Tweet, error) {
	switch e.cfg.Tier {
	case "guest", "session":
		return e.g.TweetByID(ctx, id)
	case "syndication":
		return TweetByID(ctx, e.c, id)
	case "web":
		return e.TweetFromWeb(ctx, id)
	}
	t, err := TweetByID(ctx, e.c, id)
	if err == nil {
		// A failure here costs nothing: the syndication answer already
		// stands on its own and the page is a bonus.
		if w, werr := e.TweetFromWeb(ctx, id); werr == nil {
			t = MergeTweet(t, w)
		}
		return t, nil
	}
	if w, werr := e.TweetFromWeb(ctx, id); werr == nil {
		return w, nil
	}
	if e.canGraphQL() {
		if t2, err2 := e.g.TweetByID(ctx, id); err2 == nil {
			return t2, nil
		}
	}
	return nil, err
}

// TweetFromWeb reads one tweet off its x.com status page, which is surface 8
// and needs no credential.
func (e *Engine) TweetFromWeb(ctx context.Context, id string) (*Tweet, error) {
	p, err := e.c.FetchPage(ctx, StatusPageURL(id))
	if err != nil {
		return nil, err
	}
	return p.TweetFromPage(id)
}

// UserFromWeb reads one profile off its x.com page.
func (e *Engine) UserFromWeb(ctx context.Context, handle string) (*User, error) {
	p, err := e.c.FetchPage(ctx, UserURL(handle))
	if err != nil {
		return nil, err
	}
	return p.UserFromPage(handle)
}

// TimelineFromWeb reads the postings the x.com profile page renders. It is
// fewer tweets than the syndication widget returns, and it costs no credential.
func (e *Engine) TimelineFromWeb(ctx context.Context, handle string) ([]*Tweet, error) {
	p, err := e.c.FetchPage(ctx, UserURL(handle))
	if err != nil {
		return nil, err
	}
	return p.Postings(), nil
}

// User resolves a profile, preferring Tier 0 syndication for a handle.
func (e *Engine) User(ctx context.Context, ref string, isID bool) (*User, error) {
	if e.cfg.Tier == "guest" || e.cfg.Tier == "session" {
		if isID {
			return e.g.UserByRestID(ctx, ref)
		}
		return e.g.UserByName(ctx, ref)
	}
	if e.cfg.Tier == "web" && !isID {
		return e.UserFromWeb(ctx, ref)
	}
	if !isID && e.cfg.Tier != "session" {
		u, err := UserByNameSyndication(ctx, e.c, ref)
		if err == nil {
			if w, werr := e.UserFromWeb(ctx, ref); werr == nil {
				u = MergeUser(u, w)
			}
			return u, nil
		}
		if w, werr := e.UserFromWeb(ctx, ref); werr == nil {
			return w, nil
		}
		if !e.canGraphQL() {
			return nil, err
		}
	}
	if !e.canGraphQL() {
		return nil, needGraphQL("resolving a profile by id")
	}
	if isID {
		return e.g.UserByRestID(ctx, ref)
	}
	return e.g.UserByName(ctx, ref)
}

// Timeline streams a user's tweets, using Tier 0 for the recent window and the
// GraphQL tiers to page deeper.
//
// Tier 0 has two surfaces here too. The syndication widget returns the ~100
// most recent, which is the better answer whenever it is available; the x.com
// profile page carries the visible timeline, which is fewer tweets but keeps
// the command working when the widget's window is exhausted.
func (e *Engine) Timeline(ctx context.Context, ref string, isID bool, o TimelineOpts, emit func(*Tweet) error) error {
	if e.cfg.Tier == "web" {
		if isID {
			return needGraphQL("a numeric-id timeline")
		}
		tweets, err := e.TimelineFromWeb(ctx, ref)
		if err != nil {
			return err
		}
		return streamTweets(tweets, o, emit)
	}
	// Tier 0 is the whole answer when there is no GraphQL tier to fall through
	// to, and it is the preferred answer when the caller asked for it by name.
	tier0 := !e.canGraphQL() || e.cfg.Tier == "syndication"
	if tier0 || e.cfg.Tier == "" {
		if isID {
			if tier0 {
				return needGraphQL("a numeric-id timeline")
			}
		} else {
			tweets, err := ProfileTimeline(ctx, e.c, ref, o.Limit)
			if len(tweets) == 0 {
				// The widget said nothing, so read the page before giving up.
				// An exhausted widget window is the common case and the page is
				// not on the same budget.
				if w, werr := e.TimelineFromWeb(ctx, ref); werr == nil && len(w) > 0 {
					tweets, err = w, nil
				}
			}
			if tier0 {
				if err != nil {
					return err
				}
				return streamTweets(tweets, o, emit)
			}
			// A GraphQL tier is available, so Tier 0 only answers when it is
			// enough on its own; otherwise page deeper below.
			if err == nil && len(tweets) >= o.Limit && o.Limit > 0 {
				return streamTweets(tweets, o, emit)
			}
		}
	}
	uid, err := e.g.resolveUserID(ctx, ref, isID)
	if err != nil {
		return err
	}
	return e.g.UserTweets(ctx, uid, o, emit)
}

// focalFirst moves the tweet the page is about to the front. The page renders
// the replies before it, which is right for a page and wrong for a thread.
func focalFirst(tweets []*Tweet, id string) []*Tweet {
	for i, t := range tweets {
		if t.ID != id {
			continue
		}
		out := make([]*Tweet, 0, len(tweets))
		out = append(out, t)
		out = append(out, tweets[:i]...)
		return append(out, tweets[i+1:]...)
	}
	return tweets
}

// streamTweets applies the timeline filters and limit to an in-memory slice.
func streamTweets(tweets []*Tweet, o TimelineOpts, emit func(*Tweet) error) error {
	n := 0
	for _, t := range tweets {
		if o.Media && len(t.Media) == 0 {
			continue
		}
		if !o.Replies && t.IsReply {
			continue
		}
		if err := emit(t); err != nil {
			return err
		}
		n++
		if o.Limit > 0 && n >= o.Limit {
			return nil
		}
	}
	return nil
}

// Search streams search results (GraphQL only).
func (e *Engine) Search(ctx context.Context, q SearchQuery, emit func(*Tweet) error) error {
	if !e.canGraphQL() {
		return needGraphQL("search")
	}
	return e.g.Search(ctx, q, emit)
}

// Thread streams a conversation.
//
// At tier 0 the status page is the whole answer: it renders the focal tweet
// and the replies X chose to show, each with its author and its counters. That
// is not the full tree and it is not paged, but it is a conversation and it
// costs no credential, which is more than the syndication endpoint gives.
func (e *Engine) Thread(ctx context.Context, id string, limit int, emit func(*Tweet) error) error {
	if e.canGraphQL() && e.cfg.Tier != "syndication" && e.cfg.Tier != "web" {
		return e.g.Thread(ctx, id, limit, emit)
	}
	if e.cfg.Tier != "syndication" {
		if p, err := e.c.FetchPage(ctx, StatusPageURL(id)); err == nil {
			if posts := p.Postings(); len(posts) > 0 {
				return streamTweets(focalFirst(posts, id), TimelineOpts{Replies: true, Limit: limit}, emit)
			}
		}
	}
	// The page had nothing, so fall back to the focal tweet on its own.
	t, err := TweetByID(ctx, e.c, id)
	if err != nil {
		return err
	}
	return emit(t)
}

// Followers / Following / Likers / Retweeters (GraphQL only).
func (e *Engine) Followers(ctx context.Context, ref string, isID bool, limit int, emit func(*User) error) error {
	uid, err := e.userID(ctx, ref, isID, "followers")
	if err != nil {
		return err
	}
	return e.g.Followers(ctx, uid, limit, emit)
}
func (e *Engine) Following(ctx context.Context, ref string, isID bool, limit int, emit func(*User) error) error {
	uid, err := e.userID(ctx, ref, isID, "following")
	if err != nil {
		return err
	}
	return e.g.Following(ctx, uid, limit, emit)
}
func (e *Engine) Likers(ctx context.Context, tweetID string, limit int, emit func(*User) error) error {
	if !e.canGraphQL() {
		return needGraphQL("likers")
	}
	return e.g.Likers(ctx, tweetID, limit, emit)
}
func (e *Engine) Retweeters(ctx context.Context, tweetID string, limit int, emit func(*User) error) error {
	if !e.canGraphQL() {
		return needGraphQL("retweeters")
	}
	return e.g.Retweeters(ctx, tweetID, limit, emit)
}

// Likes streams the tweets a user liked (GraphQL only).
func (e *Engine) Likes(ctx context.Context, ref string, isID bool, limit int, emit func(*Tweet) error) error {
	uid, err := e.userID(ctx, ref, isID, "likes")
	if err != nil {
		return err
	}
	return e.g.Likes(ctx, uid, limit, emit)
}

func (e *Engine) userID(ctx context.Context, ref string, isID bool, cap string) (string, error) {
	if !e.canGraphQL() {
		return "", needGraphQL(cap)
	}
	return e.g.resolveUserID(ctx, ref, isID)
}
