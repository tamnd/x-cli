package x

import (
	"encoding/json"
	"testing"
)

// synTweetFromCapture reads a syndication capture the way the reader does, so
// the edges under test come off the same object `x tweet` prints.
func synTweetFromCapture(t *testing.T, fixture, id string) *Tweet {
	t.Helper()
	var st synTweet
	if err := json.Unmarshal([]byte(capture(t, fixture)), &st); err != nil {
		t.Fatalf("%s: %v", fixture, err)
	}
	tw := st.toTweet()
	stampTweet(tw, 1, "https://cdn.syndication.twimg.com/tweet-result?id="+id)
	return tw
}

// triples renders edges for comparison, dropping the provenance so a test can
// assert what a record says separately from where it said it.
func triples(es []Edge) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.From + " " + string(e.Predicate) + " " + e.To
	}
	return out
}

func sameTriples(t *testing.T, got []Edge, want []string) {
	t.Helper()
	g := triples(got)
	if len(g) != len(want) {
		t.Fatalf("got %d edges, want %d\ngot:  %v\nwant: %v", len(g), len(want), g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Errorf("edge %d is %q, want %q", i, g[i], want[i])
		}
	}
}

// The claim in doc 04 section 2.2 is that one anonymous request is already a
// graph. It is, and this is exactly how much of one: five edges over five
// nodes, from a single tweet-result read with no credential at all.
func TestOneAnonymousTweetIsAlreadyAGraph(t *testing.T) {
	tw := synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977")
	es := MergeEdges(Edges(tw))
	sameTriples(t, es, []string{
		"x://tweet/1903142823316049977 replies_to x://tweet/1903136743634723031",
		"x://tweet/1903142823316049977 mentions x://user/jack",
		"x://tweet/1903142823316049977 mentions x://user/marmoushera",
		"x://user/guyfishermoney authored x://tweet/1903142823316049977",
		"x://user/marmoushera authored x://tweet/1903136743634723031",
	})
	for _, e := range es {
		if e.Tier != 0 || e.Surface != 1 {
			t.Errorf("%s came from s%d tier %d, want surface 1 at tier 0", e.Predicate, e.Surface, e.Tier)
		}
		if e.Source == "" {
			t.Errorf("%s has no source URL, so nothing can be checked against X", e.Predicate)
		}
	}
}

// Doc 04 section 2.2 lists in_conversation among the edges surface 1 yields. It
// is not there, and cannot be: the capture has conversation_count and no
// conversation id at all. The conversation edge comes off the GraphQL legacy
// shape, which the next test reads.
func TestSurfaceOneDoesNotKnowTheConversation(t *testing.T) {
	tw := synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977")
	if tw.ConversationID != "" {
		t.Fatalf("the capture now carries a conversation id (%q), so this is stale", tw.ConversationID)
	}
	for _, e := range Edges(tw) {
		if e.Predicate == PredInConversation {
			t.Error("an in_conversation edge appeared with nothing to build it from")
		}
	}
}

// The parent's author is free. X names it on the reply as
// in_reply_to_screen_name, so a thread can be climbed upwards without fetching
// anything, and the account at the top is a real node rather than a dangling id.
func TestTheReplyParentGetsAnAuthorNobodyFetched(t *testing.T) {
	tw := synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977")
	var found bool
	for _, e := range Edges(tw) {
		if e.Predicate == PredAuthored && e.To == "x://tweet/1903136743634723031" {
			found = true
			if e.From != "x://user/marmoushera" {
				t.Errorf("the parent is credited to %s", e.From)
			}
		}
	}
	if !found {
		t.Error("nothing says who wrote the tweet this one replies to")
	}
}

