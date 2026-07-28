package x

import (
	"strings"
	"testing"
)

// The oEmbed tests are about the blockquote, because the JSON around it is four
// strings and a number and the blockquote is the part with structure in it.

func TestOEmbedReadsTheBlockquote(t *testing.T) {
	o, err := decodeOEmbed([]byte(capture(t, "s3_oembed_20.json.gz")))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ field, got, want string }{
		{"lang", o.Lang, "en"},
		{"text", o.Text, "just setting up my twttr"},
		{"date", o.Date, "March 21, 2006"},
		{"handle", o.Handle, "jack"},
		{"author", o.AuthorName, "jack"},
		{"url", o.URL, "https://x.com/jack/status/20"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
	if !strings.HasPrefix(o.HTML, "<blockquote") {
		t.Errorf("HTML does not start with the blockquote: %.40q", o.HTML)
	}
}

// The media tweet is the one with structure inside the paragraph: a mention, a
// t.co link, and the pic.twitter.com link X shows instead of a second t.co.
// Rendering it has to keep all three, because they are the text a reader sees.
func TestOEmbedRendersTheAnchoredText(t *testing.T) {
	o, err := decodeOEmbed([]byte(capture(t, "s3_oembed_media.json.gz")))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"The Andromeda galaxy isn't making stars", // &#39; decoded
		"@NASAHubble",                // the mention anchor's text
		"https://t.co/nK1ndoydi7",    // the link
		"pic.twitter.com/1fmYXooDq2", // the media link, as X displays it
	} {
		if !strings.Contains(o.Text, want) {
			t.Errorf("text does not carry %q:\n%s", want, o.Text)
		}
	}
	if strings.Contains(o.Text, "<a ") || strings.Contains(o.Text, "&#") {
		t.Errorf("text still has markup in it:\n%s", o.Text)
	}
	if o.Handle != "NASA" || o.Date != "July 27, 2026" {
		t.Errorf("handle/date = %q/%q, want NASA/July 27, 2026", o.Handle, o.Date)
	}
}

// The permalink is the fact surface 3 has that the caller does not: ask about
// /i/status/<id> and X answers with the author in the URL.
func TestOEmbedToTweetNamesTheAuthor(t *testing.T) {
	o, err := decodeOEmbed([]byte(capture(t, "s3_oembed_20.json.gz")))
	if err != nil {
		t.Fatal(err)
	}
	o.Source = OEmbedURL(StatusPageURL("20"))
	tw := o.ToTweet("20")
	if tw.ID != "20" || tw.Kind != KindTweet {
		t.Errorf("envelope = %s/%s, want tweet/20", tw.Kind, tw.ID)
	}
	if tw.Author == nil || tw.Author.Username != "jack" {
		t.Fatalf("no author on the record: %+v", tw.Author)
	}
	if tw.URL != "https://x.com/jack/status/20" {
		t.Errorf("url = %q, want the author's permalink", tw.URL)
	}
	if len(tw.Surfaces) != 1 || tw.Surfaces[0] != "s3" {
		t.Errorf("surfaces = %v, want [s3]", tw.Surfaces)
	}
	if tw.Tier != 0 {
		t.Errorf("tier = %d, want 0: oembed needs no credential", tw.Tier)
	}
	// CreatedAt stays zero on purpose: "March 21, 2006" is a day, and a
	// timestamp at midnight would be a precision X never stated.
	if !tw.CreatedAt.IsZero() {
		t.Errorf("created_at = %s, want zero", tw.CreatedAt)
	}
}

// Plane F fills gaps and nothing else, and the merge layer is what records that
// it did. A record that already has its text keeps it, and only the fields s3
// actually filled show up in via.
func TestPlaneFOnlyFillsGaps(t *testing.T) {
	o, err := decodeOEmbed([]byte(capture(t, "s3_oembed_20.json.gz")))
	if err != nil {
		t.Fatal(err)
	}
	o.Source = OEmbedURL(StatusPageURL("20"))
	dst := &Tweet{Meta: tweetMeta("20", 8, "https://x.com/i/status/20"), Text: "the page said this"}
	got := MergeTweet(dst, o.ToTweet("20"))
	if got.Text != "the page said this" {
		t.Errorf("text = %q, plane F overwrote a filled field", got.Text)
	}
	if got.Lang != "en" {
		t.Errorf("lang = %q, want en from s3", got.Lang)
	}
	if got.Via["lang"] != "s3" || got.Via["text"] != "" {
		t.Errorf("via = %v, want lang from s3 and no claim on text", got.Via)
	}
	if !hasStr(got.Surfaces, "s3") || !hasStr(got.Surfaces, "s8") {
		t.Errorf("surfaces = %v, want both", got.Surfaces)
	}
}

