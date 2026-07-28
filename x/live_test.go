//go:build live

package x

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

// live_test.go is spec 3003 doc 06 section 5.5: the tests that talk to X.
//
//	go test -tags live ./...
//	X_LIVE_TIER=1 go test -tags live ./...
//	X_LIVE_TIER=2 go test -tags live ./...
//
// Off by default and not in CI, because a suite that fails when somebody else's
// website has a bad afternoon is a suite people learn to ignore.
//
// They assert shapes, not values. `favorite_count` moves every minute and the
// presence of `favorite_count` does not, and only the second one is a claim
// about X worth defending. The exception is tweet 20, which was posted in 2006,
// says what it says, and is not going to change its mind.
//
// The fixture suite answers "does the parser still read the bytes we captured".
// This one answers the question that actually breaks a scraper: "are those still
// the bytes X sends".

// liveEngine builds an engine at the tier the run asked for, with its state in a
// temporary directory. The isolation is the point: the developer running this
// has a session imported, and a tier-0 test that quietly used it would report
// that anonymous reads work on a machine where they were never tried. That is
// the exact defect doc 06 section 4 exists about.
func liveEngine(t *testing.T, tier int) *Engine {
	t.Helper()
	if tier > liveTier() {
		t.Skipf("needs tier %d; set X_LIVE_TIER=%d to run it", tier, tier)
	}
	o := NoOverrides()
	o.DataDir = t.TempDir()
	o.Tier = strconv.Itoa(tier)
	o.Timeout = 30 * time.Second
	cfg := Resolve(o)
	if tier == 2 {
		// Tier 2 is the one tier a temporary data dir cannot supply, because the
		// cookies live in the developer's own store. Read them from the
		// environment, which is where a live run is expected to put them.
		cfg.AuthToken, cfg.CT0 = os.Getenv("X_AUTH_TOKEN"), os.Getenv("X_CT0")
		if !cfg.HasSession() {
			t.Skip("X_LIVE_TIER=2 needs X_AUTH_TOKEN and X_CT0 in the environment")
		}
	}
	if got := cfg.TierNum(); got != tier {
		t.Fatalf("asked for tier %d and got a config at tier %d", tier, got)
	}
	return NewEngine(cfg)
}