// A retweet read through GraphQL carries the original whole, and the original's
// edges are the original's. The one that matters is the last: @nasa reposted it
// and @nasasolarsystem wrote it, and one request said both.
func TestARetweetYieldsTheOriginalsEdgesToo(t *testing.T) {
	tw := gqlTweetFromCapture(t, "2081832975524634937")
	if tw.Retweeted == nil {
		t.Fatal("the capture's retweet no longer carries the original")
	}
	sameTriples(t, MergeEdges(Edges(tw)), []string{
		"x://tweet/2081803694480261151 in_conversation x://conversation/2081803694480261151",
		"x://tweet/2081803694480261151 links_to x://link/http/go.nasa.gov/4x5qvqP",
		"x://tweet/2081803694480261151 has_media x://media/13_2081803656530190336",
		"x://tweet/2081832975524634937 reposts x://tweet/2081803694480261151",
		"x://tweet/2081832975524634937 in_conversation x://conversation/2081832975524634937",
		"x://tweet/2081832975524634937 mentions x://user/nasasolarsystem",
		"x://user/nasa authored x://tweet/2081832975524634937",
		"x://user/nasasolarsystem authored x://tweet/2081803694480261151",
	})
	// Both halves are credited to the request that carried them, including the
	// nested one, which is only true because the reader stamps what it nests.
	for _, e := range Edges(tw) {
		if e.Surface != 4 {
			t.Errorf("%s %s came from s%d, want the one request that fetched both", e.From, e.Predicate, e.Surface)
		}
	}
}

// gqlTweetFromCapture pulls one tweet out of the UserTweets capture.
func gqlTweetFromCapture(t *testing.T, id string) *Tweet {
	t.Helper()
	for _, o := range tweetResults([]byte(capture(t, "s4_usertweets_nasa.json.gz"))) {
		var r gqlTweetResult
		if json.Unmarshal(o, &r) != nil {
			continue
		}
		tw := r.build()
		if tw != nil && tw.ID == id {
			stampTweet(tw, 4, "https://x.com/i/api/graphql/UserTweets")
			return tw
		}
	}
	t.Fatalf("%s is not in the capture", id)
	return nil
}

// One photo, two surfaces, two nodes. GraphQL sends a media key and the status
// page sends only the CDN filename, and the two are different identifiers for
// the same picture, so the has_media edges do not join.
//
// This is a fact about X rather than a bug to fix here, and it is asserted so
// that the day a media node is expected to be one node, this test says why it
// is not.
func TestTheSamePhotoIsTwoMediaNodes(t *testing.T) {
	page, err := ParsePage(StatusPageURL("2081860978694594863"), capture(t, "status_media.html.gz"))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	fromPage, err := page.TweetFromPage("2081860978694594863")
	if err != nil {
		t.Fatalf("TweetFromPage: %v", err)
	}
	stampTweet(fromPage, 8, StatusPageURL("2081860978694594863"))

	got := map[int]string{}
	for _, tw := range []*Tweet{fromPage, gqlTweetFromCapture(t, "2081860978694594863")} {
		for _, e := range Edges(tw) {
			if e.Predicate == PredHasMedia {
				got[e.Surface] = e.To
			}
		}
	}
	if got[8] != "x://media/HORBhKkWIAAFjOh" {
		t.Errorf("the page addressed the photo as %q, want the CDN filename", got[8])
	}
	if got[4] != "x://media/3_2081860965780299776" {
		t.Errorf("GraphQL addressed the photo as %q, want the media key", got[4])
	}
}

// A profile's outbound link is the one edge a profile page reliably asserts.
func TestAProfileAssertsItsWebsite(t *testing.T) {
	page, err := ParsePage("https://x.com/nasa", capture(t, "profile_nasa.html.gz"))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	u, err := page.UserFromPage("nasa")
	if err != nil {
		t.Fatalf("UserFromPage: %v", err)
	}
	stampUser(u, 8, "https://x.com/nasa")
	sameTriples(t, MergeEdges(Edges(u)), []string{
		"x://user/nasa website x://link/http/www.nasa.gov/",
	})
}

