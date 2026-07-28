// Package x is the library beneath the x command line: the normalized X
// (Twitter) data model and the tiered access clients that fill it.
//
// Three free read tiers feed one set of types: Tier 0 (the public
// syndication/embed endpoint, no auth), Tier 1 (the opt-in guest-token GraphQL
// path), and Tier 2 (the GraphQL API spoken with the user's own session
// cookies). There is no paid API. Every command speaks the types in this file,
// never a tier's wire shape directly.
package x

import "time"

// Tweet (a Post) is the central object. IDs are always strings: an X snowflake
// does not fit in a JSON number without silent corruption in jq/JavaScript.
type Tweet struct {
	Meta

	Text           string    `json:"text" kit:"body"`
	CreatedAt      time.Time `json:"created_at,omitzero"`
	Lang           string    `json:"lang,omitempty"`
	Author         *User     `json:"author,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty" kit:"link,kind=x/tweet,optional"`
	ReplyTo        string    `json:"reply_to,omitempty" kit:"link,kind=x/tweet,optional"`
	ReplyToUser    string    `json:"reply_to_user,omitempty" kit:"link,kind=x/user,optional"`
	Quoted         *Tweet    `json:"quoted,omitempty"`
	Retweeted      *Tweet    `json:"retweeted,omitempty"`
	Metrics        Metrics   `json:"metrics"`
	Entities       Entities  `json:"entities,omitempty"`
	Media          []Media   `json:"media,omitempty"`
	Poll           *Poll     `json:"poll,omitempty"`
	Place          *Place    `json:"place,omitempty"`
	Source         string    `json:"source,omitempty"`
	Sensitive      bool      `json:"possibly_sensitive,omitempty"`
	ReplySettings  string    `json:"reply_settings,omitempty"`
	Edits          []string  `json:"edits,omitempty"`
	IsRetweet      bool      `json:"is_retweet,omitempty"`
	IsQuote        bool      `json:"is_quote,omitempty"`
	IsReply        bool      `json:"is_reply,omitempty"`
}

// Metrics are the engagement counts on a tweet.
//
// Every one is a pointer, because the surfaces disagree about which counters
// they publish and a counter nobody published is not a counter of zero. The
// syndication endpoint has never carried a bookmark count; a tweet nobody has
// bookmarked has none. Written as plain ints both come out as 0, and a graph
// built on that says the same false thing twice.
type Metrics struct {
	Replies     *int `json:"replies,omitempty"`
	Retweets    *int `json:"retweets,omitempty"`
	Likes       *int `json:"likes,omitempty"`
	Quotes      *int `json:"quotes,omitempty"`
	Bookmarks   *int `json:"bookmarks,omitempty"`
	Impressions *int `json:"impressions,omitempty"`
}

// Num wraps a count a surface published, so it can be told from one it did not.
func Num(n int) *int { return &n }

