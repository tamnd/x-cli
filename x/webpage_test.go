package x

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func capture(t *testing.T, name string) string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("open capture: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip %s: %v", name, err)
	}
	defer func() { _ = gz.Close() }()
	b, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func statusPage(t *testing.T) *Page {
	t.Helper()
	p, err := ParsePage(StatusPageURL("20"), capture(t, "status_20.html.gz"))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	return p
}

// The whole tier 0 claim in one test: a tweet, every counter, and its author,
// off a page that needs no credential of any kind.
func TestTweetFromStatusPage(t *testing.T) {
	tw, err := statusPage(t).TweetFromPage("20")
	if err != nil {
		t.Fatal(err)
	}
	if tw.Text != "just setting up my twttr" {
		t.Errorf("text = %q", tw.Text)
	}
	if tw.URL != "https://x.com/jack/status/20" {
		t.Errorf("url = %q", tw.URL)
	}
	want := time.Date(2006, 3, 21, 20, 50, 14, 0, time.UTC)
	if !tw.CreatedAt.Equal(want) {
		t.Errorf("created_at = %v, want %v", tw.CreatedAt, want)
	}
	// The counters are checked against a floor rather than an exact value. They
	// climb between one capture and the next, and pinning them means `x capture`
	// breaks the build every time it is used, which is a strange thing to do to
	// the command whose whole job is refreshing these files.
	//
	// A floor still catches what can actually go wrong. A parser that stops
	// finding the field gives nil, one that reads the wrong node gives a small
	// number off some other element, and one that mixes up two counters gives a
	// number from the wrong order of magnitude. All three fail here.
	for _, c := range []struct {
		name  string
		got   *int
		floor int
	}{
		{"likes", tw.Metrics.Likes, 100_000},
		{"retweets", tw.Metrics.Retweets, 50_000},
		{"replies", tw.Metrics.Replies, 5_000},
		{"quotes", tw.Metrics.Quotes, 2_000},
		// Spec 3003 put the bookmark count behind a guest token. It is here,
		// at tier 0, on the plain status page.
		{"bookmarks", tw.Metrics.Bookmarks, 5_000},
	} {
		if c.got == nil {
			t.Errorf("%s is missing", c.name)
		} else if *c.got < c.floor {
			t.Errorf("%s = %d, too small to have come from the right node (floor %d)", c.name, *c.got, c.floor)
		}
	}
	if len(tw.Surfaces) != 1 || tw.Surfaces[0] != "s8" {
		t.Errorf("surfaces = %v, want just s8", tw.Surfaces)
	}
	if tw.URI != "x://tweet/20" {
		t.Errorf("uri = %q", tw.URI)
	}

	if tw.Author == nil {
		t.Fatal("no author")
	}
	if tw.Author.Username != "jack" || tw.Author.RestID != "12" {
		t.Errorf("author = %+v", tw.Author)
	}
	if n := Val(tw.Author.Metrics.Followers); n < 1_000_000 {
		t.Errorf("followers = %d, too small to be jack's", n)
	}
	// This one is pinned because it is a fact rather than a counter: jack has
	// followed three accounts for years, and a parser reading the wrong node
	// would not land on 3.
	if Val(tw.Author.Metrics.Following) != 3 {
		t.Errorf("following = %d", Val(tw.Author.Metrics.Following))
	}
	if n := Val(tw.Author.Metrics.Tweets); n < 10_000 {
		t.Errorf("tweets = %d, too small to be jack's", n)
	}
	if tw.Author.Description != "no state is the best state" {
		t.Errorf("bio = %q", tw.Author.Description)
	}
	if !tw.Author.Verified {
		t.Error("author not marked verified")
	}
}

// A status page carries the focal tweet and three replies, which is what
// x replies returns at tier 0.
func TestPostingsFromStatusPage(t *testing.T) {
	posts := statusPage(t).Postings()
	if len(posts) != 4 {
		t.Fatalf("got %d postings, want 4", len(posts))
	}
	var focal *Tweet
	ids := map[string]bool{}
	for _, p := range posts {
		ids[p.ID] = true
		if p.ID == "20" {
			focal = p
		}
		if p.Author == nil || p.Author.Username == "" {
			t.Errorf("posting %s has no author", p.ID)
		}
		if p.Text == "" {
			t.Errorf("posting %s has no text", p.ID)
		}
	}
	if focal == nil {
		t.Error("the focal tweet is missing from the postings")
	}
	if len(ids) != 4 {
		t.Errorf("duplicate ids among the postings: %v", ids)
	}
}

