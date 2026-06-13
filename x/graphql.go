package x

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// GraphQL is the client for the web-client GraphQL the x.com browser uses
// (spec §1.6, §13). One type serves both Tier 1 (a minted guest token) and
// Tier 2 (the user's own imported cookies); the only difference is the headers
// session.go attaches. The query IDs rotate when X redeploys, so every one is
// overridable via config (spec §13.3).
type GraphQL struct {
	c   *Client
	s   *Session
	cfg Config
}

// NewGraphQL builds a GraphQL client over a shared HTTP client and session.
func NewGraphQL(c *Client, s *Session, cfg Config) *GraphQL {
	return &GraphQL{c: c, s: s, cfg: cfg}
}

// defaultQueryIDs is the known-good operation → query-id table. These are the
// public, non-secret hashes the web client ships; they rotate on redeploy and
// are overridable with `x config set graphql.query_id.<Op> <hash>` or --query-id.
var defaultQueryIDs = map[string]string{
	"TweetResultByRestId":      "8CEYnZhCp0dx9DFyyEBlbQ",
	"TweetDetail":              "meGUdoK_ryVZ0daBK-HJ2g",
	"UserByScreenName":         "681MIj51w00Aj6dY0GXnHw",
	"UserByRestId":             "IBScZCvFJadZC25ubLYNRQ",
	"UserTweets":               "RyDU3I9VJtPF-Pnl6vrRlw",
	"UserTweetsAndReplies":     "plVqzvVGaDxbFEPoOe_i-A",
	"UserMedia":                "Ecl7YvFIuRaUPonVOHzoOA",
	"Likes":                    "enfPHxWV3DDAG1XBw3obTg",
	"SearchTimeline":           "yIphfmxUO-hddQHKIOk9tA",
	"Followers":                "9jsVJ9l2uXUIKslHvJqIhw",
	"Following":                "OLm4oHZBfqWx8jbcEhWoFw",
	"Favoriters":               "E-ZTxvWWIkmOKwYdNTEefg",
	"Retweeters":               "0BoJlKAxoNPQUHRftlwZ2w",
	"ListLatestTweetsTimeline": "27HKUy8ulrflZ9Tole038g",
	"ListByRestId":             "9VW7EyVQEX88LujnchNXXA",
	"ListMembers":              "H_0zFfjp73xGZrJpY-C2IQ",
	"AudioSpaceById":           "fYAuJHiY3TmYdBmrRtIKhA",
	"HomeTimeline":             "MP5Mn45hEc4i_q_UwIHBkw",
	"Bookmarks":                "QUjXply7fA7fk05FRyajEg",
}

func (g *GraphQL) queryID(op string) string {
	if id, ok := g.cfg.QueryIDs[op]; ok && id != "" {
		return id
	}
	return defaultQueryIDs[op]
}

