package x

// Tier 0 is two surfaces, not one.
//
// The syndication endpoint gives a clean typed tweet with the author, the
// entities and the media. The x.com page gives the bookmark count, the view
// count, the author's follower and following counts, and the replies. Neither
// is a superset of the other, so the tool reads both and merges them rather
// than picking a winner and quietly dropping half the answer.
//
// The merge rule is one line: a field that is already set stays set, and the
// envelope records every surface that contributed. Nothing is overwritten, so
// the order the surfaces are merged in cannot change a value, only add one.
//
// Every field the second surface fills is recorded in `via`, which is the whole
// point of merging two surfaces rather than picking one: the record can say the
// text came from s1 and the bookmark count from s8, instead of saying "s1+s8"
// and leaving the reader to guess.

// MergeTweet folds src into dst, filling gaps and never overwriting.
func MergeTweet(dst, src *Tweet) *Tweet {
	if src == nil {
		return dst
	}
	if dst == nil {
		return src
	}
	m := merger{to: &dst.Meta, from: src.Meta.Surface()}
	m.str(&dst.Text, src.Text, "text")
	m.str(&dst.URL, src.URL, "url")
	m.str(&dst.Lang, src.Lang, "lang")
	m.str(&dst.ConversationID, src.ConversationID, "conversation_id")
	m.str(&dst.ReplyTo, src.ReplyTo, "reply_to")
	m.str(&dst.ReplyToUser, src.ReplyToUser, "reply_to_user")
	m.str(&dst.Source, src.Source, "source")
	m.str(&dst.ReplySettings, src.ReplySettings, "reply_settings")
	if dst.CreatedAt.IsZero() && !src.CreatedAt.IsZero() {
		dst.CreatedAt = src.CreatedAt
		m.note("created_at")
	}
	if !dst.Sensitive {
		dst.Sensitive = src.Sensitive
	}
	dst.IsReply = dst.IsReply || src.IsReply
	dst.IsQuote = dst.IsQuote || src.IsQuote
	dst.IsRetweet = dst.IsRetweet || src.IsRetweet
	// Enriching a tweet from a ranked set with a second surface's counters does
	// not make the set it came from chronological, so the flag survives.
	dst.Sample = dst.Sample || src.Sample

	m.num(&dst.Metrics.Likes, src.Metrics.Likes, "likes")
	m.num(&dst.Metrics.Retweets, src.Metrics.Retweets, "retweets")
	m.num(&dst.Metrics.Replies, src.Metrics.Replies, "replies")
	m.num(&dst.Metrics.Quotes, src.Metrics.Quotes, "quotes")
	m.num(&dst.Metrics.Bookmarks, src.Metrics.Bookmarks, "bookmarks")
	m.num(&dst.Metrics.Impressions, src.Metrics.Impressions, "impressions")

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
	if len(dst.Media) == 0 && len(src.Media) > 0 {
		dst.Media = src.Media
		m.note("media")
	}
	if len(dst.Edits) == 0 {
		dst.Edits = src.Edits
	}
	dst.Entities = mergeEntities(dst.Entities, src.Entities)
	m.done(src.Meta)
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
	m := merger{to: &dst.Meta, from: src.Meta.Surface()}
	if dst.Username == "" && src.Username != "" {
		dst.setHandle(src.Username)
		m.note("username")
	}
	m.str(&dst.RestID, src.RestID, "rest_id")
	m.str(&dst.Name, src.Name, "name")
	m.str(&dst.Description, src.Description, "description")
	m.str(&dst.Location, src.Location, "location")
	m.str(&dst.Website, src.Website, "website")
	m.str(&dst.VerifiedType, src.VerifiedType, "verified_type")
	m.str(&dst.ProfileImage, src.ProfileImage, "profile_image")
	m.str(&dst.ProfileBanner, src.ProfileBanner, "profile_banner")
	m.str(&dst.PinnedTweet, src.PinnedTweet, "pinned_tweet")
	fillStr(&dst.Role, src.Role)
	if dst.CreatedAt.IsZero() && !src.CreatedAt.IsZero() {
		dst.CreatedAt = src.CreatedAt
		m.note("created_at")
	}
	if !dst.Verified {
		dst.Verified = src.Verified
	}
	if !dst.Protected {
		dst.Protected = src.Protected
	}
	m.num(&dst.Metrics.Followers, src.Metrics.Followers, "followers")
	m.num(&dst.Metrics.Following, src.Metrics.Following, "following")
	m.num(&dst.Metrics.Tweets, src.Metrics.Tweets, "tweets")
	m.num(&dst.Metrics.Listed, src.Metrics.Listed, "listed")
	m.num(&dst.Metrics.Likes, src.Metrics.Likes, "likes")
	m.num(&dst.Metrics.Media, src.Metrics.Media, "media")
	dst.Entities = mergeEntities(dst.Entities, src.Entities)
	m.done(src.Meta)
	return dst
}

// merger fills one record from another and keeps the note of who filled what.
//
// from is the surface src came from, or 0 when src came from more than one, in
// which case there is nothing useful to write in via and the fields are still
// filled.
type merger struct {
	to   *Meta
	from int
}

func (m merger) str(dst *string, src, field string) {
	if *dst == "" && src != "" {
		*dst = src
		m.note(field)
	}
}

// num fills a counter from another record. A zero another surface published is
// a fact and is copied; a counter nobody published is nil and copies nothing.
func (m merger) num(dst **int, src *int, field string) {
	if *dst == nil && src != nil {
		*dst = src
		m.note(field)
	}
}

func (m merger) note(field string) {
	if m.from != 0 {
		m.to.Note(field, m.from)
	}
}

// done folds src's envelope into dst's: every surface and every URL that
// contributed, in the order they were read.
func (m merger) done(src Meta) {
	for i, code := range src.Surfaces {
		url := ""
		if i < len(src.Sources) {
			url = src.Sources[i]
		}
		m.to.Stamp(surfaceNum(code), url)
	}
	for field, code := range src.Via {
		if _, taken := m.to.Via[field]; !taken {
			m.to.Note(field, surfaceNum(code))
		}
	}
	for _, note := range src.Missed {
		if !hasStr(m.to.Missed, note) {
			m.to.Missed = append(m.to.Missed, note)
		}
	}
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

func fillStr(dst *string, src string) {
	if *dst == "" {
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
