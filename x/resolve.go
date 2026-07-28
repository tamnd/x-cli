package x

import (
	"context"
	"errors"
)

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
//
// Reserve it for the calls a guest token actually clears. Measured 2026-07-28,
// that is three operations: UserByScreenName, UserByRestId, and UserTweets.
// Everything else on the GraphQL surface answers a guest token with a 404 and an
// empty body, and telling that caller to pass --guest sends them through a
// wasted request to a second exit 4 with a different message. Use needSession
// for those.
func needGraphQL(cap string) error {
	return &NeedAuthError{
		Msg:  cap + " needs the GraphQL tier: pass --guest, or run `x auth import` to use your own session",
		Tier: 1,
	}
}

// needSession is the same thing for a call no anonymous credential reaches. It
// is the message the surface itself would return one request later, said before
// the request rather than after it.
func needSession(cap string) error { return NeedTier(cap, 2) }

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
	case "oembed":
		o, err := FetchOEmbed(ctx, e.c, id)
		if err != nil {
			return nil, err
		}
		return o.ToTweet(id), nil
	}
	t, err := TweetByID(ctx, e.c, id)
	if err == nil {
		// A failure here costs nothing: the syndication answer already
		// stands on its own and the page is a bonus.
		if w, werr := e.TweetFromWeb(ctx, id); werr == nil {
			t = MergeTweet(t, w)
		}
		return e.fillFromOEmbed(ctx, t, id), nil
	}
	if w, werr := e.TweetFromWeb(ctx, id); werr == nil {
		return e.fillFromOEmbed(ctx, w, id), nil
	}
	if e.canGraphQL() {
		if t2, err2 := e.g.TweetByID(ctx, id); err2 == nil {
			return t2, nil
		}
	}
	return nil, err
}

// OEmbed reads one tweet's embed HTML off surface 3. It is the whole of `x
// embed`, and it is the only read that never touches another surface: the bytes
// are the answer.
func (e *Engine) OEmbed(ctx context.Context, id string) (*OEmbed, error) {
	return FetchOEmbed(ctx, e.c, id)
}

// fillFromOEmbed is plane F, and it is the only plane that costs a request of
// its own, so it asks first whether it has anything to add.
//
// On the ordinary day it has not: the syndication endpoint states the text, the
// language and the author, and this returns without touching the network. It
// earns its place on the day both of the surfaces above it come back thin, and
// on any read of an /i/status/ URL where nobody has said who wrote the thing.
//
// A failure here is not a failure of the read. The caller already has an answer;
// this was an attempt to make it a better one.
func (e *Engine) fillFromOEmbed(ctx context.Context, t *Tweet, id string) *Tweet {
	if !wantsOEmbed(t) {
		return t
	}
	o, err := FetchOEmbed(ctx, e.c, id)
	if err != nil {
		return t
	}
	return MergeTweet(t, o.ToTweet(id))
}

// wantsOEmbed is whether surface 3 could still fill something surface 3 has.
func wantsOEmbed(t *Tweet) bool {
	if t == nil {
		return false
	}
	return t.Text == "" || t.Lang == "" || t.Author == nil || t.Author.Username == ""
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
	posts := p.Postings()
	markSample(posts)
	return posts, nil
}

