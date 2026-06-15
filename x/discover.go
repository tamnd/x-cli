package x

import (
	"context"
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

// NodeKind is the type of a node the walk visits.
type NodeKind string

const (
	KindTweet NodeKind = "tweet"
	KindUser  NodeKind = "user"
)

// Edge names a link the walk can follow. The string is the public vocabulary:
// it is what the user types in --follow, what lands in the store's edges.kind
// column, and what a discovered node reports as the edge it arrived by.
type Edge string

const (
	// Tier-0 edges: reachable from the object itself, no token needed.
	EdgeAuthor   Edge = "author"   // tweet -> the account that wrote it
	EdgeQuoted   Edge = "quote"    // tweet -> the tweet it quotes
	EdgeRetweet  Edge = "retweet"  // tweet -> the original it retweets
	EdgeReply    Edge = "reply"    // tweet -> the tweet it replies to (the parent)
	EdgeMention  Edge = "mention"  // tweet -> each account it @-mentions
	EdgePinned   Edge = "pinned"   // user  -> their pinned tweet
	EdgeTimeline Edge = "timeline" // user  -> their recent tweets

	// Tier-1/2 edges: need the guest or session GraphQL tier.
	EdgeReplies   Edge = "replies"   // tweet -> the replies under it
	EdgeLiker     Edge = "liker"     // tweet -> accounts that liked it
	EdgeRetweeter Edge = "retweeter" // tweet -> accounts that retweeted it
	EdgeQuotedBy  Edge = "quotedby"  // tweet -> tweets that quote it (search-backed)
	EdgeFollowing Edge = "following" // user  -> accounts they follow
	EdgeFollowers Edge = "followers" // user  -> accounts that follow them
	EdgeLikes     Edge = "likes"     // user  -> tweets they liked
)

// allEdges is the full vocabulary, in a stable display order.
var allEdges = []Edge{
	EdgeAuthor, EdgeQuoted, EdgeRetweet, EdgeReply, EdgeMention, EdgePinned, EdgeTimeline,
	EdgeReplies, EdgeLiker, EdgeRetweeter, EdgeQuotedBy, EdgeFollowing, EdgeFollowers, EdgeLikes,
}

// knownEdges indexes allEdges for validation.
var knownEdges = func() map[Edge]bool {
	m := make(map[Edge]bool, len(allEdges))
	for _, e := range allEdges {
		m[e] = true
	}
	return m
}()

// Target reports the kind of node an edge leads to.
func (e Edge) Target() NodeKind {
	switch e {
	case EdgeAuthor, EdgeMention, EdgeLiker, EdgeRetweeter, EdgeFollowing, EdgeFollowers:
		return KindUser
	default:
		return KindTweet
	}
}

// needsGraphQL reports whether an edge can only be followed with a GraphQL tier.
// The Tier-0 edges are reachable straight from the syndication object; the rest
// need a guest token or the user's session.
func (e Edge) needsGraphQL() bool {
	switch e {
	case EdgeReplies, EdgeLiker, EdgeRetweeter, EdgeQuotedBy, EdgeFollowing, EdgeFollowers, EdgeLikes:
		return true
	default:
		return false
	}
}

// EdgeSet is a chosen set of edges to follow.
type EdgeSet map[Edge]bool

// Has reports whether the set contains e (a nil set contains nothing).
func (s EdgeSet) Has(e Edge) bool { return s[e] }

// List returns the set's edges in stable display order.
func (s EdgeSet) List() []Edge {
	var out []Edge
	for _, e := range allEdges {
		if s[e] {
			out = append(out, e)
		}
	}
	return out
}

// String renders the set as a comma-separated, ordered list.
func (s EdgeSet) String() string { return joinEdges(s.List()) }

// edgePresets are the named bundles --follow accepts in place of listing edges.
// They are the everyday intents: read what a post is made of, walk a thread,
// study who engaged, map an account's network, sweep a timeline, or take it all.
var edgePresets = map[string]EdgeSet{
	"content":    newEdgeSet(EdgeAuthor, EdgeQuoted, EdgeRetweet, EdgeReply, EdgeMention, EdgePinned),
	"thread":     newEdgeSet(EdgeAuthor, EdgeReply, EdgeReplies, EdgeQuoted),
	"engagement": newEdgeSet(EdgeLiker, EdgeRetweeter, EdgeQuotedBy),
	"network":    newEdgeSet(EdgeFollowing, EdgeFollowers),
	"timeline":   newEdgeSet(EdgeTimeline, EdgePinned, EdgeAuthor),
	"all":        newEdgeSet(allEdges...),
}

// presetNames lists the presets in a friendly order for help text.
var presetNames = []string{"content", "thread", "engagement", "network", "timeline", "all"}

func newEdgeSet(edges ...Edge) EdgeSet {
	s := make(EdgeSet, len(edges))
	for _, e := range edges {
		s[e] = true
	}
	return s
}

// DefaultEdges is what a walk follows when --follow is unset: a post's content.
// It stays entirely on Tier 0, so `x discover <tweet>` works with no token.
func DefaultEdges() EdgeSet { return edgePresets["content"].clone() }

func (s EdgeSet) clone() EdgeSet {
	out := make(EdgeSet, len(s))
	for e := range s {
		out[e] = true
	}
	return out
}

// EdgeHelp is the one-line catalogue of presets and edges for flag help and
// usage errors, so the names a user can type live in exactly one place.
func EdgeHelp() string {
	return "presets: " + strings.Join(presetNames, ",") + "; edges: " + joinEdges(allEdges)
}

// ParseEdges turns a --follow spec into an EdgeSet. The spec is a comma list of
// preset names and/or edge names ("content", "thread,engagement", "author,liker").
// An empty spec yields DefaultEdges. An unknown token is a usage error naming the
// catalogue, so a typo points the user at the real vocabulary.
func ParseEdges(spec string) (EdgeSet, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return DefaultEdges(), nil
	}
	set := EdgeSet{}
	for _, part := range strings.Split(spec, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "" {
			continue
		}
		if preset, ok := edgePresets[p]; ok {
			for e := range preset {
				set[e] = true
			}
			continue
		}
		e := Edge(p)
		if !knownEdges[e] {
			return nil, fmt.Errorf("unknown edge or preset %q (%s)", p, EdgeHelp())
		}
		set[e] = true
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("no edges selected (%s)", EdgeHelp())
	}
	return set, nil
}