// defaultFeatures is the feature-flag blob X requires on most operations. X
// errors if a newly added flag is missing, so this is a maintenance burden and
// is overridable via config (spec §13.1).
const defaultFeatures = `{"android_ad_formats_media_component_render_overlay_enabled":false,"android_graphql_skip_api_media_color_palette":false,"android_professional_link_spotlight_display_enabled":false,"blue_business_profile_image_shape_enabled":false,"commerce_android_shop_module_enabled":false,"creator_subscriptions_subscription_count_enabled":false,"creator_subscriptions_tweet_preview_api_enabled":true,"freedom_of_speech_not_reach_fetch_enabled":true,"graphql_is_translatable_rweb_tweet_is_translatable_enabled":true,"hidden_profile_likes_enabled":false,"highlights_tweets_tab_ui_enabled":false,"interactive_text_enabled":false,"longform_notetweets_consumption_enabled":true,"longform_notetweets_inline_media_enabled":true,"longform_notetweets_rich_text_read_enabled":true,"longform_notetweets_richtext_consumption_enabled":true,"mobile_app_spotlight_module_enabled":false,"responsive_web_edit_tweet_api_enabled":true,"responsive_web_enhance_cards_enabled":false,"responsive_web_graphql_exclude_directive_enabled":true,"responsive_web_graphql_skip_user_profile_image_extensions_enabled":false,"responsive_web_graphql_timeline_navigation_enabled":true,"responsive_web_media_download_video_enabled":false,"responsive_web_text_conversations_enabled":false,"responsive_web_twitter_article_tweet_consumption_enabled":true,"unified_cards_destination_url_params_enabled":false,"responsive_web_twitter_blue_verified_badge_is_enabled":true,"rweb_lists_timeline_redesign_enabled":true,"spaces_2022_h2_clipping":true,"spaces_2022_h2_spaces_communities":true,"standardized_nudges_misinfo":true,"subscriptions_verification_info_enabled":true,"subscriptions_verification_info_reason_enabled":true,"subscriptions_verification_info_verified_since_enabled":true,"super_follow_badge_privacy_enabled":false,"super_follow_exclusive_tweet_notifications_enabled":false,"super_follow_tweet_api_enabled":false,"super_follow_user_api_enabled":false,"tweet_awards_web_tipping_enabled":false,"tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled":true,"tweetypie_unmention_optimization_enabled":false,"unified_cards_ad_metadata_container_dynamic_card_content_query_enabled":false,"verified_phone_label_enabled":false,"vibe_api_enabled":false,"view_counts_everywhere_api_enabled":true,"premium_content_api_read_enabled":false,"communities_web_enable_tweet_community_results_fetch":true,"responsive_web_jetfuel_frame":true,"responsive_web_grok_image_annotation_enabled":true,"responsive_web_grok_imagine_annotation_enabled":true,"rweb_tipjar_consumption_enabled":true,"profile_label_improvements_pcf_label_in_post_enabled":true,"creator_subscriptions_quote_tweet_preview_enabled":false,"c9s_tweet_anatomy_moderator_badge_enabled":true,"responsive_web_grok_analyze_post_followups_enabled":true,"rweb_video_timestamps_enabled":false,"responsive_web_grok_share_attachment_enabled":true,"articles_preview_enabled":true,"immersive_video_status_linkable_timestamps":false,"articles_api_enabled":true,"responsive_web_grok_analysis_button_from_backend":true,"rweb_video_screen_enabled":false,"payments_enabled":false,"responsive_web_profile_redirect_enabled":false,"responsive_web_grok_show_grok_translated_post":false,"responsive_web_grok_community_note_auto_translation_is_enabled":false,"profile_label_improvements_pcf_label_in_profile_enabled":false,"grok_android_analyze_trend_fetch_enabled":false,"grok_translations_community_note_auto_translation_is_enabled":false,"grok_translations_post_auto_translation_is_enabled":false,"grok_translations_community_note_translation_is_enabled":false,"grok_translations_timeline_user_bio_auto_translation_is_enabled":false,"subscriptions_feature_can_gift_premium":false,"responsive_web_twitter_article_notes_tab_enabled":false,"subscriptions_verification_info_is_identity_verified_enabled":false,"hidden_profile_subscriptions_enabled":false,"responsive_web_grok_annotations_enabled":false,"post_ctas_fetch_enabled":false,"responsive_web_grok_analyze_button_fetch_trends_enabled":false}`

func (g *GraphQL) features() string {
	if g.cfg.Features != "" {
		return g.cfg.Features
	}
	return defaultFeatures
}

// defaultFieldToggles are the per-operation fieldToggles blobs the web client
// sends alongside features. Operations not listed send no fieldToggles.
var defaultFieldToggles = map[string]string{
	"UserByScreenName":     `{"withPayments":false,"withAuxiliaryUserLabels":true}`,
	"UserByRestId":         `{"withAuxiliaryUserLabels":true}`,
	"UserTweets":           `{"withArticlePlainText":false}`,
	"UserTweetsAndReplies": `{"withArticlePlainText":false}`,
	"UserMedia":            `{"withArticlePlainText":false}`,
	"Likes":                `{"withArticlePlainText":false}`,
	"TweetDetail":          `{"withArticleRichContentState":true,"withArticlePlainText":true,"withGrokAnalyze":false,"withDisallowedReplyControls":false}`,
}

