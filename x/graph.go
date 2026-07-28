package x

import (
	"sort"
	"strconv"
	"strings"
)

// graph.go turns records into edges (spec 3003 doc 04 section 2).
//
// An edge is one claim a record makes about two nodes, carrying where the claim
// came from. Extraction is pure: no network, no cache, no config. You hand it a
// record you already have and it tells you what that record says. That is what
// makes the interesting property in doc 04 true, that reading one tweet with no
// credential produces a lot of graph, testable without a socket.
//
// Do not confuse an edge with a hop. discover.go walks hops, which are
// directions of travel; this file reads edges, which are claims. The liker hop
// goes from a tweet to the people who liked it and the edge under it points from
// each person to the tweet.

// Predicate is one name in the edge vocabulary.
//
// The vocabulary is closed. A predicate that is not on this list is a bug rather
// than an extension point, because a graph whose predicate set grows per surface
// is a graph nobody can write a query against.
type Predicate string

const (
	// From a tweet, or from the account that wrote it. All of these come out at
	// tier 0, which is the reason a credential-free read is worth anything.
	PredAuthored       Predicate = "authored"        // user  -> tweet
	PredRepliesTo      Predicate = "replies_to"      // tweet -> tweet
	PredQuotes         Predicate = "quotes"          // tweet -> tweet
	PredReposts        Predicate = "reposts"         // tweet -> tweet
	PredInConversation Predicate = "in_conversation" // tweet -> conversation
	PredMentions       Predicate = "mentions"        // tweet -> user
	PredTagged         Predicate = "tagged"          // tweet -> hashtag
	PredTaggedSymbol   Predicate = "tagged_symbol"   // tweet -> cashtag
	PredLinksTo        Predicate = "links_to"        // tweet -> link
	PredHasMedia       Predicate = "has_media"       // tweet -> media
	PredHasPoll        Predicate = "has_poll"        // tweet -> poll
	PredHasCard        Predicate = "has_card"        // tweet -> card
	PredHasNote        Predicate = "has_note"        // tweet -> note
	PredPinned         Predicate = "pinned"          // user  -> tweet
	PredWebsite        Predicate = "website"         // user  -> link

	// From a listing rather than from an object: who follows whom, who liked
	// what. A record on its own never asserts these, so they arrive from the
	// walker, and every one of them needs the user's own session.
	PredFollows    Predicate = "follows"     // user -> user
	PredFollowedBy Predicate = "followed_by" // user -> user
	PredLiked      Predicate = "liked"       // user -> tweet
	PredReposted   Predicate = "reposted"    // user -> tweet
	PredMemberOf   Predicate = "member_of"   // user -> list
	PredOwns       Predicate = "owns"        // user -> list

	// From a Space. hosted is doc 04's; spoke_in is not, and it is here because
	// the Space work in M6 found that a finished Space keeps the roster of who
	// held the microphone and forgets everyone who listened. The speakers are
	// the record's most interesting half and the vocabulary had no word for
	// them, so it has one now.
	PredHosted  Predicate = "hosted"   // user -> space
	PredSpokeIn Predicate = "spoke_in" // user -> space

	// From the trends directory.
	PredTrendingIn Predicate = "trending_in" // trend -> place
	PredChildOf    Predicate = "child_of"    // place -> place
)

// Predicates is the whole vocabulary, in the order doc 04 lists it.
var Predicates = []Predicate{
	PredAuthored, PredRepliesTo, PredQuotes, PredReposts, PredInConversation,
	PredMentions, PredTagged, PredTaggedSymbol, PredLinksTo, PredHasMedia,
	PredHasPoll, PredHasCard, PredHasNote, PredPinned, PredWebsite,
	PredFollows, PredFollowedBy, PredLiked, PredReposted, PredMemberOf, PredOwns,
	PredHosted, PredSpokeIn, PredTrendingIn, PredChildOf,
}

var knownPredicates = func() map[Predicate]bool {
	m := make(map[Predicate]bool, len(Predicates))
	for _, p := range Predicates {
		m[p] = true
	}
	return m
}()

// Known reports whether p is in the vocabulary.
func (p Predicate) Known() bool { return knownPredicates[p] }

// Functional reports whether a node can have at most one of this predicate
// leaving it. A tweet replies to one tweet and sits in one conversation; a
// tweet mentions as many accounts as it likes.
//
// It is here for one reason: it is what tells a duplicate from a contradiction.
// Two surfaces both saying tweet 123 mentions @bob is agreement. Two surfaces
// disagreeing about which tweet 123 replies to is a fact about X, and Conflicts
// finds it because replies_to is on this list.
func (p Predicate) Functional() bool {
	switch p {
	case PredRepliesTo, PredQuotes, PredReposts, PredInConversation,
		PredHasPoll, PredHasCard, PredHasNote, PredPinned, PredWebsite:
		return true
	}
	return false
}

