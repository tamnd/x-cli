package x

import "testing"

func TestParseTweetRef(t *testing.T) {
	cases := map[string]string{
		"20":                                     "20",
		"https://x.com/jack/status/20":           "20",
		"https://twitter.com/jack/status/20?s=1": "20",
		"http://mobile.twitter.com/x/status/123": "123",
		"x.com/i/web/status/456":                 "456",
	}
	for in, want := range cases {
		got, err := ParseTweetRef(in)
		if err != nil {
			t.Fatalf("ParseTweetRef(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("ParseTweetRef(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParseTweetRef("notatweet"); err == nil {
		t.Error("expected error for non-tweet ref")
	}
}

func TestParseUserRef(t *testing.T) {
	ref, isID, err := ParseUserRef("@jack", false)
	if err != nil || isID || ref != "jack" {
		t.Fatalf("got (%q,%v,%v)", ref, isID, err)
	}
	ref, _, err = ParseUserRef("https://x.com/jack/with_replies", false)
	if err != nil || ref != "jack" {
		t.Fatalf("url ref: got %q err %v", ref, err)
	}
	ref, isID, err = ParseUserRef("12", true)
	if err != nil || !isID || ref != "12" {
		t.Fatalf("forceID: got (%q,%v,%v)", ref, isID, err)
	}
}

func TestURLBuilders(t *testing.T) {
	if got := TweetURL("jack", "20"); got != "https://x.com/jack/status/20" {
		t.Errorf("TweetURL = %q", got)
	}
	if got := UserURL("jack"); got != "https://x.com/jack" {
		t.Errorf("UserURL = %q", got)
	}
}
