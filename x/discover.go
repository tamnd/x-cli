package x

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// discover.go is the breadth-first graph walker (spec §4.7). Every read in this
// package answers one question about one object; the walker chains them. From a
// seed tweet or user it follows the object's links (author, quoted/retweeted
// tweet, reply parent, mentions, then with a tier replies, likers, retweeters,
// followers, following, a timeline) and from each neighbor it follows theirs,
// hop by hop, until it runs out of depth or budget.
//
// It is engine-agnostic on purpose: Walk talks to the small grapher interface,
// not to *Engine directly, so the traversal is hermetically testable with a fake
// graph and *Engine is just the production grapher.

// NodeKind is the type of a node the walk visits. It is an alias rather than a
// type of its own because the walk's vocabulary is a subset of the one in
// identity.go, and two spellings of the same string would be one spelling too
// many. The constants live there, with the other fourteen kinds.
type NodeKind = string

// Hop names a move the walk can make. The string is the public vocabulary: it
// is what the user types in --follow and what a discovered node reports as the
// way it was reached.
//
// A hop is not an edge, and graph.go has the other one. An edge is a claim a
// record makes, with a direction fixed by the claim: @alice authored tweet 123.
// A hop is a direction of travel, and half of them run against the arrow. The
// liker hop goes from a tweet to the accounts that liked it, and the edge under
// it points the other way, from each account to the tweet. Collapsing the two
// vocabularies would cost the walk the distinctions it needs: author and
// timeline are both the authored edge, and reply and replies are both
// replies_to, but a walk that cannot tell them apart cannot climb a thread
// without also descending it.
type Hop string

const (
	// Tier-0 hops: reachable from the object itself, no token needed.
	HopAuthor   Hop = "author"   // tweet -> the account that wrote it
	HopQuoted   Hop = "quote"    // tweet -> the tweet it quotes
	HopRetweet  Hop = "retweet"  // tweet -> the original it retweets
	HopReply    Hop = "reply"    // tweet -> the tweet it replies to (the parent)
	HopMention  Hop = "mention"  // tweet -> each account it @-mentions
	HopPinned   Hop = "pinned"   // user  -> their pinned tweet
	HopTimeline Hop = "timeline" // user  -> their recent tweets

	// Tier-1/2 hops: need the guest or session GraphQL tier.
	HopReplies   Hop = "replies"   // tweet -> the replies under it
	HopLiker     Hop = "liker"     // tweet -> accounts that liked it
	HopRetweeter Hop = "retweeter" // tweet -> accounts that retweeted it
	HopQuotedBy  Hop = "quotedby"  // tweet -> tweets that quote it (search-backed)
	HopFollowing Hop = "following" // user  -> accounts they follow
	HopFollowers Hop = "followers" // user  -> accounts that follow them
	HopLikes     Hop = "likes"     // user  -> tweets they liked
)

// allHops is the full vocabulary, in a stable display order.
var allHops = []Hop{
	HopAuthor, HopQuoted, HopRetweet, HopReply, HopMention, HopPinned, HopTimeline,
	HopReplies, HopLiker, HopRetweeter, HopQuotedBy, HopFollowing, HopFollowers, HopLikes,
}

// knownHops indexes allHops for validation.
var knownHops = func() map[Hop]bool {
	m := make(map[Hop]bool, len(allHops))
	for _, e := range allHops {
		m[e] = true
	}
	return m
}()

// Target reports the kind of node a hop leads to.
func (e Hop) Target() NodeKind {
	switch e {
	case HopAuthor, HopMention, HopLiker, HopRetweeter, HopFollowing, HopFollowers:
		return KindUser
	default:
		return KindTweet
	}
}

// needsSession reports whether a hop can only be followed with the user's own
// session.
//
// The tier-0 hops are reachable straight from the syndication object or off the
// status page; the rest are GraphQL operations X denies a guest token, so a
// guest token buys none of them and the message says session.
//
// HopReplies is not on this list, and used to be. The replies under a tweet
// come off the status page now, at tier 0, so dropping that hop from an
// anonymous walk cost the walk its whole downward half for nothing.
func (e Hop) needsSession() bool {
	switch e {
	case HopLiker, HopRetweeter, HopQuotedBy, HopFollowing, HopFollowers, HopLikes:
		return true
	default:
		return false
	}
}