func (g *GraphQL) fieldToggles(op string) string {
	return defaultFieldToggles[op]
}

// getCacheTTL is the read cache window for a GraphQL GET. Single objects cache
// longer; timelines/searches cache short so re-runs stay fresh.
func gqlTTL(op string) time.Duration {
	switch op {
	case "TweetResultByRestId", "UserByScreenName", "UserByRestId", "AudioSpaceById":
		return time.Hour
	default:
		return 2 * time.Minute
	}
}

// get performs a read GraphQL GET, returning the decoded data tree.
func (g *GraphQL) get(ctx context.Context, op string, variables map[string]any) ([]byte, error) {
	id := g.queryID(op)
	if id == "" {
		return nil, fmt.Errorf("no query id for operation %q (set graphql.query_id.%s)", op, op)
	}
	vb, _ := json.Marshal(variables)
	u := fmt.Sprintf("https://x.com/i/api/graphql/%s/%s?variables=%s&features=%s",
		id, op, url.QueryEscape(string(vb)), url.QueryEscape(g.features()))
	if ft := g.fieldToggles(op); ft != "" {
		u += "&fieldToggles=" + url.QueryEscape(ft)
	}
	// A guest token X has invalidated server-side comes back as 401/403 even
	// though it has not hit our TTL. Drop it and re-mint once before giving up,
	// so a stale cached token self-heals instead of surfacing as needs-auth.
	for attempt := 0; ; attempt++ {
		h, err := g.s.authHeaders(ctx, g.c)
		if err != nil {
			return nil, err
		}
		if pu, perr := url.Parse(u); perr == nil {
			// The TID is verified against the request path only; the web client
			// hashes url.pathname, never the ?variables&features query string.
			// Hashing RequestURI() (path+query) yields a TID X rejects with an
			// empty-body 404 on its stricter endpoints (search, the follow graph)
			// while laxer ones (likes, media) wave it through — which looked like
			// a per-operation outage until the path was the common cause.
			if tid := g.transactionID(ctx, http.MethodGet, pu.Path); tid != "" {
				h.Set("x-client-transaction-id", tid)
			}
		}
		b, err := g.c.Do(ctx, Req{URL: u, Endpoint: "graphql." + op, Header: h, CacheTTL: gqlTTL(op)})
		if err == nil {
			return b, nil
		}
		if attempt == 0 && !g.s.IsUser() && isAuthReject(err) {
			g.s.invalidateGuest()
			continue
		}
		return nil, gqlError(err)
	}
}

// isAuthReject reports whether an HTTP failure is X rejecting the credentials
// (401/403), the signal that a guest token needs re-minting.
func isAuthReject(err error) bool {
	he, ok := err.(*HTTPError)
	return ok && (he.Status == 401 || he.Status == 403)
}

// gqlError maps an HTTP failure to a typed error the CLI turns into an exit code.
func gqlError(err error) error {
	he, ok := err.(*HTTPError)
	if !ok {
		return err
	}
	switch he.Status {
	case 401, 403:
		return &NeedAuthError{Msg: "X rejected this request — pass --guest or run `x auth import` to use your own session", User: true}
	case 429:
		return &RateLimitedError{Msg: "rate limited by X; try again later or slow down with --rate"}
	case 404:
		return &NotFoundError{Kind: "object", Ref: ""}
	}
	return err
}

// ---- typed result wrappers ----

