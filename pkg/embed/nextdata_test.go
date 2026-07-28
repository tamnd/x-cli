package embed

import (
	"errors"
	"testing"
)

func TestNextDataMissing(t *testing.T) {
	_, err := NextData("<html><body>no next here</body></html>")
	if !errors.Is(err, ErrNoPayload) {
		t.Fatalf("want ErrNoPayload, got %v", err)
	}
}

func TestNextDataSimple(t *testing.T) {
	const doc = `<html><body><script id="__NEXT_DATA__" type="application/json">` +
		`{"props":{"pageProps":{"timeline":{"entries":[{"type":"tweet"}]}}}}` +
		`</script></body></html>`
	next, err := NextData(doc)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := TimelineEntries(next)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
}

// An account with nothing to show renders the page without an entries key.
// That is an empty timeline, not a broken page, so it must not be an error.
func TestNextDataEmptyTimeline(t *testing.T) {
	const doc = `<script id="__NEXT_DATA__" type="application/json">` +
		`{"props":{"pageProps":{"timeline":{}}}}</script>`
	next, _ := NextData(doc)
	entries, err := TimelineEntries(next)
	if err != nil {
		t.Fatalf("empty timeline reported as an error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

// The syndication timeline page, captured live on 2026-07-28. This is the
// only anonymous route that returns a user's tweets in bulk, so the shape it
// hands back is worth pinning down.
func TestNextDataSyndicationTimeline(t *testing.T) {
	next, err := NextData(fixture(t, "jack_timeline.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := TimelineEntries(next)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 50 {
		t.Fatalf("got %d entries, want the usual hundred or so", len(entries))
	}

	first := entries[0]
	if got, _ := DigStr(first, "type"); got != "tweet" {
		t.Errorf("entry type = %q", got)
	}
	tw, ok := Dig(first, "content", "tweet")
	if !ok {
		t.Fatal("no content.tweet on the first entry")
	}
	for _, field := range []string{"id_str", "full_text", "created_at", "permalink", "lang"} {
		if v, ok := DigStr(tw, field); !ok || v == "" {
			t.Errorf("tweet has no %s", field)
		}
	}
	// Counts come back as JSON numbers, so they land as float64 here. The
	// model layer is what turns them into int64s.
	for _, field := range []string{"favorite_count", "retweet_count", "reply_count", "quote_count"} {
		v, ok := Dig(tw, field)
		if !ok {
			t.Errorf("tweet has no %s", field)
			continue
		}
		if _, ok := v.(float64); !ok {
			t.Errorf("%s is %T, want a number", field, v)
		}
	}
	if got, _ := DigStr(tw, "user", "screen_name"); got != "jack" {
		t.Errorf("author = %q, want jack", got)
	}
	// The author profile rides along in full on every entry, which is why
	// one timeline fetch answers a profile question too.
	if got, _ := Dig(tw, "user", "followers_count"); got == nil {
		t.Error("no followers_count on the embedded author")
	}
}

func TestDig(t *testing.T) {
	v := map[string]any{"a": map[string]any{"b": "c"}}
	if got, ok := DigStr(v, "a", "b"); !ok || got != "c" {
		t.Errorf("DigStr = %q %v", got, ok)
	}
	if _, ok := Dig(v, "a", "nope"); ok {
		t.Error("Dig found a key that is not there")
	}
	if _, ok := Dig(v, "a", "b", "deeper"); ok {
		t.Error("Dig walked through a string")
	}
}