// HopSet is a chosen set of hops to follow.
type HopSet map[Hop]bool

// Has reports whether the set contains e (a nil set contains nothing).
func (s HopSet) Has(e Hop) bool { return s[e] }

// List returns the set's hops in stable display order.
func (s HopSet) List() []Hop {
	var out []Hop
	for _, e := range allHops {
		if s[e] {
			out = append(out, e)
		}
	}
	return out
}

// String renders the set as a comma-separated, ordered list.
func (s HopSet) String() string { return joinHops(s.List()) }

// hopPresets are the named bundles --follow accepts in place of listing hops.
// They are the everyday intents: read what a post is made of, walk a thread,
// study who engaged, map an account's network, sweep a timeline, or take it all.
var hopPresets = map[string]HopSet{
	"content":    newHopSet(HopAuthor, HopQuoted, HopRetweet, HopReply, HopMention, HopPinned),
	"thread":     newHopSet(HopAuthor, HopReply, HopReplies, HopQuoted),
	"engagement": newHopSet(HopLiker, HopRetweeter, HopQuotedBy),
	"network":    newHopSet(HopFollowing, HopFollowers),
	"timeline":   newHopSet(HopTimeline, HopPinned, HopAuthor),
	"all":        newHopSet(allHops...),
}

// presetNames lists the presets in a friendly order for help text.
var presetNames = []string{"content", "thread", "engagement", "network", "timeline", "all"}

func newHopSet(hops ...Hop) HopSet {
	s := make(HopSet, len(hops))
	for _, e := range hops {
		s[e] = true
	}
	return s
}

// DefaultHops is what a walk follows when --follow is unset: a post's content.
// It stays entirely on Tier 0, so `x discover <tweet>` works with no token.
func DefaultHops() HopSet { return hopPresets["content"].clone() }

func (s HopSet) clone() HopSet {
	out := make(HopSet, len(s))
	for e := range s {
		out[e] = true
	}
	return out
}

// HopHelp is the one-line catalogue of presets and hops for flag help and
// usage errors, so the names a user can type live in exactly one place.
func HopHelp() string {
	return "presets: " + strings.Join(presetNames, ",") + "; hops: " + joinHops(allHops)
}

// ParseHops turns a --follow spec into a HopSet. The spec is a comma list of
// preset names and/or hop names ("content", "thread,engagement", "author,liker").
// An empty spec yields DefaultHops. An unknown token is a usage error naming the
// catalogue, so a typo points the user at the real vocabulary.
func ParseHops(spec string) (HopSet, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return DefaultHops(), nil
	}
	set := HopSet{}
	for _, part := range strings.Split(spec, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "" {
			continue
		}
		if preset, ok := hopPresets[p]; ok {
			for e := range preset {
				set[e] = true
			}
			continue
		}
		e := Hop(p)
		if !knownHops[e] {
			return nil, fmt.Errorf("unknown hop or preset %q (%s)", p, HopHelp())
		}
		set[e] = true
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("no hops selected (%s)", HopHelp())
	}
	return set, nil
}

func joinHops(hops []Hop) string {
	ss := make([]string, len(hops))
	for i, e := range hops {
		ss[i] = string(e)
	}
	return strings.Join(ss, ",")
}

// Node is one object the walk reached, tagged with how it got there: the BFS
// depth, the hop it arrived by, and the endpoint of the node it came from.
// Exactly one of Tweet/User is set, matching Kind. Node is what Walk hands to
// its callback and what the CLI renders.
type Node struct {
	Kind   NodeKind `json:"kind"`
	Depth  int      `json:"depth"`
	Via    Hop      `json:"via,omitempty"`
	Parent string   `json:"parent,omitempty"`
	Tweet  *Tweet   `json:"tweet,omitempty"`
	User   *User    `json:"user,omitempty"`
}

