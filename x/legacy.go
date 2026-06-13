package x

import (
	"strings"
	"time"
)

// The legacy object shape (spec §1.5). Both the public profile-timeline widget
// and the web-client GraphQL return tweets/users in this shape — GraphQL just
// wraps it under result.legacy (+ result.core for the author). One parser here
// feeds every surface, so the normalized types are produced identically.

// twitterTimeLayout is X's "ruby" timestamp format on legacy objects.
const twitterTimeLayout = "Mon Jan 02 15:04:05 -0700 2006"

// twitterTime parses a legacy created_at, tolerating an RFC3339 value too.
func twitterTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(twitterTimeLayout, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// sourceName extracts the client label from X's source field, which arrives as
// an HTML anchor like `<a href="...">Twitter Web App</a>`. A bare string passes
// through unchanged.
func sourceName(s string) string {
	if i := strings.IndexByte(s, '>'); i >= 0 {
		if j := strings.IndexByte(s[i+1:], '<'); j >= 0 {
			return s[i+1 : i+1+j]
		}
	}
	return s
}

// legacyTweet is the legacy tweet JSON (only the fields x maps).
type legacyTweet struct {
	IDStr                string            `json:"id_str"`
	FullText             string            `json:"full_text"`
	Text                 string            `json:"text"`
	CreatedAt            string            `json:"created_at"`
	Lang                 string            `json:"lang"`
	FavoriteCount        int               `json:"favorite_count"`
	RetweetCount         int               `json:"retweet_count"`
	ReplyCount           int               `json:"reply_count"`
	QuoteCount           int               `json:"quote_count"`
	BookmarkCount        int               `json:"bookmark_count"`
	ConversationIDStr    string            `json:"conversation_id_str"`
	InReplyToStatusIDStr string            `json:"in_reply_to_status_id_str"`
	InReplyToScreenName  string            `json:"in_reply_to_screen_name"`
	PossiblySensitive    bool              `json:"possibly_sensitive"`
	Entities             legacyEntities    `json:"entities"`
	ExtendedEntities     legacyExtEntities `json:"extended_entities"`
	User                 *legacyUser       `json:"user"` // present on the widget shape
	RetweetedStatusIDStr string            `json:"retweeted_status_id_str"`
	QuotedStatusIDStr    string            `json:"quoted_status_id_str"`
	Source               string            `json:"source"`
}

type legacyEntities struct {
	Hashtags []struct {
		Text string `json:"text"`
	} `json:"hashtags"`
	Symbols []struct {
		Text string `json:"text"`
	} `json:"symbols"`
	UserMentions []struct {
		ScreenName string `json:"screen_name"`
	} `json:"user_mentions"`
	URLs  []legacyURL   `json:"urls"`
	Media []legacyMedia `json:"media"`
}

type legacyExtEntities struct {
	Media []legacyMedia `json:"media"`
}

type legacyURL struct {
	ExpandedURL string `json:"expanded_url"`
}

type legacyMedia struct {
	MediaKey      string `json:"media_key"`
	Type          string `json:"type"`
	MediaURLHTTPS string `json:"media_url_https"`
	ExtAltText    string `json:"ext_alt_text"`
	OriginalInfo  struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"original_info"`
	VideoInfo *struct {
		DurationMillis int `json:"duration_millis"`
		Variants       []struct {
			Bitrate     int    `json:"bitrate"`
			ContentType string `json:"content_type"`
			URL         string `json:"url"`
		} `json:"variants"`
	} `json:"video_info"`
}

// legacyUser is the legacy user JSON (only the fields x maps).
type legacyUser struct {
	IDStr           string         `json:"id_str"`
	ScreenName      string         `json:"screen_name"`
	Name            string         `json:"name"`
	CreatedAt       string         `json:"created_at"`
	Description     string         `json:"description"`
	Location        string         `json:"location"`
	URL             string         `json:"url"`
	Verified        bool           `json:"verified"`
	Protected       bool           `json:"protected"`
	FollowersCount  int            `json:"followers_count"`
	FriendsCount    int            `json:"friends_count"`
	StatusesCount   int            `json:"statuses_count"`
	ListedCount     int            `json:"listed_count"`
	FavouritesCount int            `json:"favourites_count"`
	MediaCount      int            `json:"media_count"`
	ProfileImage    string         `json:"profile_image_url_https"`
	ProfileBanner   string         `json:"profile_banner_url"`
	PinnedTweetIDs  []string       `json:"pinned_tweet_ids_str"`
	Entities        *legacyUserEnt `json:"entities"`
}

type legacyUserEnt struct {
	URL struct {
		URLs []struct {
			ExpandedURL string `json:"expanded_url"`
		} `json:"urls"`
	} `json:"url"`
}

// toUser converts a legacy user. isBlueVerified comes from the GraphQL wrapper
// (it is not on the legacy object itself), so callers pass it through.
func (lu *legacyUser) toUser(isBlueVerified bool) *User {
	if lu == nil {
		return nil
	}
	u := &User{
		ID:            lu.IDStr,
		Username:      lu.ScreenName,
		Name:          lu.Name,
		CreatedAt:     twitterTime(lu.CreatedAt),
		Description:   lu.Description,
		Location:      lu.Location,
		Verified:      lu.Verified || isBlueVerified,
		Protected:     lu.Protected,
		ProfileImage:  lu.ProfileImage,
		ProfileBanner: lu.ProfileBanner,
		Metrics: UserMetrics{
			Followers: lu.FollowersCount,
			Following: lu.FriendsCount,
			Tweets:    lu.StatusesCount,
			Listed:    lu.ListedCount,
			Likes:     lu.FavouritesCount,
			Media:     lu.MediaCount,
		},
		Provenance: "graphql",
	}
	if isBlueVerified {
		u.VerifiedType = "blue"
	}
	if len(lu.PinnedTweetIDs) > 0 {
		u.PinnedTweet = lu.PinnedTweetIDs[0]
	}
	if lu.Entities != nil && len(lu.Entities.URL.URLs) > 0 {
		u.URL = lu.Entities.URL.URLs[0].ExpandedURL
	}
	if u.URL == "" {
		u.URL = lu.URL
	}
	return u
}

// toTweet converts a legacy tweet. author may be nil when the widget embeds the
// user inline (lt.User); GraphQL passes the core author explicitly. noteText is
// the long-form note_tweet text when present (GraphQL only), else "".
func (lt *legacyTweet) toTweet(author *User, noteText string) *Tweet {
	if author == nil && lt.User != nil {
		author = lt.User.toUser(false)
		author.Provenance = "syndication"
	}
	text := lt.FullText
	if text == "" {
		text = lt.Text
	}
	if noteText != "" {
		text = noteText
	}
	t := &Tweet{
		ID:             lt.IDStr,
		Text:           text,
		CreatedAt:      twitterTime(lt.CreatedAt),
		Lang:           lt.Lang,
		ConversationID: lt.ConversationIDStr,
		ReplyTo:        lt.InReplyToStatusIDStr,
		ReplyToUser:    lt.InReplyToScreenName,
		IsReply:        lt.InReplyToStatusIDStr != "",
		Sensitive:      lt.PossiblySensitive,
		Author:         author,
		Metrics: Metrics{
			Replies:   lt.ReplyCount,
			Retweets:  lt.RetweetCount,
			Likes:     lt.FavoriteCount,
			Quotes:    lt.QuoteCount,
			Bookmarks: lt.BookmarkCount,
		},
		Provenance: "graphql",
	}
	if lt.Source != "" {
		t.Source = sourceName(lt.Source)
	}
	if lt.RetweetedStatusIDStr != "" {
		t.IsRetweet = true
	}
	if lt.QuotedStatusIDStr != "" {
		t.IsQuote = true
	}
	if author != nil {
		t.URL = TweetURL(author.Username, lt.IDStr)
	} else {
		t.URL = TweetURL("", lt.IDStr)
	}
	for _, h := range lt.Entities.Hashtags {
		t.Entities.Hashtags = append(t.Entities.Hashtags, h.Text)
	}
	for _, s := range lt.Entities.Symbols {
		t.Entities.Cashtags = append(t.Entities.Cashtags, s.Text)
	}
	for _, m := range lt.Entities.UserMentions {
		t.Entities.Mentions = append(t.Entities.Mentions, m.ScreenName)
	}
	for _, u := range lt.Entities.URLs {
		if u.ExpandedURL != "" {
			t.Entities.URLs = append(t.Entities.URLs, u.ExpandedURL)
		}
	}
	// Media: extended_entities is richer (carries video variants).
	media := lt.ExtendedEntities.Media
	if len(media) == 0 {
		media = lt.Entities.Media
	}
	for _, m := range media {
		md := Media{
			Key:     m.MediaKey,
			Type:    m.Type,
			URL:     m.MediaURLHTTPS,
			AltText: m.ExtAltText,
			Width:   m.OriginalInfo.Width,
			Height:  m.OriginalInfo.Height,
		}
		if m.VideoInfo != nil {
			md.Duration = m.VideoInfo.DurationMillis
			for _, v := range m.VideoInfo.Variants {
				if v.URL == "" {
					continue
				}
				md.Variants = append(md.Variants, Variant{Bitrate: v.Bitrate, ContentType: v.ContentType, URL: v.URL})
			}
		}
		t.Media = append(t.Media, md)
	}
	return t
}
