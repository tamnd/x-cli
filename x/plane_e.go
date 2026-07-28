package x

import (
	"strconv"
	"strings"

	"github.com/tamnd/x-cli/pkg/embed"
)

// Plane E is the OpenGraph and Twitter card meta in the page head (spec 3003
// doc 02 section 5).
//
// It is the thinnest plane here and the one least likely to move, because X
// serves it for other people's crawlers rather than for its own app. When the
// Relay store shape changes overnight, og:title still says who wrote what.
//
// So it runs last and only fills gaps. Everything above it is typed and keeps
// null distinct from zero, and plane E is strings scraped out of a sentence
// meant for a link preview. It earns its place on the days the other planes
// come back empty, and on media tweets, where og:image is the media itself at
// full resolution and the width and height tags give the original size without
// touching any JSON.

// applyHeadTweet fills what the store and the microdata left empty on a status
// page.
func applyHeadTweet(t *Tweet, m *embed.Meta) {
	if m == nil {
		return
	}
	// og:description on a status page is the tweet text. On a media tweet X
	// sometimes sends the empty string rather than omitting the tag, which is
	// why this checks the value and not just the key.
	fillStr(&t.Text, m.Get("og:description", "twitter:description", "description"))
	fillStr(&t.URL, canonicalStatus(m))

	// The author, from whichever of the three shapes the page used. twitter:
	// creator is the handle on its own and og:title carries both the display
	// name and the handle, so the handle can arrive without the name and the
	// name never arrives without the handle.
	name, handle := ogName(m.Get("og:title", "twitter:title"), m.Title)
	if handle == "" {
		handle = strings.TrimPrefix(m.Get("twitter:creator"), "@")
	}
	if handle != "" || name != "" {
		if t.Author == nil {
			t.Author = &User{}
		}
		if t.Author.Username == "" && handle != "" {
			t.Author.setHandle(handle)
		}
		fillStr(&t.Author.Name, name)
	}

	// og:image is the author's avatar on a text tweet and the media itself on a
	// media tweet, at :large, with the original resolution in the width and
	// height tags. Only the second one is media, and the URL says which it is.
	img := m.Get("og:image", "twitter:image")
	switch {
	case isMediaURL(img):
		w, _ := strconv.Atoi(m.Get("og:image:width"))
		h, _ := strconv.Atoi(m.Get("og:image:height"))
		// The other planes name the same photo without the :large suffix, so
		// the match is on the base URL. When they already have it, plane E
		// still has something to add: og:image:width and og:image:height are
		// the original resolution, and the store's sizes are the rendered ones.
		if md := findMedia(t.Media, img); md != nil {
			if md.Width == 0 {
				md.Width, md.Height = w, h
			}
			break
		}
		t.Media = append(t.Media, Media{
			Type: "photo", URL: img, Width: w, Height: h,
			AltText: m.Get("og:image:alt", "twitter:image:alt"),
		})
	case isAvatarURL(img) && t.Author != nil:
		fillStr(&t.Author.ProfileImage, img)
	}
}

// findMedia is hasMedia with the answer instead of a yes or no, and it matches
// on the base URL: X names one photo as .jpg in the Relay store, .jpg:large in
// og:image, and .jpg?format=jpg&name=large in a link. Comparing the strings
// would file the same photo three times.
func findMedia(ms []Media, url string) *Media {
	want := mediaBase(url)
	for i := range ms {
		if mediaBase(ms[i].URL) == want {
			return &ms[i]
		}
	}
	return nil
}

// mediaBase strips the size off a pbs.twimg.com URL: the :large / :orig name
// suffix and the format and name query parameters.
func mediaBase(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	// The suffix is on the last path segment, after the extension, so the
	// colon in "https:" is not a candidate.
	if i := strings.LastIndexByte(u, '/'); i >= 0 {
		if j := strings.IndexByte(u[i:], ':'); j >= 0 {
			u = u[:i+j]
		}
	}
	return u
}

// applyHeadUser fills what the store and the microdata left empty on a profile
// page.
func applyHeadUser(u *User, m *embed.Meta) {
	if m == nil {
		return
	}
	name, handle := ogName(m.Get("og:title", "twitter:title"), m.Title)
	if u.Username == "" && handle != "" {
		u.setHandle(handle)
	}
	fillStr(&u.Name, name)
	fillStr(&u.Description, m.Get("og:description", "twitter:description", "description"))
	if img := m.Get("og:image", "twitter:image"); isAvatarURL(img) {
		fillStr(&u.ProfileImage, img)
	}
	fillStr(&u.URL, m.Canonical())
}

// ogName pulls the display name and the handle out of the two title shapes
// x.com uses: "jack (@jack) on X" on a status page and "jack (@jack) / X" on a
// profile. The document title is the fallback, because a page that dropped
// og:title has usually kept it.
//
// It returns what it is sure of. A title with no parenthesised handle gives
// nothing rather than guessing that the whole string is a display name, since
// on a status page that string is the tweet text.
func ogName(title, docTitle string) (name, handle string) {
	for _, s := range []string{title, docTitle} {
		if n, h := splitOGTitle(s); h != "" {
			return n, h
		}
	}
	return "", ""
}

func splitOGTitle(s string) (name, handle string) {
	i := strings.LastIndex(s, "(@")
	if i < 0 {
		return "", ""
	}
	rest := s[i+2:]
	j := strings.IndexByte(rest, ')')
	if j < 0 {
		return "", ""
	}
	// Checked before trimming, on purpose: "a (@ b)" is not a handle, and
	// trimming first would turn it into one.
	handle = rest[:j]
	if handle == "" || strings.ContainsAny(handle, " \t\n/") {
		return "", ""
	}
	return strings.TrimSpace(s[:i]), handle
}

// canonicalStatus is the page's own permalink, but only when it is one. A
// profile page and a status page both set og:url, and a tweet must not take a
// profile URL as its own.
func canonicalStatus(m *embed.Meta) string {
	u := m.Canonical()
	if strings.Contains(u, "/status/") {
		return u
	}
	return ""
}

// isMediaURL reports whether a URL is an uploaded photo rather than an avatar
// or a header. X keeps them on separate paths, which is the only thing that
// tells them apart at this plane.
func isMediaURL(u string) bool {
	return strings.Contains(u, "pbs.twimg.com/media/") ||
		strings.Contains(u, "pbs.twimg.com/ext_tw_video_thumb/") ||
		strings.Contains(u, "pbs.twimg.com/amplify_video_thumb/") ||
		strings.Contains(u, "pbs.twimg.com/tweet_video_thumb/")
}

func isAvatarURL(u string) bool { return strings.Contains(u, "pbs.twimg.com/profile_images/") }
