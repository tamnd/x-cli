package x

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// These tests are offline: the walker talks to the grapher interface, so a fake
// in-memory graph exercises the breadth-first traversal, the edge vocabulary,
// the budgets, and the graceful Tier-0 degradation with no network.

func TestParseEdges(t *testing.T) {
	if got := DefaultEdges(); len(got) != 6 || !got.Has(EdgeAuthor) || got.Has(EdgeLiker) {
		t.Fatalf("DefaultEdges = %v, want the content preset", got)
	}
	cases := []struct {
		spec string
		want []Edge
		none []Edge
	}{
		{"", []Edge{EdgeAuthor, EdgeMention, EdgePinned}, []Edge{EdgeLiker}},
		{"content", []Edge{EdgeAuthor, EdgeQuoted}, []Edge{EdgeFollowing}},
		{"thread", []Edge{EdgeReplies, EdgeReply}, []Edge{EdgeFollowers}},
		{"engagement", []Edge{EdgeLiker, EdgeRetweeter, EdgeQuotedBy}, []Edge{EdgeAuthor}},
		{"network", []Edge{EdgeFollowing, EdgeFollowers}, []Edge{EdgeLiker}},
		{"author,liker", []Edge{EdgeAuthor, EdgeLiker}, []Edge{EdgeQuoted}},
		{"all", allEdges, nil},
	}
	for _, c := range cases {
		set, err := ParseEdges(c.spec)
		if err != nil {
			t.Fatalf("ParseEdges(%q): %v", c.spec, err)
		}
		for _, e := range c.want {
			if !set.Has(e) {
				t.Errorf("ParseEdges(%q) missing %s", c.spec, e)
			}
		}
		for _, e := range c.none {
			if set.Has(e) {
				t.Errorf("ParseEdges(%q) should not contain %s", c.spec, e)
			}
		}
	}
	if _, err := ParseEdges("bogus"); err == nil {
		t.Error("ParseEdges(bogus) = nil error, want a usage error")
	}
}

func TestEdgeMeta(t *testing.T) {
	if EdgeAuthor.Target() != KindUser || EdgeQuoted.Target() != KindTweet {
		t.Error("Edge.Target classified wrong")
	}
	if !EdgeLiker.needsGraphQL() || EdgeAuthor.needsGraphQL() {
		t.Error("Edge.needsGraphQL classified wrong")
	}
}

func TestParseSeed(t *testing.T) {
	cases := []struct {
		in   string
		kind NodeKind
		ref  string
	}{
		{"123", KindTweet, "123"},
		{"https://x.com/alice/status/5", KindTweet, "5"},
		{"alice", KindUser, "alice"},
		{"@Bob", KindUser, "Bob"},
		{"https://x.com/carol", KindUser, "carol"},
	}
	for _, c := range cases {
		s, err := ParseSeed(c.in)
		if err != nil || s.Kind != c.kind || s.Ref != c.ref {
			t.Errorf("ParseSeed(%q) = (%v %q, %v), want (%v %q)", c.in, s.Kind, s.Ref, err, c.kind, c.ref)
		}
	}
}

// fakeGraph is an in-memory grapher: a tiny corpus of tweets and users plus the
// list-shaped relations, keyed the way the engine keys them.
type fakeGraph struct {
	can        bool
	tweets     map[string]*Tweet
	users      map[string]*User // lowercased handle -> user
	thread     map[string][]*Tweet
	likers     map[string][]*User
	retweeters map[string][]*User
	followers  map[string][]*User
	following  map[string][]*User
	likes      map[string][]*Tweet
	timeline   map[string][]*Tweet
	search     map[string][]*Tweet
}

func (f *fakeGraph) CanGraphQL() bool { return f.can }

func (f *fakeGraph) Tweet(_ context.Context, id string) (*Tweet, error) {
	if t, ok := f.tweets[id]; ok {
		return t, nil
	}
	return nil, &NotFoundError{Kind: "tweet", Ref: id}
}

func (f *fakeGraph) User(_ context.Context, ref string, _ bool) (*User, error) {
	if u, ok := f.users[strings.ToLower(ref)]; ok {
		return u, nil
	}
	return nil, &NotFoundError{Kind: "user", Ref: ref}
}