// A Space is a room, and the roster is what makes it a graph. hosted is doc
// 04's predicate; spoke_in is not, and it is here because a finished Space keeps
// who held the microphone and forgets everyone who listened, so the speakers are
// the only audience the record has.
func TestASpaceRosterBecomesEdges(t *testing.T) {
	s, err := parseSpace([]byte(capture(t, "s4_space_1dRJZEpyjlNGB.json.gz")), "1dRJZEpyjlNGB")
	if err != nil {
		t.Fatalf("parseSpace: %v", err)
	}
	s.Stamp(4, "https://x.com/i/api/graphql/AudioSpaceById")
	for _, u := range append(append([]*User{s.Creator}, s.Hosts...), s.Speakers...) {
		stampUser(u, 4, "https://x.com/i/api/graphql/AudioSpaceById")
	}
	sameTriples(t, MergeEdges(Edges(s)), []string{
		"x://user/mjkabir spoke_in x://space/1dRJZEpyjlNGB",
		"x://user/schelleranna hosted x://space/1dRJZEpyjlNGB",
		"x://user/usabizparty hosted x://space/1dRJZEpyjlNGB",
	})
	// The creator is also an admin on this Space, and saying so twice is one
	// claim. MergeEdges collapsed it, which is why usabizparty appears once.
	for _, e := range Edges(s) {
		if e.Tier != 1 {
			t.Errorf("%s is tier %d, want the guest tier a Space is read at", e.Predicate, e.Tier)
		}
	}
}

// The place directory is a tree and child_of is how it is walked. Worldwide has
// parent 0, which is the top rather than a place, so the chain stops rather than
// pointing at a node that does not exist.
func TestPlacesChainUpwardsAndStopAtTheTop(t *testing.T) {
	places, err := decodePlaces([]byte(capture(t, "s5_places.json.gz")), "", "", "", 0)
	if err != nil {
		t.Fatalf("decodePlaces: %v", err)
	}
	var chained, top int
	for _, p := range places {
		es := Edges(p)
		if p.WOEID == 1 {
			top++
			if len(es) != 0 {
				t.Errorf("Worldwide claims a parent: %v", triples(es))
			}
			continue
		}
		if len(es) == 1 && es[0].Predicate == PredChildOf {
			chained++
		}
	}
	if top != 1 {
		t.Errorf("found %d top-level places, want just Worldwide", top)
	}
	if chained != len(places)-1 {
		t.Errorf("%d of %d places point at a parent, want all but Worldwide", chained, len(places)-1)
	}
}

// A trend is placed by woeid rather than by the human name beside it, because
// the woeid is what the directory and `x trends` both take and the name is a
// label that changes with the locale.
func TestATrendIsPlacedByWOEID(t *testing.T) {
	trends, err := decodeTrends([]byte(capture(t, "s5_trends_us.json.gz")), 23424977, trendsPlaceURL(23424977), 3)
	if err != nil {
		t.Fatalf("decodeTrends: %v", err)
	}
	for _, tr := range trends {
		es := Edges(tr)
		if len(es) != 1 || es[0].Predicate != PredTrendingIn || es[0].To != "x://place/23424977" {
			t.Fatalf("%q yielded %v", tr.Name, triples(es))
		}
	}
}

// The two tweets that are in the captures twice, read through both surfaces at
// once. Nothing contradicts anything: across every claim that admits one answer,
// surface 4 and surface 8 agree.
//
// That is the result, and it is worth having written down, because it is the
// thing --conflicts is looking for and has not found yet. The media nodes do
// differ between the two, and that is not a contradiction: a tweet may carry
// several pictures, so two different media nodes is two claims rather than two
// answers to one question.
func TestTheSurfacesAgreeAboutTheTweetsWeHaveTwice(t *testing.T) {
	var es []Edge
	for _, c := range []struct{ id, fixture string }{
		{"20", "status_20.html.gz"},
		{"2081860978694594863", "status_media.html.gz"},
	} {
		page, err := ParsePage(StatusPageURL(c.id), capture(t, c.fixture))
		if err != nil {
			t.Fatalf("ParsePage %s: %v", c.fixture, err)
		}
		tw, err := page.TweetFromPage(c.id)
		if err != nil {
			t.Fatalf("TweetFromPage %s: %v", c.id, err)
		}
		stampTweet(tw, 8, StatusPageURL(c.id))
		es = append(es, Edges(tw)...)
	}
	es = append(es, Edges(synTweetFromCapture(t, "s1_tweet_20.json.gz", "20"))...)
	es = append(es, Edges(gqlTweetFromCapture(t, "2081860978694594863"))...)

	es = MergeEdges(es)
	if c := Conflicts(es); len(c) != 0 {
		for _, one := range c {
			t.Errorf("%s %s: %v", one.Subject, one.Predicate, triples(one.Edges))
		}
	}
	// Two surfaces agreeing is two rows, not one, which is what makes the
	// agreement visible at all.
	var authored int
	for _, e := range es {
		if e.Predicate == PredAuthored && e.To == "x://tweet/20" {
			authored++
		}
	}
	if authored != 2 {
		t.Errorf("tweet 20 has %d authorship claims, want one from each surface that read it", authored)
	}
}

