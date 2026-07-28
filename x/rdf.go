package x

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// rdf.go turns a document into triples (spec 3003 doc 04 section 4).
//
// The vocabulary is schema.org wherever a term exists and `x:` where none does,
// and that is not a taste call. X publishes schema.org microdata on every status
// and profile page, so a tweet already has a vendor-blessed RDF shape and this
// tool's job is to agree with it rather than invent a parallel one. Where the
// alignment holds, the output can be checked against X's own markup instead of
// only against this tool's expectations, which is what
// TestEveryTripleXAssertsSurvivesTheRoundTrip does.
//
// Where X asserts nothing, `x:` fills in. Every `x:` term used here is one X has
// no word for: a conversation, a hashtag, a reply relation.

// The namespaces. xnode is x's own address space, so a node keeps the same
// identity in RDF that it has everywhere else in the tool.
const (
	NSSchema = "https://schema.org/"
	// NSX resolves to a page that defines every term in it, which doc 04
	// section 4.2 asks for and a namespace IRI nobody can dereference does not
	// deliver. That is why it is the docs site and not the repo: the repo URL
	// doc 04 first wrote is a 404, and a vocabulary you cannot look up is a
	// vocabulary nobody outside this tool can use.
	NSX   = "https://x-cli.tamnd.com/ns#"
	NSXSD = "http://www.w3.org/2001/XMLSchema#"
	NSRDF = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
)

// Term is one RDF node: an IRI, a blank node, or a literal. Exactly one of the
// three is set, which the writers rely on to know how to quote it.
type Term struct {
	IRI      string
	Blank    string
	Value    string
	Datatype string

	// Lang is a literal's language tag. Nothing sets it today, because X states
	// its own text untagged and this vocabulary follows X, but a term type that
	// cannot hold one is not an RDF term and every writer here handles it.
	Lang string
}

// Triple is one statement, with the URL it was learned from. Graph is the
// N-Quads graph name and the JSON-LD @graph key, which is how provenance
// survives a merge without reifying anything.
type Triple struct {
	S, P, O Term
	Graph   string
}

func iri(s string) Term       { return Term{IRI: s} }
func blank(s string) Term     { return Term{Blank: s} }
func lit(s string) Term       { return Term{Value: s} }
func typed(s, dt string) Term { return Term{Value: s, Datatype: dt} }
func schema(name string) Term { return iri(NSSchema + name) }
func xterm(name string) Term  { return iri(NSX + name) }
func intLit(n int) Term       { return typed(strconv.Itoa(n), NSXSD+"integer") }
func (t Term) empty() bool    { return t.IRI == "" && t.Blank == "" && t.Value == "" }

// classOf maps a node kind to its class. It is doc 04 section 4.1's table, and
// the two that need a word of their own are media and the tag kinds: schema.org
// splits media by what it is, so a media node with no record is the generic
// MediaObject rather than a guess between image and video.
var classOf = map[string]Term{
	KindTweet:        schema("SocialMediaPosting"),
	KindUser:         schema("Person"),
	KindMedia:        schema("MediaObject"),
	KindLink:         schema("WebPage"),
	KindList:         schema("ItemList"),
	KindSpace:        schema("Event"),
	KindConversation: xterm("Conversation"),
	KindHashtag:      xterm("Hashtag"),
	KindCashtag:      xterm("Cashtag"),
	KindTrend:        xterm("Trend"),
	KindPlace:        schema("Place"),
	KindPoll:         xterm("Poll"),
	KindCard:         xterm("Card"),
	KindNote:         xterm("CommunityNote"),
	KindBroadcast:    schema("BroadcastEvent"),
	KindCommunity:    schema("Organization"),
}

// ClassOf is the RDF class for a node kind, and whether the kind has one. A
// search is a query rather than a thing, so it does not.
func ClassOf(kind string) (Term, bool) {
	c, ok := classOf[kind]
	return c, ok
}

