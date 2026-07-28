package x

import (
	"fmt"
	"net/url"
	"strings"
)

// identity.go is the pure layer (spec 3003 doc 04 section 1).
//
// Classify, Locate and URI never open a socket, read a cache, or look at the
// config. That is the whole point: `x url` and `x classify` are usable inside a
// shell loop over ten thousand pasted links without a single request leaving the
// machine, and every other part of the tool can name a node without fetching it.
//
// Two properties hold and both are tested. The URI for a node is stable, so the
// same tweet read anonymously and read with a session is one node. And
// Classify(URI(k, i)) returns (k, i) for every kind.

// The sixteen kinds that have a URI, plus search, which classifies but does not
// get one because a search is a query rather than a thing.
const (
	KindTweet        = "tweet"
	KindUser         = "user"
	KindConversation = "conversation"
	KindMedia        = "media"
	KindPoll         = "poll"
	KindCard         = "card"
	KindHashtag      = "hashtag"
	KindCashtag      = "cashtag"
	KindLink         = "link"
	KindList         = "list"
	KindSpace        = "space"
	KindBroadcast    = "broadcast"
	KindTrend        = "trend"
	KindPlace        = "place"
	KindNote         = "note"
	KindCommunity    = "community"
	KindSearch       = "search"
)

// Kinds is every kind Classify can return, in the order doc 04 lists them.
var Kinds = []string{
	KindTweet, KindUser, KindConversation, KindMedia, KindPoll, KindCard,
	KindHashtag, KindCashtag, KindLink, KindList, KindSpace, KindBroadcast,
	KindTrend, KindPlace, KindNote, KindCommunity, KindSearch,
}

var kindSet = func() map[string]bool {
	m := make(map[string]bool, len(Kinds))
	for _, k := range Kinds {
		m[k] = true
	}
	return m
}()

// URIScheme is the address space every record lives in.
const URIScheme = "x://"

// hostsX are the front ends that all render the same graph, so a link from any
// of them classifies to the same node.
func isXHost(h string) bool {
	switch h {
	case "x.com", "twitter.com", "t.com":
		return true
	}
	// Every nitter instance mirrors x.com's path shapes, and people paste them.
	return strings.HasPrefix(h, "nitter.")
}

// reserved are x.com path segments that are routes rather than handles. A URL
// whose first segment is one of these is not a profile, and guessing that it is
// would turn https://x.com/settings into a user node.
var reserved = map[string]bool{
	"i": true, "home": true, "explore": true, "notifications": true,
	"messages": true, "settings": true, "compose": true, "intent": true,
	"share": true, "login": true, "logout": true, "signup": true, "account": true,
	"about": true, "tos": true, "privacy": true, "search": true, "hashtag": true,
	"search-advanced": true, "who_to_follow": true, "download": true, "help": true,
}

// Classify names the node a reference points at. It accepts every URL shape a
// person can copy out of a browser plus the bare forms, and it is total over
// junk only in the sense that it returns an error rather than a guess.
func Classify(input string) (kind, id string, err error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", fmt.Errorf("empty reference")
	}
	// An x:// address classifies back to itself, which is the reversibility
	// property and also lets a URI be passed anywhere a reference is accepted.
	if rest, ok := cutPrefixFold(s, URIScheme); ok {
		k, i, found := strings.Cut(rest, "/")
		if !found || i == "" || !kindSet[k] {
			return "", "", fmt.Errorf("not an x:// address: %q", input)
		}
		return k, i, nil
	}
	switch s[0] {
	case '@':
		h := handleOf(s[1:])
		if h == "" {
			return "", "", fmt.Errorf("not a handle: %q", input)
		}
		return KindUser, h, nil
	case '#':
		if t := strings.TrimSpace(s[1:]); t != "" {
			return KindHashtag, strings.ToLower(t), nil
		}
		return "", "", fmt.Errorf("empty hashtag")
	case '$':
		if t := strings.TrimSpace(s[1:]); t != "" {
			return KindCashtag, strings.ToLower(t), nil
		}
		return "", "", fmt.Errorf("empty cashtag")
	}
	// A bare run of digits is a tweet id, not a user id, because that is what a
	// person pastes. `x user --id 12` is the explicit form for a numeric user.
	if numericRe.MatchString(s) {
		return KindTweet, s, nil
	}
	if looksLikeURL(s) {
		return classifyURL(s)
	}
	if h := handleOf(s); h != "" {
		return KindUser, h, nil
	}
	return "", "", fmt.Errorf("cannot classify %q", input)
}