// InverseFunctional reports whether a node can be on the receiving end of this
// predicate at most once. One tweet has one author, however many tweets an
// account writes, so a disagreement about authorship is only visible from this
// side.
func (p Predicate) InverseFunctional() bool {
	return p == PredAuthored
}

// Edge is one claim with its provenance.
//
// Surface is not in doc 04's struct, and it has to be: section 2.3 ranks trust
// as tier 2 over tier 1 over surface 8 over surface 2 over surface 1, and the
// bottom three of those are all tier 0. A struct carrying only the tier cannot
// implement its own ranking rule.
type Edge struct {
	From      string    `json:"from"`
	Predicate Predicate `json:"predicate"`
	To        string    `json:"to"`

	// Source is the URL that was read to learn this, and Surface which of doc
	// 01's eight it is. Tier is what that surface costs: 0 no credential, 1 a
	// guest token, 2 the user's own session.
	Source  string `json:"source,omitempty"`
	Surface int    `json:"surface,omitempty"`
	Tier    int    `json:"tier"`
}

// tier0Rank orders the credential-free surfaces by how complete their payload
// is, which is doc 04's rule of thumb: when two of them disagree about a fact,
// the more modern shape is the more likely to be current.
//
// Doc 04 ranks three of them, s8 over s2 over s1. There are five, and the other
// two are ranked here and not there. Surface 5 publishes a whole v1.1 record and
// sits with the endpoints that publish records; oembed publishes a blob of HTML
// with a handful of fields around it. The media CDN is not ranked at all,
// because it serves bytes and asserts nothing.
var tier0Rank = map[int]int{8: 5, 2: 4, 5: 3, 1: 2, 3: 1}

// Trust scores an edge's provenance so two claims about the same thing can be
// ordered. Any tier beats any tier 0, and within tier 0 the ranking is by
// surface. It is only ever compared, never published.
func (e Edge) Trust() int {
	if e.Tier > 0 {
		return 100 + e.Tier
	}
	return tier0Rank[e.Surface]
}

// Edges reads the edges a record asserts. It is pure, and it returns nil rather
// than an error for a kind that has none, because "this record says nothing
// about the graph" is an answer and not a failure.
//
// A record that carries another record whole is walked into: a quoted tweet, a
// retweeted one, a Space's creator. That is where the leverage is. Surface 1
// expands the parent and the quoted tweet in full, so one anonymous request
// yields the edges of three tweets rather than one.
func Edges(rec any) []Edge {
	switch r := rec.(type) {
	case *Tweet:
		return tweetEdges(r)
	case *User:
		return userEdges(r)
	case *Space:
		return spaceEdges(r)
	case *List:
		return listEdges(r)
	case *Trend:
		return trendEdges(r)
	case *Place:
		return placeEdges(r)
	case *Node:
		if r == nil {
			return nil
		}
		if r.Tweet != nil {
			return tweetEdges(r.Tweet)
		}
		return userEdges(r.User)
	}
	return nil
}

// emitter accumulates edges under one record's provenance, so every call site
// says what it found rather than repeating where it came from.
type emitter struct {
	meta *Meta
	out  []Edge
}

func (e *emitter) add(from string, p Predicate, to string) {
	if from == "" || to == "" {
		return
	}
	src, surface, tier := provenance(e.meta)
	e.out = append(e.out, Edge{From: from, Predicate: p, To: to, Source: src, Surface: surface, Tier: tier})
}

// provenance picks the surface an edge is credited to.
//
// One surface is the easy case. A merged record is not: several surfaces
// contributed and nothing on the record says which of them supplied a given
// link, so the highest-ranked contributor is credited. That is the honest
// reading of "this claim survived the merge", and it is deliberately generous
// rather than precise: crediting the thinnest contributor would make a merged
// record lose arguments it should win.
func provenance(m *Meta) (source string, surface, tier int) {
	if m == nil {
		return "", 0, 0
	}
	best := -1
	for i, code := range m.Surfaces {
		n := surfaceNum(code)
		if n == 0 {
			continue
		}
		if best < 0 || rankOf(n) > rankOf(surfaceNum(m.Surfaces[best])) {
			best = i
		}
	}
	if best < 0 {
		return lastStr(m.Sources), 0, m.Tier
	}
	surface = surfaceNum(m.Surfaces[best])
	if best < len(m.Sources) {
		source = m.Sources[best]
	} else {
		source = lastStr(m.Sources)
	}
	return source, surface, m.Tier
}

// rankOf orders surfaces for the merged-record case, tiers included, so a
// session read outranks an anonymous one even before the edges are compared.
func rankOf(surface int) int {
	if t := surfaceTier(surface); t > 0 {
		return 100 + t
	}
	return tier0Rank[surface]
}

