package x

import (
	"strconv"
	"strings"
)

// meta.go is the envelope every record carries (spec 3003 doc 03 section 1).
//
// It is what makes `x get`, `-o url`, the graph plane and the RDF writers work
// the same way across all sixteen kinds: whatever came back, it has a kind, an
// id, an address in x's own space, an address on the web, and a record of what
// it cost to know and where each part of it came from.
//
// It replaces the old `provenance` string, which said "s1+s8" and left the
// caller to guess which of the two gave the bookmark count.

// Meta is the head of every record.
type Meta struct {
	Kind string `json:"kind"`
	ID   string `json:"id" kit:"id"`
	URI  string `json:"uri,omitempty"`
	URL  string `json:"url,omitempty"`

	// Tier is the highest credential class any contributing surface needed, so
	// it answers "could I have got this without a login" rather than "which
	// request happened to be last".
	Tier int `json:"tier"`

	// Surfaces is every surface that contributed, in the order they were read,
	// as the short codes doc 01 numbers them: s1 through s8.
	Surfaces []string `json:"surfaces,omitempty"`

	// Sources is every URL read to build this record, in the same order.
	Sources []string `json:"sources,omitempty"`

	// Via maps a field name to the surface that filled it, and is set whenever
	// more than one surface contributed. Two surfaces that disagree about a
	// count is a fact about X, not a bug, and via is how the record says which
	// number came from where.
	Via map[string]string `json:"via,omitempty"`

	// Missed names a surface x wanted, could not read, and carried on without,
	// with the reason. A record is allowed to be thin, but the reader has to be
	// able to tell a thin record apart from a complete one: @nasa with no banner
	// because surface 2's window was spent looks exactly like @nasa with no
	// banner because there is no banner, and only one of those is a fact about
	// the account (spec 3003 doc 03 section 3.4).
	Missed []string `json:"missed,omitempty"`
}

// There is no Extra field here, and doc 03 section 2 said there would be: every
// decoder was to unmarshal into a map first, lift the keys it knows, and leave
// the rest in an Extra bag that a fixture test then asserted was empty.
//
// The bag went, the promise stayed, and it is kept somewhere better. unmodeled.go
// walks a captured response against the struct that decodes it and lists every
// path nothing reads, at any depth, through any wrapper. TestNoUnmodeledKeys runs
// that over every fixture, so an upstream addition is still a red build, without
// each decoder paying for a second pass and without a bag that only ever fills up
// for shapes somebody already thought about.

// Identify sets the kind and id and derives the two addresses from them. The
// URI is always derivable; the URL is not, because a media key and a place have
// no page of their own, and in that case it stays empty rather than made up.
func (m *Meta) Identify(kind, id string) {
	m.Kind, m.ID = kind, id
	if id == "" {
		return
	}
	m.URI = URI(kind, id)
	if m.URL == "" {
		if u, err := Locate(kind, id); err == nil {
			m.URL = u
		}
	}
}

// Stamp records that a surface contributed, what URL was read, and what tier
// that surface needs. Reading the same surface twice does not double the list.
func (m *Meta) Stamp(surface int, url string) {
	code := SurfaceCode(surface)
	if code != "" && !hasStr(m.Surfaces, code) {
		m.Surfaces = append(m.Surfaces, code)
	}
	if url != "" && !hasStr(m.Sources, url) {
		m.Sources = append(m.Sources, url)
	}
	if t := surfaceTier(surface); t > m.Tier {
		m.Tier = t
	}
}

// Note records that a surface filled a field. It is only worth saying once more
// than one surface is in play, but recording it unconditionally is cheaper than
// deciding, and the merge layer drops a via that names a single surface.
func (m *Meta) Note(field string, surface int) {
	code := SurfaceCode(surface)
	if field == "" || code == "" {
		return
	}
	if m.Via == nil {
		m.Via = map[string]string{}
	}
	m.Via[field] = code
}

// Miss records that a surface was tried and did not answer. The reason is the
// error as the user would have seen it, because "rate limited until 16:45" and
// "no such account" ask for different things from whoever is reading.
func (m *Meta) Miss(surface int, err error) {
	code := SurfaceCode(surface)
	if code == "" || err == nil {
		return
	}
	note := code + ": " + err.Error()
	if !hasStr(m.Missed, note) {
		m.Missed = append(m.Missed, note)
	}
}

// Surface is the one surface a record came from, or 0 when it came from more
// than one or from none.
func (m Meta) Surface() int {
	if len(m.Surfaces) != 1 {
		return 0
	}
	return surfaceNum(m.Surfaces[0])
}

// SurfaceCode is the short name doc 01 uses for a surface: s1 for the
// syndication tweet endpoint, s8 for x.com's own HTML, and so on.
func SurfaceCode(n int) string {
	if n < 1 || n > len(Surfaces) {
		return ""
	}
	return "s" + strconv.Itoa(n)
}

func surfaceNum(code string) int {
	if len(code) < 2 || code[0] != 's' {
		return 0
	}
	n, err := strconv.Atoi(code[1:])
	if err != nil {
		return 0
	}
	return n
}

func surfaceTier(n int) int {
	for _, s := range Surfaces {
		if s.N == n {
			return s.Tier
		}
	}
	return 0
}

// tweetMeta is the envelope for a tweet from one surface, which is how every
// decoder starts a record. It leaves the URL alone, because the decoder is
// about to read a better one off the page: /jack/status/20 is the address X
// itself publishes, and /i/status/20 is only the fallback for when nobody said
// who wrote it. Identify at the end of the decode fills that in.
func tweetMeta(id string, surface int, src string) Meta {
	var m Meta
	m.Kind, m.ID, m.URI = KindTweet, id, URI(KindTweet, id)
	m.Stamp(surface, src)
	return m
}

// setHandle records the handle in both places it belongs: Username keeps the
// casing its owner chose, and the envelope keeps the lowercased form that
// addresses the account.
func (u *User) setHandle(handle string) {
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if handle == "" {
		return
	}
	u.Username = handle
	u.Identify(KindUser, handleID(handle))
}

// handleID is the lowercased handle, which is a user's id everywhere in x.
func handleID(handle string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
}

// stampTweet marks a tweet, and everything hanging off it, as having come from
// one surface. The decoders themselves are surface-agnostic, because the same
// legacy shape arrives from the syndication widget and from GraphQL, and only
// the caller knows which door it came through.
func stampTweet(t *Tweet, surface int, src string) {
	if t == nil {
		return
	}
	t.Stamp(surface, src)
	stampUser(t.Author, surface, src)
	stampTweet(t.Quoted, surface, src)
	stampTweet(t.Retweeted, surface, src)
}

func stampUser(u *User, surface int, src string) {
	if u == nil {
		return
	}
	u.Stamp(surface, src)
}

// stampTweets is the list form, for a timeline read in one request.
func stampTweets(ts []*Tweet, surface int, src string) {
	for _, t := range ts {
		stampTweet(t, surface, src)
	}
}

// NewTweet starts a tweet record from its id, with the envelope filled in.
func NewTweet(id string) *Tweet {
	t := &Tweet{}
	t.Identify(KindTweet, id)
	return t
}

// NewUser starts a user record from a handle, which is a user's identity.
func NewUser(handle string) *User {
	u := &User{}
	u.setHandle(handle)
	return u
}