type gqlTweetResult struct {
	Typename  string          `json:"__typename"`
	RestID    string          `json:"rest_id"`
	Core      *gqlTweetCore   `json:"core"`
	Legacy    *legacyTweet    `json:"legacy"`
	Views       *gqlViews       `json:"views"`
	NoteTweet   *gqlNoteTweet   `json:"note_tweet"`
	Quoted      *gqlResultWrap  `json:"quoted_status_result"`
	EditControl *gqlEditControl `json:"edit_control"`
	Source      string          `json:"source"`
	Tweet       *gqlTweetResult `json:"tweet"` // TweetWithVisibilityResults wrapper
}

// gqlEditControl carries the edit history: every version of a tweet shares one
// list of ids, newest last. More than one id means the tweet was edited.
type gqlEditControl struct {
	EditTweetIDs []string `json:"edit_tweet_ids"`
}

type gqlTweetCore struct {
	UserResults struct {
		Result gqlUserResult `json:"result"`
	} `json:"user_results"`
}

type gqlViews struct {
	Count string `json:"count"`
}

type gqlNoteTweet struct {
	NoteTweetResults struct {
		Result struct {
			Text string `json:"text"`
		} `json:"result"`
	} `json:"note_tweet_results"`
}

type gqlResultWrap struct {
	Result *gqlTweetResult `json:"result"`
}

type gqlUserResult struct {
	Typename       string      `json:"__typename"`
	RestID         string      `json:"rest_id"`
	IsBlueVerified bool        `json:"is_blue_verified"`
	Legacy         *legacyUser `json:"legacy"`
	Core           *struct {
		Name       string `json:"name"`
		ScreenName string `json:"screen_name"`
		CreatedAt  string `json:"created_at"`
	} `json:"core"`
}

func (ur gqlUserResult) toUser() *User {
	if ur.Legacy == nil && ur.Core == nil {
		return nil
	}
	lu := ur.Legacy
	if lu == nil {
		lu = &legacyUser{}
	}
	u := lu.toUser(ur.IsBlueVerified)
	if ur.RestID != "" {
		u.ID = ur.RestID
	}
	if ur.Core != nil {
		if u.Username == "" {
			u.Username = ur.Core.ScreenName
		}
		if u.Name == "" {
			u.Name = ur.Core.Name
		}
		if u.CreatedAt.IsZero() {
			u.CreatedAt = twitterTime(ur.Core.CreatedAt)
		}
	}
	return u
}

func (r *gqlTweetResult) build() *Tweet {
	if r == nil {
		return nil
	}
	if r.Tweet != nil { // TweetWithVisibilityResults
		r = r.Tweet
	}
	if r.Legacy == nil {
		return nil
	}
	if r.Legacy.IDStr == "" {
		r.Legacy.IDStr = r.RestID
	}
	var author *User
	if r.Core != nil {
		author = r.Core.UserResults.Result.toUser()
	}
	noteText := ""
	if r.NoteTweet != nil {
		noteText = r.NoteTweet.NoteTweetResults.Result.Text
	}
	t := r.Legacy.toTweet(author, noteText)
	if t.ID == "" {
		t.ID = r.RestID
	}
	if r.Views != nil {
		t.Metrics.Impressions, _ = strconv.Atoi(r.Views.Count)
	}
	if r.Source != "" && t.Source == "" {
		t.Source = sourceName(r.Source)
	}
	if r.EditControl != nil && len(r.EditControl.EditTweetIDs) > 1 {
		t.Edits = r.EditControl.EditTweetIDs
	}
	if r.Quoted != nil && r.Quoted.Result != nil {
		t.Quoted = r.Quoted.Result.build()
		t.IsQuote = true
	}
	return t
}

// ---- generic walkers (instruction trees vary; we collect in array order) ----

func walkTweets(node any, out *[]*Tweet, cursor *string) {
	switch v := node.(type) {
	case []any:
		for _, e := range v {
			walkTweets(e, out, cursor)
		}
	case map[string]any:
		if ct, ok := v["cursorType"].(string); ok && ct == "Bottom" {
			if val, ok := v["value"].(string); ok {
				*cursor = val
			}
		}
		if isTweetResult(v) {
			if t := buildTweetFromMap(v); t != nil {
				*out = append(*out, t)
			}
			return // don't recurse into a built tweet (avoids double-counting quotes)
		}
		for _, e := range v {
			walkTweets(e, out, cursor)
		}
	}
}

