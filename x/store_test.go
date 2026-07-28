package x

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
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