// wantsOEmbed is what keeps plane F off the network on the ordinary day, so it
// is worth a test of its own: the tweet syndication already answered must not
// trigger a second request.
func TestWantsOEmbed(t *testing.T) {
	full := &Tweet{Text: "hi", Lang: "en", Author: &User{Username: "jack"}}
	for _, c := range []struct {
		name string
		tw   *Tweet
		want bool
	}{
		{"nil", nil, false},
		{"complete", full, false},
		{"no text", &Tweet{Lang: "en", Author: &User{Username: "jack"}}, true},
		{"no lang", &Tweet{Text: "hi", Author: &User{Username: "jack"}}, true},
		{"no author", &Tweet{Text: "hi", Lang: "en"}, true},
		{"author with no handle", &Tweet{Text: "hi", Lang: "en", Author: &User{}}, true},
	} {
		if got := wantsOEmbed(c.tw); got != c.want {
			t.Errorf("wantsOEmbed(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOEmbedURLAsksForNoScript(t *testing.T) {
	u := OEmbedURL("https://x.com/i/status/20")
	if !strings.Contains(u, "omit_script=1") {
		t.Errorf("%s does not ask X to leave the widget loader out", u)
	}
	if !strings.Contains(u, "url=https%3A%2F%2Fx.com%2Fi%2Fstatus%2F20") {
		t.Errorf("%s does not carry the escaped permalink", u)
	}
}

func TestOEmbedRejectsAnAnswerWithNoHTML(t *testing.T) {
	if _, err := decodeOEmbed([]byte(`{"author_name":"jack"}`)); err == nil {
		t.Error("an oembed response with no html decoded without complaint")
	}
	if _, err := decodeOEmbed([]byte(`not json`)); err == nil {
		t.Error("a non-JSON body decoded without complaint")
	}
}

// The byline is the fallback for who wrote it, for a day author_url is missing.
func TestEmbedByline(t *testing.T) {
	for _, c := range []struct{ in, name, handle string }{
		{"\u2014 jack (@jack) ", "jack", "jack"},
		{"— NASA (@NASA) ", "NASA", "NASA"},
		{"— a name with (@handle) ", "a name with", "handle"},
		{"just some text", "", ""},
		{"", "", ""},
	} {
		name, handle := embedByline(c.in)
		if name != c.name || handle != c.handle {
			t.Errorf("embedByline(%q) = %q, %q; want %q, %q", c.in, name, handle, c.name, c.handle)
		}
	}
}

// A blockquote with the anchor missing still has a tweet in it, and a shape X
// changed should degrade to fewer fields rather than to an error.
func TestOEmbedSurvivesAThinBlockquote(t *testing.T) {
	o, err := decodeOEmbed([]byte(`{"html":"\u003Cblockquote\u003E\u003Cp lang=\"fr\"\u003Ebonjour\u003C\/p\u003E\u003C\/blockquote\u003E"}`))
	if err != nil {
		t.Fatal(err)
	}
	if o.Lang != "fr" || o.Text != "bonjour" {
		t.Errorf("lang/text = %q/%q, want fr/bonjour", o.Lang, o.Text)
	}
	if o.Date != "" || o.Handle != "" {
		t.Errorf("date/handle = %q/%q, want both empty", o.Date, o.Handle)
	}
}

// X writes a multi-line tweet with <br>, and a rendered text that runs the
// lines together is a different tweet.
func TestOEmbedKeepsLineBreaks(t *testing.T) {
	o, err := decodeOEmbed([]byte(`{"html":"\u003Cblockquote\u003E\u003Cp lang=\"en\"\u003Eone\u003Cbr\u003Etwo\u003C\/p\u003E\u003C\/blockquote\u003E"}`))
	if err != nil {
		t.Fatal(err)
	}
	if o.Text != "one\ntwo" {
		t.Errorf("text = %q, want one\\ntwo", o.Text)
	}
}

func TestHandleFromProfileURL(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://x.com/jack", "jack"},
		{"https://x.com/NASA/", "NASA"},
		{"https://x.com/", ""},
		{"", ""},
	} {
		if got := handleFromProfileURL(c.in); got != c.want {
			t.Errorf("handleFromProfileURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The permalink X hands back is tagged with where the click came from, and this
// one did not come from a click.
func TestStripRefSrc(t *testing.T) {
	got := stripRefSrc("https://x.com/jack/status/20?ref_src=twsrc%5Etfw")
	if got != "https://x.com/jack/status/20" {
		t.Errorf("stripRefSrc = %q", got)
	}
}
