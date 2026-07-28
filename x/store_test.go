package x

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TweetsByAuthor must match handles case-insensitively: the store keeps X's
// canonical casing ("NASA") while users type whatever case ("nasa"), and every
// other read in the CLI is case-insensitive. A case-sensitive lookup here made
// `x export nasa` silently find nothing after `x timeline nasa --db ...`.
func TestTweetsByAuthorCaseInsensitive(t *testing.T) {
	st, err := OpenStore(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	tw := NewTweet("1")
	tw.Text, tw.Author = "hi", NewUser("NASA")
	tw.Author.RestID = "11"
	if err := st.UpsertTweet(tw); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"NASA", "nasa", "Nasa"} {
		got, err := st.TweetsByAuthor(q)
		if err != nil {
			t.Fatalf("TweetsByAuthor(%q): %v", q, err)
		}
		if len(got) != 1 {
			t.Fatalf("TweetsByAuthor(%q) = %d tweets, want 1", q, len(got))
		}
	}
}

// The store keeps the richer read. A crawl revisits nodes constantly, and a
// store that let the last write win would throw away a session read the moment
// an anonymous one touched the same tweet.
func TestTheStoreKeepsTheHigherTierRecord(t *testing.T) {
	st := tempStore(t)

	rich := NewTweet("1")
	rich.Text = "read with a session"
	rich.Meta.Tier = 2
	if err := st.PutRecord(URI(KindTweet, "1"), KindTweet, "1", 2, rich); err != nil {
		t.Fatal(err)
	}
	thin := NewTweet("1")
	thin.Text = "read anonymously"
	if err := st.PutRecord(URI(KindTweet, "1"), KindTweet, "1", 0, thin); err != nil {
		t.Fatal(err)
	}

	var record string
	var tier int
	err := st.DB().QueryRow(`SELECT tier, record FROM nodes WHERE uri=?`, URI(KindTweet, "1")).Scan(&tier, &record)
	if err != nil {
		t.Fatal(err)
	}
	if tier != 2 || !strings.Contains(record, "read with a session") {
		t.Fatalf("stored tier %d record %q, want the tier 2 read to survive", tier, record)
	}
}

// Two surfaces asserting one claim are two rows, because the source is in the
// primary key. That is what makes the stored graph answerable about agreement
// as well as about facts, and it is doc 04 section 5's reason for the key.
func TestTwoSurfacesAssertingOneClaimAreTwoRows(t *testing.T) {
	st := tempStore(t)

	claim := Edge{From: "x://user/jack", Predicate: PredAuthored, To: "x://tweet/20"}
	a := claim
	a.Source, a.Surface = "https://cdn.syndication.twimg.com/tweet-result?id=20", 1
	b := claim
	b.Source, b.Surface = "https://x.com/i/status/20", 8
	if err := st.PutEdges([]Edge{a, b, a}); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("stored %d edge rows, want 2: one per source, and the repeat collapsed", n)
	}
}