// Endpoint is the node's stable identifier inside a walk: a tweet id, or a
// "@handle" for a user. It is what hops record as src/dst and what the store
// keys a queue row by.
func (n *Node) Endpoint() string {
	if n.Kind == KindTweet {
		if n.Tweet != nil {
			return n.Tweet.ID
		}
		return ""
	}
	if n.User != nil {
		return userEndpoint(n.User, "")
	}
	return ""
}

// key is the dedup key for a hydrated node: tweets by id, users by lowercased
// handle (the same handle reached via a mention and via authorship collapse to
// one node), falling back to the numeric id when a user has no handle.
func (n *Node) key() string {
	if n.Kind == KindTweet {
		return "t:" + n.Tweet.ID
	}
	if n.User != nil && n.User.Username != "" {
		return "u:" + strings.ToLower(n.User.Username)
	}
	if n.User != nil {
		return "u#" + n.User.ID
	}
	return "u:?"
}

func userEndpoint(u *User, fallbackHandle string) string {
	if u != nil && u.Username != "" {
		return "@" + u.Username
	}
	if fallbackHandle != "" {
		return "@" + strings.TrimPrefix(fallbackHandle, "@")
	}
	if u != nil && u.ID != "" {
		return "#" + u.ID
	}
	return ""
}

// Seed is a parsed starting point for a walk.
type Seed struct {
	Kind NodeKind
	Ref  string // tweet id, or user handle / numeric id
	IsID bool   // for a user seed: Ref is a numeric account id
}

// ParseSeed classifies a raw reference into a Seed. A bare number or a status
// URL is a tweet (matching the rest of the CLI, where a bare number is a tweet
// id); anything else is read as a user handle or profile URL.
func ParseSeed(ref string) (Seed, error) {
	if id, err := ParseTweetRef(ref); err == nil {
		return Seed{Kind: KindTweet, Ref: id}, nil
	}
	h, isID, err := ParseUserRef(ref, true)
	if err != nil {
		return Seed{}, fmt.Errorf("not a tweet or user reference: %q", ref)
	}
	return Seed{Kind: KindUser, Ref: h, IsID: isID}, nil
}

// WalkOptions tunes a traversal.
type WalkOptions struct {
	Depth  int    // hops to follow from each seed (0 = seeds only)
	Max    int    // stop after emitting this many nodes (0 = unlimited)
	Fanout int    // per-hop neighbor cap (0 = unlimited)
	Hops   HopSet // hops to follow (nil = DefaultHops)

	// OnHop, if set, is called for every hop the walk traverses, before the
	// neighbor is visited, with the two endpoints, the hop, and the neighbor's
	// own provenance when the walk already holds the neighbor's record. The store
	// sink uses it to record the graph; it fires even for an already-visited
	// neighbor so the hop list stays complete.
	//
	// The meta is the listing's, not the source node's, which is what makes a
	// liked or follows edge nameable: the URL that asserted it is the one the
	// list came back on. It is nil for a hop that names a neighbor without
	// fetching it, like a reply parent or a mention.
	OnHop func(src, dst string, hop Hop, meta *Meta)

	// Note, if set, surfaces a one-line advisory (a skipped tier-only hop set, a
	// neighbor that could not be fetched). It never carries a fatal error.
	Note func(string)

	// Budget caps the upstream requests the walk may spend (0 = no cap). Doc 04
	// section 3 asks the crawler to count before it spends, and this is the
	// count: the walk checks what it has already put on the wire before pulling
	// the next node rather than discovering the ceiling by hitting it.
	//
	// It is requests, not nodes, because a node can cost anywhere from nothing
	// (a list read handed it over whole) to one per hop, and the number the rate
	// limits are written in is requests.
	Budget int

	// Left, if set, is called once when the walk stops with work still queued,
	// with how many nodes were never expanded and why. A partial crawl and a
	// finished one are different results, and a tool that does not say which one
	// it produced is lying by omission.
	Left func(n int, why string)
}

