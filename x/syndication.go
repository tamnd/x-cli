package x

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// syndicationTTL is generous: a single tweet's content rarely changes.
const syndicationTTL = 24 * time.Hour

// syndicationToken derives the non-secret token the embed widget computes from a
// tweet ID: ((id / 1e15) * pi) in base 36, with '0' and '.' stripped. No
// credential is involved; this is exactly what X's own embed JavaScript does.
func syndicationToken(id string) string {
	n := new(big.Int)
	if _, ok := n.SetString(id, 10); !ok {
		return "x"
	}
	f := new(big.Float).SetInt(n)
	f.Quo(f, big.NewFloat(1e15))
	v, _ := f.Float64()
	v *= math.Pi
	// base-36 of the fractional+integer value, like Number.prototype.toString(36)
	s := floatToBase36(v)
	s = strings.ReplaceAll(s, "0", "")
	s = strings.ReplaceAll(s, ".", "")
	if s == "" {
		return "x"
	}
	return s
}

func floatToBase36(v float64) string {
	if v < 0 {
		v = -v
	}
	ip := int64(v)
	fp := v - float64(ip)
	intPart := strconv.FormatInt(ip, 36)
	// up to 12 fractional base-36 digits, enough to match the widget output
	var frac strings.Builder
	frac.WriteByte('.')
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	for i := 0; i < 12 && fp > 0; i++ {
		fp *= 36
		d := int(fp)
		frac.WriteByte(digits[d])
		fp -= float64(d)
	}
	return intPart + frac.String()
}

// TweetByID fetches one tweet via the public syndication endpoint (Tier B).
func TweetByID(ctx context.Context, c *Client, id string) (*Tweet, error) {
	u := fmt.Sprintf("https://cdn.syndication.twimg.com/tweet-result?id=%s&token=%s&lang=en",
		url.QueryEscape(id), syndicationToken(id))
	b, err := c.Do(ctx, Req{URL: u, Endpoint: "syndication.tweet", CacheTTL: syndicationTTL})
	if err != nil {
		return nil, err
	}
	var raw synTweet
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parse syndication response: %w", err)
	}
	if raw.IDStr == "" {
		// The endpoint returns {} or a tombstone for missing/protected tweets.
		return nil, &NotFoundError{Kind: "tweet", Ref: id}
	}
	return raw.toTweet(), nil
}

// synTweet is the syndication wire shape (only the fields we map).
type synTweet struct {
	IDStr             string      `json:"id_str"`
	Text              string      `json:"text"`
	CreatedAt         string      `json:"created_at"`
	Lang              string      `json:"lang"`
	FavoriteCount     int         `json:"favorite_count"`
	ConversationCount int         `json:"conversation_count"`
	Sensitive         bool        `json:"possibly_sensitive"`
	InReplyToScreen   string      `json:"in_reply_to_screen_name"`
	InReplyToStatus   string      `json:"in_reply_to_status_id_str"`
	User              synUser     `json:"user"`
	Entities          synEntities `json:"entities"`
	Photos            []synPhoto  `json:"photos"`
	MediaDetails      []synMedia  `json:"mediaDetails"`
	Video             *synVideo   `json:"video"`
	QuotedTweet       *synTweet   `json:"quoted_tweet"`
	Parent            *synTweet   `json:"parent"`
}

type synUser struct {
	IDStr          string `json:"id_str"`
	Name           string `json:"name"`
	ScreenName     string `json:"screen_name"`
	Verified       bool   `json:"verified"`
	IsBlueVerified bool   `json:"is_blue_verified"`
	ProfileImage   string `json:"profile_image_url_https"`
}

type synEntities struct {
	Hashtags []struct {
		Text string `json:"text"`
	} `json:"hashtags"`
	Symbols []struct {
		Text string `json:"text"`
	} `json:"symbols"`
	UserMentions []struct {
		ScreenName string `json:"screen_name"`
	} `json:"user_mentions"`
	URLs []struct {
		ExpandedURL string `json:"expanded_url"`
	} `json:"urls"`
}

