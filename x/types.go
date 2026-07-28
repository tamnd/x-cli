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

	// Sample says this tweet arrived in a set X ranked rather than a set X
	// walked backwards in time (spec 3003 doc 03 section 2.4).
	//
	// It is a fact about how the record was obtained, not about the tweet, and
	// it is on the tweet because the tweet is what a caller holds. Filtering
	// "the last week" over a ranked set answers a question nobody asked: the
	// widget handed back @jack's most-liked posts from 2006 to 2025, in like
	// order, and the last week of them is not the last week of anything.
	Sample bool `json:"sample,omitempty"`
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

// Place is somewhere on the map, and X publishes it in two shapes that share a
// vocabulary and almost nothing else. A tweet's geotag has a full name and one
// of X's own hex place ids; an entry in the trends directory has a woeid, a
// parent, and no full name at all. They are one kind because X calls them both a
// place and a caller asking for `x://place/23424977` should not have to know
// which door it came through. Every field is optional, so a record shows what
// its source published and stays quiet about the rest.
type Place struct {
	Meta

	FullName string `json:"full_name,omitempty"`
	Name     string `json:"name,omitempty"`

	// WOEID is the Where On Earth ID, from the trends directory. Yahoo! ran the
	// scheme and shut it down in 2019; X kept the numbers, so this is now an
	// identifier with no authority behind it that nonetheless still works.
	WOEID int64 `json:"woeid,omitempty"`

	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	PlaceType   string `json:"place_type,omitempty"`

	// PlaceTypeCode is the numeric form of PlaceType. It is worth keeping
	// alongside the name because the directory sends two different codes, 9 and
	// 22, both named "Unknown", and only the code tells them apart.
	PlaceTypeCode int `json:"place_type_code,omitempty"`

	// ParentID is the woeid this place sits inside, so a town points at its
	// country. Worldwide has 0, which is the top.
	ParentID int64 `json:"parent_id,omitempty"`
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
//
// The fields are what AudioSpaceById sends, measured on a finished Space. An
// earlier draft had Lang, Ticketed, Topics and a subscriber count, none of which
// appear on the wire, and host ids where X sends whole profiles.
type Space struct {
	Meta

	// State is X's own word, capitalised as X capitalises it: Running,
	// Scheduled, Ended, TimedOut, Canceled. It is not normalised, because a
	// Space that timed out and a Space the host ended are not the same event and
	// only X knows which happened.
	State string `json:"state"`

	Title string `json:"title,omitempty"`

	// Creator is who made the Space, which is not always who hosted it. Hosts
	// are the admins in the room.
	Creator   *User   `json:"creator,omitempty"`
	Hosts     []*User `json:"hosts,omitempty"`
	Speakers  []*User `json:"speakers,omitempty"`
	Listeners []*User `json:"listeners,omitempty"`

	// Participants is X's own count of the room, and it does not have to agree
	// with the lengths of the three lists above: a finished Space reports the
	// roster it kept and a total of its own.
	Participants int `json:"participant_count,omitempty"`

	// LiveListeners is how many heard it live, ReplayWatched how many played it
	// back afterwards.
	LiveListeners int `json:"live_listeners,omitempty"`
	ReplayWatched int `json:"replay_watched,omitempty"`

	// MediaKey addresses the audio itself. It is the handle a replay is fetched
	// by, and it is not a tweet id.
	MediaKey string `json:"media_key,omitempty"`

	// ReplayStart is how far into the recording the replay begins, in
	// milliseconds. It is the one duration in a record of timestamps, so it is
	// named for what it is rather than joining the times below.
	ReplayStart int `json:"replay_start_ms,omitempty"`

	// Replayable is whether the recording can be played back at all, Clippable
	// whether listeners may cut pieces out of it. They are separate switches
	// and a host can turn either off.
	Replayable   bool `json:"replayable,omitempty"`
	Clippable    bool `json:"clippable,omitempty"`
	Locked       bool `json:"locked,omitempty"`
	EmployeeOnly bool `json:"employee_only,omitempty"`
	DisallowJoin bool `json:"disallow_join,omitempty"`

	CreatedAt      time.Time `json:"created_at,omitzero"`
	ScheduledStart time.Time `json:"scheduled_start,omitzero"`
	StartedAt      time.Time `json:"started_at,omitzero"`
	EndedAt        time.Time `json:"ended_at,omitzero"`
	UpdatedAt      time.Time `json:"updated_at,omitzero"`
}

// Trend is one trending topic in one place at one moment. All three parts
// matter: the same name trending in Tokyo and in London is two different facts,
// and either of them a week ago is a third.
type Trend struct {
	Meta

	Name string `json:"name"`

	// Query is the search X wants run for this trend, percent-encoded, which is
	// not always the name: a multi-word trend arrives quoted.
	Query     string `json:"query,omitempty"`
	SearchURL string `json:"search_url,omitempty"`

	// Volume is how many posts X counted, and is absent rather than zero when X
	// did not say. That distinction is the whole reason it is a pointer, and it
	// earns its keep: tweet_volume came back null on all 294 trends across six
	// places on the day this was written, so a zero here would be the record
	// inventing a measurement for every trend on X.
	Volume *int `json:"tweet_volume,omitempty"`

	// Promoted says X sold this slot. Null on every capture so far, which is
	// what an unauthenticated read would be expected to see.
	Promoted bool `json:"promoted,omitempty"`

	WOEID     int64  `json:"woeid"`
	PlaceName string `json:"place_name,omitempty"`

	// AsOf is when X computed the list, not when it was read. The two differ by
	// however long the answer sat in a cache, X's or ours.
	AsOf time.Time `json:"as_of,omitzero"`

	// Rank is the position in the list X returned, counting from 1. It is not in
	// the payload; the order is, and this is the order written down, because a
	// stored trend with no rank only says that a thing was trending somewhere in
	// a list of fifty.
	Rank int `json:"rank"`
}

// Bucket is one time-bucketed tweet count (from x counts).
type Bucket struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Count int       `json:"count"`
}