// grapher is the slice of the engine the walker needs. *Engine satisfies it; a
// test supplies a fake. Every method matches *Engine exactly.
type grapher interface {
	HasSession() bool
	Spent() int
	Tweet(ctx context.Context, id string) (*Tweet, error)
	User(ctx context.Context, ref string, isID bool) (*User, error)
	Timeline(ctx context.Context, ref string, isID bool, o TimelineOpts, emit func(*Tweet) error) error
	Thread(ctx context.Context, id string, limit int, emit func(*Tweet) error) error
	Replies(ctx context.Context, id string, limit int, emit func(*Tweet) error) (*int, error)
	Followers(ctx context.Context, ref string, isID bool, limit int, emit func(*User) error) error
	Following(ctx context.Context, ref string, isID bool, limit int, emit func(*User) error) error
	Likers(ctx context.Context, tweetID string, limit int, emit func(*User) error) error
	Retweeters(ctx context.Context, tweetID string, limit int, emit func(*User) error) error
	Likes(ctx context.Context, ref string, isID bool, limit int, emit func(*Tweet) error) error
	Search(ctx context.Context, q SearchQuery, emit func(*Tweet) error) error
}

// Walker performs the breadth-first traversal over a grapher.
type Walker struct{ g grapher }

// NewWalker builds a Walker over any grapher (the engine in production, a fake in
// tests).
func NewWalker(g grapher) *Walker { return &Walker{g: g} }

// Walk runs the engine's traversal. It is the production entry point: it builds a
// Walker over the engine and walks the seeds. See Walker.Walk.
func (e *Engine) Walk(ctx context.Context, seeds []Seed, opts WalkOptions, emit func(*Node) error) error {
	return NewWalker(e).Walk(ctx, seeds, opts, emit)
}

// frontier is a queued, possibly-not-yet-hydrated node. List reads (a timeline,
// the followers) hand back fully built entities, so those rides carry tweet/user
// already and skip the per-pop fetch; a mention or a reply parent carries only a
// reference and is fetched when it is popped.
type frontier struct {
	kind   NodeKind
	ref    string
	isID   bool
	depth  int
	via    Hop
	parent string
	tweet  *Tweet
	user   *User
}

func (f frontier) key() string {
	if f.kind == KindTweet {
		if f.tweet != nil {
			return "t:" + f.tweet.ID
		}
		return "t:" + f.ref
	}
	if f.user != nil && f.user.Username != "" {
		return "u:" + strings.ToLower(f.user.Username)
	}
	return "u:" + strings.ToLower(strings.TrimPrefix(f.ref, "@"))
}

// Walk visits the seeds and their links in breadth-first order, calling emit for
// each node as it is reached. It returns when the queue drains, the node budget
// (opts.Max) is hit, emit returns an error, or a seed cannot be fetched. Hops
// that need a tier are dropped (with a Note) when none is configured, so a
// Tier-0 walk always produces what it can rather than erroring.
func (w *Walker) Walk(ctx context.Context, seeds []Seed, opts WalkOptions, emit func(*Node) error) error {
	hops := opts.Hops
	if hops == nil {
		hops = DefaultHops()
	}
	if !w.g.HasSession() {
		var dropped []Hop
		for _, e := range hops.List() {
			if e.needsSession() {
				delete(hops, e)
				dropped = append(dropped, e)
			}
		}
		if len(dropped) > 0 && opts.Note != nil {
			opts.Note("skipping hops that need your own session (" + joinHops(dropped) +
				"); run `x auth import` to follow them")
		}
		if opts.Depth > 0 && len(hops) == 0 {
			return needSession("every selected hop")
		}
	}

	visited := map[string]bool{}
	queue := make([]frontier, 0, len(seeds))
	for _, s := range seeds {
		queue = append(queue, frontier{kind: s.Kind, ref: s.Ref, isID: s.IsID})
	}

	emitted := 0
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		f := queue[0]
		queue = queue[1:]
		if visited[f.key()] {
			continue
		}
		visited[f.key()] = true

		// The budget is checked before the node is pulled, not after, so the
		// walk stops one short of the ceiling rather than one over it.
		if opts.Budget > 0 && f.needsFetch() && w.g.Spent() >= opts.Budget {
			left(opts, len(queue)+1, fmt.Sprintf("request budget of %d spent", opts.Budget))
			return nil
		}

		node, err := w.hydrate(ctx, f)
		if err != nil {
			if f.depth == 0 {
				return err // a seed we cannot fetch is fatal, like a single read
			}
			// An empty window is not this node's problem, it is every node's
			// problem, so the walk stops instead of grinding through the rest of
			// the queue collecting the same error. Exit code 5, and the error
			// names the bucket and when it comes back.
			var rl *RateLimitedError
			if errors.As(err, &rl) {
				left(opts, len(queue)+1, "rate limited")
				return err
			}
			if opts.Note != nil {
				opts.Note(fmt.Sprintf("skip %s %s: %v", f.kind, f.ref, err))
			}
			continue
		}
		visited[node.key()] = true // collapse handle/id aliases of the same node

		if err := emit(node); err != nil {
			return err
		}
		emitted++
		if opts.Max > 0 && emitted >= opts.Max {
			left(opts, len(queue), fmt.Sprintf("node budget of %d reached", opts.Max))
			return nil
		}
		if f.depth >= opts.Depth {
			continue
		}
		for _, nb := range w.neighbors(ctx, node, hops, opts) {
			if !visited[nb.key()] {
				queue = append(queue, nb)
			}
		}
	}
	return nil
}

