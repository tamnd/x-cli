package x

import (
	"strings"
	"testing"

	"github.com/tamnd/x-cli/pkg/embed"
)

// Plane E is the fallback of last resort, so the test that matters is that it
// can carry a record on its own: strip the store and the microdata off a real
// capture and see what the head alone still says.
func TestPlaneEAloneCarriesATweet(t *testing.T) {
	p, err := ParsePage(StatusPageURL("20"), capture(t, "status_20.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	tw := &Tweet{}
	applyHeadTweet(tw, p.Head)
	if tw.Text == "" {
		t.Error("no text: og:description is the tweet on a status page")
	}
	if tw.Author == nil || tw.Author.Username == "" {
		t.Fatalf("no author: og:title carries the handle, got %+v", tw.Author)
	}
	if !strings.Contains(tw.URL, "/status/20") {
		t.Errorf("url = %q, want the permalink", tw.URL)
	}
}

func TestPlaneEAloneCarriesAUser(t *testing.T) {
	p, err := ParsePage("https://x.com/nasa", capture(t, "profile_nasa.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	u := &User{}
	applyHeadUser(u, p.Head)
	if u.Username == "" {
		t.Error("no handle: og:title is `name (@handle) / X` on a profile")
	}
	if u.Description == "" {
		t.Error("no description: og:description is the bio on a profile")
	}
	if u.ProfileImage == "" {
		t.Error("no avatar: og:image is the profile image on a profile")
	}
}

// It fills gaps and never overwrites, because everything above it is typed and
// plane E is a sentence written for a link preview.
func TestPlaneENeverOverwrites(t *testing.T) {
	p, err := ParsePage(StatusPageURL("20"), capture(t, "status_20.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	tw := &Tweet{Text: "from the store", Author: &User{Name: "from the store"}}
	tw.Author.setHandle("fromthestore")
	applyHeadTweet(tw, p.Head)
	if tw.Text != "from the store" || tw.Author.Name != "from the store" ||
		tw.Author.Username != "fromthestore" {
		t.Errorf("plane E overwrote a better plane: %q by %q (@%s)",
			tw.Text, tw.Author.Name, tw.Author.Username)
	}
}

// og:image is the author's avatar on a text tweet and the media itself on a
// media tweet. Only the second is media, and taking the first as media would
// give every text tweet a photo it does not have.
func TestPlaneETellsMediaFromAvatar(t *testing.T) {
	media := "https://pbs.twimg.com/media/HORBhKkWIAAFjOh.jpg:large"
	m := &embed.Meta{
		Property: map[string]string{
			"og:image": media, "og:image:width": "7005", "og:image:height": "4066",
		},
		Name: map[string]string{"twitter:creator": "@NASA"},
	}
	tw := &Tweet{}
	applyHeadTweet(tw, m)
	if len(tw.Media) != 1 {
		t.Fatalf("media = %v, want the one photo", tw.Media)
	}
	if tw.Media[0].URL != media || tw.Media[0].Width != 7005 || tw.Media[0].Height != 4066 {
		t.Errorf("media = %+v, want the original resolution off the width and height tags", tw.Media[0])
	}
	if tw.Author == nil || tw.Author.Username != "NASA" {
		t.Errorf("author = %+v, want NASA off twitter:creator", tw.Author)
	}

	avatar := "https://pbs.twimg.com/profile_images/1/azNjKOSH_200x200.jpg"
	tw = &Tweet{}
	applyHeadTweet(tw, &embed.Meta{
		Property: map[string]string{"og:image": avatar, "og:title": "jack (@jack) on X"},
	})
	if len(tw.Media) != 0 {
		t.Errorf("media = %v, want none: an avatar is not an attachment", tw.Media)
	}
	if tw.Author == nil || tw.Author.ProfileImage != avatar {
		t.Errorf("author = %+v, want the avatar on the author", tw.Author)
	}
}

func TestOGName(t *testing.T) {
	for _, c := range []struct {
		title, doc   string
		name, handle string
	}{
		{title: "jack (@jack) on X", name: "jack", handle: "jack"},
		{title: "jack (@jack) / X", name: "jack", handle: "jack"},
		{title: "NASA (@NASA) on X", name: "NASA", handle: "NASA"},
		// The document title carries it when og:title has gone.
		{doc: "jack (@jack) / X", name: "jack", handle: "jack"},
		// A display name that itself contains a parenthesis. The last one wins,
		// because the handle is always at the end.
		{title: "Someone (not really) (@someone) on X", name: "Someone (not really)", handle: "someone"},
		// Nothing to be sure of. On a status page og:title can be the tweet
		// text, and calling that a display name would be worse than saying
		// nothing.
		{title: "just setting up my twttr"},
		{title: "a (@ b) on X"},
		{title: "(@) on X"},
	} {
		name, handle := ogName(c.title, c.doc)
		if name != c.name || handle != c.handle {
			t.Errorf("ogName(%q, %q) = %q, %q; want %q, %q",
				c.title, c.doc, name, handle, c.name, c.handle)
		}
	}
}

// A tweet must not take a profile page's og:url as its permalink.
func TestPlaneEWillNotTakeAProfileURLForATweet(t *testing.T) {
	tw := &Tweet{}
	applyHeadTweet(tw, &embed.Meta{Property: map[string]string{"og:url": "https://x.com/jack"}})
	if tw.URL != "" {
		t.Errorf("url = %q, want none: that is a profile, not a status", tw.URL)
	}
}

// Every meta tag on an x.com page carries a nonce that changes per response.
// It is not data, and one leaking into a record makes every fixture comparison
// fail for no reason.
func TestHeadMetaDropsTheNonce(t *testing.T) {
	body := capture(t, "status_20.html.gz")
	if !strings.Contains(body, "nonce=") {
		t.Skip("the capture has no nonce to drop")
	}
	p, err := ParsePage(StatusPageURL("20"), body)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []map[string]string{p.Head.Property, p.Head.Name, p.Head.Link} {
		for k, v := range m {
			if strings.Contains(k, "nonce") || strings.Contains(v, "nonce") {
				t.Errorf("a nonce reached the head metadata: %q = %q", k, v)
			}
		}
	}
	tw, err := p.TweetFromPage("20")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tw.Text, "nonce") {
		t.Errorf("a nonce reached the record: %q", tw.Text)
	}
}

// X names one photo three ways: bare in the Relay store, with a :large suffix
// in og:image, and with query parameters in a link. Comparing the strings would
// file the same photo three times.
func TestMediaBase(t *testing.T) {
	const want = "https://pbs.twimg.com/media/HORBhKkWIAAFjOh.jpg"
	for _, u := range []string{
		want,
		want + ":large",
		want + ":orig",
		want + "?format=jpg&name=large",
		want + ":large?format=jpg",
	} {
		if got := mediaBase(u); got != want {
			t.Errorf("mediaBase(%q) = %q, want %q", u, got, want)
		}
	}
	// Different photos stay different, and a URL with no size suffix is left
	// exactly as it came.
	other := "https://pbs.twimg.com/media/OTHER.jpg"
	if mediaBase(other) == want {
		t.Error("two different photos collapsed into one")
	}
	if got := mediaBase("https://video.twimg.com/ext_tw_video/1/pu/vid/x.mp4"); got != "https://video.twimg.com/ext_tw_video/1/pu/vid/x.mp4" {
		t.Errorf("mediaBase mangled a video url: %q", got)
	}
}

// On a real media status page, plane E must not add a second copy of the photo
// the store already has, and it must contribute the original resolution the
// store does not carry.
func TestPlaneEMergesTheMediaTheStoreAlreadyHas(t *testing.T) {
	p, err := ParsePage(StatusPageURL("2081860978694594863"), capture(t, "status_media.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Head.Get("og:image"); !strings.HasSuffix(got, ":large") {
		t.Fatalf("og:image = %q, want the :large form this test exists for", got)
	}
	tw, err := p.TweetFromPage("2081860978694594863")
	if err != nil {
		t.Fatal(err)
	}
	if len(tw.Media) != 1 {
		t.Fatalf("media = %d entries, want 1: %+v", len(tw.Media), tw.Media)
	}
	m := tw.Media[0]
	if m.Width != 7005 || m.Height != 4066 {
		t.Errorf("media = %dx%d, want 7005x4066 off og:image:width and og:image:height", m.Width, m.Height)
	}
	if m.AltText == "" {
		t.Error("no alt text: the microdata carries it and plane E must not have replaced the entry")
	}
}
