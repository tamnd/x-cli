package x

import (
	"strconv"
	"time"
)

// ttl.go is doc 01 section 11's cache table as code, in one place, because a
// TTL that lives at its call site is a TTL nobody can check against the doc.
//
// The windows are not guesses. They come from what each surface does: the CDN
// states its own max-age on a fresh tweet, a profile's counters move on the
// order of minutes, and a media URL carries a content hash so it can never go
// stale at all.
const (
	// A tweet's text will not change. Its counts drift, slowly, and nobody
	// minds.
	ttlTweet = 24 * time.Hour

	// Except in the first hour, where the counts are the story and the CDN's
	// own max-age says 60 seconds.
	ttlFreshTweet = 60 * time.Second

	ttlProfile  = 15 * time.Minute   // counts move
	ttlTimeline = 5 * time.Minute    // new tweets arrive
	ttlTrends   = 5 * time.Minute    // roughly how fast they move
	ttlPlaces   = 7 * 24 * time.Hour // the place directory barely changes

	// X says a hundred years on an oEmbed. That is not a serious number to
	// honour, and a month is long enough that nobody pays for the request.
	ttlOEmbed = 30 * 24 * time.Hour

	// x.com publishes no limit on its own pages, so do not lean on it.
	ttlPage = 5 * time.Minute
)

// tweetEpoch is X's snowflake epoch, 2010-11-04 01:42:54.657 UTC. Ids minted
// before it are the sequential ones from 2006 to 2010, which no arithmetic
// recovers a timestamp from.
const tweetEpoch = 1288834974657

// firstSnowflake is the smallest id that carries a timestamp. Anything below it
// is old by definition, which is the answer the caller wants anyway.
const firstSnowflake = 1 << 22

// tweetAge is how long ago a tweet id says it was minted, and false for an id
// too old to say. It is the shift the whole snowflake scheme is built on: the
// top 41 bits are milliseconds since the epoch above.
func tweetAge(id string) (time.Duration, bool) {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil || n < firstSnowflake {
		return 0, false
	}
	minted := time.UnixMilli(int64(n>>22) + tweetEpoch)
	return time.Since(minted), true
}

// notATweetID is why a read refused an id without asking X about it.
const notATweetID = "not an id X could have minted"

// possibleTweetID reports whether an id could name a tweet at all.
//
// It exists because of what surface 1 does with one that could not. X answers
// 400 with `{"error":"Bad request."}` rather than 404, so `x tweet
// 12345678901234567890` came back as an unclassified failure at exit 1 with a
// line of JSON in it, where a reader wanted "no such tweet" at exit 6. The same
// id then cost three more requests on the way down through the web page, oembed
// and GraphQL, all of which were going to say the same thing.
//
// The check is the snowflake's own arithmetic rather than a mapping of 400 to
// not-found, and that is deliberate. Mapping the status would mean that the day
// the token in the URL stops being accepted, every tweet in the world reports as
// deleted, quietly and at exit 6. Reading the id says only what the id says.
//
// A day of slack on the future bound, because the clock this runs on is not X's.
func possibleTweetID(id string) bool {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return false
	}
	if n < firstSnowflake {
		// The sequential ids from 2006 to 2010, of which tweet 20 is one. They
		// carry no timestamp, so there is nothing here to disbelieve.
		return true
	}
	minted := time.UnixMilli(int64(n>>22) + tweetEpoch)
	return !minted.After(time.Now().Add(24 * time.Hour))
}

// tweetTTL is how long a tweet is worth caching, which depends on how old it
// is. An hour after it was posted its counts have stopped being news.
func tweetTTL(id string) time.Duration {
	age, ok := tweetAge(id)
	if ok && age < time.Hour {
		return ttlFreshTweet
	}
	return ttlTweet
}