func emitTweets(list []*Tweet, limit int, emit func(*Tweet) error) error {
	for i, t := range list {
		if limit > 0 && i >= limit {
			break
		}
		if err := emit(t); err != nil {
			return err
		}
	}
	return nil
}

func emitUsers(list []*User, limit int, emit func(*User) error) error {
	for i, u := range list {
		if limit > 0 && i >= limit {
			break
		}
		if err := emit(u); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeGraph) Timeline(_ context.Context, ref string, _ bool, o TimelineOpts, emit func(*Tweet) error) error {
	return emitTweets(f.timeline[strings.ToLower(ref)], o.Limit, emit)
}
func (f *fakeGraph) Thread(_ context.Context, id string, limit int, emit func(*Tweet) error) error {
	return emitTweets(f.thread[id], limit, emit)
}
func (f *fakeGraph) Likes(_ context.Context, ref string, _ bool, limit int, emit func(*Tweet) error) error {
	return emitTweets(f.likes[strings.ToLower(ref)], limit, emit)
}
func (f *fakeGraph) Search(_ context.Context, q SearchQuery, emit func(*Tweet) error) error {
	return emitTweets(f.search[q.Raw], q.Limit, emit)
}
func (f *fakeGraph) Followers(_ context.Context, ref string, _ bool, limit int, emit func(*User) error) error {
	return emitUsers(f.followers[strings.ToLower(ref)], limit, emit)
}
func (f *fakeGraph) Following(_ context.Context, ref string, _ bool, limit int, emit func(*User) error) error {
	return emitUsers(f.following[strings.ToLower(ref)], limit, emit)
}
func (f *fakeGraph) Likers(_ context.Context, tweetID string, limit int, emit func(*User) error) error {
	return emitUsers(f.likers[tweetID], limit, emit)
}
func (f *fakeGraph) Retweeters(_ context.Context, tweetID string, limit int, emit func(*User) error) error {
	return emitUsers(f.retweeters[tweetID], limit, emit)
}

// sampleGraph wires a small corpus: tweet 1 by alice quotes tweet 2 (by carol)
// and mentions bob; alice pins tweet 1 and follows bob; bob liked tweet 1.
func sampleGraph(can bool) *fakeGraph {
	alice := &User{ID: "a", Username: "alice", PinnedTweet: "1"}
	bob := &User{ID: "b", Username: "bob"}
	carol := &User{ID: "c", Username: "carol"}
	t2 := &Tweet{ID: "2", Text: "quoted", Author: carol}
	t1 := &Tweet{ID: "1", Text: "hello @bob", Author: alice, Quoted: t2,
		Entities: Entities{Mentions: []string{"bob"}}}
	return &fakeGraph{
		can:       can,
		tweets:    map[string]*Tweet{"1": t1, "2": t2},
		users:     map[string]*User{"alice": alice, "bob": bob, "carol": carol},
		likers:    map[string][]*User{"1": {bob}},
		following: map[string][]*User{"alice": {bob}},
	}
}

func collect(t *testing.T, g grapher, seeds []Seed, opts WalkOptions) ([]*Node, []string, error) {
	t.Helper()
	var nodes []*Node
	var edges []string
	opts.OnEdge = func(src, dst string, e Edge) { edges = append(edges, src+" -"+string(e)+"-> "+dst) }
	err := NewWalker(g).Walk(context.Background(), seeds, opts, func(n *Node) error {
		nodes = append(nodes, n)
		return nil
	})
	return nodes, edges, err
}

func keys(nodes []*Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.key()
	}
	return out
}

func TestWalkContent(t *testing.T) {
	g := sampleGraph(true)
	seeds := []Seed{{Kind: KindTweet, Ref: "1"}}
	nodes, edges, err := collect(t, g, seeds, WalkOptions{Depth: 1, Edges: DefaultEdges()})
	if err != nil {
		t.Fatal(err)
	}
	// BFS: the seed, then its content neighbors in author/quote/mention order.
	want := []string{"t:1", "u:alice", "t:2", "u:bob"}
	got := keys(nodes)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("nodes = %v, want %v", got, want)
	}
	if nodes[1].Via != EdgeAuthor || nodes[1].Parent != "1" {
		t.Errorf("author node = via %q parent %q, want author/1", nodes[1].Via, nodes[1].Parent)
	}
	wantEdges := map[string]bool{
		"1 -author-> @alice": true,
		"1 -quote-> 2":       true,
		"1 -mention-> @bob":  true,
	}
	for _, e := range edges {
		delete(wantEdges, e)
	}
	if len(wantEdges) != 0 {
		t.Errorf("missing edges %v (got %v)", wantEdges, edges)
	}
}

