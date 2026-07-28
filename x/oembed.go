package x

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// oembed.go is surface 3, and plane F on top of it (spec 3003 doc 02 section 6).
//
// publish.x.com/oembed is the oldest door into X still open, because it exists
// for other people's blogs rather than for X's own app. It takes a permalink and
// returns a blockquote meant to be pasted into a page, with the author's name,
// the handle, the text with its links already anchored, and the date as a reader
// would read it.
//
// Two things come out of that. `x embed` prints the blockquote verbatim, which
// is the whole reason the surface exists and is why it is a byte command. And
// plane F reads three fields out of it that nothing else gives this cheaply: the
// lang attribute on the paragraph, the rendered text, and who wrote it, which is
// the one thing an /i/status/ URL does not carry.
//
// It runs last and only when the planes above it left a gap, because it is a
// second request rather than another read of a page already in hand.

// OEmbed is surface 3's answer for one tweet.
type OEmbed struct {
	// HTML is the blockquote exactly as X wrote it, down to the trailing
	// newlines. `x embed` prints this and nothing else, so anything that
	// normalized it would be changing the thing the user asked for.
	HTML string

	// URL is the permalink X echoed back, which names the author even when the
	// request did not: ask about /i/status/20 and this comes back /jack/status/20.
	URL        string
	AuthorName string
	AuthorURL  string
	Handle     string

	// Lang is the language attribute X put on the paragraph.
	Lang string

	// Text is the blockquote rendered down to what a reader sees: entities
	// decoded, anchors replaced by their display text, <br> by a newline. It is
	// close to the tweet text and not identical to it, because X shortens a link
	// to pic.twitter.com/xyz in the display and the raw text keeps the t.co.
	Text string

	// Date is the date string as X printed it, "March 21, 2006". It stays a
	// string: it has no time and no zone, and turning it into a timestamp would
	// state a precision X did not.
	Date string

	// Source is the request this came from, for the record envelope.
	Source string
}

// oembedResp is the wire shape. Everything X sends is named here, including the
// fields nothing reads, so the unmodeled walk has something to compare against
// and an addition upstream turns into a red build.
type oembedResp struct {
	URL          string `json:"url"`
	AuthorName   string `json:"author_name"`
	AuthorURL    string `json:"author_url"`
	HTML         string `json:"html"`
	Width        *int   `json:"width"`
	Height       *int   `json:"height"`
	Type         string `json:"type"`
	CacheAge     string `json:"cache_age"`
	ProviderName string `json:"provider_name"`
	ProviderURL  string `json:"provider_url"`
	Version      string `json:"version"`
}

// OEmbedURL is the surface 3 request for a permalink.
//
// omit_script=1 keeps X's widget loader out of the answer. The tool has no use
// for a <script> tag and printing one it did not have to would be handing the
// user something to paste that phones home.
func OEmbedURL(permalink string) string {
	return "https://publish.x.com/oembed?url=" + url.QueryEscape(permalink) + "&omit_script=1"
}

// FetchOEmbed reads one tweet's embed HTML. The id is enough: publish.x.com
// resolves /i/status/<id> and answers with the author's own permalink.
func FetchOEmbed(ctx context.Context, c *Client, id string) (*OEmbed, error) {
	u := OEmbedURL(StatusPageURL(id))
	b, err := c.Do(ctx, Req{URL: u, Endpoint: "oembed", CacheTTL: ttlOEmbed})
	if err != nil {
		if nf := asNotFound(err, "tweet", id); nf != nil {
			return nil, nf
		}
		return nil, err
	}
	o, err := decodeOEmbed(b)
	if err != nil {
		return nil, err
	}
	o.Source = u
	return o, nil
}

