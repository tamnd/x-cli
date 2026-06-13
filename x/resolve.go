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

// GraphQL returns the GraphQL client (writes, advanced reads).
func (e *Engine) GraphQL() *GraphQL { return e.g }

// canGraphQL reports whether a GraphQL tier (1 or 2) is available.
func (e *Engine) canGraphQL() bool {
	return e.cfg.HasSession() || e.cfg.AllowGuest || e.cfg.Tier == "guest" || e.cfg.Tier == "session"
}

// needGraphQL returns the actionable need-auth error for a GraphQL-only call.
func needGraphQL(cap string) error {
	return &NeedAuthError{Msg: cap + " needs the GraphQL tier — pass --guest, or run `x auth import` to use your own session"}
}

// Tweet resolves one tweet, preferring Tier 0 syndication.
func (e *Engine) Tweet(ctx context.Context, id string) (*Tweet, error) {
	switch e.cfg.Tier {
	case "guest", "session":
		return e.g.TweetByID(ctx, id)
	case "syndication":
		return TweetByID(ctx, e.c, id)
	}
	t, err := TweetByID(ctx, e.c, id)
	if err == nil {
		return t, nil
	}
	if e.canGraphQL() {
		if t2, err2 := e.g.TweetByID(ctx, id); err2 == nil {
			return t2, nil
		}
	}
	return nil, err
}

// User resolves a profile, preferring Tier 0 syndication for a handle.
func (e *Engine) User(ctx context.Context, ref string, isID bool) (*User, error) {
	if e.cfg.Tier == "guest" || e.cfg.Tier == "session" {
		if isID {
			return e.g.UserByRestID(ctx, ref)
		}
		return e.g.UserByName(ctx, ref)
	}
	if !isID && e.cfg.Tier != "session" {
		if u, err := UserByNameSyndication(ctx, e.c, ref); err == nil {
			return u, nil
		} else if !e.canGraphQL() {
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
func (e *Engine) Timeline(ctx context.Context, ref string, isID bool, o TimelineOpts, emit func(*Tweet) error) error {
	// Tier 0: the profile-timeline widget returns the ~100 most recent.
	if e.cfg.Tier == "" || e.cfg.Tier == "syndication" {
		if !isID {
			tweets, err := ProfileTimeline(ctx, e.c, ref, o.Limit)
			if err == nil && len(tweets) > 0 && !e.canGraphQL() {
				return streamTweets(tweets, o, emit)
			}
			if err == nil && len(tweets) > 0 && e.cfg.Tier == "syndication" {
				return streamTweets(tweets, o, emit)
			}
		}
		if e.cfg.Tier == "syndication" {
			return needGraphQL("a numeric-id timeline")
		}
	}
	if !e.canGraphQL() {
		// Fall back to the Tier 0 window if we have a handle.
		if !isID {
			tweets, err := ProfileTimeline(ctx, e.c, ref, o.Limit)
			if err != nil {
				return err
			}
			return streamTweets(tweets, o, emit)
		}
		return needGraphQL("a numeric-id timeline")
	}
	uid, err := e.g.resolveUserID(ctx, ref, isID)
	if err != nil {
		return err
	}
	return e.g.UserTweets(ctx, uid, o, emit)
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

// Thread streams a conversation, falling back to Tier 0 self-thread.
func (e *Engine) Thread(ctx context.Context, id string, limit int, emit func(*Tweet) error) error {
	if e.canGraphQL() && e.cfg.Tier != "syndication" {
		return e.g.Thread(ctx, id, limit, emit)
	}
	// Tier 0: the focal tweet plus its embedded parent chain, best-effort.
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
