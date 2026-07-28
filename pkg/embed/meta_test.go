package embed

import "testing"

func TestHeadMeta(t *testing.T) {
	const doc = `<html><head>
		<title>  a title  </title>
		<meta property="og:title" content="first">
		<meta property="og:title" content="second">
		<meta name="twitter:card" content="summary">
		<link rel="canonical" href="https://x.com/jack">
	</head></html>`
	m, err := HeadMeta(doc)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "a title" {
		t.Errorf("title = %q", m.Title)
	}
	// A repeated OpenGraph property is a list and the first one leads.
	if got := m.Get("og:title"); got != "first" {
		t.Errorf("og:title = %q, want first", got)
	}
	if got := m.Get("og:missing", "twitter:card"); got != "summary" {
		t.Errorf("fallback lookup = %q", got)
	}
	if got := m.Canonical(); got != "https://x.com/jack" {
		t.Errorf("canonical = %q", got)
	}
}

// The head tags are the plane X maintains for other people's crawlers, so
// they are the last thing to move. Both real pages carry the full set.
func TestHeadMetaRealPages(t *testing.T) {
	cases := []struct {
		file      string
		wantURL   string
		wantType  string
		titleHas  string
		wantImage bool
	}{
		{"sp2.html.gz", "https://x.com/jack/status/20", "article", "just setting up my twttr", true},
		{"prof.html.gz", "https://x.com/jack", "profile", "jack", true},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			m, err := HeadMeta(fixture(t, c.file))
			if err != nil {
				t.Fatal(err)
			}
			if got := m.Get("og:url"); got != c.wantURL {
				t.Errorf("og:url = %q, want %q", got, c.wantURL)
			}
			if got := m.Get("og:type"); got != c.wantType {
				t.Errorf("og:type = %q, want %q", got, c.wantType)
			}
			if m.Get("og:site_name") == "" {
				t.Error("no og:site_name")
			}
			if c.wantImage && m.Get("og:image") == "" {
				t.Error("no og:image")
			}
			if m.Get("og:description") == "" {
				t.Error("no og:description")
			}
			if m.Title == "" {
				t.Error("no title")
			}
		})
	}
}

// The app-link tags carry the native deep link, which is the one place a
// page states its own canonical twitter:// address.
func TestHeadMetaAppLinks(t *testing.T) {
	m, err := HeadMeta(fixture(t, "sp2.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	got := m.Get("al:ios:url")
	if got != "twitter://status?id=20" {
		t.Errorf("al:ios:url = %q", got)
	}
}