// left reports an early stop once, and says nothing when the queue is empty,
// because a walk that ran out of graph did not stop early.
func left(opts WalkOptions, n int, why string) {
	if opts.Left != nil && n > 0 {
		opts.Left(n, why)
	}
}

// needsFetch reports whether popping this item costs a request. A list read
// hands back whole records, so most of a wide walk is free.
func (f frontier) needsFetch() bool {
	return (f.kind == KindTweet && f.tweet == nil) || (f.kind == KindUser && f.user == nil)
}

// hydrate turns a frontier item into a Node, fetching the object when the item
// did not already carry it.
func (w *Walker) hydrate(ctx context.Context, f frontier) (*Node, error) {
	n := &Node{Kind: f.kind, Depth: f.depth, Via: f.via, Parent: f.parent, Tweet: f.tweet, User: f.user}
	switch f.kind {
	case KindTweet:
		if n.Tweet == nil {
			t, err := w.g.Tweet(ctx, f.ref)
			if err != nil {
				return nil, err
			}
			n.Tweet = t
		}
	case KindUser:
		if n.User == nil {
			u, err := w.g.User(ctx, f.ref, f.isID)
			if err != nil {
				return nil, err
			}
			n.User = u
		}
	}
	return n, nil
}

// neighbors expands a node into its outbound frontier under the chosen hops,
// recording each hop via opts.OnHop. The per-hop fanout caps every list read
// and the inline mention loop, so one hop can never page an account's whole
// follower graph unless the caller asked for it (Fanout 0).
func (w *Walker) neighbors(ctx context.Context, n *Node, hops HopSet, opts WalkOptions) []frontier {
	var out []frontier
	cap := opts.Fanout
	src := n.Endpoint()

	addTweet := func(via Hop, id string, t *Tweet) {
		dst := id
		var meta *Meta
		if t != nil {
			dst, meta = t.ID, &t.Meta
		}
		if opts.OnHop != nil {
			opts.OnHop(src, dst, via, meta)
		}
		out = append(out, frontier{kind: KindTweet, ref: id, depth: n.Depth + 1, via: via, parent: src, tweet: t})
	}
	addUser := func(via Hop, handle string, isID bool, u *User) {
		dst := userEndpoint(u, handle)
		var meta *Meta
		if u != nil {
			meta = &u.Meta
		}
		if opts.OnHop != nil {
			opts.OnHop(src, dst, via, meta)
		}
		out = append(out, frontier{kind: KindUser, ref: handle, isID: isID, depth: n.Depth + 1, via: via, parent: src, user: u})
	}

	switch n.Kind {
	case KindTweet:
		t := n.Tweet
		if hops.Has(HopAuthor) && t.Author != nil && t.Author.Username != "" {
			addUser(HopAuthor, t.Author.Username, false, t.Author)
		}
		if hops.Has(HopQuoted) && t.Quoted != nil && t.Quoted.ID != "" {
			addTweet(HopQuoted, t.Quoted.ID, t.Quoted)
		}
		if hops.Has(HopRetweet) && t.Retweeted != nil && t.Retweeted.ID != "" {
			addTweet(HopRetweet, t.Retweeted.ID, t.Retweeted)
		}
		if hops.Has(HopReply) && t.ReplyTo != "" {
			addTweet(HopReply, t.ReplyTo, nil)
		}
		if hops.Has(HopMention) {
			for i, m := range t.Entities.Mentions {
				if cap > 0 && i >= cap {
					break
				}
				addUser(HopMention, m, false, nil)
			}
		}
		if hops.Has(HopReplies) {
			// Replies and not Thread. Thread also walks upward, and an ancestor
			// arriving on the `replies` hop would be a hop that points the
			// wrong way: the upward hop is HopReply, and it is already followed
			// from in_reply_to above.
			_, err := w.g.Replies(ctx, t.ID, cap, func(r *Tweet) error {
				addTweet(HopReplies, r.ID, r)
				return nil
			})
			w.note(opts, err)
		}
		if hops.Has(HopLiker) {
			w.note(opts, w.g.Likers(ctx, t.ID, cap, func(u *User) error {
				addUser(HopLiker, u.Username, false, u)
				return nil
			}))
		}
		if hops.Has(HopRetweeter) {
			w.note(opts, w.g.Retweeters(ctx, t.ID, cap, func(u *User) error {
				addUser(HopRetweeter, u.Username, false, u)
				return nil
			}))
		}
		if hops.Has(HopQuotedBy) {
			q := SearchQuery{Raw: "quoted_tweet_id:" + t.ID, Product: "Latest", Limit: cap}
			w.note(opts, w.g.Search(ctx, q, func(r *Tweet) error {
				addTweet(HopQuotedBy, r.ID, r)
				return nil
			}))
		}
	case KindUser:
		u := n.User
		if hops.Has(HopPinned) && u.PinnedTweet != "" {
			addTweet(HopPinned, u.PinnedTweet, nil)
		}
		if hops.Has(HopTimeline) {
			w.note(opts, w.g.Timeline(ctx, u.Username, false, TimelineOpts{Limit: cap}, func(r *Tweet) error {
				addTweet(HopTimeline, r.ID, r)
				return nil
			}))
		}
		if hops.Has(HopFollowing) {
			w.note(opts, w.g.Following(ctx, u.Username, false, cap, func(f *User) error {
				addUser(HopFollowing, f.Username, false, f)
				return nil
			}))
		}
		if hops.Has(HopFollowers) {
			w.note(opts, w.g.Followers(ctx, u.Username, false, cap, func(f *User) error {
				addUser(HopFollowers, f.Username, false, f)
				return nil
			}))
		}
		if hops.Has(HopLikes) {
			w.note(opts, w.g.Likes(ctx, u.Username, false, cap, func(r *Tweet) error {
				addTweet(HopLikes, r.ID, r)
				return nil
			}))
		}
	}
	return out
}

// note surfaces a non-fatal list-read failure (a protected account, a transient
// rate limit) as an advisory and keeps the walk going on the rest of the graph.
func (w *Walker) note(opts WalkOptions, err error) {
	if err != nil && opts.Note != nil {
		opts.Note(err.Error())
	}
}

// CanGraphQL reports whether a GraphQL tier is available. It exports the internal
// check so the walker (and any other consumer) can read the engine's capability.
func (e *Engine) CanGraphQL() bool { return e.canGraphQL() }

// Spent is how many requests this engine's client has put on the wire, which is
// what the walk's --budget is counted in.
func (e *Engine) Spent() int { return e.c.Spent() }

// HasSession reports whether the user's own session is configured. It is what
// the walker asks, because every hop the walker can be denied is denied to a
// guest token too, and a --tier ceiling below the session tier means the walker
// should not count on one either.
func (e *Engine) HasSession() bool {
	return e.cfg.HasSession() && e.cfg.Tier != "syndication" && e.cfg.Tier != "web" && e.cfg.Tier != "guest"
}