// liveTier is the highest tier this run may use. Zero by default, so the plain
// `go test -tags live ./...` is the anonymous run and nothing else.
func liveTier() int {
	n, err := strconv.Atoi(os.Getenv("X_LIVE_TIER"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func liveCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// Tweet 20 is the exception to the shapes-not-values rule. It is the first
// tweet, it is immutable, its author is not going anywhere, and its timestamp
// has been the same for twenty years. If any of this stops being true, the
// change is on X's end and this test is how we hear about it.
func TestLiveTweet20IsStillTheFirstTweet(t *testing.T) {
	tw, err := liveEngine(t, 0).Tweet(liveCtx(t), "20")
	if err != nil {
		t.Fatal(err)
	}
	if tw.Text != "just setting up my twttr" {
		t.Errorf("tweet 20 says %q now", tw.Text)
	}
	if tw.Author == nil || tw.Author.Username != "jack" {
		t.Errorf("tweet 20 has a different author: %+v", tw.Author)
	}
	if want := "2006-03-21T20:50:14Z"; tw.CreatedAt.UTC().Format(time.RFC3339) != want {
		t.Errorf("tweet 20 was posted at %s, not %s", tw.CreatedAt.UTC().Format(time.RFC3339), want)
	}
	if tw.URL != "https://x.com/jack/status/20" {
		t.Errorf("tweet 20 lives at %q now", tw.URL)
	}
	// Counters only ever go up on a tweet nobody can edit, so a floor is a real
	// assertion and an equality would be a recurring false alarm.
	if tw.Metrics.Likes == nil || *tw.Metrics.Likes < 300000 {
		t.Errorf("tweet 20 is down to %v likes, which would be new", tw.Metrics.Likes)
	}
}

// The tier-0 read is the one the whole design rests on, so it is worth checking
// that it still returns a whole record rather than a husk with an id in it.
func TestLiveAnonymousTweetHasItsShape(t *testing.T) {
	tw, err := liveEngine(t, 0).Tweet(liveCtx(t), "20")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		field string
		ok    bool
	}{
		{"kind", tw.Kind == KindTweet},
		{"uri", tw.URI != ""},
		{"tier, which must be 0 on an anonymous read", tw.Tier == 0},
		{"surfaces", len(tw.Surfaces) > 0},
		{"sources", len(tw.Sources) > 0},
		{"text", tw.Text != ""},
		{"lang", tw.Lang != ""},
		{"created_at", !tw.CreatedAt.IsZero()},
		{"author", tw.Author != nil && tw.Author.RestID != ""},
	} {
		if !c.ok {
			t.Errorf("no %s on an anonymous tweet read", c.field)
		}
	}
}

func TestLiveAnonymousUserHasItsShape(t *testing.T) {
	u, err := liveEngine(t, 0).UserFromWeb(liveCtx(t), "jack")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		field string
		ok    bool
	}{
		{"kind", u.Kind == KindUser},
		{"id, the lowercased handle", u.ID == "jack"},
		{"rest_id", u.RestID != ""},
		{"name", u.Name != ""},
		{"description", u.Description != ""},
		{"created_at", !u.CreatedAt.IsZero()},
		{"followers", u.Metrics.Followers != nil && *u.Metrics.Followers > 0},
		{"profile_image", u.ProfileImage != ""},
	} {
		if !c.ok {
			t.Errorf("no %s on an anonymous profile read", c.field)
		}
	}
	// @jack's account was created the same day as tweet 20, and that is as fixed
	// as the tweet is.
	if got := u.CreatedAt.UTC().Format("2006-01-02"); got != "2006-03-21" {
		t.Errorf("@jack now says it joined on %s", got)
	}
}

// A timeline is the read where the shape is not the meaning: the widget hands
// back a ranked sample for some accounts, and a sample answers a different
// question than "what did they post". Whatever comes back, the flag has to say
// which one it is.
func TestLiveAnonymousTimelineIsMarkedForWhatItIs(t *testing.T) {
	var got []*Tweet
	err := liveEngine(t, 0).Timeline(liveCtx(t), "jack", false, TimelineOpts{Limit: 5},
		func(tw *Tweet) error { got = append(got, tw); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no tweets from an anonymous timeline read")
	}
	for _, tw := range got {
		if tw.ID == "" || tw.Text == "" {
			t.Errorf("a timeline row came back empty: %+v", tw)
		}
		if tw.Author == nil || tw.Author.ID != "jack" {
			t.Errorf("a row of @jack's timeline is by somebody else: %+v", tw.Author)
		}
	}
	t.Logf("%d rows, sample=%v", len(got), got[0].Sample)
}

// Trends are a tier-0 read of a v1.1 endpoint that has been about to be turned
// off for years, so knowing it still answers is most of the value here.
func TestLiveAnonymousTrends(t *testing.T) {
	ts, err := liveEngine(t, 0).Trends(liveCtx(t), 23424977, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) == 0 {
		t.Fatal("nothing trending in the US, which would be new")
	}
	for _, tr := range ts {
		if tr.Name == "" || tr.URL == "" {
			t.Errorf("a trend came back without a name or a link: %+v", tr)
		}
	}
}

// The guest tier is a token X mints for anyone who asks, and the thing most
// likely to break is the asking, not the reading.
func TestLiveGuestUserByScreenName(t *testing.T) {
	u, err := liveEngine(t, 1).User(liveCtx(t), "nasa", false)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != "nasa" || u.RestID == "" || u.Name == "" {
		t.Errorf("thin record from the guest tier: %+v", u)
	}
	if !hasStr(u.Surfaces, "s4") {
		t.Errorf("a guest read did not go through surface 4: %v", u.Surfaces)
	}
}

// Search is the one read that needs the user's own session, and it is the one
// worth checking at tier 2 because it is where a rotated query id shows up
// first.
func TestLiveSessionSearch(t *testing.T) {
	var got []*Tweet
	err := liveEngine(t, 2).Search(liveCtx(t), SearchQuery{Raw: "golang", Limit: 5},
		func(tw *Tweet) error { got = append(got, tw); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no results for golang, which would be surprising")
	}
	for _, tw := range got {
		if tw.ID == "" || tw.Author == nil {
			t.Errorf("a search row came back without an id or an author: %+v", tw)
		}
	}
}
