package x

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNoUnmodeledKeys is the build-failing test from spec 3003 doc 06 section
// 5.3. Every committed fixture goes through the decoder that would read it, and
// any key X sent that no struct names is a failure unless it is on the list
// below with a reason.
//
// The point is that an upstream addition should be a red build rather than
// missing data. It has already earned its keep: it is how the profile fields X
// moved off `legacy` into `location`, `avatar`, `verification` and `privacy`
// were found, after `x user @nasa` had quietly been returning no location, no
// profile image, and "blue" for a government account.
//
// When this fails, either model the key or add it here with a reason. Adding it
// here without a reason is the one thing that defeats the test.
func TestNoUnmodeledKeys(t *testing.T) {
	for _, c := range []struct {
		fixture string
		at      func([]byte) []json.RawMessage
		into    any
		// stop is a nested result that is a record in its own right. The walker
		// finds it wherever it is buried and checks it against its own type, so
		// checking it a second time through its parent would report every key
		// twice under a longer path.
		stop []string
	}{
		{fixture: "s1_tweet_20.json.gz", at: whole, into: synTweet{}},
		{fixture: "s1_reply_with_parent.json.gz", at: whole, into: synTweet{}, stop: []string{"parent"}},
		{fixture: "s1_reply_with_parent.json.gz", at: parents, into: synTweet{}},
		{fixture: "s3_oembed_20.json.gz", at: whole, into: oembedResp{}},
		{fixture: "s3_oembed_media.json.gz", at: whole, into: oembedResp{}},
		{fixture: "s4_user_nasa.json.gz", at: userResults, into: gqlUserResult{}},
		{fixture: "s4_usertweets_nasa.json.gz", at: tweetResults, into: gqlTweetResult{},
			stop: []string{"core.user_results.result", "quoted_status_result.result",
				"legacy.retweeted_status_result.result"}},
		{fixture: "s4_usertweets_nasa.json.gz", at: userResults, into: gqlUserResult{}},
		{fixture: "s4_space_1dRJZEpyjlNGB.json.gz", at: whole, into: gqlSpace{},
			stop: []string{"data.audioSpace.metadata.creator_results.result",
				"data.audioSpace.participants.admins[].user_results.result",
				"data.audioSpace.participants.speakers[].user_results.result",
				"data.audioSpace.participants.listeners[].user_results.result"}},
		{fixture: "s4_space_1dRJZEpyjlNGB.json.gz", at: userResults, into: gqlUserResult{}},
		{fixture: "s4_space_1MnxnMDeQLeJO.json.gz", at: whole, into: gqlSpace{},
			stop: []string{"data.audioSpace.metadata.creator_results.result",
				"data.audioSpace.participants.admins[].user_results.result",
				"data.audioSpace.participants.speakers[].user_results.result",
				"data.audioSpace.participants.listeners[].user_results.result"}},
		{fixture: "s4_space_1MnxnMDeQLeJO.json.gz", at: userResults, into: gqlUserResult{}},
		{fixture: "s5_trends_us.json.gz", at: elements, into: trendsResp{}},
		{fixture: "s5_places.json.gz", at: elements, into: wirePlace{}},
	} {
		raw := []byte(capture(t, c.fixture))
		objs := c.at(raw)
		if len(objs) == 0 {
			t.Fatalf("%s: found nothing to decode, so the fixture or the walk is wrong", c.fixture)
		}
		seen := map[string]bool{}
		for _, o := range objs {
			for _, p := range unmodeled(o, c.into) {
				seen[p] = true
			}
		}
		var left []string
		for p := range seen {
			if unreadKeys[p] || under(p, c.stop) {
				continue
			}
			left = append(left, p)
		}
		if len(left) > 0 {
			t.Errorf("%s sends %d key(s) nothing reads, over %d object(s):\n%s\nmodel them, or list them in unreadKeys with a reason",
				c.fixture, len(left), len(objs), unmodeledReport(sortStrings(left)))
		}
	}
}

