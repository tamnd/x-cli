package x

import "strings"

// Tier 0 is two surfaces, not one.
//
// The syndication endpoint gives a clean typed tweet with the author, the
// entities and the media. The x.com page gives the bookmark count, the view
// count, the author's follower and following counts, and the replies. Neither
// is a superset of the other, so the tool reads both and merges them rather
// than picking a winner and quietly dropping half the answer.
//
// The merge rule is one line: a field that is already set stays set, and the
// provenance records every surface that contributed. Nothing is overwritten,
// so the order the surfaces are merged in cannot change a value, only add one.

// MergeTweet folds src into dst, filling gaps and never overwriting.
func MergeTweet(dst, src *Tweet) *Tweet {
	if src == nil {
		return dst
	}
	if dst == nil {
		return src
	}
	fillStr(&dst.Text, src.Text)
	fillStr(&dst.URL, src.URL)
	fillStr(&dst.Lang, src.Lang)
	fillStr(&dst.ConversationID, src.ConversationID)
	fillStr(&dst.ReplyTo, src.ReplyTo)
	fillStr(&dst.ReplyToUser, src.ReplyToUser)
	fillStr(&dst.Source, src.Source)
	fillStr(&dst.ReplySettings, src.ReplySettings)
	if dst.CreatedAt.IsZero() {
		dst.CreatedAt = src.CreatedAt
	}
	if !dst.Sensitive {
		dst.Sensitive = src.Sensitive
	}
	dst.IsReply = dst.IsReply || src.IsReply
	dst.IsQuote = dst.IsQuote || src.IsQuote
	dst.IsRetweet = dst.IsRetweet || src.IsRetweet

	fillInt(&dst.Metrics.Likes, src.Metrics.Likes)
	fillInt(&dst.Metrics.Retweets, src.Metrics.Retweets)
	fillInt(&dst.Metrics.Replies, src.Metrics.Replies)
	fillInt(&dst.Metrics.Quotes, src.Metrics.Quotes)
	fillInt(&dst.Metrics.Bookmarks, src.Metrics.Bookmarks)
	fillInt(&dst.Metrics.Impressions, src.Metrics.Impressions)

	dst.Author = MergeUser(dst.Author, src.Author)
	if dst.Quoted == nil {
		dst.Quoted = src.Quoted
	}
	if dst.Retweeted == nil {
		dst.Retweeted = src.Retweeted
	}
	if dst.Place == nil {
		dst.Place = src.Place
	}
	if dst.Poll == nil {
		dst.Poll = src.Poll
	}
	if len(dst.Media) == 0 {
		dst.Media = src.Media
	}
	if len(dst.Edits) == 0 {
		dst.Edits = src.Edits
	}
	dst.Entities = mergeEntities(dst.Entities, src.Entities)
	dst.Provenance = mergeProvenance(dst.Provenance, src.Provenance)
	return dst
}

// MergeUser folds src into dst the same way.
func MergeUser(dst, src *User) *User {
	if src == nil {
		return dst
	}
	if dst == nil {
		return src
	}
	fillStr(&dst.ID, src.ID)
	fillStr(&dst.Username, src.Username)
	fillStr(&dst.Name, src.Name)
	fillStr(&dst.Description, src.Description)
	fillStr(&dst.Location, src.Location)
	fillStr(&dst.URL, src.URL)
	fillStr(&dst.VerifiedType, src.VerifiedType)
	fillStr(&dst.ProfileImage, src.ProfileImage)
	fillStr(&dst.ProfileBanner, src.ProfileBanner)
	fillStr(&dst.PinnedTweet, src.PinnedTweet)
	fillStr(&dst.Kind, src.Kind)
	if dst.CreatedAt.IsZero() {
		dst.CreatedAt = src.CreatedAt
	}
	if !dst.Verified {
		dst.Verified = src.Verified
	}
	if !dst.Protected {
		dst.Protected = src.Protected
	}
	fillInt(&dst.Metrics.Followers, src.Metrics.Followers)
	fillInt(&dst.Metrics.Following, src.Metrics.Following)
	fillInt(&dst.Metrics.Tweets, src.Metrics.Tweets)
	fillInt(&dst.Metrics.Listed, src.Metrics.Listed)
	fillInt(&dst.Metrics.Likes, src.Metrics.Likes)
	fillInt(&dst.Metrics.Media, src.Metrics.Media)
	dst.Entities = mergeEntities(dst.Entities, src.Entities)
	dst.Provenance = mergeProvenance(dst.Provenance, src.Provenance)
	return dst
}

func mergeEntities(dst, src Entities) Entities {
	if len(dst.Hashtags) == 0 {
		dst.Hashtags = src.Hashtags
	}
	if len(dst.Cashtags) == 0 {
		dst.Cashtags = src.Cashtags
	}
	if len(dst.Mentions) == 0 {
		dst.Mentions = src.Mentions
	}
	if len(dst.URLs) == 0 {
		dst.URLs = src.URLs
	}
	return dst
}

// mergeProvenance keeps every surface that contributed, in the order they were
// merged, so that a record says where it came from rather than where it was
// last touched.
func mergeProvenance(dst, src string) string {
	if src == "" {
		return dst
	}
	if dst == "" {
		return src
	}
	for _, p := range strings.Split(dst, "+") {
		if p == src {
			return dst
		}
	}
	return dst + "+" + src
}

func fillStr(dst *string, src string) {
	if *dst == "" {
		*dst = src
	}
}

func fillInt(dst *int, src int) {
	if *dst == 0 {
		*dst = src
	}
}

func hasStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
