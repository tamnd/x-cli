package embed

import (
	"strings"
	"testing"
)

func storeFrom(t *testing.T, file string) *Store {
	t.Helper()
	d, err := Seroval(fixture(t, file))
	if err != nil {
		t.Fatalf("Seroval: %v", err)
	}
	s, err := RelayStore(d)
	if err != nil {
		t.Fatalf("RelayStore: %v", err)
	}
	return s
}

// A root field key carries its GraphQL arguments inlined, so a lookup by
// field name has to ignore everything from the paren on.
func TestStoreFieldIgnoresArguments(t *testing.T) {
	s := storeFrom(t, "sp2.html.gz")
	if _, ok := s.Field("tweet_result_by_rest_id"); !ok {
		t.Error("could not find tweet_result_by_rest_id by name")
	}
	// The name has to match up to the paren and no further, or a lookup for
	// one field would pick up any field that starts with the same letters.
	if _, ok := s.Field("tweet_result"); ok {
		t.Error("a prefix that stops mid-name matched")
	}
	got := s.Fields("tweet_result_by_rest_id")
	if len(got) != 1 {
		t.Errorf("got %d matching keys, want 1", len(got))
	}
	for k := range got {
		if !strings.Contains(k, `rest_id:"20"`) {
			t.Errorf("key %q lost its arguments", k)
		}
	}
}

// The whole reason this package exists: read a tweet, with every count, off a
// page anybody can fetch.
func TestStoreResolveTweet(t *testing.T) {
	s := storeFrom(t, "sp2.html.gz")
	f, ok := s.Field("tweet_result_by_rest_id")
	if !ok {
		t.Fatal("no tweet field")
	}
	res, ok := s.Resolve(f).(map[string]any)
	if !ok {
		t.Fatalf("resolved to %T", s.Resolve(f))
	}
	tweet, ok := res["result"].(map[string]any)
	if !ok {
		t.Fatal("no result under TweetResults")
	}
	if tweet["rest_id"] != "20" {
		t.Errorf("rest_id = %v", tweet["rest_id"])
	}
	if got, _ := DigStr(tweet, "details", "full_text"); got != "just setting up my twttr" {
		t.Errorf("full_text = %q", got)
	}

	counts, ok := tweet["counts"].(map[string]any)
	if !ok {
		t.Fatal("no counts")
	}
	for _, c := range []struct {
		key  string
		want int64
	}{
		{"favorite_count", 307403},
		{"retweet_count", 124855},
		{"reply_count", 17964},
		{"quote_count", 6805},
		// This one is the correction. Spec 3003 doc 00 said the bookmark
		// count needs a guest token. The status page ships it at tier 0.
		{"bookmark_count", 21256},
	} {
		if counts[c.key] != c.want {
			t.Errorf("%s = %v, want %d", c.key, counts[c.key], c.want)
		}
	}

	// X leaves views null on a tweet this old, and null is the answer, not
	// zero. The microdata on the same page agrees.
	views, ok := tweet["views"].(map[string]any)
	if !ok {
		t.Fatal("no views record")
	}
	if views["count"] != nil {
		t.Errorf("views.count = %v, want null", views["count"])
	}

	author, _ := Dig(tweet, "core", "user_results", "result")
	if got, _ := DigStr(author, "core", "screen_name"); got != "jack" {
		t.Errorf("author screen_name = %q", got)
	}
	if got, _ := Dig(author, "relationship_counts", "followers"); got != int64(10548148) {
		t.Errorf("followers = %v", got)
	}
}

// The profile page splits its store across the router payload and one
// streamed chunk. If the two halves are not merged the user record resolves
// but its timeline does not, so this asserts both are present.
func TestStoreMergesStreamedChunks(t *testing.T) {
	d, err := Seroval(fixture(t, "prof.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Chunks) == 0 {
		t.Fatal("no streamed chunk, so this test proves nothing")
	}
	full, err := RelayStore(d)
	if err != nil {
		t.Fatal(err)
	}
	routerOnly, err := RelayStore(&Doc{Router: d.Router})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Records) <= len(routerOnly.Records) {
		t.Errorf("merging the chunk added nothing: %d vs %d records",
			len(full.Records), len(routerOnly.Records))
	}
	if _, ok := full.Field("user_result_by_screen_name"); !ok {
		t.Error("no user_result_by_screen_name after merging")
	}
}

func TestStoreTyped(t *testing.T) {
	s := storeFrom(t, "sp2.html.gz")
	tweets := s.Typed("Tweet")
	if len(tweets) != 4 {
		t.Fatalf("got %d Tweet records, want 4", len(tweets))
	}
	for _, tw := range tweets {
		if _, ok := DigStr(tw, "rest_id"); !ok {
			t.Errorf("Tweet record with no rest_id: %v", tw)
		}
	}
}

// The store is cyclic: a tweet points at its author, the author's timeline
// points back at the tweet. The walk has to terminate and say where it turned
// around rather than silently truncating.
func TestStoreCycle(t *testing.T) {
	s := &Store{Records: map[string]any{
		"a": map[string]any{idKey: "a", typeKey: "A", "to": map[string]any{refKey: "b"}},
		"b": map[string]any{idKey: "b", typeKey: "B", "to": map[string]any{refKey: "a"}},
	}}
	got, ok := s.ResolveID("a")
	if !ok {
		t.Fatal("record a not found")
	}
	back, _ := Dig(got, "to", "to")
	m, ok := back.(map[string]any)
	if !ok {
		t.Fatalf("cycle resolved to %T", back)
	}
	if m["__stub"] != "cycle" {
		t.Errorf("came back around to %v, want a cycle stub", m)
	}
	if m[typeKey] != "A" {
		t.Errorf("the stub lost the typename: %v", m)
	}
}

// A ref with no record behind it is normal. The server sends the part of the
// store the viewport needed and no more, so a dangling ref is a fact about
// the page, not a parse failure.
func TestStoreDanglingRef(t *testing.T) {
	s := &Store{Records: map[string]any{
		"a": map[string]any{idKey: "a", "to": map[string]any{refKey: "gone"}},
	}}
	got, _ := s.ResolveID("a")
	m, _ := Dig(got, "to")
	stub, ok := m.(map[string]any)
	if !ok || stub["__stub"] != "missing" {
		t.Errorf("dangling ref resolved to %#v, want a missing stub", m)
	}
}

func TestStoreRefs(t *testing.T) {
	s := &Store{Records: map[string]any{
		rootID: map[string]any{idKey: rootID, "list": map[string]any{refsKey: []any{"x", "y"}}},
		"x":    map[string]any{idKey: "x", "n": int64(1)},
		"y":    map[string]any{idKey: "y", "n": int64(2)},
	}}
	f, ok := s.Field("list")
	if !ok {
		t.Fatal("no list field")
	}
	items, ok := s.Resolve(f).([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("__refs resolved to %#v", s.Resolve(f))
	}
	if got, _ := Dig(items[1], "n"); got != int64(2) {
		t.Errorf("second item = %v", got)
	}
}

func TestRelayStoreMissing(t *testing.T) {
	_, err := RelayStore(&Doc{Router: map[string]any{"nothing": 1}})
	if err == nil || !strings.Contains(err.Error(), "relayRecords") {
		t.Fatalf("want a relayRecords error, got %v", err)
	}
}