func joinEdges(edges []Edge) string {
	ss := make([]string, len(edges))
	for i, e := range edges {
		ss[i] = string(e)
	}
	return strings.Join(ss, ",")
}

// Node is one object the walk reached, tagged with how it got there: the BFS
// depth, the edge it arrived by, and the endpoint of the node it came from.
// Exactly one of Tweet/User is set, matching Kind. Node is what Walk hands to
// its callback and what the CLI renders.
type Node struct {
	Kind   NodeKind `json:"kind"`
	Depth  int      `json:"depth"`
	Via    Edge     `json:"via,omitempty"`
	Parent string   `json:"parent,omitempty"`
	Tweet  *Tweet   `json:"tweet,omitempty"`
	User   *User    `json:"user,omitempty"`
}

// Endpoint is the node's stable identifier inside a walk: a tweet id, or a
// "@handle" for a user. It is what edges record as src/dst and what the store
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
	Depth  int     // hops to follow from each seed (0 = seeds only)
	Max    int     // stop after emitting this many nodes (0 = unlimited)
	Fanout int     // per-edge neighbor cap (0 = unlimited)
	Edges  EdgeSet // edges to follow (nil = DefaultEdges)

	// OnEdge, if set, is called for every edge the walk traverses, before the
	// neighbor is visited, with the two endpoints and the edge. The store sink
	// uses it to record the graph; it fires even for an already-visited neighbor
	// so the edge list stays complete.
	OnEdge func(src, dst string, edge Edge)

	// Note, if set, surfaces a one-line advisory (a skipped tier-only edge set, a
	// neighbor that could not be fetched). It never carries a fatal error.
	Note func(string)
}