// User resolves a profile.
//
// Nothing about a user needs tier 1 outright: surface 2 and surface 8 between
// them have every field (spec 3003 doc 03 section 3.4). Tier 1 is still the
// first choice when it is available, because it is one request rather than two
// and its window is 150 in fifteen minutes where the profile widget's is 30.
// Whichever answered, the record says so, and whatever was tried and did not
// answer is recorded as a miss rather than left to look like an account with
// nothing to say.
func (e *Engine) User(ctx context.Context, ref string, isID bool) (*User, error) {
	if e.cfg.Tier == "web" && !isID {
		return e.UserFromWeb(ctx, ref)
	}
	if isID {
		// A numeric id addresses nothing at tier 0: neither the widget nor the
		// profile page has a route that takes one.
		if !e.canGraphQL() {
			return nil, needGraphQL("resolving a profile by id")
		}
		return e.g.UserByRestID(ctx, ref)
	}
	var missed error
	if e.canGraphQL() && e.cfg.Tier != "syndication" && e.cfg.Tier != "web" {
		u, err := e.g.UserByName(ctx, ref)
		if err == nil {
			return u, nil
		}
		if e.cfg.Tier == "guest" || e.cfg.Tier == "session" {
			// The tier was asked for by name, so falling back to a thinner one
			// silently would answer a question nobody asked.
			return nil, err
		}
		var nf *NotFoundError
		if errors.As(err, &nf) {
			// There is no such account, which is an answer and not a failure.
			// Asking two more surfaces gets the same answer more slowly, and if
			// one of them is rate limited it gets a worse one.
			return nil, err
		}
		missed = err
	}
	return e.userTier0(ctx, ref, missed)
}

// userTier0 reads a profile off both tier-0 surfaces and merges them. Neither
// is a superset of the other and neither is reliable on its own: the widget
// window is 30 requests, and the profile page is whatever x.com served that
// second. What did not answer goes in the envelope.
func (e *Engine) userTier0(ctx context.Context, handle string, missed error) (*User, error) {
	u, serr := UserByNameSyndication(ctx, e.c, handle)
	w, werr := e.UserFromWeb(ctx, handle)
	if u == nil && w == nil {
		if serr != nil {
			return nil, serr
		}
		return nil, werr
	}
	u = MergeUser(u, w)
	u.Miss(4, missed)
	u.Miss(2, serr)
	u.Miss(8, werr)
	return u, nil
}