func lastStr(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[len(ss)-1]
}

// tweetEdges is where most of the graph comes from.
func tweetEdges(t *Tweet) []Edge {
	if t == nil || t.ID == "" {
		return nil
	}
	e := &emitter{meta: &t.Meta}
	self := URI(KindTweet, t.ID)

	if t.Author != nil && t.Author.Username != "" {
		e.add(userURI(t.Author.Username), PredAuthored, self)
	}
	if t.ReplyTo != "" {
		parent := URI(KindTweet, t.ReplyTo)
		e.add(self, PredRepliesTo, parent)
		// The account that wrote the parent, which surface 1 names even when it
		// does not expand the parent itself. It is one free edge into a tweet
		// nobody has fetched yet, and it is what makes a thread walkable
		// upwards from any reply.
		if t.ReplyToUser != "" {
			e.add(userURI(t.ReplyToUser), PredAuthored, parent)
		}
	}
	if t.ConversationID != "" {
		e.add(self, PredInConversation, URI(KindConversation, t.ConversationID))
	}
	if t.Quoted != nil && t.Quoted.ID != "" {
		e.add(self, PredQuotes, URI(KindTweet, t.Quoted.ID))
	}
	if t.Retweeted != nil && t.Retweeted.ID != "" {
		e.add(self, PredReposts, URI(KindTweet, t.Retweeted.ID))
	}
	for _, m := range t.Entities.Mentions {
		e.add(self, PredMentions, userURI(m))
	}
	for _, h := range t.Entities.Hashtags {
		e.add(self, PredTagged, URI(KindHashtag, strings.ToLower(h)))
	}
	for _, c := range t.Entities.Cashtags {
		e.add(self, PredTaggedSymbol, URI(KindCashtag, strings.ToLower(c)))
	}
	for _, u := range t.Entities.URLs {
		e.add(self, PredLinksTo, linkURI(u))
	}
	for _, m := range t.Media {
		e.add(self, PredHasMedia, mediaURI(m))
	}
	if t.Poll != nil {
		e.add(self, PredHasPoll, URI(KindPoll, t.ID))
	}

	// The tweets this record carries whole. Their edges are theirs, under their
	// own provenance, which happens to be the same request.
	out := e.out
	out = append(out, tweetEdges(t.Quoted)...)
	out = append(out, tweetEdges(t.Retweeted)...)
	return out
}

func userEdges(u *User) []Edge {
	if u == nil || u.Username == "" {
		return nil
	}
	e := &emitter{meta: &u.Meta}
	self := userURI(u.Username)
	if u.PinnedTweet != "" {
		e.add(self, PredPinned, URI(KindTweet, u.PinnedTweet))
	}
	if u.Website != "" {
		e.add(self, PredWebsite, linkURI(u.Website))
	}
	return e.out
}

// spaceEdges reads the roster. The creator is credited as a host even when the
// admin list already names them, because the two are not the same claim: X sends
// the creator separately and a Space can be created by an account that is not in
// the room. Duplicates collapse in MergeEdges.
func spaceEdges(s *Space) []Edge {
	if s == nil || s.ID == "" {
		return nil
	}
	e := &emitter{meta: &s.Meta}
	self := URI(KindSpace, s.ID)
	if s.Creator != nil && s.Creator.Username != "" {
		e.add(userURI(s.Creator.Username), PredHosted, self)
	}
	for _, u := range s.Hosts {
		if u != nil {
			e.add(userURI(u.Username), PredHosted, self)
		}
	}
	for _, u := range s.Speakers {
		if u != nil {
			e.add(userURI(u.Username), PredSpokeIn, self)
		}
	}
	return e.out
}

func listEdges(l *List) []Edge {
	if l == nil || l.ID == "" || l.Owner == nil {
		return nil
	}
	e := &emitter{meta: &l.Meta}
	e.add(userURI(l.Owner.Username), PredOwns, URI(KindList, l.ID))
	return e.out
}

// trendEdges places a trend. The place is the woeid the trend was read for, not
// the human name beside it, because the name is a label and the woeid is the
// identifier the directory and `x trends` both take.
func trendEdges(t *Trend) []Edge {
	if t == nil || t.ID == "" || t.WOEID == 0 {
		return nil
	}
	e := &emitter{meta: &t.Meta}
	e.add(URI(KindTrend, t.ID), PredTrendingIn, URI(KindPlace, woeidStr(t.WOEID)))
	return e.out
}

// placeEdges walks the directory upwards. Worldwide has parent 0, which is the
// top rather than a place, so the chain ends there.
func placeEdges(p *Place) []Edge {
	if p == nil || p.WOEID == 0 || p.ParentID == 0 {
		return nil
	}
	e := &emitter{meta: &p.Meta}
	e.add(URI(KindPlace, woeidStr(p.WOEID)), PredChildOf, URI(KindPlace, woeidStr(p.ParentID)))
	return e.out
}