// Locate is the inverse: the canonical live address for a node, or an error for
// a kind that has no page of its own.
//
// A tweet locates to /i/status/<id> rather than /<handle>/status/<id> because
// that form needs nothing but the id, and X redirects it to the handle form.
func Locate(kind, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("no id for kind %q", kind)
	}
	switch kind {
	case KindTweet, KindConversation, KindPoll, KindCard, KindNote:
		return "https://x.com/i/status/" + id, nil
	case KindUser:
		return "https://x.com/" + id, nil
	case KindHashtag:
		return "https://x.com/hashtag/" + url.PathEscape(id), nil
	case KindCashtag:
		return "https://x.com/search?q=" + url.QueryEscape("$"+id), nil
	case KindSearch:
		return "https://x.com/search?q=" + url.QueryEscape(id), nil
	case KindList:
		return "https://x.com/i/lists/" + id, nil
	case KindSpace:
		return "https://x.com/i/spaces/" + id, nil
	case KindCommunity:
		return "https://x.com/i/communities/" + id, nil
	case KindBroadcast:
		return "https://x.com/i/broadcasts/" + id, nil
	case KindTrend:
		// A trend id is woeid/name, and only the name is searchable.
		_, name, ok := strings.Cut(id, "/")
		if !ok {
			name = id
		}
		return "https://x.com/search?q=" + url.QueryEscape(name), nil
	case KindLink:
		return unpackLink(id)
	case KindMedia:
		// A media key is not addressable on its own. Only the CDN form is, and
		// only when the id came from a CDN URL in the first place.
		return "", fmt.Errorf("a media key is not an address on its own")
	case KindPlace:
		return "", fmt.Errorf("a place is a geotag, not a page")
	}
	return "", fmt.Errorf("unknown kind %q", kind)
}

// URI is the address of a node in x's own space. It is derived from kind and id
// alone, so two surfaces that disagree about everything else still agree here.
func URI(kind, id string) string { return URIScheme + kind + "/" + id }

// SplitURI takes an address back apart. It splits on the first slash after the
// scheme, not the last, because a link id and a trend id both carry slashes of
// their own and only the leading segment is the kind.
func SplitURI(uri string) (kind, id string, ok bool) {
	rest, ok := cutPrefixFold(uri, URIScheme)
	if !ok {
		return "", "", false
	}
	kind, id, ok = strings.Cut(rest, "/")
	if !ok || kind == "" || id == "" {
		return "", "", false
	}
	return kind, id, true
}

// ---- internals ----

func looksLikeURL(s string) bool {
	return strings.Contains(s, "://") || strings.Contains(s, "/") || strings.Contains(s, ".")
}

// handleOf normalises a handle. X treats handles case-insensitively and hands
// them back in the author's chosen casing, so @NASA and @nasa have to be one
// node; the display casing rides on the record, not on the identity.
func handleOf(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, "@"))
	if s == "" || len(s) > 15 {
		return ""
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return ""
	}
	return strings.ToLower(s)
}

func classifyURL(raw string) (string, string, error) {
	s := raw
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", "", fmt.Errorf("not a URL: %q", raw)
	}
	host := strings.ToLower(u.Hostname())
	for _, p := range []string{"www.", "mobile.", "m."} {
		host = strings.TrimPrefix(host, p)
	}
	switch host {
	case "pbs.twimg.com", "video.twimg.com", "abs.twimg.com", "ton.twimg.com":
		return classifyMediaURL(u)
	}
	if isXHost(host) {
		if k, id, ok := classifyXPath(u); ok {
			return k, id, nil
		}
		// A link into x.com that names no node is still a link, not an error.
	}
	return KindLink, packLink(u), nil
}