type synPhoto struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type synMedia struct {
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

type synVideo struct {
	DurationMs int    `json:"durationMs"`
	Poster     string `json:"poster"`
	Variants   []struct {
		Type string `json:"type"`
		Src  string `json:"src"`
	} `json:"variants"`
}

func (s *synTweet) toTweet() *Tweet {
	t := &Tweet{
		ID:          s.IDStr,
		Text:        s.Text,
		Lang:        s.Lang,
		Sensitive:   s.Sensitive,
		ReplyTo:     s.InReplyToStatus,
		ReplyToUser: s.InReplyToScreen,
		IsReply:     s.InReplyToStatus != "",
		Metrics:     Metrics{Likes: s.FavoriteCount, Replies: s.ConversationCount},
		Provenance:  "syndication",
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, s.CreatedAt)
	t.Author = &User{
		ID:           s.User.IDStr,
		Username:     s.User.ScreenName,
		Name:         s.User.Name,
		Verified:     s.User.Verified || s.User.IsBlueVerified,
		ProfileImage: s.User.ProfileImage,
		Provenance:   "syndication",
	}
	if s.User.IsBlueVerified {
		t.Author.VerifiedType = "blue"
	}
	t.URL = TweetURL(s.User.ScreenName, s.IDStr)
	// Entities.
	for _, h := range s.Entities.Hashtags {
		t.Entities.Hashtags = append(t.Entities.Hashtags, h.Text)
	}
	for _, h := range s.Entities.Symbols {
		t.Entities.Cashtags = append(t.Entities.Cashtags, h.Text)
	}
	for _, m := range s.Entities.UserMentions {
		t.Entities.Mentions = append(t.Entities.Mentions, m.ScreenName)
	}
	for _, u := range s.Entities.URLs {
		if u.ExpandedURL != "" {
			t.Entities.URLs = append(t.Entities.URLs, u.ExpandedURL)
		}
	}
	// Media: prefer mediaDetails (richer), fall back to photos/video.
	if len(s.MediaDetails) > 0 {
		for _, m := range s.MediaDetails {
			md := Media{
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
	} else {
		for _, p := range s.Photos {
			t.Media = append(t.Media, Media{Type: "photo", URL: p.URL, Width: p.Width, Height: p.Height})
		}
		if s.Video != nil {
			md := Media{Type: "video", Preview: s.Video.Poster, Duration: s.Video.DurationMs}
			for _, v := range s.Video.Variants {
				md.Variants = append(md.Variants, Variant{ContentType: v.Type, URL: v.Src})
			}
			t.Media = append(t.Media, md)
		}
	}
	if s.QuotedTweet != nil {
		t.Quoted = s.QuotedTweet.toTweet()
		t.IsQuote = true
	}
	return t
}

// UserByNameSyndication resolves a profile from a single public tweet by the
// user when possible. The syndication API has no direct profile endpoint, so
// Tier B user resolution is best-effort: it reads the public profile timeline
// widget. Returns a NotFoundError if the handle yields nothing.
func UserByNameSyndication(ctx context.Context, c *Client, handle string) (*User, error) {
	u := "https://syndication.twitter.com/srv/timeline-profile/screen-name/" + url.PathEscape(handle)
	b, err := c.Do(ctx, Req{URL: u, Endpoint: "syndication.profile", CacheTTL: time.Hour})
	if err != nil {
		return nil, err
	}
	// The page embeds a __NEXT_DATA__ JSON blob. Older widget shapes carried a
	// standalone contextProvider.user; the current shape only has the author on
	// each timeline entry, so fall back to that.
	if usr, ok := extractProfileFromNextData(b); ok {
		usr.Provenance = "syndication"
		return usr, nil
	}
	if raw, ok := extractNextData(b); ok {
		if usr, ok := profileFromTimeline(raw, handle); ok {
			usr.Provenance = "syndication"
			return usr, nil
		}
	}
	return nil, &NotFoundError{Kind: "user", Ref: handle}
}

// profileFromTimeline derives the profile from a timeline entry's tweet author.
func profileFromTimeline(raw json.RawMessage, handle string) (*User, bool) {
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
		return nil, false
	}
	for _, e := range data.Props.PageProps.Timeline.Entries {
		lt := e.Content.Tweet
		if lt == nil || lt.User == nil {
			continue
		}
		if handle != "" && !strings.EqualFold(lt.User.ScreenName, handle) {
			continue
		}
		return lt.User.toUser(false), true
	}
	return nil, false
}

func extractProfileFromNextData(page []byte) (*User, bool) {
	const marker = `__NEXT_DATA__" type="application/json">`
	i := strings.Index(string(page), marker)
	if i < 0 {
		return nil, false
	}
	rest := string(page)[i+len(marker):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		return nil, false
	}
	var data struct {
		Props struct {
			PageProps struct {
				ContextProvider struct {
					User struct {
						IDStr        string `json:"id_str"`
						ScreenName   string `json:"screen_name"`
						Name         string `json:"name"`
						Description  string `json:"description"`
						Location     string `json:"location"`
						Verified     bool   `json:"verified"`
						FollowersCnt int    `json:"followers_count"`
						FriendsCnt   int    `json:"friends_count"`
						StatusesCnt  int    `json:"statuses_count"`
						ProfileImage string `json:"profile_image_url_https"`
					} `json:"user"`
				} `json:"contextProvider"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(rest[:j]), &data); err != nil {
		return nil, false
	}
	cu := data.Props.PageProps.ContextProvider.User
	if cu.ScreenName == "" {
		return nil, false
	}
	return &User{
		ID:           cu.IDStr,
		Username:     cu.ScreenName,
		Name:         cu.Name,
		Description:  cu.Description,
		Location:     cu.Location,
		Verified:     cu.Verified,
		ProfileImage: cu.ProfileImage,
		Metrics: UserMetrics{
			Followers: cu.FollowersCnt,
			Following: cu.FriendsCnt,
			Tweets:    cu.StatusesCnt,
		},
	}, true
}

// ProfileTimeline returns a profile's recent public tweets from the
// timeline-profile widget (Tier 0, no auth). The widget embeds a __NEXT_DATA__
// blob whose timeline entries are legacy tweets; one legacy parser maps them.
func ProfileTimeline(ctx context.Context, c *Client, handle string, _ int) ([]*Tweet, error) {
	u := "https://syndication.twitter.com/srv/timeline-profile/screen-name/" + url.PathEscape(handle)
	b, err := c.Do(ctx, Req{URL: u, Endpoint: "syndication.profile", CacheTTL: 5 * time.Minute})
	if err != nil {
		return nil, err
	}
	raw, ok := extractNextData(b)
	if !ok {
		return nil, &NotFoundError{Kind: "user", Ref: handle}
	}
	var data struct {
		Props struct {
			PageProps struct {
				Timeline struct {
					Entries []struct {
						Type    string `json:"type"`
						Content struct {
							Tweet *legacyTweet `json:"tweet"`
						} `json:"content"`
					} `json:"entries"`
				} `json:"timeline"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parse profile timeline: %w", err)
	}
	var out []*Tweet
	for _, e := range data.Props.PageProps.Timeline.Entries {
		if e.Content.Tweet == nil || e.Content.Tweet.IDStr == "" {
			continue
		}
		out = append(out, e.Content.Tweet.toTweet(nil, ""))
	}
	if len(out) == 0 {
		return nil, &NotFoundError{Kind: "timeline", Ref: handle}
	}
	return out, nil
}

// extractNextData pulls the __NEXT_DATA__ JSON island out of a widget page.
func extractNextData(page []byte) (json.RawMessage, bool) {
	const marker = `__NEXT_DATA__" type="application/json">`
	s := string(page)
	i := strings.Index(s, marker)
	if i < 0 {
		return nil, false
	}
	rest := s[i+len(marker):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		return nil, false
	}
	return json.RawMessage(rest[:j]), true
}

// NotFoundError marks a missing/deleted/protected entity (exit code 6).
type NotFoundError struct {
	Kind string
	Ref  string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s (deleted, suspended, or protected)", e.Kind, e.Ref)
}

// statusForBody is a tiny helper used by tests to detect tombstones.
func looksDeleted(b []byte) bool { return len(b) < 3 || string(b) == "{}" }

var _ = http.MethodGet
var _ = looksDeleted