// woeidStr writes a woeid the way Identify writes it, so a place named by a
// trend and the same place read out of the directory are one node.
func woeidStr(w int64) string { return strconv.FormatInt(w, 10) }

// userURI addresses an account by its handle, lowercased, because X treats
// @NASA and @nasa as one account and a graph that keeps both has two nodes for
// one thing.
func userURI(handle string) string {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if h == "" {
		return ""
	}
	return URI(KindUser, h)
}

// linkURI addresses an outbound link. Classify already knows how to fold a URL
// into the scheme/host/path form, and a string it cannot read is not made into
// a node rather than being made into a wrong one.
func linkURI(u string) string {
	kind, id, err := Classify(u)
	if err != nil || id == "" {
		return ""
	}
	return URI(kind, id)
}

// mediaURI addresses an attachment. The media key is the right identifier and
// half the surfaces do not send one, so the CDN URL is read for it instead:
// Classify pulls the id straight out of the path.
func mediaURI(m Media) string {
	if m.Key != "" {
		return URI(KindMedia, m.Key)
	}
	for _, u := range []string{m.URL, m.Preview} {
		if u == "" {
			continue
		}
		if kind, id, err := Classify(u); err == nil && kind == KindMedia {
			return URI(KindMedia, id)
		}
	}
	return ""
}

// MergeEdges sorts, and collapses edges that are the same claim from the same
// source. Two surfaces asserting one thing stay two edges, because that is the
// agreement the store is built to record and the input `x edges --conflicts`
// reads.
func MergeEdges(es []Edge) []Edge {
	SortEdges(es)
	out := es[:0]
	var prev Edge
	for i, e := range es {
		if i > 0 && e.From == prev.From && e.Predicate == prev.Predicate && e.To == prev.To && e.Source == prev.Source {
			continue
		}
		out = append(out, e)
		prev = e
	}
	return out
}

// SortEdges puts edges in a stable order: by subject, then predicate in
// vocabulary order, then object. Vocabulary order rather than alphabetical, so
// a tweet's edges read as authored, replies_to, in_conversation, mentions, which
// is the order somebody would describe the tweet in.
func SortEdges(es []Edge) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.Predicate != b.Predicate {
			return predOrder(a.Predicate) < predOrder(b.Predicate)
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Source < b.Source
	})
}

func predOrder(p Predicate) int {
	for i, q := range Predicates {
		if p == q {
			return i
		}
	}
	return len(Predicates)
}

// Conflict is one disagreement: two or more sources that cannot both be right.
type Conflict struct {
	// Subject and Predicate name the claim. For an inverse-functional
	// predicate the subject is the object end, because that is the side that
	// can only have one.
	Subject   string    `json:"subject"`
	Predicate Predicate `json:"predicate"`
	Edges     []Edge    `json:"edges"`
}

// Winner is the edge with the most trustworthy provenance. It is what a
// consumer that has to pick one should pick, and the point of keeping the rest
// is that nothing forces them to.
func (c Conflict) Winner() Edge {
	best := c.Edges[0]
	for _, e := range c.Edges[1:] {
		if e.Trust() > best.Trust() {
			best = e
		}
	}
	return best
}

// Conflicts finds the claims that contradict each other.
//
// Not every repeat is a contradiction. Two surfaces both saying a tweet mentions
// @bob agree, and there is nothing to report. A contradiction needs a predicate
// that admits one answer, so this looks at the functional predicates from the
// subject side and the inverse-functional ones from the object side, and reports
// a group only when the answers differ.
//
// Nothing is resolved here. The groups come back whole, in the order their
// subjects sort, and the caller decides what to do with them.
func Conflicts(es []Edge) []Conflict {
	type key struct {
		subject string
		pred    Predicate
	}
	groups := map[key][]Edge{}
	var order []key
	note := func(k key, e Edge) {
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], e)
	}
	for _, e := range es {
		if e.Predicate.Functional() {
			note(key{e.From, e.Predicate}, e)
		}
		if e.Predicate.InverseFunctional() {
			note(key{e.To, e.Predicate}, e)
		}
	}
	var out []Conflict
	for _, k := range order {
		g := groups[k]
		if !disagree(g) {
			continue
		}
		SortEdges(g)
		out = append(out, Conflict{Subject: k.subject, Predicate: k.pred, Edges: g})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return predOrder(out[i].Predicate) < predOrder(out[j].Predicate)
	})
	return out
}

// disagree reports whether a group holds more than one distinct answer. The
// ends compared are the two that are not the key, which for a functional
// predicate is the object and for an inverse-functional one the subject.
func disagree(g []Edge) bool {
	for _, e := range g[1:] {
		if e.To != g[0].To || e.From != g[0].From {
			return true
		}
	}
	return false
}
