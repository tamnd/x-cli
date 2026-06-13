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
	ID             string    `json:"id"`
	URL            string    `json:"url"`
	Text           string    `json:"text"`
	CreatedAt      time.Time `json:"created_at"`
	Lang           string    `json:"lang,omitempty"`
	Author         *User     `json:"author,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	ReplyTo        string    `json:"reply_to,omitempty"`
	ReplyToUser    string    `json:"reply_to_user,omitempty"`
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
	Provenance     string    `json:"provenance,omitempty"`
}

// Metrics are the engagement counts on a tweet. The public ones are present on
// most tiers; impressions/bookmarks may be zero where a tier does not expose them.
type Metrics struct {
	Replies     int `json:"replies"`
	Retweets    int `json:"retweets"`
	Likes       int `json:"likes"`
	Quotes      int `json:"quotes"`
	Bookmarks   int `json:"bookmarks"`
	Impressions int `json:"impressions"`
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
	ID            string      `json:"id"`
	Username      string      `json:"username"`
	Name          string      `json:"name"`
	CreatedAt     time.Time   `json:"created_at,omitempty"`
	Description   string      `json:"description,omitempty"`
	Location      string      `json:"location,omitempty"`
	URL           string      `json:"url,omitempty"`
	Verified      bool        `json:"verified,omitempty"`
	VerifiedType  string      `json:"verified_type,omitempty"`
	Protected     bool        `json:"protected,omitempty"`
	Metrics       UserMetrics `json:"metrics"`
	ProfileImage  string      `json:"profile_image,omitempty"`
	ProfileBanner string      `json:"profile_banner,omitempty"`
	PinnedTweet   string      `json:"pinned_tweet,omitempty"`
	Entities      Entities    `json:"entities,omitempty"`
	Kind          string      `json:"kind,omitempty"` // follower|following|liker|retweeter|... when in a list
	Provenance    string      `json:"provenance,omitempty"`
}

// UserMetrics are the public counters on a profile.
type UserMetrics struct {
	Followers int `json:"followers"`
	Following int `json:"following"`
	Tweets    int `json:"tweets"`
	Listed    int `json:"listed"`
	Likes     int `json:"likes,omitempty"`
	Media     int `json:"media,omitempty"`
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
	EndDateTime  time.Time    `json:"end_datetime,omitempty"`
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
	ID          string `json:"id"`
	FullName    string `json:"full_name"`
	Name        string `json:"name,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	PlaceType   string `json:"place_type,omitempty"`
}

// List is an X List.
type List struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Owner       *User     `json:"owner,omitempty"`
	Members     int       `json:"member_count"`
	Followers   int       `json:"follower_count"`
	Private     bool      `json:"private,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

// Space is an audio Space.
type Space struct {
	ID             string    `json:"id"`
	State          string    `json:"state"` // live|scheduled|ended
	Title          string    `json:"title,omitempty"`
	HostIDs        []string  `json:"host_ids,omitempty"`
	SpeakerIDs     []string  `json:"speaker_ids,omitempty"`
	Participants   int       `json:"participant_count,omitempty"`
	Subscribers    int       `json:"subscriber_count,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	ScheduledStart time.Time `json:"scheduled_start,omitempty"`
	EndedAt        time.Time `json:"ended_at,omitempty"`
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
