package x

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/x-cli/pkg/embed"
)

// localName is the last segment of a schema.org IRI, so the two sides can be
// compared on the term rather than on how each spelled the namespace.
func localName(s string) string {
	if i := strings.LastIndexAny(s, "/#"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// claim is one statement flattened for comparison: a subject, a property, and a
// value as text. Microdata has no datatypes and no blank node labels, so this is
// as much structure as the two sides can share.
type claim struct{ subject, prop, value string }

// ours flattens the tool's triples into claims, resolving each counter's blank
// node into the counter's own reading. X states a counter as a nested item with
// no identity of its own, so a blank node label can never match; what can match
// is "this subject has this many of this action", which is what both sides mean.
func ours(ts []Triple) map[claim]bool {
	counters := map[string]map[string]string{}
	for _, t := range ts {
		if t.S.Blank == "" {
			continue
		}
		c := counters[t.S.Blank]
		if c == nil {
			c = map[string]string{}
			counters[t.S.Blank] = c
		}
		switch t.P.IRI {
		case NSSchema + "interactionType":
			c["type"] = localName(t.O.IRI)
		case NSSchema + "name":
			c["name"] = t.O.Value
		case NSSchema + "userInteractionCount":
			c["count"] = t.O.Value
		}
	}
	out := map[claim]bool{}
	for _, t := range ts {
		if t.S.Blank != "" {
			continue
		}
		subject := t.S.IRI
		prop := localName(t.P.IRI)
		switch {
		case t.O.Blank != "":
			c := counters[t.O.Blank]
			out[claim{subject, prop, c["type"] + " " + c["name"] + " " + c["count"]}] = true
		case t.O.IRI != "":
			out[claim{subject, prop, t.O.IRI}] = true
		default:
			out[claim{subject, prop, normalizeValue(t.O.Value)}] = true
		}
	}
	return out
}

// theirs reads the same claims off X's own microdata for one posting.
func theirs(it *embed.Item, subject, authorURI string) []claim {
	var out []claim
	add := func(s, p, v string) {
		if v != "" {
			out = append(out, claim{s, p, normalizeValue(v)})
		}
	}
	add(subject, "identifier", it.Str("identifier"))
	add(subject, "articleBody", it.Str("articleBody"))
	add(subject, "datePublished", it.Str("datePublished"))
	add(subject, "url", it.Str("url"))
	add(subject, "commentCount", it.Str("commentCount"))
	for _, c := range it.Items("interactionStatistic") {
		add(subject, "interactionStatistic", counterText(c))
	}
	if a := it.Item("author"); a != nil {
		add(authorURI, "identifier", a.Str("identifier"))
		add(authorURI, "name", a.Str("name"))
		add(authorURI, "alternateName", a.Str("alternateName"))
		add(authorURI, "url", a.Str("url"))
		add(authorURI, "image", a.Str("image"))
		for _, c := range a.Items("interactionStatistic") {
			add(authorURI, "interactionStatistic", counterText(c))
		}
		for _, c := range a.Items("agentInteractionStatistic") {
			add(authorURI, "agentInteractionStatistic", counterText(c))
		}
	}
	return out
}

func counterText(c *embed.Item) string {
	return localName(c.Str("interactionType")) + " " + c.Str("name") + " " + c.Str("userInteractionCount")
}

// normalizeValue makes the two sides' spellings of the same value comparable.
// X writes a timestamp with milliseconds and this tool writes it without, and
// that is a difference in lexical form rather than in what was said.
func normalizeValue(s string) string {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts.UTC().Format(time.RFC3339)
		}
	}
	return s
}

// This is the correctness property doc 04 section 4.2 asks for, and it is a much
// stronger one than "the Turtle parses". X publishes schema.org microdata on its
// own status pages, so there is a vendor statement of what a tweet is in RDF
// terms. The test reads plane C off a saved status page, builds the record the
// normal way, serialises it, and asserts that every claim X made is in the
// output. If the vocabulary drifts from X's, this fails.
func TestEveryClaimXAssertsIsInTheRDF(t *testing.T) {
	page, err := ParsePage(StatusPageURL("20"), capture(t, "status_20.html.gz"))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	tw, err := page.TweetFromPage("20")
	if err != nil {
		t.Fatalf("TweetFromPage: %v", err)
	}
	got := ours(Triples(Graph(tw)))

	var posting *embed.Item
	for _, it := range embed.Find(page.Items, "SocialMediaPosting") {
		if it.Str("identifier") == "20" {
			posting = it
		}
	}
	if posting == nil {
		t.Fatal("the capture has no SocialMediaPosting for tweet 20, so there is nothing to check against")
	}

	want := theirs(posting, "x://tweet/20", "x://user/jack")
	if len(want) < 15 {
		t.Fatalf("only %d claims read off the page, which is too few to be a real check", len(want))
	}
	t.Logf("checking %d claims X asserts on the page", len(want))
	for _, c := range want {
		if !got[c] {
			t.Errorf("X asserts %s %s %q and the RDF does not", c.subject, c.prop, c.value)
		}
	}
}