// predIRI maps a predicate to its RDF term. The bool is whether the triple runs
// backwards from the edge: `authored` is a claim by an account about a tweet,
// and schema.org spells the same claim as the tweet's schema:author, so that one
// edge comes out reversed.
//
// Doc 04 section 4.1 has rows for eleven of the twenty-five. The other fourteen
// are here because a table with holes in it is not a serialisation: an edge with
// no term would be silently dropped, and a graph that quietly loses claims is
// worse than one that spells a few of them in a vendor namespace.
var predIRI = map[Predicate]struct {
	term     Term
	reversed bool
}{
	PredAuthored:       {schema("author"), true},
	PredRepliesTo:      {xterm("repliesTo"), false},
	PredQuotes:         {schema("citation"), false},
	PredReposts:        {xterm("reposts"), false},
	PredInConversation: {xterm("inConversation"), false},
	PredMentions:       {schema("mentions"), false},
	PredTagged:         {schema("keywords"), false},
	PredTaggedSymbol:   {xterm("taggedSymbol"), false},
	PredLinksTo:        {schema("citation"), false},
	PredHasMedia:       {schema("associatedMedia"), false},
	PredHasPoll:        {xterm("hasPoll"), false},
	PredHasCard:        {xterm("hasCard"), false},
	PredHasNote:        {xterm("hasNote"), false},
	PredPinned:         {xterm("pinned"), false},
	PredWebsite:        {schema("sameAs"), false},
	PredFollows:        {xterm("follows"), false},
	PredFollowedBy:     {xterm("follows"), true},
	PredLiked:          {xterm("liked"), false},
	PredReposted:       {xterm("reposted"), false},
	PredMemberOf:       {schema("memberOf"), false},
	PredOwns:           {xterm("owns"), false},
	PredHosted:         {xterm("hosted"), false},
	PredSpokeIn:        {xterm("spokeIn"), false},
	PredTrendingIn:     {schema("location"), false},
	PredChildOf:        {schema("containedInPlace"), false},
}

// PredicateIRI is the RDF term for a predicate, and whether the statement runs
// against the edge.
func PredicateIRI(p Predicate) (term Term, reversed, ok bool) {
	m, ok := predIRI[p]
	return m.term, m.reversed, ok
}

// Triples turns a document into statements: a class and its literals for every
// node, and one statement per claim.
//
// Nodes with no record still get their class, because an address and a type is
// real information about a node nobody fetched, and it is what makes the output
// joinable with a later crawl that does fetch it.
func Triples(d Document) []Triple {
	var out []Triple
	b := &builder{out: &out}
	for _, n := range d.Nodes {
		if c, ok := ClassOf(n.Kind); ok {
			b.src = recordSource(n.Record)
			b.add(iri(n.URI), iri(NSRDF+"type"), c)
		}
		b.record(n)
	}
	for _, e := range d.Edges {
		m, ok := predIRI[e.Predicate]
		if !ok {
			continue
		}
		s, o := iri(e.From), iri(e.To)
		if m.reversed {
			s, o = o, s
		}
		out = append(out, Triple{S: s, P: m.term, O: o, Graph: e.Source})
	}
	return out
}

// builder accumulates statements that share a source URL.
type builder struct {
	out *[]Triple
	src string
	// bn numbers the blank nodes for the interaction counters. They are numbered
	// per document rather than per node so two counters never collide.
	bn int
}

func (b *builder) add(s, p, o Term) {
	if o.empty() {
		return
	}
	*b.out = append(*b.out, Triple{S: s, P: p, O: o, Graph: b.src})
}

// counter emits one schema:InteractionCounter, which is how schema.org spells a
// count of an action and how X spells it in its own microdata. The label is the
// name X puts on it, kept so the cross-check test can match X's markup literally.
//
// prop is interactionStatistic or agentInteractionStatistic, and the difference
// is who did the thing. Followers are something done to an account and follows
// are something it did, so they are counts of the same action pointing opposite
// ways, and schema.org has two properties for exactly that. X uses both
// correctly on its own pages, which settles the question of what this tool
// should do.
func (b *builder) counter(s Term, prop, action, label string, n *int) {
	if n == nil {
		return
	}
	b.bn++
	node := blank(fmt.Sprintf("c%d", b.bn))
	b.add(s, schema(prop), node)
	b.add(node, iri(NSRDF+"type"), schema("InteractionCounter"))
	b.add(node, schema("interactionType"), schema(action))
	if label != "" {
		b.add(node, schema("name"), lit(label))
	}
	b.add(node, schema("userInteractionCount"), intLit(*n))
}

// record writes the literals a node's record carries. A node the read only
// named has no record and no literals, which is the truth rather than a gap.
func (b *builder) record(n GraphNode) {
	switch r := n.Record.(type) {
	case *Tweet:
		b.tweet(iri(n.URI), r)
	case *User:
		b.user(iri(n.URI), r)
	case *Space:
		b.space(iri(n.URI), r)
	case *List:
		b.list(iri(n.URI), r)
	}
}