// classifyMediaURL pulls the stable part out of a CDN address. pbs keys are the
// filename; video keys are the numeric id in the path, which is the tweet the
// video hangs off.
func classifyMediaURL(u *url.URL) (string, string, error) {
	segs := pathSegs(u)
	if len(segs) == 0 {
		return "", "", fmt.Errorf("no media id in %q", u.String())
	}
	last := segs[len(segs)-1]
	if i := strings.LastIndexByte(last, '.'); i > 0 {
		last = last[:i]
	}
	if u.Hostname() == "pbs.twimg.com" || strings.HasSuffix(u.Hostname(), ".pbs.twimg.com") {
		return KindMedia, last, nil
	}
	// video.twimg.com/ext_tw_video/<id>/pu/vid/1280x720/x.mp4
	for _, seg := range segs[1:] {
		if numericRe.MatchString(seg) {
			return KindMedia, seg, nil
		}
	}
	return KindMedia, last, nil
}

// classifyXPath handles every x.com route that names a node. It reports false
// when the path is a route with nothing addressable behind it, such as /home.
func classifyXPath(u *url.URL) (string, string, bool) {
	segs := pathSegs(u)
	if len(segs) == 0 {
		return "", "", false
	}
	head := strings.ToLower(segs[0])
	switch head {
	case "i":
		return classifyIPath(segs[1:])
	case "hashtag":
		if len(segs) > 1 && segs[1] != "" {
			return KindHashtag, strings.ToLower(segs[1]), true
		}
	case "search", "search-advanced":
		// ?q= is the one query parameter that is the id rather than tracking.
		if q := strings.TrimSpace(u.Query().Get("q")); q != "" {
			if t, ok := strings.CutPrefix(q, "#"); ok && !strings.ContainsAny(t, " \t") {
				return KindHashtag, strings.ToLower(t), true
			}
			if t, ok := strings.CutPrefix(q, "$"); ok && !strings.ContainsAny(t, " \t") {
				return KindCashtag, strings.ToLower(t), true
			}
			return KindSearch, q, true
		}
	}
	if reserved[head] {
		return "", "", false
	}
	// /<handle>/status/<id>, with any of /photo/1, /video/1, /likes, /retweets,
	// /analytics hanging off the end.
	if len(segs) >= 3 && (segs[1] == "status" || segs[1] == "statuses") && numericRe.MatchString(segs[2]) {
		return KindTweet, segs[2], true
	}
	// /<handle>, and /<handle>/with_replies, /media, /likes, /followers, ...
	if h := handleOf(head); h != "" {
		return KindUser, h, true
	}
	return "", "", false
}

func classifyIPath(segs []string) (string, string, bool) {
	if len(segs) == 0 {
		return "", "", false
	}
	switch strings.ToLower(segs[0]) {
	case "status", "statuses":
		if len(segs) > 1 && numericRe.MatchString(segs[1]) {
			return KindTweet, segs[1], true
		}
	case "web":
		// /i/web/status/<id>
		return classifyIPath(segs[1:])
	case "lists", "list":
		if len(segs) > 1 && segs[1] != "" {
			return KindList, segs[1], true
		}
	case "spaces":
		if len(segs) > 1 && segs[1] != "" {
			return KindSpace, segs[1], true
		}
	case "communities":
		if len(segs) > 1 && segs[1] != "" {
			return KindCommunity, segs[1], true
		}
	case "broadcasts":
		if len(segs) > 1 && segs[1] != "" {
			return KindBroadcast, segs[1], true
		}
	}
	return "", "", false
}

// packLink flattens a URL into a link id: scheme/host/path, with the query kept
// when there is one, because dropping it would point at a different page.
// Tracking parameters that x.com itself adds are stripped first.
func packLink(u *url.URL) string {
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	q := u.Query()
	for _, p := range []string{"ref_src", "ref_url", "s", "t", "twclid", "utm_source", "utm_medium", "utm_campaign"} {
		q.Del(p)
	}
	id := scheme + "/" + strings.ToLower(u.Host) + u.EscapedPath()
	if e := q.Encode(); e != "" {
		id += "?" + e
	}
	return id
}

func unpackLink(id string) (string, error) {
	scheme, rest, ok := strings.Cut(id, "/")
	if !ok || rest == "" {
		return "", fmt.Errorf("not a link id: %q", id)
	}
	return scheme + "://" + rest, nil
}

func pathSegs(u *url.URL) []string {
	p := strings.Trim(u.EscapedPath(), "/")
	if p == "" {
		return nil
	}
	segs := strings.Split(p, "/")
	out := segs[:0]
	for _, s := range segs {
		if s != "" {
			d, err := url.PathUnescape(s)
			if err != nil {
				d = s
			}
			out = append(out, d)
		}
	}
	return out
}

func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return "", false
}
