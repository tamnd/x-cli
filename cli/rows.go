package cli

import (
	"strconv"
	"strings"

	"github.com/tamnd/x-cli/x"
)

// Entity → Row mappers (spec §4.4): a curated default column set, full object
// as Value. IDs stay strings throughout.

func tweetRow(t *x.Tweet) Row {
	author := ""
	if t.Author != nil {
		author = t.Author.Username
	}
	created := ""
	if !t.CreatedAt.IsZero() {
		created = t.CreatedAt.Format("2006-01-02 15:04")
	}
	return Row{
		Cols: []string{"id", "created", "author", "likes", "rt", "replies", "text", "url"},
		Vals: []string{
			t.ID, created, author,
			count(t.Metrics.Likes), count(t.Metrics.Retweets), count(t.Metrics.Replies),
			oneline(t.Text), t.URL,
		},
		Value: t,
	}
}

func userRow(u *x.User) Row {
	cols := []string{"username", "name", "followers", "following", "tweets", "verified", "url"}
	vals := []string{
		u.Username, oneline(u.Name),
		count(u.Metrics.Followers), count(u.Metrics.Following), count(u.Metrics.Tweets),
		yn(u.Verified), x.UserURL(u.Username),
	}
	if u.Role != "" {
		cols = append([]string{"role"}, cols...)
		vals = append([]string{u.Role}, vals...)
	}
	return Row{Cols: cols, Vals: vals, Value: u}
}

// spaceRow renders one audio Space. The columns answer "what was this, and did
// it happen": a Scheduled Space has a time in the future and nobody in the room,
// one that Ended or TimedOut has both.
//
// when is the start if it started and the scheduled start if it did not, because
// a Space that never ran still has a time worth printing and two columns for it
// would be a column of blanks either way.
func spaceRow(s *x.Space) Row {
	host := ""
	if s.Creator != nil {
		host = s.Creator.Username
	}
	when := s.StartedAt
	if when.IsZero() {
		when = s.ScheduledStart
	}
	stamp := ""
	if !when.IsZero() {
		stamp = when.Format("2006-01-02 15:04")
	}
	return Row{
		Cols: []string{"id", "state", "when", "host", "speakers", "live", "replays", "title", "url"},
		Vals: []string{
			s.ID, s.State, stamp, host,
			itoa(len(s.Speakers)), itoa(s.LiveListeners), itoa(s.ReplayWatched),
			oneline(s.Title), s.URL,
		},
		Value: s,
	}
}

// nodeRow renders a graph node discovered by `x discover` / `x crawl`. The
// curated columns read the walk at a glance (how deep, by which edge, and a
// one-line who/what) while the full typed node (with the nested tweet or user)
// rides in Value for json, jsonl, and templates.
func nodeRow(n *x.Node) Row {
	var id, who, summary, url string
	switch n.Kind {
	case x.KindTweet:
		t := n.Tweet
		id = t.ID
		if t.Author != nil {
			who = "@" + t.Author.Username
		}
		summary = oneline(t.Text)
		url = t.URL
	case x.KindUser:
		u := n.User
		id = u.ID
		who = "@" + u.Username
		summary = oneline(u.Name)
		url = x.UserURL(u.Username)
	}
	return Row{
		Cols:  []string{"depth", "via", "kind", "id", "who", "summary", "url"},
		Vals:  []string{itoa(n.Depth), string(n.Via), n.Kind, id, who, summary, url},
		Value: n,
	}
}

// mediaRow renders one media item. url is the fetchable address the caller
// resolved with x.MediaURL, which is not always m.URL: a photo carries its size
// in the URL and a video has no single one.
func mediaRow(m x.Media, url string) Row {
	return Row{
		Cols:  []string{"type", "w", "h", "dur_ms", "alt", "url"},
		Vals:  []string{m.Type, itoa(m.Width), itoa(m.Height), itoa(m.Duration), oneline(m.AltText), url},
		Value: mediaValue{Media: m, URL: url},
	}
}

// mediaValue carries the resolved URL alongside the media for -o url/json.
type mediaValue struct {
	x.Media
	URL string `json:"url"`
}

func pollOptionRow(p *x.Poll, o x.PollOption) Row {
	return Row{
		Cols:  []string{"position", "label", "votes", "status"},
		Vals:  []string{itoa(o.Position), oneline(o.Label), itoa(o.Votes), p.VotingStatus},
		Value: o,
	}
}

// trendRow renders one trending topic. The volume column has been a column of
// dashes on every capture taken, because X sends tweet_volume null; it stays
// because a dash is the honest rendering of "X did not say" and dropping the
// column would hide that X used to say.
func trendRow(t *x.Trend) Row {
	return Row{
		Cols:  []string{"rank", "name", "volume", "place", "url"},
		Vals:  []string{itoa(t.Rank), oneline(t.Name), count(t.Volume), t.PlaceName, t.URL},
		Value: t,
	}
}

// placeRow renders one entry of the woeid directory. The woeid comes first
// because it is the thing the reader came for: they are going to paste it into
// `x trends`.
func placeRow(p *x.Place) Row {
	return Row{
		Cols:  []string{"woeid", "name", "country", "type"},
		Vals:  []string{strconv.FormatInt(p.WOEID, 10), oneline(p.Name), p.Country, p.PlaceType},
		Value: p,
	}
}

func bucketRow(b x.Bucket) Row {
	return Row{
		Cols:  []string{"start", "end", "count"},
		Vals:  []string{b.Start.Format("2006-01-02 15:04"), b.End.Format("2006-01-02 15:04"), itoa(b.Count)},
		Value: b,
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// count is for a counter no surface published, which prints as an empty cell.
// Printing 0 there would be the table stating a number nobody gave it.
func count(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

// yn is for a table column, where a blank cell is the readable spelling of no.
func yn(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

// yesno is for a key and value listing, where a blank value reads as missing
// data rather than as an answer. `x auth status` says no out loud.
func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func oneline(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		return s[:79] + "…"
	}
	return s
}
