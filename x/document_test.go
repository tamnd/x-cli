package x

import "testing"

// A document is the join of the two things `x get` and `x edges` print, so the
// test is that it holds both halves and loses neither: every node an edge
// addresses is in it, and the ones the read carried whole come with a record.
func TestADocumentHoldsBothHalvesOfTheGraph(t *testing.T) {
	tw := synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977")
	doc := Graph(tw)

	sameTriples(t, doc.Edges, []string{
		"x://tweet/1903142823316049977 replies_to x://tweet/1903136743634723031",
		"x://tweet/1903142823316049977 mentions x://user/jack",
		"x://tweet/1903142823316049977 mentions x://user/marmoushera",
		"x://user/guyfishermoney authored x://tweet/1903142823316049977",
		"x://user/marmoushera authored x://tweet/1903136743634723031",
	})

	want := []string{
		"x://tweet/1903136743634723031",
		"x://tweet/1903142823316049977",
		"x://user/guyfishermoney",
		"x://user/jack",
		"x://user/marmoushera",
	}
	if len(doc.Nodes) != len(want) {
		t.Fatalf("got %d nodes, want %d: %v", len(doc.Nodes), len(want), doc.Nodes)
	}
	for i, n := range doc.Nodes {
		if n.URI != want[i] {
			t.Errorf("node %d is %s, want %s", i, n.URI, want[i])
		}
	}
}

// The honest half of a document: a mention is a claim about an account nobody
// fetched, so the account is a node with no record. Two of these five were
// read, the tweet and its author, and the read says which.
func TestOnlyTheNodesTheReadCarriedHaveRecords(t *testing.T) {
	doc := Graph(synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977"))
	read := map[string]bool{}
	for _, n := range doc.Nodes {
		if n.Record != nil {
			read[n.URI] = true
		}
	}
	for _, uri := range []string{"x://tweet/1903142823316049977", "x://user/guyfishermoney"} {
		if !read[uri] {
			t.Errorf("%s was read but carries no record", uri)
		}
	}
	for _, uri := range []string{"x://user/jack", "x://tweet/1903136743634723031"} {
		if read[uri] {
			t.Errorf("%s was only named, so it should carry no record", uri)
		}
	}
	if len(read) != 2 {
		t.Errorf("got %d records in the document, want 2: %v", len(read), read)
	}
}

// Kind and id come back off the URI, because the document is addressed in x's
// own space and a caller who wants to fetch the rest of it needs the pair.
func TestADocumentNodeKnowsItsKindAndId(t *testing.T) {
	doc := Graph(synTweetFromCapture(t, "s1_tweet_20.json.gz", "20"))
	for _, n := range doc.Nodes {
		kind, id, ok := SplitURI(n.URI)
		if !ok {
			t.Fatalf("%s is not an address", n.URI)
		}
		if n.Kind != kind || n.ID != id {
			t.Errorf("%s split to %s/%s, want %s/%s", n.URI, n.Kind, n.ID, kind, id)
		}
	}
}

// Two reads of the same graph make one document rather than two, which is the
// whole reason `x graph` takes more than one reference.
func TestTwoReadsMakeOneDocument(t *testing.T) {
	a := synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977")
	b := synTweetFromCapture(t, "s1_tweet_20.json.gz", "20")
	doc := Graph(a, b)
	seen := map[string]int{}
	for _, n := range doc.Nodes {
		seen[n.URI]++
	}
	if seen["x://user/jack"] != 1 {
		t.Errorf("@jack appears %d times, want once: he is mentioned by one tweet and wrote the other", seen["x://user/jack"])
	}
	if seen["x://tweet/20"] != 1 || seen["x://tweet/1903142823316049977"] != 1 {
		t.Errorf("a read appears more than once: %v", seen)
	}
}