func walkUsers(node any, out *[]*User, cursor *string) {
	switch v := node.(type) {
	case []any:
		for _, e := range v {
			walkUsers(e, out, cursor)
		}
	case map[string]any:
		if ct, ok := v["cursorType"].(string); ok && ct == "Bottom" {
			if val, ok := v["value"].(string); ok {
				*cursor = val
			}
		}
		if tn, _ := v["__typename"].(string); tn == "User" {
			if u := buildUserFromMap(v); u != nil {
				*out = append(*out, u)
				return // a real user node: don't recurse into its own subtree
			}
			// A bare User wrapper with no core/legacy is the timeline owner
			// (Followers/Following nest the list under result.timeline). Fall
			// through and keep walking so we reach the entries it contains.
		}
		for _, e := range v {
			walkUsers(e, out, cursor)
		}
	}
}

func isTweetResult(m map[string]any) bool {
	tn, _ := m["__typename"].(string)
	if tn != "Tweet" && tn != "TweetWithVisibilityResults" {
		return false
	}
	if tn == "TweetWithVisibilityResults" {
		return true
	}
	_, hasLegacy := m["legacy"]
	return hasLegacy
}

func buildTweetFromMap(m map[string]any) *Tweet {
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var r gqlTweetResult
	if err := json.Unmarshal(b, &r); err != nil {
		return nil
	}
	return r.build()
}

func buildUserFromMap(m map[string]any) *User {
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	var r gqlUserResult
	if err := json.Unmarshal(b, &r); err != nil {
		return nil
	}
	return r.toUser()
}

// collectTweets / collectUsers decode a GraphQL response and return entities in
// array order plus the bottom cursor for the next page.
func collectTweets(b []byte) ([]*Tweet, string) {
	var tree any
	if err := json.Unmarshal(b, &tree); err != nil {
		return nil, ""
	}
	var out []*Tweet
	var cursor string
	walkTweets(tree, &out, &cursor)
	return out, cursor
}

func collectUsers(b []byte) ([]*User, string) {
	var tree any
	if err := json.Unmarshal(b, &tree); err != nil {
		return nil, ""
	}
	var out []*User
	var cursor string
	walkUsers(tree, &out, &cursor)
	return out, cursor
}

// ---- single-object reads ----

// TweetByID resolves one tweet via TweetResultByRestId.
func (g *GraphQL) TweetByID(ctx context.Context, id string) (*Tweet, error) {
	b, err := g.get(ctx, "TweetResultByRestId", map[string]any{
		"tweetId":                id,
		"withCommunity":          false,
		"includePromotedContent": false,
		"withVoice":              false,
	})
	if err != nil {
		return nil, err
	}
	tweets, _ := collectTweets(b)
	if len(tweets) == 0 {
		return nil, &NotFoundError{Kind: "tweet", Ref: id}
	}
	return tweets[0], nil
}

// UserByName resolves a profile via UserByScreenName.
func (g *GraphQL) UserByName(ctx context.Context, handle string) (*User, error) {
	b, err := g.get(ctx, "UserByScreenName", map[string]any{
		"screen_name": handle,
	})
	if err != nil {
		return nil, err
	}
	users, _ := collectUsers(b)
	if len(users) == 0 {
		return nil, &NotFoundError{Kind: "user", Ref: handle}
	}
	return users[0], nil
}

// UserByRestID resolves a profile by numeric id via UserByRestId.
func (g *GraphQL) UserByRestID(ctx context.Context, id string) (*User, error) {
	b, err := g.get(ctx, "UserByRestId", map[string]any{"userId": id})
	if err != nil {
		return nil, err
	}
	users, _ := collectUsers(b)
	if len(users) == 0 {
		return nil, &NotFoundError{Kind: "user", Ref: id}
	}
	return users[0], nil
}