// Agreement is not conflict. Two surfaces both saying a tweet mentions the same
// account is the ordinary case, and reporting it would bury the one that
// matters.
func TestAgreementIsNotAConflict(t *testing.T) {
	es := []Edge{
		{From: "x://tweet/1", Predicate: PredMentions, To: "x://user/bob", Source: "a", Surface: 1},
		{From: "x://tweet/1", Predicate: PredMentions, To: "x://user/bob", Source: "b", Surface: 8},
		{From: "x://tweet/1", Predicate: PredMentions, To: "x://user/carol", Source: "b", Surface: 8},
		{From: "x://tweet/1", Predicate: PredRepliesTo, To: "x://tweet/0", Source: "a", Surface: 1},
		{From: "x://tweet/1", Predicate: PredRepliesTo, To: "x://tweet/0", Source: "b", Surface: 8},
	}
	if c := Conflicts(es); len(c) != 0 {
		t.Errorf("got %d conflicts, want none: %+v", len(c), c)
	}
}

// A disagreement about a single-valued claim is kept whole and not resolved.
// Winner says which one a consumer that has to choose should choose; the losing
// edge is still in the group, which is the point.
func TestAContradictionIsReportedAndNotResolved(t *testing.T) {
	es := []Edge{
		{From: "x://tweet/1", Predicate: PredRepliesTo, To: "x://tweet/0", Source: "syndication", Surface: 1},
		{From: "x://tweet/1", Predicate: PredRepliesTo, To: "x://tweet/9", Source: "x.com", Surface: 8},
		{From: "x://user/a", Predicate: PredAuthored, To: "x://tweet/1", Source: "syndication", Surface: 1},
		{From: "x://user/b", Predicate: PredAuthored, To: "x://tweet/1", Source: "session", Surface: 7, Tier: 2},
	}
	cs := Conflicts(es)
	if len(cs) != 2 {
		t.Fatalf("got %d conflicts, want 2: %+v", len(cs), cs)
	}
	// Both are about the same tweet, so they come back in vocabulary order.
	//
	// Authorship first, and it is only contradictory from the tweet's side,
	// because an account writes as many tweets as it likes and a tweet has one
	// author. The session read wins on tier.
	if cs[0].Subject != "x://tweet/1" || cs[0].Predicate != PredAuthored {
		t.Fatalf("first conflict is %s %s", cs[0].Subject, cs[0].Predicate)
	}
	if w := cs[0].Winner(); w.From != "x://user/b" {
		t.Errorf("the session read lost to %s", w.From)
	}
	// Then the reply parent, disagreed about by two tier-0 surfaces. The page
	// wins because doc 04 ranks the more modern payload higher.
	if cs[1].Subject != "x://tweet/1" || cs[1].Predicate != PredRepliesTo {
		t.Fatalf("second conflict is %s %s", cs[1].Subject, cs[1].Predicate)
	}
	if w := cs[1].Winner(); w.To != "x://tweet/9" || w.Surface != 8 {
		t.Errorf("s8 lost to s%d", w.Surface)
	}
	if len(cs[1].Edges) != 2 {
		t.Error("the losing edge was dropped, which is the one thing --conflicts exists to prevent")
	}
}

// Any tier beats any tier 0, whatever surface the tier-0 claim came from.
func TestTrustPutsAnyCredentialAboveNone(t *testing.T) {
	page := Edge{Surface: 8, Tier: 0}
	guest := Edge{Surface: 4, Tier: 1}
	session := Edge{Surface: 7, Tier: 2}
	if session.Trust() <= guest.Trust() || guest.Trust() <= page.Trust() {
		t.Errorf("trust ordered %d %d %d", session.Trust(), guest.Trust(), page.Trust())
	}
	if page.Trust() <= (Edge{Surface: 1}).Trust() {
		t.Error("the status page did not outrank the embed payload")
	}
}