// Timeline streams a user's tweets, using Tier 0 for the recent window and the
// GraphQL tiers to page deeper.
//
// Tier 0 has two surfaces here too. The syndication widget returns up to a
// hundred entries, which is the better answer whenever it is available; the
// x.com profile page carries the visible timeline, which is fewer tweets but
// keeps the command working when the widget's window is exhausted. Whichever
// answered, a surface that was tried and did not is recorded on the records.
//
// The widget's answer is not always a timeline. It hands back a ranked sample
// for some accounts (doc 02 section 1.4), and a sample is the wrong answer to
// "the last twenty tweets" however many rows it has, so when a GraphQL tier is
// available the cursor walk wins and the sample is only used as the fallback.
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
	//
	// A replies-inclusive read is tier 0's whether the caller asked or not,
	// because the guest tier answers it with nothing. UserTweetsAndReplies does
	// not wall a guest token the way the rest of the operation list does; it
	// returns 200 and `{"data":{"user":{"result":{"timeline":{}}}}}`, an empty
	// envelope that reads as an account which has never replied to anybody. The
	// syndication widget does carry replies: @jack's came back as 40 entries
	// with 4 of them replies. So `x replies jack --guest` used to say "no
	// results" where `x replies jack` answered, and adding a credential should
	// not cost you the answer.
	tier0 := !e.canGraphQL() || e.cfg.Tier == "syndication" || (o.Replies && !e.cfg.HasSession())
	if tier0 || e.cfg.Tier == "" {
		if isID {
			if o.Replies && !e.cfg.HasSession() {
				// Tier 0 has no route that takes a numeric id, and the two
				// tiers that do are a guest token that answers this one empty
				// and a session that answers it properly.
				return NeedTier("a numeric-id timeline with replies", 2)
			}
			if tier0 {
				return needGraphQL("a numeric-id timeline")
			}
		} else {
			tweets, err := e.timelineTier0(ctx, ref, o.Limit)
			if tier0 {
				if err != nil {
					return err
				}
				return streamTweets(tweets, o, emit)
			}
			// A GraphQL tier is available, so Tier 0 only answers when it is
			// enough on its own and is a timeline; otherwise page deeper below.
			if err == nil && o.Limit > 0 && len(tweets) >= o.Limit && !sampled(tweets) {
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

// timelineTier0 reads a timeline off the widget, falling back to the x.com page.
//
// The two are not interchangeable and the fallback is much thinner, so the
// records say which one answered and name the one that did not. An exhausted
// widget window is the common case here: it is 30 requests in fifteen minutes,
// and without the note a five-tweet answer looks like an account that posted
// five times.
func (e *Engine) timelineTier0(ctx context.Context, handle string, limit int) ([]*Tweet, error) {
	tweets, err := ProfileTimeline(ctx, e.c, handle, limit)
	if len(tweets) > 0 {
		return tweets, err
	}
	w, werr := e.TimelineFromWeb(ctx, handle)
	if werr != nil || len(w) == 0 {
		return tweets, err
	}
	for _, t := range w {
		t.Miss(2, err)
	}
	return w, nil
}

// sampled reports whether a set came back ranked rather than walked. The flag
// is on every record of such a set, so the first one answers for all of them.
func sampled(tweets []*Tweet) bool {
	return len(tweets) > 0 && tweets[0].Sample
}

// MediaTab streams the profile's media tab.
//
// Timeline with Media set filters a timeline down to the tweets that carry
// media, which is a different thing: the tier 0 window is the last hundred
// tweets, so a profile that posts mostly text answers with almost nothing. The
// media tab is X's own index of every post with a picture in it, back as far as
// paging goes, and it is a GraphQL operation, so it costs a tier.
func (e *Engine) MediaTab(ctx context.Context, ref string, isID bool, limit int, emit func(*Tweet) error) error {
	uid, err := e.userID(ctx, ref, isID, "the media tab")
	if err != nil {
		return err
	}
	return e.g.UserTweets(ctx, uid, TimelineOpts{Media: true, Limit: limit}, emit)
}

// pageReplies separates the tweet a status page is about from the replies to it.
//
// The page is a conversation and not a reply list: it renders what is above the
// tweet as well as what is below, and Postings hands the focal tweet back last,
// so position tells you nothing about which is which. The page for
// 1903142823316049977 came back as [its parent, three replies, the tweet], and
// the page carries no in-reply-to ids to sort it out with.
//
// The ids sort it out. A snowflake counts upward with time, so anything older
// than the focal tweet was written before it and cannot be a reply to it. That
// drops the ancestors, and a quoted tweet too if one is rendered, which is the
// right answer in both cases. Descendants deeper than one level stay, because
// the conversation under a tweet is what somebody asking for its replies means.
//
// A page that does not carry its own focal tweet is possible, because the
// renderer can drop it. The split still works: the comparison is against the id
// asked for, which the caller has either way, and not against a tweet the page
// may not have rendered.
func pageReplies(tweets []*Tweet, id string) (focal *Tweet, replies []*Tweet) {
	for _, t := range tweets {
		if t.ID == id {
			if focal == nil {
				focal = t
			}
			continue
		}
		if idLess(t.ID, id) {
			continue
		}
		replies = append(replies, t)
	}
	return focal, replies
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
		return needSession("search")
	}
	return e.g.Search(ctx, q, emit)
}

// wantsConversationAPI reports whether to read the conversation with
// TweetDetail rather than by walking it.
//
// It asks for a session rather than for GraphQL, which the rest of the engine
// asks for, because TweetDetail is one of the calls X refuses a guest token:
// `x thread --guest` used to answer "TweetDetail needs tier 2" while the same
// command with no credential at all answered with the thread. A guest token is
// worth nothing here, so it changes nothing here.
//
// --tier syndication and --tier web are honoured as ceilings, as everywhere.
func (e *Engine) wantsConversationAPI() bool {
	return e.cfg.HasSession() && e.cfg.Tier != "syndication" && e.cfg.Tier != "web"
}

// Thread streams a conversation, root first.
//
// A thread is the ancestors of a tweet and then the tweet, which surface 1
// gives away at every tier, followed by whatever replies the tier can see below
// it. The upward half is the part that is always right: it is the actual chain,
// in order, however deep. The downward half is three replies at tier 0 and 1 and
// the tree at tier 2, and `x replies` is where that distinction gets said out
// loud.
//
// Ordering matters more here than in a timeline. A thread read root first is a
// conversation; the same tweets in page order are a screenshot of a web page.
func (e *Engine) Thread(ctx context.Context, id string, limit int, emit func(*Tweet) error) error {
	if e.wantsConversationAPI() {
		return e.g.Thread(ctx, id, limit, emit)
	}
	chain, cerr := ParentChain(ctx, e.c, id, limit)
	if len(chain) == 0 {
		return cerr
	}
	// The status page renders the ancestors as well as the replies, so the two
	// halves of the walk overlap and the overlap has to be dropped. A thread
	// that prints a tweet twice reads as a bug even when both copies are right.
	said := map[string]bool{}
	n := 0
	say := func(t *Tweet) (bool, error) {
		if said[t.ID] {
			return true, nil
		}
		said[t.ID] = true
		if err := emit(t); err != nil {
			return false, err
		}
		n++
		return limit <= 0 || n < limit, nil
	}
	for _, t := range chain {
		more, err := say(t)
		if err != nil || !more {
			return err
		}
	}
	if cerr != nil {
		return cerr
	}
	if e.cfg.Tier == "syndication" {
		// --tier syndication asked for surface 1 and nothing else, and the
		// replies live on the status page.
		return nil
	}
	_, replies, err := RepliesFromPage(ctx, e.c, id)
	if err != nil {
		// The ancestors are a real answer, so a page that would not load is a
		// thinner thread rather than a failed command.
		return nil
	}
	for _, t := range replies {
		more, err := say(t)
		if err != nil || !more {
			return err
		}
	}
	return nil
}

// Replies streams the replies to a tweet, which is the direction X charges for.
//
// The focal tweet is not emitted: `x replies 20` was asked for the replies, and
// `x thread 20` is the command that wants the tweet as well. What the focal
// tweet is for is its reply counter, which comes back alongside so the caller
// can say three of how many.
//
// The count is X's `conversation_count`, which counts the whole conversation
// rather than the direct replies, so it is an over-count of what was asked for.
// It is still the only denominator any tier-0 surface publishes, and a warning
// that says "3 of 17964" is useful in a way that "3" is not.
func (e *Engine) Replies(ctx context.Context, id string, limit int, emit func(*Tweet) error) (total *int, err error) {
	if e.wantsConversationAPI() {
		// TweetDetail returns the focal tweet with the tree under it, so the
		// filter drops the one the caller already has.
		return nil, e.g.Thread(ctx, id, limit, func(t *Tweet) error {
			if t.ID == id {
				return nil
			}
			return emit(t)
		})
	}
	if e.cfg.Tier == "syndication" {
		return nil, Unsupported("replies to a tweet on surface 1",
			"the syndication endpoint carries a tweet's parent but never its replies")
	}
	focal, replies, err := RepliesFromPage(ctx, e.c, id)
	if err != nil {
		return nil, err
	}
	if focal != nil {
		total = focal.Metrics.Replies
	}
	return total, streamTweets(replies, TimelineOpts{Replies: true, Limit: limit}, emit)
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
		return needSession("likers")
	}
	return e.g.Likers(ctx, tweetID, limit, emit)
}
func (e *Engine) Retweeters(ctx context.Context, tweetID string, limit int, emit func(*User) error) error {
	if !e.canGraphQL() {
		return needSession("retweeters")
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

// Trends reads what is trending in one place, off surface 5. There is no tier
// to resolve: it is one route, it answers on the public bearer alone, and a
// guest token would only shrink the budget, so the engine passes it through.
func (e *Engine) Trends(ctx context.Context, woeid int64, limit int) ([]*Trend, error) {
	return Trends(ctx, e.c, woeid, limit)
}

// Places reads the woeid directory, filtered.
func (e *Engine) Places(ctx context.Context, query, country, placeType string, limit int) ([]*Place, error) {
	return Places(ctx, e.c, query, country, placeType, limit)
}

func (e *Engine) userID(ctx context.Context, ref string, isID bool, cap string) (string, error) {
	if !e.canGraphQL() {
		return "", needSession(cap)
	}
	return e.g.resolveUserID(ctx, ref, isID)
}