// resolveUserID turns a handle (or already-numeric id) into a numeric user id.
func (g *GraphQL) resolveUserID(ctx context.Context, ref string, isID bool) (string, error) {
	if isID {
		return ref, nil
	}
	u, err := g.UserByName(ctx, ref)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

// ---- paginated tweet timelines ----

// TimelineOpts controls a user-timeline read.
type TimelineOpts struct {
	Replies bool
	Media   bool
	Limit   int
}

// UserTweets streams a user's tweets, paging UserTweets/…AndReplies/…Media.
func (g *GraphQL) UserTweets(ctx context.Context, userID string, o TimelineOpts, emit func(*Tweet) error) error {
	op := "UserTweets"
	if o.Media {
		op = "UserMedia"
	} else if o.Replies {
		op = "UserTweetsAndReplies"
	}
	return g.pageTweets(ctx, op, func(cursor string) map[string]any {
		return map[string]any{
			"userId":                                 userID,
			"count":                                  20,
			"cursor":                                 cursor,
			"includePromotedContent":                 false,
			"withQuickPromoteEligibilityTweetFields": false,
			"withVoice":                              false,
			"withV2Timeline":                         true,
		}
	}, o.Limit, emit)
}

// SearchQuery controls a search read.
type SearchQuery struct {
	Raw     string
	Product string // Top|Latest|People|Photos|Videos
	Limit   int
}

// Search streams search results via SearchTimeline.
func (g *GraphQL) Search(ctx context.Context, q SearchQuery, emit func(*Tweet) error) error {
	product := q.Product
	if product == "" {
		product = "Latest"
	}
	return g.pageTweets(ctx, "SearchTimeline", func(cursor string) map[string]any {
		return map[string]any{
			"rawQuery":    q.Raw,
			"count":       20,
			"cursor":      cursor,
			"querySource": "typed_query",
			"product":     product,
		}
	}, q.Limit, emit)
}

// Thread streams a conversation via TweetDetail (focal tweet + replies).
func (g *GraphQL) Thread(ctx context.Context, focalID string, limit int, emit func(*Tweet) error) error {
	return g.pageTweets(ctx, "TweetDetail", func(cursor string) map[string]any {
		return map[string]any{
			"focalTweetId":                           focalID,
			"cursor":                                 cursor,
			"referrer":                               "tweet",
			"with_rux_injections":                    false,
			"includePromotedContent":                 false,
			"withCommunity":                          true,
			"withQuickPromoteEligibilityTweetFields": false,
			"withBirdwatchNotes":                     false,
			"withVoice":                              false,
			"withV2Timeline":                         true,
		}
	}, limit, emit)
}

// ListTweets streams a List's timeline via ListLatestTweetsTimeline.
func (g *GraphQL) ListTweets(ctx context.Context, listID string, limit int, emit func(*Tweet) error) error {
	return g.pageTweets(ctx, "ListLatestTweetsTimeline", func(cursor string) map[string]any {
		return map[string]any{"listId": listID, "count": 20, "cursor": cursor}
	}, limit, emit)
}

// Home streams the user's reverse-chron home timeline (session only).
func (g *GraphQL) Home(ctx context.Context, limit int, emit func(*Tweet) error) error {
	return g.pageTweets(ctx, "HomeTimeline", func(cursor string) map[string]any {
		return map[string]any{
			"count":                  20,
			"cursor":                 cursor,
			"includePromotedContent": false,
			"latestControlAvailable": true,
			"withCommunity":          true,
			"seenTweetIds":           []string{},
		}
	}, limit, emit)
}

// Bookmarks streams the user's bookmarks (session only).
func (g *GraphQL) Bookmarks(ctx context.Context, limit int, emit func(*Tweet) error) error {
	return g.pageTweets(ctx, "Bookmarks", func(cursor string) map[string]any {
		return map[string]any{"count": 20, "cursor": cursor, "includePromotedContent": false}
	}, limit, emit)
}

// Likes streams the tweets a user has liked.
func (g *GraphQL) Likes(ctx context.Context, userID string, limit int, emit func(*Tweet) error) error {
	return g.pageTweets(ctx, "Likes", func(cursor string) map[string]any {
		return map[string]any{
			"userId":                 userID,
			"count":                  20,
			"cursor":                 cursor,
			"includePromotedContent": false,
			"withV2Timeline":         true,
		}
	}, limit, emit)
}

// pageTweets is the shared cursor loop for every tweet timeline. It streams rows
// as they arrive, stops at limit, and breaks when a page yields nothing new.
func (g *GraphQL) pageTweets(ctx context.Context, op string, vars func(cursor string) map[string]any, limit int, emit func(*Tweet) error) error {
	seen := map[string]bool{}
	cursor := ""
	n := 0
	for {
		b, err := g.get(ctx, op, vars(cursor))
		if err != nil {
			if n > 0 {
				return nil // partial result already streamed
			}
			return err
		}
		tweets, next := collectTweets(b)
		fresh := 0
		for _, t := range tweets {
			if t == nil || t.ID == "" || seen[t.ID] {
				continue
			}
			seen[t.ID] = true
			fresh++
			if err := emit(t); err != nil {
				return err
			}
			n++
			if limit > 0 && n >= limit {
				return nil
			}
		}
		if next == "" || next == cursor || fresh == 0 {
			return nil
		}
		cursor = next
	}
}

// ---- paginated user lists ----

// engagementVars are the extra toggles the tweet-engagement readers
// (Favoriters, Retweeters) still require; the follow-graph readers reject them.
var engagementVars = map[string]any{
	"withDownvotePerspective":     false,
	"withReactionsMetadata":       false,
	"withReactionsPerspective":    false,
	"withSuperFollowsTweetFields": false,
	"withSuperFollowsUserFields":  false,
}

// Followers / Following / Likers / Retweeters stream User rows.
func (g *GraphQL) Followers(ctx context.Context, userID string, limit int, emit func(*User) error) error {
	return g.pageUsers(ctx, "Followers", "follower", userID, "userId", nil, limit, emit)
}
func (g *GraphQL) Following(ctx context.Context, userID string, limit int, emit func(*User) error) error {
	return g.pageUsers(ctx, "Following", "following", userID, "userId", nil, limit, emit)
}
func (g *GraphQL) Likers(ctx context.Context, tweetID string, limit int, emit func(*User) error) error {
	return g.pageUsers(ctx, "Favoriters", "liker", tweetID, "tweetId", engagementVars, limit, emit)
}
func (g *GraphQL) Retweeters(ctx context.Context, tweetID string, limit int, emit func(*User) error) error {
	return g.pageUsers(ctx, "Retweeters", "retweeter", tweetID, "tweetId", engagementVars, limit, emit)
}

func (g *GraphQL) pageUsers(ctx context.Context, op, kind, id, idKey string, extra map[string]any, limit int, emit func(*User) error) error {
	seen := map[string]bool{}
	cursor := ""
	n := 0
	for {
		vars := map[string]any{
			idKey:                    id,
			"count":                  20,
			"cursor":                 cursor,
			"includePromotedContent": false,
		}
		maps.Copy(vars, extra)
		b, err := g.get(ctx, op, vars)
		if err != nil {
			if n > 0 {
				return nil
			}
			return err
		}
		users, next := collectUsers(b)
		fresh := 0
		for _, u := range users {
			if u == nil || u.ID == "" || seen[u.ID] {
				continue
			}
			seen[u.ID] = true
			fresh++
			u.Kind = kind
			if err := emit(u); err != nil {
				return err
			}
			n++
			if limit > 0 && n >= limit {
				return nil
			}
		}
		if next == "" || next == cursor || fresh == 0 {
			return nil
		}
		cursor = next
	}
}
