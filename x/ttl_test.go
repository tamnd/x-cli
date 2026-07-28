package x

import (
	"strconv"
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
