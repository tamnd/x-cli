package x

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// A timeline is the one read where the answer's shape is not the answer's
// meaning. Twenty rows can be the last twenty tweets or twenty of the account's
// most-liked posts from the last twenty years, and the payload says which only
// if you look at the ids.

// widget parses a syndication profile-widget capture the way ProfileTimeline
// does, without the request in front of it.
func widget(t *testing.T, name string) []*Tweet {
	t.Helper()
	raw, ok := extractNextData([]byte(capture(t, name)))
	if !ok {
		t.Fatalf("no __NEXT_DATA__ in %s", name)
	}
	var data struct {
		Props struct {
			PageProps struct {
				Timeline struct {
					Entries []struct {
						Content struct {
							Tweet *legacyTweet `json:"tweet"`
						} `json:"content"`
					} `json:"entries"`
				} `json:"timeline"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	var out []*Tweet
	for _, e := range data.Props.PageProps.Timeline.Entries {
		if e.Content.Tweet == nil || e.Content.Tweet.IDStr == "" {
			continue
		}
		out = append(out, e.Content.Tweet.toTweet(nil, ""))
	}
	if len(out) == 0 {
		t.Fatalf("%s carried no tweets", name)
	}
	markSample(out)
	return out
}

// The widget returns two different things under one URL and never says which.
// @NASA posts several times a day and gets a window; @jack has not posted in a
// while and gets a ranking. Both captures are live, taken the same afternoon.
func TestTheWidgetReturnsAWindowOrARanking(t *testing.T) {
	window := widget(t, "s2_timeline_nasa.html.gz")
	if len(window) != 20 {
		t.Errorf("the @NASA capture has %d entries, want 20", len(window))
	}
	for i, tw := range window {
		if tw.Sample {
			t.Fatalf("entry %d of a chronological window is flagged as a sample", i)
		}
	}
	for i := 1; i < len(window); i++ {
		if idLess(window[i-1].ID, window[i].ID) {
			t.Fatalf("the @NASA capture is not in id order at %d, so this test proves nothing", i)
		}
	}

	ranked := widget(t, "s2_timeline_jack.html.gz")
	if len(ranked) != 101 {
		t.Errorf("the @jack capture has %d entries, want 101", len(ranked))
	}
	for i, tw := range ranked {
		if !tw.Sample {
			t.Fatalf("entry %d of a ranked set is not flagged", i)
		}
	}
	// The evidence for the flag, restated as an assertion: a hundred entries
	// drawn from two decades is not a recent window under any reading of the
	// word. Doc 02 section 1.4 has the capture it came from.
	oldest, newest := ranked[0].CreatedAt.Year(), ranked[0].CreatedAt.Year()
	for _, tw := range ranked {
		if y := tw.CreatedAt.Year(); y < oldest {
			oldest = y
		} else if y > newest {
			newest = y
		}
	}
	if newest-oldest < 15 {
		t.Errorf("the ranked capture spans %d to %d, want the whole account", oldest, newest)
	}
}

// A window with one post lifted out of place is still a window. X pins posts and
// promotes them to the front, and calling that a ranked sample would put the
// caveat on the answer people asked for.
func TestOneDisplacedPostIsNotARanking(t *testing.T) {
	ids := []string{"90", "100", "99", "98", "97"} // an old post pinned first
	set := make([]*Tweet, len(ids))
	for i, id := range ids {
		set[i] = &Tweet{Meta: Meta{ID: id}}
	}
	markSample(set)
	if set[0].Sample {
		t.Error("a pinned post at the front turned a window into a sample")
	}

	// Two out of place is a different claim, and that is where it tips.
	shuffled := []*Tweet{{Meta: Meta{ID: "100"}}, {Meta: Meta{ID: "20"}}, {Meta: Meta{ID: "99"}}, {Meta: Meta{ID: "30"}}, {Meta: Meta{ID: "98"}}}
	markSample(shuffled)
	if !shuffled[0].Sample {
		t.Error("a set in no time order at all was not flagged")
	}
}

// idLess is comparing 19-digit strings, and the naive string compare gets that
// wrong the moment two ids differ in length.
func TestSnowflakesCompareAsNumbers(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"20", "1833951636005552366", true},
		{"1833951636005552366", "20", false},
		{"2081860978694594863", "2081856004237545809", false},
		{"2081856004237545809", "2081860978694594863", true},
		{"20", "20", false},
	} {
		if got := idLess(c.a, c.b); got != c.want {
			t.Errorf("idLess(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

// The walk ends when X stops offering timeline, and a short page is not that.
// The page size is X's choice: doc 01 section 4.3 walked @NASA and got pages of
// 5, 9, 12 and 20 in one run, so a client that stops at the first small page
// truncates an archive at a random depth and reports it as the whole thing.
func TestTheWalkEndsOnTheCursorAndNotOnAShortPage(t *testing.T) {
	for _, c := range []struct {
		why          string
		next, cursor string
		empty        int
		wantEnded    bool
	}{
		{"a fresh cursor keeps going", "c2", "c1", 0, false},
		{"a page with nothing new on it is not the end", "c2", "c1", 1, false},
		{"nor are two", "c2", "c1", 2, false},
		{"three in a row is", "c2", "c1", 3, true},
		{"no bottom cursor is the end", "", "c1", 0, true},
		{"so is being handed back the cursor we sent", "c1", "c1", 0, true},
	} {
		if got := walkEnded(c.next, c.cursor, c.empty); got != c.wantEnded {
			t.Errorf("%s: walkEnded(%q, %q, %d) = %v", c.why, c.next, c.cursor, c.empty, got)
		}
	}
}

// A walk that hands back six hundred of the thousand tweets asked for and exits
// zero is telling the caller the account has six hundred tweets. The rows stay,
// the error says how far it got, and the exit code stays that of whatever
// stopped it.
func TestAPartialWalkKeepsItsRowsAndItsReason(t *testing.T) {
	cause := &RateLimitedError{Msg: "rate limited by X on graphql.UserTweets", Endpoint: "graphql.UserTweets"}
	err := partial(612, "tweets", cause)

	var pe *PartialError
	if !errors.As(err, &pe) || pe.Got != 612 {
		t.Fatalf("got %#v", err)
	}
	if !strings.HasPrefix(err.Error(), "stopped after 612 tweets: ") {
		t.Errorf("the message does not lead with how far it got: %q", err)
	}
	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatal("the cause is not reachable, so the exit code would be 1")
	}
	if f := FailureOf(err, "nasa", 1); f.Code != 5 || f.Surface != "graphql.UserTweets" {
		t.Errorf("the failure record says code %d surface %q, want 5 and the endpoint", f.Code, f.Surface)
	}

	// Nothing delivered is not a partial result, it is a failure, and "stopped
	// after 0 tweets" says less than the cause does on its own.
	if got := partial(0, "tweets", cause); got != error(cause) {
		t.Errorf("got %#v, want the bare cause", got)
	}
	if got := partial(5, "tweets", nil); got != nil {
		t.Errorf("got %#v for a walk that ended cleanly", got)
	}
}