func (b *builder) tweet(s Term, t *Tweet) {
	b.src = t.LastSource()
	b.add(s, schema("identifier"), lit(t.ID))
	// A plain literal rather than a language-tagged one, which is the
	// weaker RDF and the right call: X states articleBody untagged in its
	// own microdata, and the point of this vocabulary is to agree with the
	// source. The language is not lost, it is the schema:inLanguage below.
	b.add(s, schema("articleBody"), lit(t.Text))
	if !t.CreatedAt.IsZero() {
		b.add(s, schema("datePublished"), typed(t.CreatedAt.UTC().Format(time.RFC3339), NSXSD+"dateTime"))
	}
	b.add(s, schema("inLanguage"), lit(t.Lang))
	b.add(s, schema("url"), iri(t.URL))
	if t.Metrics.Replies != nil {
		b.add(s, schema("commentCount"), intLit(*t.Metrics.Replies))
	}
	// The action types and the names are X's own choices in its microdata, which
	// is why a quote is an InteractAction rather than something more
	// descriptive: matching the source is worth more here than being clever.
	// Bookmarks and impressions are this tool's, because no X page states them
	// and there was nothing to copy.
	b.counter(s, "interactionStatistic", "LikeAction", "Likes", t.Metrics.Likes)
	b.counter(s, "interactionStatistic", "ShareAction", "Retweets", t.Metrics.Retweets)
	b.counter(s, "interactionStatistic", "InteractAction", "Quotes", t.Metrics.Quotes)
	b.counter(s, "interactionStatistic", "ReplyAction", "Replies", t.Metrics.Replies)
	b.counter(s, "interactionStatistic", "BookmarkAction", "Bookmarks", t.Metrics.Bookmarks)
	b.counter(s, "interactionStatistic", "ViewAction", "Views", t.Metrics.Impressions)
	for _, m := range t.Media {
		b.media(iri(mediaURI(m)), m)
	}
}

// media types a media node properly once there is a record to type it from.
// schema.org splits media by what it is and a key on its own does not say, so
// this is the only place an image can be told from a video.
func (b *builder) media(s Term, m Media) {
	class := schema("ImageObject")
	if m.Type != "photo" {
		class = schema("VideoObject")
	}
	b.add(s, iri(NSRDF+"type"), class)
	b.add(s, schema("contentUrl"), iri(m.URL))
	b.add(s, schema("caption"), lit(m.AltText))
	if m.Width > 0 {
		b.add(s, schema("width"), intLit(m.Width))
	}
	if m.Height > 0 {
		b.add(s, schema("height"), intLit(m.Height))
	}
	if m.Duration > 0 {
		b.add(s, schema("duration"), typed(isoDuration(m.Duration), NSXSD+"duration"))
	}
}

func (b *builder) user(s Term, u *User) {
	b.src = u.LastSource()
	b.add(s, schema("identifier"), lit(u.RestID))
	b.add(s, schema("name"), lit(u.Name))
	b.add(s, schema("alternateName"), lit(u.Username))
	b.add(s, schema("description"), lit(u.Description))
	b.add(s, schema("url"), iri(u.URL))
	b.add(s, schema("sameAs"), iri(u.Website))
	b.add(s, schema("image"), iri(u.ProfileImage))
	if u.Location != "" {
		b.add(s, schema("homeLocation"), lit(u.Location))
	}
	if !u.CreatedAt.IsZero() {
		b.add(s, xterm("joined"), typed(u.CreatedAt.UTC().Format(time.RFC3339), NSXSD+"dateTime"))
	}
	// The properties and the names are the ones X puts on these counters in its
	// own markup. Followers are done to the account, follows and posts are done
	// by it, which is the interactionStatistic / agentInteractionStatistic split.
	b.counter(s, "interactionStatistic", "FollowAction", "Follows", u.Metrics.Followers)
	b.counter(s, "agentInteractionStatistic", "FollowAction", "Following", u.Metrics.Following)
	b.counter(s, "agentInteractionStatistic", "WriteAction", "Tweets", u.Metrics.Tweets)
}

func (b *builder) space(s Term, sp *Space) {
	b.src = sp.LastSource()
	b.add(s, schema("identifier"), lit(sp.ID))
	b.add(s, schema("name"), lit(sp.Title))
	b.add(s, schema("url"), iri(sp.URL))
	b.add(s, xterm("state"), lit(sp.State))
	if !sp.StartedAt.IsZero() {
		b.add(s, schema("startDate"), typed(sp.StartedAt.UTC().Format(time.RFC3339), NSXSD+"dateTime"))
	}
	if !sp.EndedAt.IsZero() {
		b.add(s, schema("endDate"), typed(sp.EndedAt.UTC().Format(time.RFC3339), NSXSD+"dateTime"))
	}
	if sp.Participants > 0 {
		b.add(s, schema("maximumAttendeeCapacity"), intLit(sp.Participants))
	}
}

