package x

import (
	"strings"
	"testing"
)

// The size in a photo URL is the whole reason this file exists: the URL a plane
// recorded is whatever X happened to write there, and a download that keeps it
// saves a re-encode of the picture rather than the picture.
func TestPhotoURLAsksForTheSize(t *testing.T) {
	for _, c := range []struct{ name, in, size, want string }{
		{"a bare photo url defaults to the original",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh.jpg", "",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh?format=jpg&name=orig"},
		{"an asked-for size wins",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh.jpg", "small",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh?format=jpg&name=small"},
		{"the old colon form is replaced, not appended to",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh.jpg:large", "orig",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh?format=jpg&name=orig"},
		{"a size already in the query is replaced",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh?format=jpg&name=medium", "thumb",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh?format=jpg&name=thumb"},
		{"the format is kept, so a png does not come back as a jpg",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh.png", "orig",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh?format=png&name=orig"},
		{"a profile image is on pbs too",
			"https://pbs.twimg.com/profile_images/1321163587679784960/0ZxKlABK_normal.jpg", "orig",
			"https://pbs.twimg.com/profile_images/1321163587679784960/0ZxKlABK_normal?format=jpg&name=orig"},
		{"a video thumbnail has no sizes, so it is left alone",
			"https://video.twimg.com/ext_tw_video_thumb/1/pu/img/x.jpg", "orig",
			"https://video.twimg.com/ext_tw_video_thumb/1/pu/img/x.jpg"},
		{"a url with no extension to name a format is left alone",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh", "orig",
			"https://pbs.twimg.com/media/HORBhKkWIAAFjOh"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := PhotoURL(c.in, c.size)
			if err != nil {
				t.Fatalf("PhotoURL: %v", err)
			}
			if got != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestPhotoURLRejectsASizeNobodyServes(t *testing.T) {
	if _, err := PhotoURL("https://pbs.twimg.com/media/x.jpg", "huge"); err == nil {
		t.Fatal("a made-up size was accepted")
	} else if !strings.Contains(err.Error(), "orig") {
		t.Errorf("the error should list the sizes that do exist, got %q", err)
	}
	if _, err := PhotoURL("", "orig"); err == nil {
		t.Error("an empty url was accepted")
	}
	if err := CheckSize("orig"); err != nil {
		t.Errorf("orig is a size: %v", err)
	}
	if err := CheckSize(""); err == nil {
		t.Error("the empty size should be caught by the caller, not passed through")
	}
}

// A video is several encodings of one thing, and the playlist is not the one to
// save to disk however good an answer it is for a player.
func TestMediaURLPicksTheBestMP4(t *testing.T) {
	m := Media{Type: "video", Variants: []Variant{
		{ContentType: "application/x-mpegURL", URL: "https://video.twimg.com/a/pl/x.m3u8"},
		{Bitrate: 632000, ContentType: "video/mp4", URL: "https://video.twimg.com/a/vid/320x568/low.mp4"},
		{Bitrate: 2176000, ContentType: "video/mp4", URL: "https://video.twimg.com/a/vid/720x1280/high.mp4"},
		{Bitrate: 950000, ContentType: "video/mp4", URL: "https://video.twimg.com/a/vid/480x852/mid.mp4"},
	}}
	got, err := MediaURL(m, "orig", "")
	if err != nil {
		t.Fatalf("MediaURL: %v", err)
	}
	if !strings.Contains(got, "high.mp4") {
		t.Errorf("got %s, want the 2176000 rendition", got)
	}
	// The size means nothing to a video, and asking for a small one does not
	// silently hand back a different file.
	small, err := MediaURL(m, "small", "")
	if err != nil || small != got {
		t.Errorf("size changed a video url: got %s, %v", small, err)
	}
}

func TestMediaURLPicksTheVariantYouName(t *testing.T) {
	m := Media{Type: "video", Variants: []Variant{
		{ContentType: "application/x-mpegURL", URL: "https://video.twimg.com/a/pl/x.m3u8"},
		{Bitrate: 632000, ContentType: "video/mp4", URL: "https://video.twimg.com/a/vid/320x568/low.mp4"},
		{Bitrate: 2176000, ContentType: "video/mp4", URL: "https://video.twimg.com/a/vid/720x1280/high.mp4"},
	}}
	for _, want := range []string{"320x568", "632000", "low.mp4"} {
		got, err := MediaURL(m, "", want)
		if err != nil {
			t.Fatalf("--variant %s: %v", want, err)
		}
		if !strings.Contains(got, "low.mp4") {
			t.Errorf("--variant %s got %s", want, got)
		}
	}
	if got, err := MediaURL(m, "", "1080p"); err == nil {
		t.Errorf("a variant that is not there was accepted: %s", got)
	} else if !strings.Contains(err.Error(), "320x568") {
		t.Errorf("the error should list what is on offer, got %q", err)
	}
}

// A photo asked for by rendition is a mistake worth naming, because the answer
// the caller would otherwise get is a photo that ignored the flag.
func TestMediaURLRejectsAVariantOnAPhoto(t *testing.T) {
	m := Media{Type: "photo", URL: "https://pbs.twimg.com/media/x.jpg"}
	if _, err := MediaURL(m, "orig", "720x1280"); err == nil {
		t.Fatal("a variant on a photo was accepted")
	}
}

// An animated gif is served as an mp4 with one rendition and no playlist, so it
// goes down the variant path and there is nothing to choose between.
func TestMediaURLReadsAGIF(t *testing.T) {
	m := Media{Type: "animated_gif", Variants: []Variant{
		{Bitrate: 0, ContentType: "video/mp4", URL: "https://video.twimg.com/tweet_video/x.mp4"},
	}}
	got, err := MediaURL(m, "orig", "")
	if err != nil || !strings.HasSuffix(got, "x.mp4") {
		t.Errorf("got %s, %v", got, err)
	}
}

// A variant list with no mp4 in it at all still has to answer with something.
func TestMediaURLFallsBackToWhateverThereIs(t *testing.T) {
	m := Media{Type: "video", Variants: []Variant{
		{ContentType: "application/x-mpegURL", URL: "https://video.twimg.com/a/pl/x.m3u8"},
	}}
	got, err := MediaURL(m, "", "")
	if err != nil || !strings.HasSuffix(got, ".m3u8") {
		t.Errorf("got %s, %v", got, err)
	}
	if _, err := MediaURL(Media{Type: "video", Variants: []Variant{{Bitrate: 1}}}, "", ""); err == nil {
		t.Error("variants with no urls in them were accepted")
	}
}

func TestVariantNamesItself(t *testing.T) {
	for _, c := range []struct {
		v    Variant
		want string
	}{
		{Variant{Bitrate: 632000, URL: "https://video.twimg.com/a/vid/320x568/low.mp4"}, "320x568"},
		{Variant{Bitrate: 632000, URL: "https://video.twimg.com/a/low.mp4"}, "632000"},
		{Variant{ContentType: "application/x-mpegURL", URL: "https://video.twimg.com/a/x.m3u8"}, "application/x-mpegURL"},
	} {
		if got := VariantName(c.v); got != c.want {
			t.Errorf("VariantName(%s) = %s, want %s", c.v.URL, got, c.want)
		}
	}
}

// The end of the chain, against a real capture: the media a page carried
// resolves to a URL that asks the CDN for the original.
func TestMediaOnACapturedPageResolves(t *testing.T) {
	p, err := ParsePage(StatusPageURL("2081860978694594863"), capture(t, "status_media.html.gz"))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	posts := p.Postings()
	if len(posts) == 0 {
		t.Fatal("the capture has no tweets in it")
	}
	n := 0
	for _, t2 := range posts {
		for _, m := range t2.Media {
			u, err := MediaURL(m, DefaultMediaSize, "")
			if err != nil {
				t.Errorf("%s: %v", m.Key, err)
				continue
			}
			if strings.Contains(u, "pbs.twimg.com") && !strings.Contains(u, "name=orig") {
				t.Errorf("%s resolved to %s, which is not the original", m.Key, u)
			}
			n++
		}
	}
	if n == 0 {
		t.Fatal("the media capture parsed to no media at all")
	}
}
