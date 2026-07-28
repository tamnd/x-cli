package x

import "sort"

// document.go assembles a graph as one value (spec 3003 doc 05, `x graph`).
//
// `x edges` prints claims and `x get` prints records, and a caller who wants
// both has to run two commands and join them by hand. A graph document is the
// join: every claim the reads assert, and every node those claims address, in
// one object. It makes no requests of its own, so what it can say about a node
// is exactly what the reads happened to carry.

// GraphNode is one node in a document: its address, and the record behind it
// when a read actually carried one.
//
// Most nodes have no record. A tweet names the accounts it mentions and the
// links it carries without fetching any of them, and a document that only
// listed the nodes it had records for would drop most of the graph. So an
// addressed-but-unread node is still a node, with Record left empty, which is
// also the honest shape: it says this exists and we have not looked.
type GraphNode struct {
	URI    string `json:"uri"`
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Record any    `json:"record,omitempty"`
}

// Document is a graph as one value.
type Document struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []Edge      `json:"edges"`
}

// Graph turns records into one document. The edges are every claim the records
// make, merged and sorted the way `x edges` prints them; the nodes are every
// endpoint those claims address, plus the records themselves, since a record
// with nothing to say about anyone else is still a node.
func Graph(recs ...any) Document {
	var edges []Edge
	carried := map[string]any{}
	for _, rec := range recs {
		edges = append(edges, Edges(rec)...)
		carry(rec, carried)
	}
	edges = MergeEdges(edges)

	seen := map[string]bool{}
	var uris []string
	add := func(uri string) {
		if uri == "" || seen[uri] {
			return
		}
		seen[uri] = true
		uris = append(uris, uri)
	}
	for _, e := range edges {
		add(e.From)
		add(e.To)
	}
	for uri := range carried {
		add(uri)
	}
	sort.Strings(uris)

	doc := Document{Edges: edges, Nodes: make([]GraphNode, 0, len(uris))}
	for _, uri := range uris {
		kind, id, ok := SplitURI(uri)
		if !ok {
			continue
		}
		doc.Nodes = append(doc.Nodes, GraphNode{URI: uri, Kind: kind, ID: id, Record: carried[uri]})
	}
	return doc
}

// carry indexes every record a read handed over, including the ones riding
// inside another. A quoted tweet arrives whole inside its quoter and a tweet's
// author arrives whole inside the tweet, and pretending we only have the top
// record would throw away data already paid for.
func carry(rec any, into map[string]any) {
	switch r := rec.(type) {
	case *Tweet:
		if r == nil || r.ID == "" {
			return
		}
		into[URI(KindTweet, r.ID)] = r
		carry(r.Author, into)
		carry(r.Quoted, into)
		carry(r.Retweeted, into)
	case *User:
		if r == nil || r.Username == "" {
			return
		}
		into[userURI(r.Username)] = r
	case *Space:
		if r == nil || r.ID == "" {
			return
		}
		into[URI(KindSpace, r.ID)] = r
		carry(r.Creator, into)
		for _, u := range r.Hosts {
			carry(u, into)
		}
		for _, u := range r.Speakers {
			carry(u, into)
		}
	case *List:
		if r == nil || r.ID == "" {
			return
		}
		into[URI(KindList, r.ID)] = r
		carry(r.Owner, into)
	}
}
