package x

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	numericRe    = regexp.MustCompile(`^\d+$`)
	statusPathRe = regexp.MustCompile(`/status(?:es)?/(\d+)`)
)

// ParseTweetRef normalizes any of the accepted tweet references (a bare ID, a
// status URL on x.com/twitter.com/mobile, with or without scheme/query) to the
// canonical numeric tweet ID.
func ParseTweetRef(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty tweet reference")
	}
	if numericRe.MatchString(s) {
		return s, nil
	}
	if m := statusPathRe.FindStringSubmatch(s); m != nil {
		return m[1], nil
	}
	return "", fmt.Errorf("not a tweet id or status URL: %q", s)
}

// ParseUserRef normalizes any accepted user reference to a handle (without the
// leading @) or, when forceID is set or the value is a profile URL ending in a
// numeric id, returns the value as-is. The second return reports whether the
// result should be treated as a numeric user ID rather than a handle.
func ParseUserRef(s string, forceID bool) (ref string, isID bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false, fmt.Errorf("empty user reference")
	}
	s = strings.TrimPrefix(s, "@")
	// A profile URL: take the first path segment after the host.
	if strings.Contains(s, "/") || strings.Contains(s, ".com") {
		s = stripHost(s)
		if i := strings.IndexAny(s, "/?#"); i >= 0 {
			s = s[:i]
		}
	}
	if s == "" {
		return "", false, fmt.Errorf("could not extract a username from the reference")
	}
	if forceID && numericRe.MatchString(s) {
		return s, true, nil
	}
	return s, false, nil
}

// spaceIDRe is X's own id shape for a Space: base 62, no punctuation, thirteen
// characters on every one measured. It is not a snowflake and it is not a
// handle, and nothing in the string says which it is, so this only rules out the
// references that cannot be an id at all.
var spaceIDRe = regexp.MustCompile(`^[0-9A-Za-z]+$`)

// ParseSpaceRef normalizes a Space reference to its id. It takes the bare id or
// any x.com/i/spaces/<id> link, including the /peek the web client hangs off a
// shared one.
func ParseSpaceRef(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty space reference")
	}
	if strings.Contains(s, "/") {
		kind, id, err := Classify(s)
		if err != nil || kind != KindSpace {
			return "", fmt.Errorf("not a Space id or a spaces URL: %q", s)
		}
		return id, nil
	}
	if !spaceIDRe.MatchString(s) {
		return "", fmt.Errorf("not a Space id or a spaces URL: %q", s)
	}
	return s, nil
}

func stripHost(s string) string {
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	for _, h := range []string{"mobile.twitter.com/", "twitter.com/", "mobile.x.com/", "x.com/", "www.x.com/", "www.twitter.com/"} {
		if strings.HasPrefix(s, h) {
			return s[len(h):]
		}
	}
	// No known host prefix; if it still has a slash, drop everything up to it.
	if i := strings.Index(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// TweetURL builds the canonical permalink for a tweet.
func TweetURL(author, id string) string {
	// Without a handle the canonical form is /i/status/<id>, which is what
	// Locate returns, so a tweet has one address whichever way it was built.
	if author == "" {
		author = "i"
	}
	return "https://x.com/" + author + "/status/" + id
}

// UserURL builds the canonical permalink for a profile.
func UserURL(handle string) string { return "https://x.com/" + handle }
