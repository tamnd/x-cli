package embed

import (
	"strings"

	"golang.org/x/net/html"
)

// OpenGraph and the Twitter card tags are the thinnest plane here and the one
// least likely to move, because X serves them for other people's crawlers
// rather than for its own app. When everything else on this page changes, the
// og:title still says who wrote what.

// Meta is the head metadata of a page: OpenGraph properties, Twitter card
// names, the canonical link and the title, flattened into one map each.
type Meta struct {
	// Property holds tags keyed by their property attribute, which is what
	// OpenGraph and the app-link tags use.
	Property map[string]string `json:"property,omitempty"`
	// Name holds tags keyed by their name attribute, which is what the
	// Twitter card tags and the plain description use.
	Name map[string]string `json:"name,omitempty"`
	// Link holds link elements keyed by rel.
	Link map[string]string `json:"link,omitempty"`
	// Title is the document title.
	Title string `json:"title,omitempty"`
}

// HeadMeta reads the head metadata of a document.
func HeadMeta(doc string) (*Meta, error) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return nil, err
	}
	m := &Meta{
		Property: map[string]string{},
		Name:     map[string]string{},
		Link:     map[string]string{},
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "meta":
				if isNonce(attr(n, "name")) || isNonce(attr(n, "property")) {
					// x.com ships <meta name="csp-nonce"> and it changes on
					// every response. Keeping it would put a fresh random
					// string in every record and make every capture comparison
					// fail for a reason that has nothing to do with the data.
					// The nonce attribute every other meta tag carries never
					// gets read at all, because this only reads named keys.
					break
				}
				content := attr(n, "content")
				if p := attr(n, "property"); p != "" {
					// A repeated property is a list in OpenGraph, and the
					// first one is the primary. Keep that one.
					if _, seen := m.Property[p]; !seen {
						m.Property[p] = content
					}
				}
				if name := attr(n, "name"); name != "" {
					if _, seen := m.Name[name]; !seen {
						m.Name[name] = content
					}
				}
			case "link":
				if rel := attr(n, "rel"); rel != "" {
					if _, seen := m.Link[rel]; !seen {
						m.Link[rel] = attr(n, "href")
					}
				}
			case "title":
				if m.Title == "" {
					m.Title = strings.TrimSpace(text(n))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return m, nil
}

// isNonce reports whether a meta key is a per-response nonce rather than data.
func isNonce(k string) bool { return strings.Contains(strings.ToLower(k), "nonce") }

// Get returns the first of the given keys that is set, checking property
// first and then name. Callers ask for og:title before twitter:title without
// having to know which one this particular page bothered to send.
func (m *Meta) Get(keys ...string) string {
	for _, k := range keys {
		if v, ok := m.Property[k]; ok && v != "" {
			return v
		}
		if v, ok := m.Name[k]; ok && v != "" {
			return v
		}
	}
	return ""
}

// Canonical returns the page's own idea of its URL.
func (m *Meta) Canonical() string {
	if v := m.Link["canonical"]; v != "" {
		return v
	}
	return m.Get("og:url")
}
