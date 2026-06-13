package x

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Export renders a user's stored tweets as browseable Markdown (spec §3.3):
// one file per month plus an index, media linked. It reads the local store.
func Export(s *Store, username, outDir string) (int, error) {
	tweets, err := s.TweetsByAuthor(username)
	if err != nil {
		return 0, err
	}
	if len(tweets) == 0 {
		return 0, fmt.Errorf("no stored tweets for @%s (crawl first with --db)", username)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 0, err
	}
	byMonth := map[string][]*Tweet{}
	for _, t := range tweets {
		key := t.CreatedAt.Format("2006-01")
		if t.CreatedAt.IsZero() {
			key = "undated"
		}
		byMonth[key] = append(byMonth[key], t)
	}
	months := make([]string, 0, len(byMonth))
	for m := range byMonth {
		months = append(months, m)
	}
	sort.Strings(months)

	var idx strings.Builder
	fmt.Fprintf(&idx, "# @%s — %d tweets\n\n", username, len(tweets))
	for _, m := range months {
		fmt.Fprintf(&idx, "- [%s](%s.md) (%d)\n", m, m, len(byMonth[m]))
		if err := writeMonth(outDir, m, byMonth[m]); err != nil {
			return 0, err
		}
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.md"), []byte(idx.String()), 0o644); err != nil {
		return 0, err
	}
	return len(tweets), nil
}

func writeMonth(outDir, month string, tweets []*Tweet) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", month)
	for _, t := range tweets {
		ts := t.CreatedAt.Format(time.RFC3339)
		fmt.Fprintf(&b, "### %s\n\n", ts)
		fmt.Fprintf(&b, "%s\n\n", t.Text)
		fmt.Fprintf(&b, "♥ %d  ↺ %d  💬 %d — [%s](%s)\n\n",
			t.Metrics.Likes, t.Metrics.Retweets, t.Metrics.Replies, t.ID, t.URL)
		for _, m := range t.Media {
			u := m.URL
			if u == "" && len(m.Variants) > 0 {
				u = m.Variants[len(m.Variants)-1].URL
			}
			if u != "" {
				fmt.Fprintf(&b, "![%s](%s)\n\n", m.Type, u)
			}
		}
		b.WriteString("---\n\n")
	}
	return os.WriteFile(filepath.Join(outDir, month+".md"), []byte(b.String()), 0o644)
}