// Val reads a count, treating an unpublished one as zero. It is for arithmetic
// and for a table cell, never for deciding whether the count is known.
func Val(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// setNum fills a counter from a surface that published one, and leaves a
// counter another surface already filled alone.
func setNum(dst **int, n int, ok bool) {
	if ok && *dst == nil {
		*dst = Num(n)
	}
}

// Entities are the parsed surface features of a tweet or a bio.
type Entities struct {
	Hashtags []string `json:"hashtags,omitempty"`
	Cashtags []string `json:"cashtags,omitempty"`
	Mentions []string `json:"mentions,omitempty"`
	URLs     []string `json:"urls,omitempty"`
}

// User is an account/profile.
type User struct {
	Meta

	// Username is the handle in the casing its owner chose. Meta.ID is the same
	// handle lowercased, because X treats @NASA and @nasa as one account and a
	// graph that keeps both is a graph with two nodes for one thing.
	Username string `json:"username"`

	// RestID is the numeric account id. Both are needed and neither replaces the
	// other: the handle addresses the account, the numeric id is what UserTweets
	// takes, and a handle can be given up and taken by someone else.
	RestID string `json:"rest_id,omitempty"`

	Name          string      `json:"name"`
	CreatedAt     time.Time   `json:"created_at,omitzero"`
	Description   string      `json:"description,omitempty" kit:"body"`
	Location      string      `json:"location,omitempty"`
	Website       string      `json:"website,omitempty"`
	Verified      bool        `json:"verified,omitempty"`
	VerifiedType  string      `json:"verified_type,omitempty"`
	Protected     bool        `json:"protected,omitempty"`
	Metrics       UserMetrics `json:"metrics"`
	ProfileImage  string      `json:"profile_image,omitempty"`
	ProfileBanner string      `json:"profile_banner,omitempty"`
	PinnedTweet   string      `json:"pinned_tweet,omitempty" kit:"link,kind=x/tweet,optional"`
	Entities      Entities    `json:"entities,omitempty"`

	// Role is why this user is in the answer: follower, following, liker,
	// retweeter, member. It is a property of the listing, not of the account,
	// which is why it is empty when the user was asked for directly.
	Role string `json:"role,omitempty"`
}

// UserMetrics are the public counters on a profile, pointers for the same
// reason: the profile page states followers, following and tweets, and says
// nothing at all about listed, likes or media.
type UserMetrics struct {
	Followers *int `json:"followers,omitempty"`
	Following *int `json:"following,omitempty"`
	Tweets    *int `json:"tweets,omitempty"`
	Listed    *int `json:"listed,omitempty"`
	Likes     *int `json:"likes,omitempty"`
	Media     *int `json:"media,omitempty"`
}

// Media is one attached photo, video, or gif.
type Media struct {
	Key       string    `json:"key,omitempty"`
	Type      string    `json:"type"` // photo|video|animated_gif
	URL       string    `json:"url,omitempty"`
	Preview   string    `json:"preview_image,omitempty"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	Duration  int       `json:"duration_ms,omitempty"`
	AltText   string    `json:"alt_text,omitempty"`
	Variants  []Variant `json:"variants,omitempty"`
	ViewCount int       `json:"view_count,omitempty"`
}

// Variant is one encoding of a video/gif.
type Variant struct {
	Bitrate     int    `json:"bitrate"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
}

// Poll is an attached poll.
type Poll struct {
	ID           string       `json:"id,omitempty"`
	Options      []PollOption `json:"options"`
	DurationMin  int          `json:"duration_minutes,omitempty"`
	EndDateTime  time.Time    `json:"end_datetime,omitzero"`
	VotingStatus string       `json:"voting_status,omitempty"`
}

// PollOption is one choice in a poll.
type PollOption struct {
	Position int    `json:"position"`
	Label    string `json:"label"`
	Votes    int    `json:"votes"`
}

// Place is a geotag.
type Place struct {
	Meta

	FullName    string `json:"full_name"`
	Name        string `json:"name,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	PlaceType   string `json:"place_type,omitempty"`
}

// List is an X List.
type List struct {
	Meta

	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Owner       *User     `json:"owner,omitempty"`
	Members     int       `json:"member_count"`
	Followers   int       `json:"follower_count"`
	Private     bool      `json:"private,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitzero"`
}

// Space is an audio Space.
type Space struct {
	Meta

	State          string    `json:"state"` // live|scheduled|ended
	Title          string    `json:"title,omitempty"`
	HostIDs        []string  `json:"host_ids,omitempty"`
	SpeakerIDs     []string  `json:"speaker_ids,omitempty"`
	Participants   int       `json:"participant_count,omitempty"`
	Subscribers    int       `json:"subscriber_count,omitempty"`
	StartedAt      time.Time `json:"started_at,omitzero"`
	ScheduledStart time.Time `json:"scheduled_start,omitzero"`
	EndedAt        time.Time `json:"ended_at,omitzero"`
	Lang           string    `json:"lang,omitempty"`
	Ticketed       bool      `json:"is_ticketed,omitempty"`
	Topics         []string  `json:"topics,omitempty"`
}

// Trend is one trending topic.
type Trend struct {
	Name        string `json:"name"`
	Query       string `json:"query,omitempty"`
	TweetVolume int    `json:"tweet_volume,omitempty"`
	URL         string `json:"url,omitempty"`
	Location    string `json:"location,omitempty"`
}

// TrendLocation is a place trends can be asked for (a Yahoo! WOEID).
type TrendLocation struct {
	WOEID       int    `json:"woeid"`
	Name        string `json:"name"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	PlaceType   string `json:"place_type,omitempty"`
	ParentID    int    `json:"parentid,omitempty"`
}

// Bucket is one time-bucketed tweet count (from x counts).
type Bucket struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Count int       `json:"count"`
}