// A test for the test. The list above is only worth anything if unmodeled
// actually finds a key nobody reads, at every depth and through every wrapper
// shape, and a walker that quietly returns nothing would pass every fixture.
func TestUnmodeledFindsKeysAtEveryDepth(t *testing.T) {
	type inner struct {
		Known string `json:"known"`
	}
	type outer struct {
		Known    string           `json:"known"`
		Skipped  string           `json:"-"`
		Inner    inner            `json:"inner"`
		List     []inner          `json:"list"`
		ByName   map[string]inner `json:"by_name"`
		Verbatim json.RawMessage  `json:"verbatim"`
	}
	raw := []byte(`{
		"known": "a", "surprise": 1, "skipped": 2,
		"inner": {"known": "b", "deep": 3},
		"list": [{"known": "c"}, {"known": "d", "in_list": 4}],
		"by_name": {"anything": {"known": "e", "in_map": 5}},
		"verbatim": {"whatever": {"nested": 6}}
	}`)
	got := unmodeled(raw, outer{})
	want := []string{"by_name.{}.in_map", "inner.deep", "list[].in_list", "skipped", "surprise"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// unreadKeys is every key the committed fixtures carry that x does not read,
// with the reason. Nothing goes in here that a fixture has not actually sent,
// so the list is a description of X rather than a guess about it.
var unreadKeys = keySet(
	// The viewer's relationship to the account or the tweet. It answers "what
	// have you done about this", and at tier 0 and tier 1 nobody is asking, so
	// it is the same empty answer every time.
	`follow_request_sent super_followed_by super_following
	 relationship_perspectives legacy.follow_request_sent legacy.notifications
	 legacy.bookmarked legacy.favorited legacy.retweeted
	 legacy.blocked_by legacy.blocking legacy.muting legacy.following
	 legacy.followed_by legacy.can_dm legacy.can_media_tag`,

	// What can be sold here. A tip jar, a subscription, who may send a DM.
	`business_account creator_subscriptions_count super_follow_eligible
	 tipjar_settings dm_permissions media_permissions`,

	// x.com's own rendering hints. The shape to clip an avatar to and whether
	// to draw a Grok button are facts about the web client, not the account.
	`profile_image_shape profile_sort_enabled is_profile_translatable
	 profile_description_language affiliates_highlighted_label
	 parody_commentary_fan_label has_graduated_access verification_info
	 legacy_extended_profile legacy.has_custom_timelines legacy.default_profile
	 legacy.default_profile_image legacy.profile_interstitial_type
	 legacy.translator_type legacy.is_translator
	 legacy.needs_phone_verification legacy.possibly_sensitive_editable
	 grok_analysis_button is_translatable unmention_data views.state
	 news_action_type user.highlighted_label user.profile_image_shape
	 has_nft_avatar identity_profile_labels_highlighted_label
	 __typename`,

	// Dead since the REST v1.1 days. X still sends them and they are still null.
	`legacy.time_zone legacy.utc_offset legacy.fast_followers_count`,

	// A second address for a node the record already identifies. The user id in
	// its GraphQL node form, the author and reply-target as numeric ids when
	// the record names them by handle, and the quote flag when the quoted
	// tweet's own presence says it.
	`id legacy.user_id_str legacy.in_reply_to_user_id_str
	 legacy.is_quote_status in_reply_to_user_id_str
	 entities.user_mentions[].id_str entities.user_mentions[].name`,

	// Offsets into the text, on surface 1's own entity shape. Same reason as
	// the GraphQL ones below: the text is on the record, so a position in it is
	// something a reader computes.
	`entities.user_mentions[].indices`,

	// Kept on legacy for readers that have not migrated to the siblings X moved
	// these to. Reading both would mean picking a winner on every field, and the
	// siblings already win: see gqlUserResult.
	`legacy.name legacy.screen_name legacy.created_at legacy.id_str
	 legacy.location legacy.verified legacy.protected legacy.description
	 legacy.profile_image_url_https legacy.normal_followers_count`,

	// Where a thing sits in the string. The string is in the record, so an
	// offset into it is something a reader can compute and x does not store.
	`display_text_range legacy.display_text_range legacy.entities.description
	 legacy.entities.url.urls[].indices legacy.entities.url.urls[].url
	 legacy.entities.url.urls[].display_url
	 profile_bio.entities.description
	 profile_bio.entities.url.urls[].indices
	 profile_bio.entities.url.urls[].url
	 profile_bio.entities.url.urls[].display_url
	 legacy.entities.urls[].indices legacy.entities.urls[].url
	 legacy.entities.urls[].display_url
	 legacy.entities.user_mentions[].indices
	 legacy.entities.user_mentions[].id_str
	 legacy.entities.user_mentions[].name
	 legacy.entities.media[].indices legacy.entities.media[].url
	 legacy.entities.media[].display_url legacy.entities.media[].id_str
	 legacy.entities.media[].source_status_id_str
	 legacy.entities.media[].source_user_id_str
	 legacy.extended_entities.media[].indices
	 legacy.extended_entities.media[].url
	 legacy.extended_entities.media[].display_url
	 legacy.extended_entities.media[].id_str
	 legacy.extended_entities.media[].source_status_id_str
	 legacy.extended_entities.media[].source_user_id_str`,

	// Account-level moderation state. legacy.possibly_sensitive here is about
	// the account, not the tweet, and x does read the tweet's. The withheld
	// pair is empty on every capture so far, and modeling a shape from an empty
	// value is how a decoder ends up wrong about the one account that has one.
	`legacy.possibly_sensitive legacy.withheld_description legacy.withheld_scope`,

	// Whether the tweet can still be edited, and for how long. The edit history
	// is what x records, and it comes from edit_tweet_ids on the same object.
	`edit_control edit_control.editable_until_msecs edit_control.edits_remaining
	 edit_control.is_edit_eligible isEdited isStaleEdit`,

	// The media plane is M1 and the card and long-form planes are M2. Listed
	// rather than left to fail, so the test stays useful in the meantime, and
	// listed by name so landing those milestones is a matter of deleting lines.
	`card professional timeline note_tweet.is_expandable
	 note_tweet.note_tweet_results.result.id
	 note_tweet.note_tweet_results.result.entity_set
	 note_tweet.note_tweet_results.result.media
	 note_tweet.note_tweet_results.result.richtext`,

	// Media detail the M1 plane will read: alt text, download policy, the crop
	// rectangles, the rendered sizes, the aspect ratio.
	`legacy.entities.media[].additional_media_info
	 legacy.entities.media[].allow_download_status
	 legacy.entities.media[].expanded_url
	 legacy.entities.media[].ext_media_availability
	 legacy.entities.media[].features legacy.entities.media[].media_results
	 legacy.entities.media[].original_info.focus_rects
	 legacy.entities.media[].sizes legacy.entities.media[].video_info.aspect_ratio
	 legacy.extended_entities.media[].additional_media_info
	 legacy.extended_entities.media[].allow_download_status
	 legacy.extended_entities.media[].expanded_url
	 legacy.extended_entities.media[].ext_media_availability
	 legacy.extended_entities.media[].features
	 legacy.extended_entities.media[].media_results
	 legacy.extended_entities.media[].original_info.focus_rects
	 legacy.extended_entities.media[].sizes
	 legacy.extended_entities.media[].video_info.aspect_ratio`,
)

// under reports whether a path is inside one of the nested results that get
// checked on their own.
func under(path string, stop []string) bool {
	for _, s := range stop {
		if strings.HasPrefix(path, s+".") {
			return true
		}
	}
	return false
}

// keys splits whitespace-separated key lists into a set, so the list above can
// be grouped and commented instead of being one entry per line.
func keySet(groups ...string) map[string]bool {
	out := map[string]bool{}
	for _, g := range groups {
		for _, k := range strings.Fields(g) {
			out[k] = true
		}
	}
	return out
}

func sortStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}

// whole is the fixture itself, for a surface that answers with one object.
func whole(b []byte) []json.RawMessage { return []json.RawMessage{b} }

// elements is each member of a top-level array, for the v1.1 routes, which
// answer with one. trends/place.json wraps its single result in an array out of
// REST-era habit and trends/available.json really is a list.
func elements(b []byte) []json.RawMessage {
	var out []json.RawMessage
	if json.Unmarshal(b, &out) != nil {
		return nil
	}
	return out
}

// parents is the tweet a reply replies to, which surface 1 expands in full. It
// is checked on its own because it is the same shape as its child and reporting
// its keys through the child would prefix every path with "parent." and miss the
// entries below.
func parents(b []byte) []json.RawMessage {
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil {
		return nil
	}
	if p, ok := obj["parent"]; ok && string(p) != "null" {
		return []json.RawMessage{p}
	}
	return nil
}

// tweetResults and userResults find every object in a response that a decoder
// would be handed, by the same __typename test the decoders use. Walking rather
// than naming a path is the point: a timeline buries results under pages of
// instruction and module wrappers, and the walk does not care how deep.
func tweetResults(b []byte) []json.RawMessage {
	return results(b, "Tweet", "TweetWithVisibilityResults")
}
func userResults(b []byte) []json.RawMessage { return results(b, "User") }

func results(b []byte, typenames ...string) []json.RawMessage {
	want := map[string]bool{}
	for _, tn := range typenames {
		want[tn] = true
	}
	var out []json.RawMessage
	var walk func(json.RawMessage)
	walk = func(m json.RawMessage) {
		var obj map[string]json.RawMessage
		if json.Unmarshal(m, &obj) == nil {
			var tn string
			if json.Unmarshal(obj["__typename"], &tn) == nil && want[tn] {
				out = append(out, m)
			}
			for _, v := range obj {
				walk(v)
			}
			return
		}
		var arr []json.RawMessage
		if json.Unmarshal(m, &arr) == nil {
			for _, v := range arr {
				walk(v)
			}
		}
	}
	walk(b)
	return out
}