// The vocabulary is closed, and this is what closed means: a name nobody wrote
// down is not a predicate.
func TestTheVocabularyIsClosed(t *testing.T) {
	if len(Predicates) != 25 {
		t.Errorf("the vocabulary has %d predicates, want the 24 from doc 04 plus spoke_in", len(Predicates))
	}
	seen := map[Predicate]bool{}
	for _, p := range Predicates {
		if seen[p] {
			t.Errorf("%s is listed twice", p)
		}
		seen[p] = true
		if !p.Known() {
			t.Errorf("%s is in the list and not known", p)
		}
	}
	if Predicate("replies").Known() || Predicate("").Known() {
		t.Error("a name nobody wrote down passed as a predicate")
	}
}

// Which predicates a record can assert on its own, and which ones need a
// listing or a field nothing models yet. The list is here so that the gap is
// written down rather than discovered later by someone wondering where their
// has_card edges went.
func TestEveryPredicateIsEmittedOrAccountedFor(t *testing.T) {
	emitted := map[Predicate]bool{}
	note := func(rec any) {
		for _, e := range Edges(rec) {
			emitted[e.Predicate] = true
		}
	}
	note(synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977"))
	note(gqlTweetFromCapture(t, "2081832975524634937"))
	note(gqlTweetFromCapture(t, "2081860978694594863"))
	if page, err := ParsePage("https://x.com/nasa", capture(t, "profile_nasa.html.gz")); err == nil {
		if u, err := page.UserFromPage("nasa"); err == nil {
			note(u)
		}
	}
	if s, err := parseSpace([]byte(capture(t, "s4_space_1dRJZEpyjlNGB.json.gz")), "1dRJZEpyjlNGB"); err == nil {
		note(s)
	}
	if places, err := decodePlaces([]byte(capture(t, "s5_places.json.gz")), "", "", "", 3); err == nil {
		for _, p := range places {
			note(p)
		}
	}
	if trends, err := decodeTrends([]byte(capture(t, "s5_trends_us.json.gz")), 23424977, "", 1); err == nil {
		for _, tr := range trends {
			note(tr)
		}
	}

	// Why each of the rest is absent from the captures above.
	accounted := map[Predicate]string{
		PredQuotes:       "no capture holds a quote tweet yet",
		PredTagged:       "no capture holds a tweet with a hashtag",
		PredTaggedSymbol: "no capture holds a tweet with a cashtag",
		PredHasPoll:      "no capture holds a poll",
		PredHasCard:      "the model has no card field, so nothing can assert it",
		PredHasNote:      "the model has no community note field, so nothing can assert it",
		PredPinned:       "the profile capture has no pinned tweet",
		PredOwns:         "reading a list needs a session",
		PredMemberOf:     "list membership needs a session and a listing, not a record",
		PredFollows:      "a listing, not a record: it comes from the walker with a session",
		PredFollowedBy:   "a listing, not a record",
		PredLiked:        "a listing, not a record",
		PredReposted:     "a listing, not a record",
	}
	for _, p := range Predicates {
		if emitted[p] {
			if why, ok := accounted[p]; ok {
				t.Errorf("%s is emitted after all, so %q is stale", p, why)
			}
			continue
		}
		if _, ok := accounted[p]; !ok {
			t.Errorf("%s is neither emitted by any capture nor accounted for", p)
		}
	}
}

// MergeEdges collapses one claim from one source and keeps one claim from two,
// because two surfaces agreeing is the evidence the store is built to hold.
func TestMergeKeepsOneRowPerSource(t *testing.T) {
	es := MergeEdges([]Edge{
		{From: "x://tweet/1", Predicate: PredMentions, To: "x://user/bob", Source: "a"},
		{From: "x://tweet/1", Predicate: PredMentions, To: "x://user/bob", Source: "a"},
		{From: "x://tweet/1", Predicate: PredMentions, To: "x://user/bob", Source: "b"},
	})
	if len(es) != 2 {
		t.Fatalf("got %d edges, want the duplicate collapsed and the second source kept: %+v", len(es), es)
	}
	if es[0].Source != "a" || es[1].Source != "b" {
		t.Errorf("sources came back %q and %q", es[0].Source, es[1].Source)
	}
}

// Nothing here opens a socket, and a record with nothing to say is not an error.
func TestExtractionIsTotalAndQuiet(t *testing.T) {
	for _, rec := range []any{
		(*Tweet)(nil), (*User)(nil), (*Space)(nil), (*List)(nil), (*Trend)(nil), (*Place)(nil), (*Node)(nil),
		&Tweet{}, &User{}, &Space{}, "not a record", 42, nil,
	} {
		if es := Edges(rec); len(es) != 0 {
			t.Errorf("%T yielded %v", rec, triples(es))
		}
	}
}

// The four hops that are the only evidence for what they find. Nothing either
// endpoint says asserts these: a liker's profile does not mention the tweet and
// the tweet does not list its likers, so if the walk does not record them
// nobody does.
func TestTheListingHopsAreTheOnlyEvidence(t *testing.T) {
	liker := NewUser("alice")
	liker.Stamp(7, "https://x.com/i/api/graphql/Favoriters")

	cases := []struct {
		hop      Hop
		src, dst string
		want     string
	}{
		{HopLiker, "20", "@alice", "x://user/alice liked x://tweet/20"},
		{HopRetweeter, "20", "@alice", "x://user/alice reposted x://tweet/20"},
		{HopFollowing, "@jack", "@alice", "x://user/jack follows x://user/alice"},
		// A followers listing of @jack says alice follows jack, so the edge
		// points the way the claim points and not the way the walk travelled.
		{HopFollowers, "@jack", "@alice", "x://user/alice follows x://user/jack"},
		{HopLikes, "@jack", "20", "x://user/jack liked x://tweet/20"},
	}
	for _, c := range cases {
		e, ok := HopEdge(c.hop, c.src, c.dst, &liker.Meta)
		if !ok {
			t.Fatalf("the %s hop yielded no edge", c.hop)
		}
		if got := e.From + " " + string(e.Predicate) + " " + e.To; got != c.want {
			t.Errorf("the %s hop is %q, want %q", c.hop, got, c.want)
		}
		if e.Surface != 7 || e.Source == "" {
			t.Errorf("the %s hop lost the listing's provenance: s%d %q", c.hop, e.Surface, e.Source)
		}
	}
}

// Walking the author hop and reading the tweet are the same claim, arrived at
// two ways. They have to agree, because a store fed by both would otherwise
// hold one account authoring a tweet and another not.
func TestAHopAndARecordAgreeAboutTheSameClaim(t *testing.T) {
	tw := synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977")
	hop, ok := HopEdge(HopAuthor, tw.ID, "@"+tw.Author.Username, &tw.Meta)
	if !ok {
		t.Fatal("the author hop yielded no edge")
	}
	var rec Edge
	for _, e := range Edges(tw) {
		if e.Predicate == PredAuthored && e.To == URI(KindTweet, tw.ID) {
			rec = e
		}
	}
	if hop.From != rec.From || hop.Predicate != rec.Predicate || hop.To != rec.To {
		t.Fatalf("the hop says %s %s %s and the record says %s %s %s",
			hop.From, hop.Predicate, hop.To, rec.From, rec.Predicate, rec.To)
	}
}

// An account reached by numeric id has no address, because doc 04 section 1.3
// addresses accounts by handle. Inventing a second spelling would split one
// account into two nodes, so the hop yields nothing instead.
func TestAnAccountKnownOnlyByIdIsNotANode(t *testing.T) {
	known := NewUser("jack")
	known.Stamp(1, "https://cdn.syndication.twimg.com/tweet-result?id=20")
	if e, ok := HopEdge(HopAuthor, "20", "#12", &known.Meta); ok {
		t.Fatalf("got %s %s %s, want no edge at all", e.From, e.Predicate, e.To)
	}
}

// A hop with no record at the far end has no source either, and a claim nobody
// can be pointed at for is not worth a row. Nothing is lost: the reply parent
// and the mention are both asserted by the record at the near end.
func TestAHopWithNoProvenanceIsNotRecorded(t *testing.T) {
	if e, ok := HopEdge(HopReply, "1903142823316049977", "1903136743634723031", nil); ok {
		t.Fatalf("got %s %s %s from %q, want no edge", e.From, e.Predicate, e.To, e.Source)
	}
}