// The class table is doc 04 section 4.1's, and a kind with no class is a node
// that would serialise as an untyped address. Every kind that can be a node has
// one; a search is a query rather than a thing, so it does not.
func TestEveryNodeKindHasAClass(t *testing.T) {
	for _, kind := range []string{
		KindTweet, KindUser, KindMedia, KindLink, KindList, KindSpace,
		KindConversation, KindHashtag, KindCashtag, KindTrend, KindPlace,
		KindPoll, KindCard, KindNote, KindBroadcast, KindCommunity,
	} {
		if _, ok := ClassOf(kind); !ok {
			t.Errorf("kind %s has no RDF class, so it would serialise untyped", kind)
		}
	}
	if _, ok := ClassOf(KindSearch); ok {
		t.Error("a search got a class, and a query is not a thing")
	}
}

// A predicate with no term is a claim the serialisation drops on the floor.
// Doc 04's own table covers eleven of the twenty-five, which is why this test
// exists: the other fourteen are easy to forget when the vocabulary grows.
func TestEveryPredicateHasATerm(t *testing.T) {
	for _, p := range Predicates {
		term, _, ok := PredicateIRI(p)
		if !ok {
			t.Errorf("predicate %s has no RDF term, so an edge with it would vanish", p)
			continue
		}
		if !strings.HasPrefix(term.IRI, NSSchema) && !strings.HasPrefix(term.IRI, NSX) {
			t.Errorf("predicate %s maps to %s, which is in neither namespace", p, term.IRI)
		}
	}
}

// authored is the one edge that runs backwards. The claim is an account's, and
// schema.org spells it as the tweet's author, so the triple has to flip or the
// output says something schema.org does not mean.
func TestAuthorshipFlipsToMatchSchemaOrg(t *testing.T) {
	tw := synTweetFromCapture(t, "s1_tweet_20.json.gz", "20")
	for _, tr := range Triples(Graph(tw)) {
		if tr.P.IRI != NSSchema+"author" {
			continue
		}
		if tr.S.IRI != "x://tweet/20" || tr.O.IRI != "x://user/jack" {
			t.Errorf("schema:author runs %s -> %s, want the tweet to the account", tr.S.IRI, tr.O.IRI)
		}
		return
	}
	t.Error("no schema:author triple at all")
}

// Every serialisation carries provenance, which is doc 04 section 4.3. Two of
// them have somewhere to put it and two need a flag, and this checks that the
// two that carry it always do.
func TestQuadsAndJSONLDCarryTheSource(t *testing.T) {
	tw := synTweetFromCapture(t, "s1_tweet_20.json.gz", "20")
	ts := Triples(Graph(tw))
	src := "https://cdn.syndication.twimg.com/tweet-result?id=20"

	nq, err := WriteRDF("nq", ts, RDFOptions{})
	if err != nil {
		t.Fatalf("nq: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(nq), "\n") {
		if !strings.Contains(line, src) {
			t.Errorf("a quad with no graph name: %s", line)
		}
	}

	ld, err := WriteRDF("jsonld", ts, RDFOptions{})
	if err != nil {
		t.Fatalf("jsonld: %v", err)
	}
	var doc struct {
		Graph []struct {
			ID    string `json:"@id"`
			Inner []any  `json:"@graph"`
		} `json:"@graph"`
	}
	if err := json.Unmarshal([]byte(ld), &doc); err != nil {
		t.Fatalf("the JSON-LD does not parse: %v", err)
	}
	if len(doc.Graph) != 1 || !strings.Contains(doc.Graph[0].ID, src) {
		t.Fatalf("want one named graph for %s, got %d", src, len(doc.Graph))
	}
	if len(doc.Graph[0].Inner) == 0 {
		t.Error("the named graph is empty")
	}
}

// Turtle and N-Triples have nowhere to put a source, so provenance costs
// reification and reification outnumbers the data. That is the whole reason it
// is behind a flag, and this is the measurement.
func TestReificationIsOptionalAndExpensive(t *testing.T) {
	ts := Triples(Graph(synTweetFromCapture(t, "s1_tweet_20.json.gz", "20")))
	plain, err := WriteRDF("nt", ts, RDFOptions{})
	if err != nil {
		t.Fatalf("nt: %v", err)
	}
	with, err := WriteRDF("nt", ts, RDFOptions{Provenance: true})
	if err != nil {
		t.Fatalf("nt --provenance: %v", err)
	}
	p, w := strings.Count(plain, "\n"), strings.Count(with, "\n")
	if w <= p*4 {
		t.Errorf("reified output is %d lines against %d plain, which is fewer than the five-per-statement it should cost", w, p)
	}
	if strings.Contains(plain, "Statement") {
		t.Error("plain N-Triples reified something without being asked")
	}
}

// An unknown format is the user's mistake, not a panic and not an empty file.
func TestAnUnknownFormatIsAnError(t *testing.T) {
	if _, err := WriteRDF("rdfxml", nil, RDFOptions{}); err == nil {
		t.Error("rdfxml was accepted, and nothing here writes it")
	}
	for _, f := range RDFFormats {
		if _, err := WriteRDF(f, nil, RDFOptions{}); err != nil {
			t.Errorf("%s is advertised and rejected: %v", f, err)
		}
	}
}
