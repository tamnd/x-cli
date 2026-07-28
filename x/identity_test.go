package x

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type urlCase struct {
	line       int
	in         string
	kind, id   string
	expandable bool
}

// loadCorpus reads testdata/urls.txt and expands every x.com case across the
// other front ends, which is what turns a readable hundred-line file into the
// few hundred cases doc 06 section 5.4 asks for.
func loadCorpus(t *testing.T) []urlCase {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "urls.txt"))
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer func() { _ = f.Close() }()

	var out []urlCase
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			t.Fatalf("urls.txt:%d: want three tab-separated columns, got %d", n, len(parts))
		}
		c := urlCase{line: n, in: parts[0], kind: parts[1], id: parts[2]}
		// A link case names its host in its id, so rewriting the host would
		// change the expected answer rather than test that it stays the same.
		c.expandable = c.kind != KindLink && strings.Contains(c.in, "//x.com/")
		out = append(out, c)
		if !c.expandable {
			continue
		}
		for _, host := range []string{"twitter.com", "www.x.com", "mobile.twitter.com", "nitter.net"} {
			alt := c
			alt.in = strings.Replace(c.in, "//x.com/", "//"+host+"/", 1)
			out = append(out, alt)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	return out
}

func TestClassifyCorpus(t *testing.T) {
	cases := loadCorpus(t)
	if len(cases) < 200 {
		t.Fatalf("corpus expanded to %d cases, want a few hundred", len(cases))
	}
	for _, c := range cases {
		kind, id, err := Classify(c.in)
		if err != nil {
			t.Errorf("urls.txt:%d: Classify(%q): %v", c.line, c.in, err)
			continue
		}
		if kind != c.kind || id != c.id {
			t.Errorf("urls.txt:%d: Classify(%q) = (%s, %s), want (%s, %s)",
				c.line, c.in, kind, id, c.kind, c.id)
		}
	}
}

// Four kinds live on a page that belongs to another kind. A conversation, a
// poll, a card and a note are all rendered by the status page of the tweet they
// hang off, so Locate is many-to-one there on purpose and classifying the result
// gives back the tweet. A trend is the same story against the search page. These
// kinds are reached from a record, never from a pasted link, so no round trip is
// lost.
var sharesAPageWithAnotherKind = map[string]bool{
	KindConversation: true, KindPoll: true, KindCard: true, KindNote: true, KindTrend: true,
}

// Locate(Classify(u)) is the canonical form of u, and running it twice has to
// land in the same place. A canonical form that is not a fixed point would make
// the same node appear twice in a crawl.
func TestLocateIsAFixedPoint(t *testing.T) {
	for _, c := range loadCorpus(t) {
		kind, id, err := Classify(c.in)
		if err != nil || sharesAPageWithAnotherKind[kind] {
			continue
		}
		canon, err := Locate(kind, id)
		if err != nil {
			continue // a kind with no page of its own, which is allowed
		}
		k2, i2, err := Classify(canon)
		if err != nil {
			t.Errorf("urls.txt:%d: Classify(Locate(%s, %s) = %q): %v", c.line, kind, id, canon, err)
			continue
		}
		if k2 != kind || i2 != id {
			t.Errorf("urls.txt:%d: %q canonicalised to %q, which classifies as (%s, %s) not (%s, %s)",
				c.line, c.in, canon, k2, i2, kind, id)
		}
	}
}