// Storing a node stores what it says. A walk that recorded only the hops it
// travelled would keep a graph of its own route rather than a graph of X.
func TestStoringANodeStoresItsClaims(t *testing.T) {
	st := tempStore(t)

	tw := synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977")
	if err := st.UpsertNode(&Node{Kind: KindTweet, Tweet: tw}); err != nil {
		t.Fatal(err)
	}

	rows, err := st.DB().Query(`SELECT from_uri, predicate, to_uri FROM edges ORDER BY from_uri, predicate, to_uri`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var f, p, to string
		if err := rows.Scan(&f, &p, &to); err != nil {
			t.Fatal(err)
		}
		got = append(got, f+" "+p+" "+to)
	}
	want := []string{
		"x://tweet/1903142823316049977 mentions x://user/jack",
		"x://tweet/1903142823316049977 mentions x://user/marmoushera",
		"x://tweet/1903142823316049977 replies_to x://tweet/1903136743634723031",
		"x://user/guyfishermoney authored x://tweet/1903142823316049977",
		"x://user/marmoushera authored x://tweet/1903136743634723031",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stored edges:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	var uri, kind string
	if err := st.DB().QueryRow(`SELECT uri, kind FROM nodes`).Scan(&uri, &kind); err != nil {
		t.Fatal(err)
	}
	if uri != "x://tweet/1903142823316049977" || kind != KindTweet {
		t.Fatalf("stored node %s %s, want the tweet addressed by its URI", kind, uri)
	}
}

// An older store has an edges table of hop names in the shape this one cannot
// use. Opening it must not fail and must not throw the rows away.
func TestAnOlderStoreKeepsItsHopRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE edges (src TEXT, dst TEXT, kind TEXT, PRIMARY KEY (src, dst, kind));
	  INSERT INTO edges VALUES ('20','@jack','author')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := OpenStore(path)
	if err != nil {
		t.Fatalf("opening a store written by an older build: %v", err)
	}
	defer func() { _ = st.Close() }()

	if err := st.PutEdges([]Edge{{From: "x://user/jack", Predicate: PredAuthored, To: "x://tweet/20", Source: "https://x.com/i/status/20"}}); err != nil {
		t.Fatalf("writing a claim into the new table: %v", err)
	}
	var kept int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM edges_hops`).Scan(&kept); err != nil {
		t.Fatalf("the old rows are gone: %v", err)
	}
	if kept != 1 {
		t.Fatalf("kept %d old rows, want the crawl that was already there", kept)
	}
}

func tempStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// The store is a graph, and this is the read that proves it. A crawl writes
// nodes and edges and `x export --format` walks them back out with no network
// at all, so what goes in has to come out whole.
func TestTheStoreReadsBackAsAGraph(t *testing.T) {
	st := tempStore(t)
	tw := NewTweet("20")
	tw.Text, tw.Author = "just setting up my twttr", NewUser("jack")
	if err := st.UpsertNode(&Node{Kind: KindTweet, Tweet: tw}); err != nil {
		t.Fatal(err)
	}

	doc, err := st.Document(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	byURI := map[string]GraphNode{}
	for _, n := range doc.Nodes {
		byURI[n.URI] = n
	}
	if _, ok := byURI["x://tweet/20"]; !ok {
		t.Fatalf("the tweet is not in the document: %v", byURI)
	}
	if _, ok := byURI["x://tweet/20"].Record.(*Tweet); !ok {
		t.Errorf("the tweet came back without its record, as %T", byURI["x://tweet/20"].Record)
	}
	if len(doc.Edges) == 0 {
		t.Error("the document has no edges, so nothing was claimed")
	}
	if len(Triples(doc)) == 0 {
		t.Error("the document makes no triples, so an export would write an empty file")
	}
}

// Every endpoint an edge names is addressed, even when nobody fetched it. An
// export that dropped them would write triples whose object has no type, which
// is a graph with holes where the interesting parts are.
func TestAnEdgeNeverPointsAtNothing(t *testing.T) {
	st := tempStore(t)
	if err := st.PutEdges([]Edge{{
		From: URI(KindTweet, "20"), Predicate: PredMentions, To: userURI("nasa"), Source: "test",
	}}); err != nil {
		t.Fatal(err)
	}

	doc, err := st.Document(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"x://tweet/20": KindTweet, "x://user/nasa": KindUser}
	for _, n := range doc.Nodes {
		if want[n.URI] != n.Kind {
			t.Errorf("node %s has kind %q, want %q", n.URI, n.Kind, want[n.URI])
		}
		delete(want, n.URI)
	}
	if len(want) != 0 {
		t.Errorf("the edge names %v and the document does not address them", want)
	}
}

// --kind keeps the claims with that kind at one end. Narrowing to the subject
// instead would drop authorship, which runs from the account to the post, and an
// export of tweets with no author on any of them is not worth writing.
func TestAKindFilterKeepsEveryClaimTouchingThatKind(t *testing.T) {
	st := tempStore(t)
	tw := NewTweet("20")
	tw.Text, tw.Author = "hi", NewUser("jack")
	if err := st.UpsertNode(&Node{Kind: KindTweet, Tweet: tw}); err != nil {
		t.Fatal(err)
	}
	u := NewUser("jack")
	u.RestID, u.PinnedTweet = "12", "20"
	if err := st.UpsertNode(&Node{Kind: KindUser, User: u}); err != nil {
		t.Fatal(err)
	}

	doc, err := st.Document(Filter{Kind: KindTweet})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range doc.Nodes {
		if n.Kind != KindTweet && n.Record != nil {
			t.Errorf("--kind tweet came back with a %s record for %s", n.Kind, n.URI)
		}
	}
	var authored bool
	for _, e := range doc.Edges {
		from, _, _ := SplitURI(e.From)
		to, _, _ := SplitURI(e.To)
		if from != KindTweet && to != KindTweet {
			t.Errorf("--kind tweet kept %s %s %s, which touches no tweet", e.From, e.Predicate, e.To)
		}
		if e.Predicate == PredAuthored {
			authored = true
		}
	}
	if !authored {
		t.Error("--kind tweet dropped authorship, so the export would not say who wrote anything")
	}
}

// --since is when the record was captured. A tweet from 2006 read this morning
// is something learned this morning, and an export answering "what is new" that
// filtered on datePublished would never show it.
func TestSinceFiltersOnWhenTheRecordWasCaptured(t *testing.T) {
	st := tempStore(t)
	old := NewTweet("20")
	old.Text, old.Author = "old news", NewUser("jack")
	if err := st.UpsertNode(&Node{Kind: KindTweet, Tweet: old}); err != nil {
		t.Fatal(err)
	}
	backdate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, table := range []string{"nodes", "edges"} {
		if _, err := st.DB().Exec(`UPDATE `+table+` SET captured=?`, backdate); err != nil {
			t.Fatal(err)
		}
	}
	fresh := NewTweet("21")
	fresh.Text, fresh.Author = "new news", NewUser("jack")
	if err := st.UpsertNode(&Node{Kind: KindTweet, Tweet: fresh}); err != nil {
		t.Fatal(err)
	}

	doc, err := st.Document(Filter{Since: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range doc.Nodes {
		if n.URI == "x://tweet/20" && n.Record != nil {
			t.Error("--since brought back the record captured in 2020")
		}
	}
	var found bool
	for _, n := range doc.Nodes {
		if n.URI == "x://tweet/21" && n.Record != nil {
			found = true
		}
	}
	if !found {
		t.Error("--since dropped the record captured just now")
	}
}
