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
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip %s: %v", name, err)
	}
	defer gz.Close()
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
	for _, c := range []struct {
		name string
		got  int
		want int
	}{
		{"likes", tw.Metrics.Likes, 307403},
		{"retweets", tw.Metrics.Retweets, 124855},
		{"replies", tw.Metrics.Replies, 17964},
		{"quotes", tw.Metrics.Quotes, 6805},
		// Spec 3003 put the bookmark count behind a guest token. It is here,
		// at tier 0, on the plain status page.
		{"bookmarks", tw.Metrics.Bookmarks, 21256},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if tw.Provenance != "s8" {
		t.Errorf("provenance = %q, want s8", tw.Provenance)
	}

	if tw.Author == nil {
		t.Fatal("no author")
	}
	if tw.Author.Username != "jack" || tw.Author.ID != "12" {
		t.Errorf("author = %+v", tw.Author)
	}
	if tw.Author.Metrics.Followers != 10548148 {
		t.Errorf("followers = %d", tw.Author.Metrics.Followers)
	}
	if tw.Author.Metrics.Following != 3 {
		t.Errorf("following = %d", tw.Author.Metrics.Following)
	}
	if tw.Author.Metrics.Tweets != 30786 {
		t.Errorf("tweets = %d", tw.Author.Metrics.Tweets)
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
	if tw.Metrics.Likes != 5195 {
		t.Errorf("likes = %d, want 5195", tw.Metrics.Likes)
	}
	if tw.Metrics.Impressions != 454161 {
		t.Errorf("views = %d, want 454161", tw.Metrics.Impressions)
	}
	if tw.Author == nil || tw.Author.Username != "lexfridman" {
		t.Errorf("author = %+v", tw.Author)
	}
}

// X leaves the view count null on a tweet from 2006, and both planes agree.
// Reporting zero would be a claim X never made.
func TestViewsAbsentStaysZero(t *testing.T) {
	tw, err := statusPage(t).TweetFromPage("20")
	if err != nil {
		t.Fatal(err)
	}
	if tw.Metrics.Impressions != 0 {
		t.Errorf("impressions = %d, want 0 for a tweet X reports no views for",
			tw.Metrics.Impressions)
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
	if u.ID != "12" || u.Username != "jack" {
		t.Errorf("user = %+v", u)
	}
	if u.Name == "" {
		t.Error("no display name")
	}
	if u.Description == "" {
		t.Error("no bio")
	}
	if u.Metrics.Followers == 0 {
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
	if u.ID != "11348282" || u.Username != "nasa" || u.Name != "NASA" {
		t.Errorf("user = %+v", u)
	}
	if u.Metrics.Followers < 90000000 {
		t.Errorf("followers = %d, want the real count", u.Metrics.Followers)
	}
	if u.Metrics.Following != 119 {
		t.Errorf("following = %d, want 119", u.Metrics.Following)
	}
	if u.Metrics.Tweets != 74261 {
		t.Errorf("tweets = %d, want 74261", u.Metrics.Tweets)
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

// A status page renders the replies before the tweet they reply to, which is
// right for a page and wrong for a thread.
func TestFocalFirst(t *testing.T) {
	posts := statusPage(t).Postings()
	if len(posts) < 2 {
		t.Fatalf("got %d postings, want the focal tweet and its replies", len(posts))
	}
	if posts[0].ID == "20" {
		t.Skip("the capture already leads with the focal tweet, so this proves nothing")
	}
	got := focalFirst(posts, "20")
	if got[0].ID != "20" {
		t.Errorf("thread leads with %s, want the focal tweet", got[0].ID)
	}
	if len(got) != len(posts) {
		t.Errorf("reordering dropped a tweet: %d became %d", len(posts), len(got))
	}
	seen := map[string]bool{}
	for _, p := range got {
		if seen[p.ID] {
			t.Errorf("reordering duplicated %s", p.ID)
		}
		seen[p.ID] = true
	}
}

// A tweet that is not in the list leaves the order alone rather than panicking.
func TestFocalFirstWithNoFocalTweet(t *testing.T) {
	posts := statusPage(t).Postings()
	got := focalFirst(posts, "does-not-exist")
	if len(got) != len(posts) || got[0].ID != posts[0].ID {
		t.Error("an unknown id should leave the order untouched")
	}
}
