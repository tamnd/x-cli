package x

import "testing"

// The envelope is the one thing every record shares, so these tests pin the
// three promises it makes: an id addresses the thing in x's own space, a stamp
// says what the answer cost, and a merge of two surfaces says which surface
// filled which field instead of shrugging and writing "s1+s8".

func TestIdentifyDerivesBothAddresses(t *testing.T) {
	tw := NewTweet("20")
	if tw.Kind != KindTweet || tw.ID != "20" {
		t.Fatalf("kind/id = %q/%q, want tweet/20", tw.Kind, tw.ID)
	}
	if tw.URI != "x://tweet/20" {
		t.Errorf("uri = %q, want x://tweet/20", tw.URI)
	}
	if tw.URL != TweetURL("", "20") {
		t.Errorf("url = %q, want %q", tw.URL, TweetURL("", "20"))
	}

	// A URL the decoder already read off the page wins, because /jack/status/20
	// is the address X publishes and /i/status/20 is only the fallback.
	tw.URL = "https://x.com/jack/status/20"
	tw.Identify(KindTweet, "20")
	if tw.URL != "https://x.com/jack/status/20" {
		t.Errorf("Identify overwrote a better url with %q", tw.URL)
	}
}

func TestUserIdentityIsTheLowercasedHandle(t *testing.T) {
	u := NewUser("@NASA")
	if u.Username != "NASA" {
		t.Errorf("username = %q, want the casing its owner chose", u.Username)
	}
	if u.ID != "nasa" || u.URI != "x://user/nasa" {
		t.Errorf("id/uri = %q/%q, want nasa/x://user/nasa", u.ID, u.URI)
	}
	// The numeric id is a separate fact and does not replace the handle.
	u.RestID = "11348282"
	if u.ID == u.RestID {
		t.Error("the numeric id took over the identity")
	}
}

func TestStampRecordsTheSurfaceTierAndSource(t *testing.T) {
	var m Meta
	m.Stamp(1, "https://cdn.syndication.twimg.com/tweet-result?id=20")
	m.Stamp(4, "https://x.com/i/api/graphql/TweetResultByRestId")
	m.Stamp(1, "https://cdn.syndication.twimg.com/tweet-result?id=20")

	if len(m.Surfaces) != 2 || m.Surfaces[0] != "s1" || m.Surfaces[1] != "s4" {
		t.Errorf("surfaces = %v, want [s1 s4] in the order they were read", m.Surfaces)
	}
	if len(m.Sources) != 2 {
		t.Errorf("sources = %v, want the same read counted once", m.Sources)
	}
	// s4 is the guest door, so knowing this cost a guest token.
	if m.Tier != 1 {
		t.Errorf("tier = %d, want 1", m.Tier)
	}
	if m.Surface() != 0 {
		t.Errorf("Surface() = %d, want 0 when more than one contributed", m.Surface())
	}
}

func TestTierNeverDrops(t *testing.T) {
	var m Meta
	m.Stamp(7, "https://x.com/i/api/graphql/SearchTimeline")
	m.Stamp(1, "https://cdn.syndication.twimg.com/tweet-result?id=20")
	if m.Tier != 2 {
		t.Errorf("tier = %d, want 2: a cheap second read does not make the answer free", m.Tier)
	}
}

func TestMergeNamesTheSurfaceBehindEachField(t *testing.T) {
	// What the syndication endpoint knows.
	a := NewTweet("20")
	a.Text = "just setting up my twttr"
	a.Metrics.Likes = 100
	stampTweet(a, 1, "https://cdn.syndication.twimg.com/tweet-result?id=20")

	// What only the page knows.
	b := NewTweet("20")
	b.Text = "just setting up my twttr"
	b.Metrics.Likes = 99
	b.Metrics.Bookmarks = 7
	b.Metrics.Impressions = 1234
	stampTweet(b, 8, "https://x.com/jack/status/20")

	got := MergeTweet(a, b)
	if got.Metrics.Bookmarks != 7 || got.Metrics.Impressions != 1234 {
		t.Fatalf("merge did not fill the gaps: %+v", got.Metrics)
	}
	if got.Metrics.Likes != 100 {
		t.Errorf("likes = %d, want the first surface's 100 kept", got.Metrics.Likes)
	}
	if got.Via["bookmarks"] != "s8" || got.Via["impressions"] != "s8" {
		t.Errorf("via = %v, want the page named for both counts", got.Via)
	}
	if _, said := got.Via["likes"]; said {
		t.Error("via named a surface for a field the merge did not fill")
	}
	if len(got.Surfaces) != 2 || got.Surfaces[0] != "s1" || got.Surfaces[1] != "s8" {
		t.Errorf("surfaces = %v, want [s1 s8]", got.Surfaces)
	}
	if len(got.Sources) != 2 {
		t.Errorf("sources = %v, want both URLs", got.Sources)
	}
}

func TestMergeCarriesTheOtherRecordsVia(t *testing.T) {
	// A record built from two surfaces already carries a via of its own, and
	// folding it into a third must not lose it.
	a := NewTweet("20")
	stampTweet(a, 1, "https://cdn.syndication.twimg.com/tweet-result?id=20")

	b := NewTweet("20")
	b.Metrics.Bookmarks = 7
	stampTweet(b, 8, "https://x.com/jack/status/20")
	b.Note("bookmarks", 8)
	b.Stamp(4, "https://x.com/i/api/graphql/TweetResultByRestId")
	b.Source = "Twitter Web Client"
	b.Note("source", 4)

	got := MergeTweet(a, b)
	if got.Via["bookmarks"] != "s8" || got.Via["source"] != "s4" {
		t.Errorf("via = %v, want s8 for bookmarks and s4 for source", got.Via)
	}
	if got.Tier != 1 {
		t.Errorf("tier = %d, want 1 through the guest door b used", got.Tier)
	}
}
