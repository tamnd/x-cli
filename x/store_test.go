package x

import (
	"path/filepath"
	"testing"
)

// TweetsByAuthor must match handles case-insensitively: the store keeps X's
// canonical casing ("NASA") while users type whatever case ("nasa"), and every
// other read in the CLI is case-insensitive. A case-sensitive lookup here made
// `x export nasa` silently find nothing after `x timeline nasa --db ...`.
func TestTweetsByAuthorCaseInsensitive(t *testing.T) {
	st, err := OpenStore(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	tw := &Tweet{ID: "1", Text: "hi", Author: &User{ID: "11", Username: "NASA"}}
	if err := st.UpsertTweet(tw); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"NASA", "nasa", "Nasa"} {
		got, err := st.TweetsByAuthor(q)
		if err != nil {
			t.Fatalf("TweetsByAuthor(%q): %v", q, err)
		}
		if len(got) != 1 {
			t.Fatalf("TweetsByAuthor(%q) = %d tweets, want 1", q, len(got))
		}
	}
}