// Reversibility, over every kind rather than every URL: a URI is an address the
// tool can hand back to itself.
func TestURIRoundTrips(t *testing.T) {
	ids := map[string]string{
		KindTweet:        "20",
		KindUser:         "jack",
		KindConversation: "1833951636005552366",
		KindMedia:        "3_2080776588996563188",
		KindPoll:         "2081011830927806892",
		KindCard:         "2081011830927806892",
		KindHashtag:      "golang",
		KindCashtag:      "tsla",
		KindLink:         "https/example.com/a",
		KindList:         "1418136314401153025",
		KindSpace:        "1YpKkZWmvvvGj",
		KindBroadcast:    "1zqKVvvvvvvvB",
		KindTrend:        "1/golang",
		KindPlace:        "23424977",
		KindNote:         "2079993592798478689",
		KindCommunity:    "1493446837214187523",
		KindSearch:       "golang",
	}
	if len(ids) != len(Kinds) {
		t.Fatalf("the round-trip table covers %d kinds, Kinds has %d", len(ids), len(Kinds))
	}
	for _, kind := range Kinds {
		id := ids[kind]
		k, i, err := Classify(URI(kind, id))
		if err != nil {
			t.Errorf("Classify(%q): %v", URI(kind, id), err)
			continue
		}
		if k != kind || i != id {
			t.Errorf("URI round trip: (%s, %s) came back as (%s, %s)", kind, id, k, i)
		}
	}
}

// The three rules from doc 04 section 1.1 that resolve the ambiguities.
func TestClassifyAmbiguityRules(t *testing.T) {
	t.Run("bare digits are a tweet id", func(t *testing.T) {
		kind, id, err := Classify("12")
		if err != nil || kind != KindTweet || id != "12" {
			t.Errorf("Classify(12) = (%s, %s, %v), want a tweet", kind, id, err)
		}
	})
	t.Run("handles are lowercased", func(t *testing.T) {
		for _, in := range []string{"@NASA", "NASA", "https://x.com/NASA", "https://twitter.com/NaSa"} {
			kind, id, err := Classify(in)
			if err != nil || kind != KindUser || id != "nasa" {
				t.Errorf("Classify(%q) = (%s, %s, %v), want user nasa", in, kind, id, err)
			}
		}
	})
	t.Run("tracking parameters are dropped but q is the id", func(t *testing.T) {
		if _, id, _ := Classify("https://x.com/jack/status/20?s=46&t=xyz"); id != "20" {
			t.Errorf("tracking parameters changed the id: %q", id)
		}
		if kind, id, _ := Classify("https://x.com/search?q=golang&src=typed_query"); kind != KindSearch || id != "golang" {
			t.Errorf("search q = (%s, %s)", kind, id)
		}
	})
}

// Every case in the corpus must classify, and nothing outside it should be
// silently accepted as a node.
func TestClassifyRejectsJunk(t *testing.T) {
	for _, in := range []string{"", "   ", "@", "#", "$", "x://", "x://nope/1", "@this-handle-is-far-too-long"} {
		if kind, id, err := Classify(in); err == nil {
			t.Errorf("Classify(%q) = (%s, %s), want an error", in, kind, id)
		}
	}
}

// A search classifies but has no URI, because a search is a query rather than a
// thing. It still locates, because the query has a page.
func TestSearchLocatesButIsNotANode(t *testing.T) {
	u, err := Locate(KindSearch, "golang")
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://x.com/search?q=golang" {
		t.Errorf("Locate(search, golang) = %q", u)
	}
}

func TestLocateRefusesKindsWithNoPage(t *testing.T) {
	for _, kind := range []string{KindMedia, KindPlace, "not-a-kind"} {
		if u, err := Locate(kind, "1"); err == nil {
			t.Errorf("Locate(%s, 1) = %q, want an error", kind, u)
		}
	}
}

// The many-to-one part of Locate, stated rather than assumed.
func TestKindsThatShareTheStatusPage(t *testing.T) {
	for _, kind := range []string{KindConversation, KindPoll, KindCard, KindNote} {
		u, err := Locate(kind, "20")
		if err != nil {
			t.Fatalf("Locate(%s, 20): %v", kind, err)
		}
		if u != "https://x.com/i/status/20" {
			t.Errorf("Locate(%s, 20) = %q, want the status page it hangs off", kind, u)
		}
	}
}

func TestLocateTweetDoesNotNeedTheHandle(t *testing.T) {
	u, err := Locate(KindTweet, "20")
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://x.com/i/status/20" {
		t.Errorf("Locate(tweet, 20) = %q", u)
	}
}
