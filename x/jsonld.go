package x

import (
	"encoding/json"
	"strings"
)

// jsonld.go is the fourth serialisation (spec 3003 doc 04 section 4.3).
//
// JSON-LD carries provenance the way N-Quads does rather than the way Turtle
// does: one named graph per source URL, so nothing has to be reified and a
// consumer that ignores the nesting still reads plain JSON objects. That is the
// point of the format here. A crawl that read the same tweet from three
// surfaces produces three graphs, and which surface said what survives the file.

// writeJSONLD groups triples into one named graph per source, then each graph
// into one object per subject.
func writeJSONLD(ts []Triple) (string, error) {
	doc := map[string]any{"@context": jsonldContext()}

	var order []string
	graphs := map[string][]Triple{}
	for _, t := range ts {
		if _, seen := graphs[t.Graph]; !seen {
			order = append(order, t.Graph)
		}
		graphs[t.Graph] = append(graphs[t.Graph], t)
	}

	var out []any
	for _, g := range order {
		nodes := jsonldNodes(graphs[g])
		if g == "" {
			// Statements nothing claimed. They are still data, so they go in
			// the default graph rather than under a made-up source.
			out = append(out, nodes...)
			continue
		}
		out = append(out, map[string]any{"@id": g, "@graph": nodes})
	}
	doc["@graph"] = out

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// jsonldContext maps the prefixes the writer uses. It is written out in full so
// the file is self-describing: a consumer needs no network fetch to expand it.
func jsonldContext() map[string]any {
	return map[string]any{
		"schema": NSSchema,
		"x":      NSX,
		"xsd":    NSXSD,
	}
}

func jsonldNodes(ts []Triple) []any {
	var order []string
	by := map[string]map[string]any{}
	for _, t := range ts {
		id := jsonldID(t.S)
		if id == "" {
			continue
		}
		n, seen := by[id]
		if !seen {
			n = map[string]any{"@id": id}
			by[id] = n
			order = append(order, id)
		}
		key, val := jsonldKey(t.P), jsonldValue(t.O)
		if key == "@type" {
			val = compactIRI(t.O.IRI)
		}
		switch prev := n[key].(type) {
		case nil:
			n[key] = val
		case []any:
			n[key] = append(prev, val)
		default:
			n[key] = []any{prev, val}
		}
	}
	out := make([]any, 0, len(order))
	for _, id := range order {
		out = append(out, by[id])
	}
	return out
}

func jsonldID(t Term) string {
	switch {
	case t.IRI != "":
		return t.IRI
	case t.Blank != "":
		return "_:" + t.Blank
	}
	return ""
}

// jsonldKey compacts a predicate IRI to a prefixed name, with rdf:type spelled
// as @type because that is the keyword every JSON-LD reader already knows.
func jsonldKey(p Term) string {
	if p.IRI == NSRDF+"type" {
		return "@type"
	}
	return compactIRI(p.IRI)
}

func compactIRI(s string) string {
	for prefix, ns := range map[string]string{"schema:": NSSchema, "x:": NSX, "xsd:": NSXSD} {
		if rest, ok := strings.CutPrefix(s, ns); ok {
			return prefix + rest
		}
	}
	return s
}

// jsonldValue writes an object. A resource becomes a node reference rather than
// a string, which is the difference between a link and a piece of text and the
// reason the format is worth using at all.
func jsonldValue(o Term) any {
	if id := jsonldID(o); id != "" {
		return map[string]any{"@id": id}
	}
	switch {
	case o.Lang != "":
		return map[string]any{"@value": o.Value, "@language": o.Lang}
	case o.Datatype != "":
		return map[string]any{"@value": o.Value, "@type": compactIRI(o.Datatype)}
	}
	return o.Value
}