// The replies come only from the microdata, so this is the test that catches
// a regression in the merge when the Relay store has no record for them.
func TestReplyMetricsFromMicrodata(t *testing.T) {
	p := statusPage(t)
	tw, err := p.TweetFromPage("1770825760162353449")
	if err != nil {
		t.Fatal(err)
	}
	if tw.Text != "@jack Hello from the future" {
		t.Errorf("text = %q", tw.Text)
	}
	if n := Val(tw.Metrics.Likes); n < 1_000 {
		t.Errorf("likes = %d, too small to have come from the right node", n)
	}
	// Views are an order of magnitude above likes on this reply, which is the
	// pair that catches the two counters being read off each other's node.
	if n := Val(tw.Metrics.Impressions); n < 100_000 {
		t.Errorf("views = %d, too small to have come from the right node", n)
	}
	if tw.Author == nil || tw.Author.Username != "lexfridman" {
		t.Errorf("author = %+v", tw.Author)
	}
}

// X leaves the view count null on a tweet from 2006, and both planes agree.
// Reporting zero would be a claim X never made, so the counter stays absent.
func TestViewsAbsentStaysUnknown(t *testing.T) {
	tw, err := statusPage(t).TweetFromPage("20")
	if err != nil {
		t.Fatal(err)
	}
	if tw.Metrics.Impressions != nil {
		t.Errorf("impressions = %d, want no count at all for a tweet X reports no views for",
			*tw.Metrics.Impressions)
	}
}