func (b *builder) list(s Term, l *List) {
	b.src = l.LastSource()
	b.add(s, schema("identifier"), lit(l.ID))
	b.add(s, schema("name"), lit(l.Name))
	b.add(s, schema("description"), lit(l.Description))
	b.add(s, schema("url"), iri(l.URL))
	if l.Members > 0 {
		b.add(s, schema("numberOfItems"), intLit(l.Members))
	}
}

// isoDuration writes milliseconds the way xsd:duration wants them, to the
// second, because that is the precision a video length is meaningful at.
func isoDuration(ms int) string {
	return "PT" + strconv.Itoa(ms/1000) + "S"
}

// recordSource is the URL a record was last read from, for a node whose class
// triple is written before the record is walked.
func recordSource(rec any) string {
	if m, ok := metaOf(rec); ok {
		return m.LastSource()
	}
	return ""
}

func metaOf(rec any) (*Meta, bool) {
	switch r := rec.(type) {
	case *Tweet:
		return &r.Meta, true
	case *User:
		return &r.Meta, true
	case *Space:
		return &r.Meta, true
	case *List:
		return &r.Meta, true
	}
	return nil, false
}

// LastSource is the URL a record was most recently read from. A record merged
// from several surfaces has several, and the last one is the one that produced
// the shape being serialised.
func (m *Meta) LastSource() string {
	if m == nil || len(m.Sources) == 0 {
		return ""
	}
	return m.Sources[len(m.Sources)-1]
}

// ---- serialisation ----

// RDFOptions controls what the writers emit beyond the data itself.
type RDFOptions struct {
	// Provenance turns on reified statements in Turtle and N-Triples. It is off
	// by default because reification costs four triples per statement and
	// outnumbers the data, and because the two formats that carry a graph name
	// (N-Quads, JSON-LD) never need it.
	Provenance bool
}

// WriteRDF serialises triples in the named format: nt, ttl, jsonld, nq.
func WriteRDF(format string, ts []Triple, opts RDFOptions) (string, error) {
	switch strings.ToLower(format) {
	case "nt", "ntriples", "n-triples":
		return writeNT(ts, opts, false), nil
	case "nq", "nquads", "n-quads":
		return writeNT(ts, opts, true), nil
	case "ttl", "turtle":
		return writeTTL(ts, opts), nil
	case "jsonld", "json-ld":
		return writeJSONLD(distinct(ts, true))
	}
	return "", fmt.Errorf("unknown RDF format %q: use nt, ttl, jsonld, or nq", format)
}