func TestWalkBudget(t *testing.T) {
	g := sampleGraph(true)
	nodes, _, err := collect(t, g, []Seed{{Kind: KindTweet, Ref: "1"}},
		WalkOptions{Depth: 3, Max: 2, Edges: DefaultEdges()})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("emitted %d nodes, want 2 (Max budget)", len(nodes))
	}
}

func TestWalkDedup(t *testing.T) {
	// alice pins tweet 1, so a depth-2 walk from tweet 1 reaches tweet 1 again
	// through the author's pin. It must be visited exactly once.
	g := sampleGraph(true)
	nodes, _, err := collect(t, g, []Seed{{Kind: KindTweet, Ref: "1"}},
		WalkOptions{Depth: 2, Edges: newEdgeSet(EdgeAuthor, EdgePinned)})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, n := range nodes {
		seen[n.key()]++
	}
	if seen["t:1"] != 1 {
		t.Fatalf("tweet 1 visited %d times, want 1", seen["t:1"])
	}
}

func TestWalkDegradeToEmpty(t *testing.T) {
	// engagement is all GraphQL edges; with no tier and depth>0 there is nothing
	// left to follow, which is an actionable need-auth error, not silence.
	g := sampleGraph(false)
	var notes []string
	set, _ := ParseEdges("engagement")
	_, _, err := collect(t, g, []Seed{{Kind: KindTweet, Ref: "1"}}, WalkOptions{
		Depth: 1, Edges: set, Note: func(s string) { notes = append(notes, s) },
	})
	var na *NeedAuthError
	if !errors.As(err, &na) {
		t.Fatalf("err = %v, want NeedAuthError", err)
	}
	if len(notes) == 0 {
		t.Error("expected a note about skipped tier-only edges")
	}
}

func TestWalkDegradePartial(t *testing.T) {
	// With --follow all and no tier, the Tier-0 edges still resolve while the
	// GraphQL ones are dropped with a note; the walk produces what it can.
	g := sampleGraph(false)
	var notes []string
	set, _ := ParseEdges("all")
	nodes, _, err := collect(t, g, []Seed{{Kind: KindTweet, Ref: "1"}}, WalkOptions{
		Depth: 1, Edges: set, Note: func(s string) { notes = append(notes, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) < 3 {
		t.Fatalf("emitted %d nodes, want the seed plus Tier-0 neighbors", len(nodes))
	}
	joined := strings.Join(notes, " ")
	if !strings.Contains(joined, "liker") || !strings.Contains(joined, "following") {
		t.Errorf("note did not list dropped edges: %q", joined)
	}
}

func TestWalkSeedNotFound(t *testing.T) {
	g := sampleGraph(true)
	_, _, err := collect(t, g, []Seed{{Kind: KindTweet, Ref: "404"}}, WalkOptions{Depth: 1})
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v, want NotFoundError for an unfetchable seed", err)
	}
}

func TestWalkUserSeedNetwork(t *testing.T) {
	g := sampleGraph(true)
	nodes, edges, err := collect(t, g, []Seed{{Kind: KindUser, Ref: "alice"}},
		WalkOptions{Depth: 1, Edges: newEdgeSet(EdgeFollowing, EdgePinned)})
	if err != nil {
		t.Fatal(err)
	}
	got := keys(nodes)
	// alice, then her pinned tweet and the account she follows.
	if got[0] != "u:alice" {
		t.Fatalf("first node = %q, want u:alice", got[0])
	}
	want := map[string]bool{"t:1": false, "u:bob": false}
	for _, k := range got[1:] {
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	if !want["t:1"] || !want["u:bob"] {
		t.Errorf("nodes = %v, want pinned tweet 1 and followed bob", got)
	}
	found := false
	for _, e := range edges {
		if e == "@alice -following-> @bob" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing following edge in %v", edges)
	}
}