func TestUserFromProfilePage(t *testing.T) {
	p, err := ParsePage(UserURL("jack"), capture(t, "profile_jack.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.UserFromPage("jack")
	if err != nil {
		t.Fatal(err)
	}
	if u.RestID != "12" || u.ID != "jack" || u.Username != "jack" {
		t.Errorf("user = %+v", u)
	}
	if u.Name == "" {
		t.Error("no display name")
	}
	if u.Description == "" {
		t.Error("no bio")
	}
	if Val(u.Metrics.Followers) == 0 {
		t.Error("no follower count")
	}
	if u.URL != "https://x.com/jack" {
		t.Errorf("url = %q", u.URL)
	}
}

// The profile page streams its timeline in after the page element closes.
// Nine tweets, at tier 0, from a route the spec had written off.
func TestPostingsFromProfilePage(t *testing.T) {
	p, err := ParsePage(UserURL("jack"), capture(t, "profile_jack.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	posts := p.Postings()
	if len(posts) != 9 {
		t.Fatalf("got %d timeline postings, want 9", len(posts))
	}
	for _, tw := range posts {
		if tw.ID == "" || tw.Text == "" {
			t.Errorf("thin posting: %+v", tw)
		}
		if tw.CreatedAt.IsZero() {
			t.Errorf("posting %s has no timestamp", tw.ID)
		}
	}
}

// Four of @jack's nine postings are somebody else's tweets: the page renders a
// reply with the tweet it answers, and the JSON-LD carries both. Postings is
// right to return them, because on a status page the other authors are the
// replies and they are the answer. A timeline is not a status page, and
// TimelineFromWeb filters.
//
// This is the fixture half of a defect found live: `x timeline jack --tier 0`
// listed @callebtc and @wesbillman among @jack's tweets, and the bytes that
// prove it were already committed here.
func TestAProfileTimelineIsOnlyTheAccountsOwnTweets(t *testing.T) {
	p, err := ParsePage(UserURL("jack"), capture(t, "profile_jack.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	all := p.Postings()
	mine := byAuthor(p.Postings(), "jack")
	if len(mine) == len(all) {
		t.Fatal("the fixture no longer carries a reply parent, so this test proves nothing; recapture a profile page with a reply on it")
	}
	if len(mine) != 5 {
		t.Errorf("got %d of @jack's own tweets out of %d postings, want 5", len(mine), len(all))
	}
	for _, tw := range mine {
		if tw.Author == nil || tw.Author.ID != "jack" {
			t.Errorf("tweet %s is by %+v, not @jack", tw.ID, tw.Author)
		}
	}
	// The handle is matched the way every id in x is: lowercased.
	if len(byAuthor(p.Postings(), "JACK")) != len(mine) {
		t.Error("the filter is case sensitive, and a handle's casing is its owner's business")
	}
}

// NASA does not reply on its profile, so every posting is its own. It is the
// case that says the filter is a filter and not a sieve.
func TestAProfileWithNoRepliesKeepsEveryPosting(t *testing.T) {
	p, err := ParsePage(UserURL("nasa"), capture(t, "profile_nasa.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	all := p.Postings()
	if len(all) == 0 {
		t.Fatal("no postings on the NASA profile capture")
	}
	if got := byAuthor(p.Postings(), "nasa"); len(got) != len(all) {
		t.Errorf("kept %d of %d of @nasa's own postings", len(got), len(all))
	}
}

// The NASA page is the one that caught the counter bug. Its Relay store has no
// user record at all, so every field here comes from the microdata, and two of
// the three counters sit under agentInteractionStatistic rather than
// interactionStatistic. Reading only the latter reported following 0 and
// tweets 0 for an account whose page states both.
func TestUserFromProfilePageWithoutARelayRecord(t *testing.T) {
	p, err := ParsePage(UserURL("nasa"), capture(t, "profile_nasa.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Store.Field("user_result_by_screen_name"); ok {
		t.Fatal("this fixture is meant to have no user record in its relay store")
	}
	u, err := p.UserFromPage("nasa")
	if err != nil {
		t.Fatal(err)
	}
	if u.RestID != "11348282" || u.ID != "nasa" || u.Name != "NASA" {
		t.Errorf("user = %+v", u)
	}
	if Val(u.Metrics.Followers) < 90000000 {
		t.Errorf("followers = %d, want the real count", Val(u.Metrics.Followers))
	}
	if Val(u.Metrics.Following) != 119 {
		t.Errorf("following = %d, want 119", Val(u.Metrics.Following))
	}
	if Val(u.Metrics.Tweets) != 74261 {
		t.Errorf("tweets = %d, want 74261", Val(u.Metrics.Tweets))
	}
	if u.Location != "Pale Blue Dot" {
		t.Errorf("location = %q", u.Location)
	}
	if u.ProfileImage == "" {
		t.Error("no avatar")
	}
	if !hasStr(u.Entities.URLs, "http://www.nasa.gov/") {
		t.Errorf("the bio link is missing: %v", u.Entities.URLs)
	}
	if u.CreatedAt.IsZero() {
		t.Error("no join date")
	}
}

// The same page carries the timeline, which is how x timeline keeps working
// when the syndication window is spent.
func TestTimelineFromProfilePageNasa(t *testing.T) {
	p, err := ParsePage(UserURL("nasa"), capture(t, "profile_nasa.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	posts := p.Postings()
	if len(posts) == 0 {
		t.Fatal("no postings on the profile page")
	}
	for _, tw := range posts {
		if tw.ID == "" || tw.Text == "" {
			t.Errorf("thin posting: %+v", tw)
		}
	}
}

func TestParsePageRejectsAnEmptyDocument(t *testing.T) {
	_, err := ParsePage("https://x.com/i/status/20", "<html><body>nope</body></html>")
	if err == nil {
		t.Fatal("want an error for a page with no data planes")
	}
}

func TestStatusPageURL(t *testing.T) {
	if got := StatusPageURL("20"); got != "https://x.com/i/status/20" {
		t.Errorf("StatusPageURL = %q", got)
	}
}

// A status page mixes the tweet in with the conversation around it, and
// pageReplies is what pulls the two apart. The halves it returns are what
// `x thread` and `x replies` each want.
//
// Tweet 20 is the root of its conversation, so everything else on its page is
// below it and the split only has to lift the tweet out.
func TestPageReplies(t *testing.T) {
	posts := statusPage(t).Postings()
	if len(posts) < 2 {
		t.Fatalf("got %d postings, want the focal tweet and its replies", len(posts))
	}
	focal, replies := pageReplies(posts, "20")
	if focal == nil || focal.ID != "20" {
		t.Fatalf("got focal %v, want tweet 20", focal)
	}
	if len(replies) != len(posts)-1 {
		t.Errorf("splitting %d postings left %d replies, want %d", len(posts), len(replies), len(posts)-1)
	}
	for _, r := range replies {
		if r.ID == "20" {
			t.Error("the focal tweet came back among its own replies")
		}
	}
}

// The page for a reply also renders what the reply is replying to, and that is
// not a reply to it. Nothing on the page says so, so the split goes by id: the
// parent was posted first, so its snowflake is smaller.
func TestPageRepliesDropsWhatIsAboveTheTweet(t *testing.T) {
	p, err := ParsePage(StatusPageURL("1903142823316049977"), capture(t, "status_reply.html.gz"))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	posts := p.Postings()
	if !hasTweet(posts, "1903136743634723031") {
		t.Fatal("the page no longer renders the parent, so this test is not testing anything")
	}
	focal, replies := pageReplies(posts, "1903142823316049977")
	if focal == nil {
		t.Fatal("the page did not yield its own tweet")
	}
	if hasTweet(replies, "1903136743634723031") {
		t.Error("the tweet's parent came back as a reply to it")
	}
	if len(replies) != len(posts)-2 {
		t.Errorf("split %d postings into %d replies, want %d: the tweet and its parent both come out", len(posts), len(replies), len(posts)-2)
	}
}

// The renderer can drop the focal tweet, and the split still works, because it
// compares against the id asked for rather than against a tweet that may not be
// on the page.
func TestPageRepliesWithNoFocalTweet(t *testing.T) {
	posts := statusPage(t).Postings()
	focal, replies := pageReplies(posts, "19")
	if focal != nil {
		t.Errorf("found a focal tweet %s that is not on the page", focal.ID)
	}
	if len(replies) != len(posts) {
		t.Errorf("got %d replies from %d postings, want all of them", len(replies), len(posts))
	}
}