func decodeOEmbed(b []byte) (*OEmbed, error) {
	var r oembedResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("parse oembed response: %w", err)
	}
	if r.HTML == "" {
		return nil, fmt.Errorf("oembed answered without the embed html")
	}
	o := &OEmbed{
		HTML:       r.HTML,
		URL:        r.URL,
		AuthorName: r.AuthorName,
		AuthorURL:  r.AuthorURL,
		Handle:     handleFromProfileURL(r.AuthorURL),
	}
	readBlockquote(o)
	return o, nil
}

// readBlockquote fills the parsed fields off the HTML. It is not required to
// find anything: a blockquote X changed the shape of still prints, and every
// field it fills is one plane F may or may not get to contribute.
func readBlockquote(o *OEmbed) {
	doc, err := html.Parse(strings.NewReader(o.HTML))
	if err != nil {
		return
	}
	var para, link *html.Node
	var byline strings.Builder
	seenPara := false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.ElementNode && n.Data == "p" && para == nil:
			para = n
			seenPara = true
			return // the paragraph's own anchors are the tweet's links, not the date
		case n.Type == html.ElementNode && n.Data == "a" && seenPara:
			// The date is the last link after the paragraph, and it is the one
			// that points at the tweet.
			if strings.Contains(nodeAttr(n, "href"), "/status/") {
				link = n
			}
		case n.Type == html.TextNode && seenPara && link == nil:
			byline.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if para != nil {
		o.Lang = nodeAttr(para, "lang")
		o.Text = renderText(para)
	}
	if link != nil {
		o.Date = strings.TrimSpace(renderText(link))
		if o.URL == "" {
			o.URL = stripRefSrc(nodeAttr(link, "href"))
		}
	}
	// The byline, "&mdash; jack (@jack) ", is the fallback for who wrote it,
	// for the day author_url is missing and the permalink is the /i/ form.
	if o.Handle == "" {
		name, handle := embedByline(byline.String())
		o.Handle = handle
		if o.AuthorName == "" {
			o.AuthorName = name
		}
	}
}

// embedByline pulls the display name and the handle out of the text X puts
// between the paragraph and the date. It is the same "name (@handle)" shape as
// og:title with an em dash in front of it, so it is the same reader.
func embedByline(s string) (name, handle string) {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "—- \t\n")
	return splitOGTitle(s)
}

// renderText is a node rendered down to what a reader sees. An anchor
// contributes its display text, which is how pic.twitter.com/xyz and the @
// mentions survive, and a <br> contributes the newline it stands for.
func renderText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		if n.Type == html.ElementNode && n.Data == "br" {
			b.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func nodeAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// stripRefSrc drops the ref_src X tags its own permalinks with. It is a
// statement about where the click came from, and this one did not come from a
// click at all.
func stripRefSrc(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

// handleFromProfileURL is the handle in an author_url, and nothing when the URL
// is not a profile.
func handleFromProfileURL(u string) string {
	i := strings.LastIndexByte(strings.TrimSuffix(u, "/"), '/')
	if i < 0 {
		return ""
	}
	h := strings.TrimSuffix(u, "/")[i+1:]
	if h == "" || strings.ContainsAny(h, ". ?#") {
		return ""
	}
	return h
}

// ToTweet is the oEmbed answer as a tweet record of its own, so the merge layer
// folds it in the way it folds every other surface and `via` says which fields
// came from s3. Nothing here overwrites: a record that already has its text
// keeps it, and plane F only shows up in `via` for what it actually filled.
//
// CreatedAt is deliberately not set. Date is a day with no time and no zone,
// and a timestamp at midnight is a claim X never made.
func (o *OEmbed) ToTweet(id string) *Tweet {
	t := &Tweet{Meta: tweetMeta(id, 3, o.Source)}
	t.Text = strings.TrimSpace(o.Text)
	t.Lang = o.Lang
	t.URL = o.URL
	if o.Handle != "" || o.AuthorName != "" {
		u := &User{Name: o.AuthorName}
		u.setHandle(o.Handle)
		u.Stamp(3, o.Source)
		t.Author = u
	}
	t.Identify(KindTweet, id)
	return t
}
