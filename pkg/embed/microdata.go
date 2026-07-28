package embed

import (
	"strings"

	"golang.org/x/net/html"
)

// X renders schema.org microdata into every status and profile page, in the
// DOM rather than as a JSON-LD script. Spec 3003 doc 02 called this plane
// JSON-LD; that was wrong, and the correction is recorded there.
//
// This matters more than it sounds. The microdata carries the engagement
// counters, the author, the reply count and the timestamps, with no credential
// at all, and it is X's own statement about its own content in a public
// vocabulary. When the Relay store moves a field, this plane usually still has
// it, which is why it exists as a second reading of the same page rather than
// as a fallback nobody maintains.

// Item is one schema.org item: a type, and properties that are either strings
// or nested items.
type Item struct {
	Type  string           `json:"type"`
	ID    string           `json:"id,omitempty"`
	Props map[string][]any `json:"props"`
}

// Microdata extracts every schema.org item that no other item claims as a
// property of its own.
//
// Two shapes on x.com make that rule less obvious than it sounds.
//
// The focal tweet of a status page sits inside the page's Collection element
// but carries no itemprop, so it is a top level item that merely happens to be
// nested. Reading nesting as containment loses it.
//
// A profile page streams its timeline in late, as a run of <ul> blocks that
// land after the ProfilePage element rather than inside it. Those tweets do
// carry itemprop="hasPart", but there is no item above them to be part of, so
// dropping every itemprop element loses the whole timeline. Nine tweets, at
// tier 0, on a page that is supposed to give nothing.
func Microdata(doc string) ([]*Item, error) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return nil, err
	}
	var out []*Item
	var walk func(n *html.Node, inScope bool)
	walk = func(n *html.Node, inScope bool) {
		scope := n.Type == html.ElementNode && hasAttr(n, "itemscope")
		if scope && !(inScope && hasAttr(n, "itemprop")) {
			out = append(out, item(n))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inScope || scope)
		}
	}
	walk(root, false)
	return out, nil
}

// item builds one item from the properties beneath it.
//
// It descends through plain elements, which are just layout, and stops at any
// element that opens a new scope. A scope with an itemprop becomes a nested
// value; a scope without one is somebody else's item and is left alone.
func item(n *html.Node) *Item {
	it := &Item{
		Type:  short(attr(n, "itemtype")),
		ID:    attr(n, "itemid"),
		Props: map[string][]any{},
	}
	var walk func(*html.Node)
	walk = func(parent *html.Node) {
		for c := parent.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			name := attr(c, "itemprop")
			scope := hasAttr(c, "itemscope")
			switch {
			case name == "" && scope:
				// A top level item that happens to sit inside this one.
			case name == "":
				walk(c)
			case scope:
				it.Props[name] = append(it.Props[name], item(c))
			default:
				it.Props[name] = append(it.Props[name], propValue(c))
			}
		}
	}
	walk(n)
	return it
}

// propValue reads a property off an element the way the microdata spec says
// to: the value comes from whichever attribute suits the element, and falls
// back to the text content.
func propValue(n *html.Node) string {
	for _, a := range []struct{ tag, at string }{
		{"meta", "content"},
		{"a", "href"},
		{"link", "href"},
		{"img", "src"},
		{"source", "src"},
		{"video", "src"},
		{"audio", "src"},
		{"iframe", "src"},
		{"time", "datetime"},
		{"data", "value"},
	} {
		if n.Data == a.tag {
			if v := attr(n, a.at); v != "" {
				return v
			}
		}
	}
	return strings.TrimSpace(text(n))
}

// short trims the schema.org prefix off a type so that a caller can compare
// against SocialMediaPosting rather than the whole URL. A type from another
// vocabulary is left alone.
func short(t string) string {
	for _, p := range []string{"https://schema.org/", "http://schema.org/"} {
		if s, ok := strings.CutPrefix(t, p); ok {
			return s
		}
	}
	return t
}

// Find returns every item of a type, at any depth, in document order.
func Find(items []*Item, typename string) []*Item {
	var out []*Item
	var walk func([]*Item)
	walk = func(in []*Item) {
		for _, it := range in {
			if it.Type == typename {
				out = append(out, it)
			}
			for _, vals := range it.Props {
				for _, v := range vals {
					if child, ok := v.(*Item); ok {
						walk([]*Item{child})
					}
				}
			}
		}
	}
	walk(items)
	return out
}

// Str returns the first string value of a property.
func (it *Item) Str(name string) string {
	for _, v := range it.Props[name] {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Item returns the first nested item value of a property.
func (it *Item) Item(name string) *Item {
	for _, v := range it.Props[name] {
		if child, ok := v.(*Item); ok {
			return child
		}
	}
	return nil
}

// Items returns every nested item value of a property.
func (it *Item) Items(name string) []*Item {
	var out []*Item
	for _, v := range it.Props[name] {
		if child, ok := v.(*Item); ok {
			out = append(out, child)
		}
	}
	return out
}

// Counter reads one InteractionCounter off an item by its interaction type.
//
// X states every engagement number this way, so a like count arrives as an
// InteractionCounter whose interactionType is LikeAction. The second return
// says whether the counter was there at all, because zero and absent are
// different answers and doc 03 treats them as different.
func (it *Item) Counter(prop, action string) (string, bool) {
	for _, c := range it.Items(prop) {
		if short(c.Str("interactionType")) == action {
			v := c.Str("userInteractionCount")
			return v, v != ""
		}
	}
	return "", false
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, name string) bool {
	for _, a := range n.Attr {
		if a.Key == name {
			return true
		}
	}
	return false
}

func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return b.String()
}
