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
			itoa(t.Metrics.Likes), itoa(t.Metrics.Retweets), itoa(t.Metrics.Replies),
			oneline(t.Text), t.URL,
		},
		Value: t,
	}
}

func userRow(u *x.User) Row {
	cols := []string{"username", "name", "followers", "following", "tweets", "verified", "url"}
	vals := []string{
		u.Username, oneline(u.Name),
		itoa(u.Metrics.Followers), itoa(u.Metrics.Following), itoa(u.Metrics.Tweets),
		yn(u.Verified), x.UserURL(u.Username),
	}
	if u.Kind != "" {
		cols = append([]string{"kind"}, cols...)
		vals = append([]string{u.Kind}, vals...)
	}
	return Row{Cols: cols, Vals: vals, Value: u}
}

func mediaRow(m x.Media) Row {
	best := m.URL
	if best == "" && len(m.Variants) > 0 {
		best = m.Variants[len(m.Variants)-1].URL
	}
	return Row{
		Cols: []string{"type", "w", "h", "dur_ms", "alt", "url"},
		Vals: []string{m.Type, itoa(m.Width), itoa(m.Height), itoa(m.Duration), oneline(m.AltText), best},
		Value: mediaValue{Media: m, URL: best},
	}
}

// mediaValue carries a resolved best URL alongside the media for -o url/json.
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

func bucketRow(b x.Bucket) Row {
	return Row{
		Cols:  []string{"start", "end", "count"},
		Vals:  []string{b.Start.Format("2006-01-02 15:04"), b.End.Format("2006-01-02 15:04"), itoa(b.Count)},
		Value: b,
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func yn(b bool) string {
	if b {
		return "yes"
	}
	return ""
}

func oneline(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		return s[:79] + "…"
	}
	return s
}