// grapher is the slice of the engine the walker needs. *Engine satisfies it; a
// test supplies a fake. Every method matches *Engine exactly.
type grapher interface {
	CanGraphQL() bool
	Tweet(ctx context.Context, id string) (*Tweet, error)
	User(ctx context.Context, ref string, isID bool) (*User, error)
	Timeline(ctx context.Context, ref string, isID bool, o TimelineOpts, emit func(*Tweet) error) error
	Thread(ctx context.Context, id string, limit int, emit func(*Tweet) error) error
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
	via    Edge
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
// (opts.Max) is hit, emit returns an error, or a seed cannot be fetched. Edges
// that need a tier are dropped (with a Note) when none is configured, so a
// Tier-0 walk always produces what it can rather than erroring.
func (w *Walker) Walk(ctx context.Context, seeds []Seed, opts WalkOptions, emit func(*Node) error) error {
	edges := opts.Edges
	if edges == nil {
		edges = DefaultEdges()
	}
	if !w.g.CanGraphQL() {
		var dropped []Edge
		for _, e := range edges.List() {
			if e.needsGraphQL() {
				delete(edges, e)
				dropped = append(dropped, e)
			}
		}
		if len(dropped) > 0 && opts.Note != nil {
			opts.Note("skipping edges that need a tier (" + joinEdges(dropped) +
				"); pass --guest or run `x auth import` to follow them")
		}
		if opts.Depth > 0 && len(edges) == 0 {
			return needGraphQL("every selected edge")
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

		node, err := w.hydrate(ctx, f)
		if err != nil {
			if f.depth == 0 {
				return err // a seed we cannot fetch is fatal, like a single read
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
			return nil
		}
		if f.depth >= opts.Depth {
			continue
		}
		for _, nb := range w.neighbors(ctx, node, edges, opts) {
			if !visited[nb.key()] {
				queue = append(queue, nb)
			}
		}
	}
	return nil
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

// neighbors expands a node into its outbound frontier under the chosen edges,
// recording each edge via opts.OnEdge. The per-edge fanout caps every list read
// and the inline mention loop, so one hop can never page an account's whole
// follower graph unless the caller asked for it (Fanout 0).
func (w *Walker) neighbors(ctx context.Context, n *Node, edges EdgeSet, opts WalkOptions) []frontier {
	var out []frontier
	cap := opts.Fanout
	src := n.Endpoint()

	addTweet := func(via Edge, id string, t *Tweet) {
		dst := id
		if t != nil {
			dst = t.ID
		}
		if opts.OnEdge != nil {
			opts.OnEdge(src, dst, via)
		}
		out = append(out, frontier{kind: KindTweet, ref: id, depth: n.Depth + 1, via: via, parent: src, tweet: t})
	}
	addUser := func(via Edge, handle string, isID bool, u *User) {
		dst := userEndpoint(u, handle)
		if opts.OnEdge != nil {
			opts.OnEdge(src, dst, via)
		}
		out = append(out, frontier{kind: KindUser, ref: handle, isID: isID, depth: n.Depth + 1, via: via, parent: src, user: u})
	}

	switch n.Kind {
	case KindTweet:
		t := n.Tweet
		if edges.Has(EdgeAuthor) && t.Author != nil && t.Author.Username != "" {
			addUser(EdgeAuthor, t.Author.Username, false, t.Author)
		}
		if edges.Has(EdgeQuoted) && t.Quoted != nil && t.Quoted.ID != "" {
			addTweet(EdgeQuoted, t.Quoted.ID, t.Quoted)
		}
		if edges.Has(EdgeRetweet) && t.Retweeted != nil && t.Retweeted.ID != "" {
			addTweet(EdgeRetweet, t.Retweeted.ID, t.Retweeted)
		}
		if edges.Has(EdgeReply) && t.ReplyTo != "" {
			addTweet(EdgeReply, t.ReplyTo, nil)
		}
		if edges.Has(EdgeMention) {
			for i, m := range t.Entities.Mentions {
				if cap > 0 && i >= cap {
					break
				}
				addUser(EdgeMention, m, false, nil)
			}
		}
		if edges.Has(EdgeReplies) {
			w.note(opts, w.g.Thread(ctx, t.ID, cap, func(r *Tweet) error {
				if r.ID == t.ID {
					return nil // the focal tweet is the node itself
				}
				addTweet(EdgeReplies, r.ID, r)
				return nil
			}))
		}
		if edges.Has(EdgeLiker) {
			w.note(opts, w.g.Likers(ctx, t.ID, cap, func(u *User) error {
				addUser(EdgeLiker, u.Username, false, u)
				return nil
			}))
		}
		if edges.Has(EdgeRetweeter) {
			w.note(opts, w.g.Retweeters(ctx, t.ID, cap, func(u *User) error {
				addUser(EdgeRetweeter, u.Username, false, u)
				return nil
			}))
		}
		if edges.Has(EdgeQuotedBy) {
			q := SearchQuery{Raw: "quoted_tweet_id:" + t.ID, Product: "Latest", Limit: cap}
			w.note(opts, w.g.Search(ctx, q, func(r *Tweet) error {
				addTweet(EdgeQuotedBy, r.ID, r)
				return nil
			}))
		}
	case KindUser:
		u := n.User
		if edges.Has(EdgePinned) && u.PinnedTweet != "" {
			addTweet(EdgePinned, u.PinnedTweet, nil)
		}
		if edges.Has(EdgeTimeline) {
			w.note(opts, w.g.Timeline(ctx, u.Username, false, TimelineOpts{Limit: cap}, func(r *Tweet) error {
				addTweet(EdgeTimeline, r.ID, r)
				return nil
			}))
		}
		if edges.Has(EdgeFollowing) {
			w.note(opts, w.g.Following(ctx, u.Username, false, cap, func(f *User) error {
				addUser(EdgeFollowing, f.Username, false, f)
				return nil
			}))
		}
		if edges.Has(EdgeFollowers) {
			w.note(opts, w.g.Followers(ctx, u.Username, false, cap, func(f *User) error {
				addUser(EdgeFollowers, f.Username, false, f)
				return nil
			}))
		}
		if edges.Has(EdgeLikes) {
			w.note(opts, w.g.Likes(ctx, u.Username, false, cap, func(r *Tweet) error {
				addTweet(EdgeLikes, r.ID, r)
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
