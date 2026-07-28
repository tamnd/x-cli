package x

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// snowflakeAt builds the id X would mint at a given time, which is the only way
// to test the age rule without pinning it to a tweet that keeps getting older.
func snowflakeAt(t time.Time) string {
	return strconv.FormatUint(uint64(t.UnixMilli()-tweetEpoch)<<22, 10)
}

func TestAFreshTweetCachesForAMinuteAndAnOldOneForADay(t *testing.T) {
	fresh := snowflakeAt(time.Now().Add(-5 * time.Minute))
	if got := tweetTTL(fresh); got != ttlFreshTweet {
		t.Errorf("a five minute old tweet caches for %v, want %v: its counts are the story", got, ttlFreshTweet)
	}
	old := snowflakeAt(time.Now().Add(-48 * time.Hour))
	if got := tweetTTL(old); got != ttlTweet {
		t.Errorf("a two day old tweet caches for %v, want %v", got, ttlTweet)
	}
}

func TestATweetFromBeforeSnowflakesCachesForADay(t *testing.T) {
	// Tweet 20 is from 2006 and its id carries no timestamp at all. Guessing
	// young would spend a request a minute on a tweet that has not changed in
	// twenty years.
	if _, ok := tweetAge("20"); ok {
		t.Error("a 2006 id claimed to carry a timestamp")
	}
	if got := tweetTTL("20"); got != ttlTweet {
		t.Errorf("tweet 20 caches for %v, want %v", got, ttlTweet)
	}
	if got := tweetTTL("not-an-id"); got != ttlTweet {
		t.Errorf("junk caches for %v, want %v", got, ttlTweet)
	}
}

func TestSnowflakeAgeMatchesARealTweet(t *testing.T) {
	// 1841479827131064700 was posted on 2024-10-02. The arithmetic is the whole
	// of the epoch claim, so pin it against a date rather than against itself.
	age, ok := tweetAge("1841479827131064700")
	if !ok {
		t.Fatal("a 2024 id carries no timestamp")
	}
	posted := time.Now().Add(-age)
	if posted.Year() != 2024 || posted.Month() != time.October {
		t.Errorf("minted %v, want October 2024", posted.UTC().Format("2006-01-02"))
	}
}

func TestGraphQLCachesATweetByItsOwnAge(t *testing.T) {
	fresh := snowflakeAt(time.Now().Add(-time.Minute))
	if got := gqlTTL("TweetResultByRestId", map[string]any{"tweetId": fresh}); got != ttlFreshTweet {
		t.Errorf("TweetResultByRestId on a fresh tweet caches for %v, want %v", got, ttlFreshTweet)
	}
	if got := gqlTTL("TweetDetail", map[string]any{"focalTweetId": "20"}); got != ttlTweet {
		t.Errorf("TweetDetail on tweet 20 caches for %v, want %v", got, ttlTweet)
	}
	if got := gqlTTL("UserByScreenName", map[string]any{"screen_name": "nasa"}); got != ttlProfile {
		t.Errorf("a profile caches for %v, want %v", got, ttlProfile)
	}
	if got := gqlTTL("SearchTimeline", nil); got != ttlTimeline {
		t.Errorf("a search caches for %v, want %v", got, ttlTimeline)
	}
}

// X answers 400 with `{"error":"Bad request."}` for an id it will not even look
// up, and only 404 for one it looked up and did not find. That put an id past
// the snowflake range at exit 1 with a line of JSON in the message, where the
// reader wanted "no such tweet" at exit 6, and it cost four requests to get
// there.
//
// The id is read rather than the status mapped, on purpose. Mapping 400 to
// not-found would mean that the day the token in the syndication URL stops being
// accepted, every tweet on X reports as deleted at exit 6 and nothing says
// otherwise.
func TestAnIDThatCouldNotNameATweet(t *testing.T) {
	for _, c := range []struct {
		id   string
		want bool
		why  string
	}{
		{"20", true, "the first tweet, from before snowflakes"},
		{"1", true, "a sequential id, which carries no timestamp to disbelieve"},
		{"1833951636005552366", true, "a real tweet from 2024"},
		{"2082201201714614765", true, "a real tweet from 2026"},
		{"12345678901234567890", false, "fits in 64 bits and is minted decades from now"},
		{"99999999999999999999", false, "does not fit in 64 bits at all"},
		{"", false, "empty"},
		{"jack", false, "a handle, not an id"},
		{"-20", false, "negative"},
		{"0x14", false, "hex"},
	} {
		if got := possibleTweetID(c.id); got != c.want {
			t.Errorf("possibleTweetID(%q) = %v, want %v: %s", c.id, got, c.want, c.why)
		}
	}
}

// The bound moves with the clock rather than sitting at a hardcoded id, so an id
// minted a second from now is fine and one minted next year is not.
func TestTheFutureBoundIsTheClock(t *testing.T) {
	if !possibleTweetID(snowflakeAt(time.Now().Add(time.Hour))) {
		t.Error("an id an hour ahead is inside the slack and should pass")
	}
	if possibleTweetID(snowflakeAt(time.Now().AddDate(1, 0, 0))) {
		t.Error("an id a year ahead should not")
	}
}

// The two not-found reasons are different facts and have to read differently.
// "deleted, suspended, or protected" is what X said when it was asked. An id it
// would never have minted was never asked about, and telling a reader with a
// typo that the tweet used to be there sends them looking for an archive.
func TestNotFoundSaysWhichKindOfNotFound(t *testing.T) {
	asked := (&NotFoundError{Kind: "tweet", Ref: "1"}).Error()
	if !strings.Contains(asked, "deleted, suspended, or protected") {
		t.Errorf("a tweet X was asked about reads as %q", asked)
	}
	refused := (&NotFoundError{Kind: "tweet", Ref: "12345678901234567890", Why: notATweetID}).Error()
	if !strings.Contains(refused, notATweetID) {
		t.Errorf("an impossible id reads as %q", refused)
	}
	if strings.Contains(refused, "deleted") {
		t.Errorf("an impossible id should not claim the tweet was deleted: %q", refused)
	}
}