// distinct drops repeats. Two surfaces asserting the same thing is two rows in
// the store on purpose, and in N-Quads and JSON-LD they stay two statements
// because the graph name keeps them apart. N-Triples and Turtle have no graph
// name, so the same statement twice is just the same statement twice, and
// printing it twice says nothing extra while looking like a bug.
func distinct(ts []Triple, keepGraph bool) []Triple {
	out := make([]Triple, 0, len(ts))
	seen := map[string]bool{}
	for _, t := range ts {
		k := ntTerm(t.S) + " " + ntTerm(t.P) + " " + ntTerm(t.O)
		if keepGraph {
			k += " " + t.Graph
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out
}

// RDFFormats is every serialisation, for a flag's help and for a test that
// wants to run them all.
var RDFFormats = []string{"nt", "ttl", "jsonld", "nq"}

// writeNT writes N-Triples, or N-Quads when quads is set. N-Quads is the format
// that carries provenance for free: the source URL is the graph name, so a merge
// of two crawls keeps knowing which read said what.
func writeNT(ts []Triple, opts RDFOptions, quads bool) string {
	var b strings.Builder
	for _, t := range distinct(ts, quads) {
		b.WriteString(ntTerm(t.S) + " " + ntTerm(t.P) + " " + ntTerm(t.O))
		if quads && t.Graph != "" {
			b.WriteString(" <" + escapeIRI(t.Graph) + ">")
		}
		b.WriteString(" .\n")
	}
	// Reification runs over every triple rather than the distinct ones, because
	// the whole point of it is the source, and two sources for one statement is
	// the case worth writing down.
	if opts.Provenance && !quads {
		for i, t := range ts {
			b.WriteString(reifyNT(t, i))
		}
	}
	return b.String()
}

// reifyNT states who said a statement, in the only way N-Triples can: four more
// triples describing the statement itself. It is behind a flag for exactly that
// reason.
func reifyNT(t Triple, i int) string {
	s := fmt.Sprintf("_:s%d", i)
	var b strings.Builder
	b.WriteString(s + " <" + NSRDF + "type> <" + NSRDF + "Statement> .\n")
	b.WriteString(s + " <" + NSRDF + "subject> " + ntTerm(t.S) + " .\n")
	b.WriteString(s + " <" + NSRDF + "predicate> " + ntTerm(t.P) + " .\n")
	b.WriteString(s + " <" + NSRDF + "object> " + ntTerm(t.O) + " .\n")
	if t.Graph != "" {
		b.WriteString(s + " <" + NSX + "source> <" + escapeIRI(t.Graph) + "> .\n")
	}
	return b.String()
}

func ntTerm(t Term) string {
	switch {
	case t.IRI != "":
		return "<" + escapeIRI(t.IRI) + ">"
	case t.Blank != "":
		return "_:" + t.Blank
	case t.Lang != "":
		return quote(t.Value) + "@" + t.Lang
	case t.Datatype != "":
		return quote(t.Value) + "^^<" + t.Datatype + ">"
	}
	return quote(t.Value)
}

// writeTTL writes Turtle grouped by subject, which is the only reason to prefer
// it over N-Triples: a human reads a node at a time.
func writeTTL(ts []Triple, opts RDFOptions) string {
	var b strings.Builder
	b.WriteString("@prefix rdf:    <" + NSRDF + "> .\n")
	b.WriteString("@prefix schema: <" + NSSchema + "> .\n")
	b.WriteString("@prefix x:      <" + NSX + "> .\n")
	b.WriteString("@prefix xsd:    <" + NSXSD + "> .\n\n")

	order, bySubject := groupBySubject(distinct(ts, false))
	for _, s := range order {
		g := bySubject[s]
		b.WriteString(ttlTerm(g[0].S) + "\n")
		for i, t := range g {
			end := " ;\n"
			if i == len(g)-1 {
				end = " .\n"
			}
			b.WriteString("  " + ttlTerm(t.P) + " " + ttlTerm(t.O) + end)
		}
		b.WriteString("\n")
	}
	if opts.Provenance {
		for i, t := range ts {
			b.WriteString(reifyNT(t, i))
		}
	}
	return b.String()
}

// groupBySubject keeps first-seen subject order, so the node a caller asked
// about leads the file instead of whatever sorts first.
func groupBySubject(ts []Triple) ([]string, map[string][]Triple) {
	var order []string
	by := map[string][]Triple{}
	for _, t := range ts {
		k := ntTerm(t.S)
		if _, seen := by[k]; !seen {
			order = append(order, k)
		}
		by[k] = append(by[k], t)
	}
	return order, by
}

func ttlTerm(t Term) string {
	if t.IRI != "" {
		// `a` before the prefixes, because rdf:type is spelled `a` in Turtle
		// and matching the rdf: prefix first would spell it the long way.
		if t.IRI == NSRDF+"type" {
			return "a"
		}
		for prefix, ns := range map[string]string{"rdf:": NSRDF, "schema:": NSSchema, "x:": NSX, "xsd:": NSXSD} {
			if rest, ok := strings.CutPrefix(t.IRI, ns); ok && isPName(rest) {
				return prefix + rest
			}
		}
	}
	if t.Datatype != "" {
		if rest, ok := strings.CutPrefix(t.Datatype, NSXSD); ok {
			return quote(t.Value) + "^^xsd:" + rest
		}
	}
	return ntTerm(t)
}

// isPName reports whether a local name can be written after a prefix without
// escaping. An x:// URI cannot, which is why node addresses stay in angle
// brackets even in Turtle.
func isPName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func quote(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`,
	)
	return `"` + r.Replace(s) + `"`
}

// escapeIRI keeps the few characters an IRI cannot contain out of one. X's URLs
// carry query strings with spaces in them often enough for this to matter.
func escapeIRI(s string) string {
	r := strings.NewReplacer(
		"<", "%3C", ">", "%3E", `"`, "%22", " ", "%20",
		"{", "%7B", "}", "%7D", "|", "%7C", `\`, "%5C", "^", "%5E", "`", "%60",
	)
	return r.Replace(s)
}
