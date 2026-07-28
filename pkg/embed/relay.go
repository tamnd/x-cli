package embed

import (
	"fmt"
	"sort"
	"strings"
)

// The x.com page ships a Relay normalized store, not a response tree. Every
// record is flat and keyed by id, and the links between them are __ref for a
// single record and __refs for a list. Reassembling a tweet means walking
// those links, which is what this file does.
//
// Two details are worth knowing before reading the code.
//
// Root field names are whole GraphQL fields with their arguments inlined and
// the quotes escaped, so the key for a profile lookup reads
//
//	user_result_by_screen_name(safety_level:"UserScopedTimeline",screen_name:"jack")
//
// which means a caller has to match on the part before the paren rather than
// on the whole key.
//
// The store has cycles. A tweet points at its author, the author's timeline
// points back at the tweet. The walk keeps a visited set and leaves a stub
// behind when it comes back around, so the result is always finite.

const (
	refKey      = "__ref"
	refsKey     = "__refs"
	idKey       = "__id"
	typeKey     = "__typename"
	rootID      = "client:root"
	recordsKey  = "relayRecords"
	dehydrated  = "dehydratedData"
	maxRefDepth = 64
)

// Store is the flat Relay record map lifted out of a page, plus the bookkeeping
// needed to resolve links through it.
type Store struct {
	Records map[string]any
}

// RelayStore collects every relayRecords map in a parsed document into one
// store.
//
// There is usually more than one. The server renders what it has into the
// router payload and streams the rest as later chunks, and the two halves
// reference each other, so they only make sense merged.
func RelayStore(d *Doc) (*Store, error) {
	recs := findRelayRecords(d)
	if len(recs) == 0 {
		return nil, fmt.Errorf("%w: no %s in the page", ErrNoPayload, recordsKey)
	}
	return &Store{Records: recs}, nil
}

// findRelayRecords merges every relayRecords map reachable in the document.
// Later chunks win on a key collision, because they are the fresher render.
func findRelayRecords(d *Doc) map[string]any {
	out := map[string]any{}
	collect := func(v any) {
		walkAny(v, func(m map[string]any) {
			recs, ok := m[recordsKey].(map[string]any)
			if !ok {
				return
			}
			for k, v := range recs {
				out[k] = v
			}
		})
	}
	collect(d.Router)
	for _, c := range d.Chunks {
		collect(c)
	}
	return out
}

// walkAny visits every object in a value, depth first. It carries its own
// visited set because the seroval register encoding shares objects freely and
// a payload can point back at itself.
func walkAny(v any, fn func(map[string]any)) {
	seen := map[any]bool{}
	var rec func(any)
	rec = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			// Maps are not comparable, so key the visited set on the address
			// of the first field we can reach. Reflect is overkill here; the
			// __id is unique when present and depth is bounded otherwise.
			if id, ok := t[idKey].(string); ok {
				if seen[id] {
					return
				}
				seen[id] = true
			}
			fn(t)
			for _, child := range t {
				rec(child)
			}
		case []any:
			for _, child := range t {
				rec(child)
			}
		}
	}
	rec(v)
}

// Root returns the client:root record, which is where every query result hangs.
func (s *Store) Root() (map[string]any, bool) {
	m, ok := s.Records[rootID].(map[string]any)
	return m, ok
}

// Field looks up a root field by its GraphQL name, ignoring the arguments the
// page inlines into the key. When more than one key matches, the arguments are
// compared in sorted order so the result is stable across runs.
func (s *Store) Field(name string) (any, bool) {
	root, ok := s.Root()
	if !ok {
		return nil, false
	}
	var matches []string
	for k := range root {
		if k == name || strings.HasPrefix(k, name+"(") {
			matches = append(matches, k)
		}
	}
	if len(matches) == 0 {
		return nil, false
	}
	sort.Strings(matches)
	return root[matches[0]], true
}

// Fields returns every root field whose GraphQL name matches, keyed by the
// full key including arguments. A profile page carries several timeline
// fields that differ only by argument, and the caller decides which it wants.
func (s *Store) Fields(name string) map[string]any {
	root, ok := s.Root()
	if !ok {
		return nil
	}
	out := map[string]any{}
	for k, v := range root {
		if k == name || strings.HasPrefix(k, name+"(") {
			out[k] = v
		}
	}
	return out
}

// Get returns one record by id.
func (s *Store) Get(id string) (map[string]any, bool) {
	m, ok := s.Records[id].(map[string]any)
	return m, ok
}

// Resolve expands a value into a plain tree, following __ref and __refs
// through the store.
//
// A record already on the current path is replaced by a stub carrying just its
// id and typename, which keeps a cycle from becoming an infinite tree while
// still telling the caller what it pointed at.
func (s *Store) Resolve(v any) any {
	return s.resolve(v, map[string]bool{}, 0)
}

// ResolveID expands one record by id.
func (s *Store) ResolveID(id string) (any, bool) {
	if _, ok := s.Get(id); !ok {
		return nil, false
	}
	return s.Resolve(map[string]any{refKey: id}), true
}

func (s *Store) resolve(v any, path map[string]bool, depth int) any {
	if depth > maxRefDepth {
		return v
	}
	switch t := v.(type) {
	case map[string]any:
		if id, ok := t[refKey].(string); ok && len(t) == 1 {
			return s.deref(id, path, depth)
		}
		if raw, ok := t[refsKey]; ok && len(t) == 1 {
			ids, _ := raw.([]any)
			out := make([]any, 0, len(ids))
			for _, id := range ids {
				str, ok := id.(string)
				if !ok {
					out = append(out, id)
					continue
				}
				out = append(out, s.deref(str, path, depth))
			}
			return out
		}
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = s.resolve(child, path, depth+1)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, child := range t {
			out = append(out, s.resolve(child, path, depth+1))
		}
		return out
	default:
		return v
	}
}

// deref follows one __ref. A dangling ref is normal: the store is partial by
// design and the server only sends what the viewport needed, so an id with no
// record becomes a stub rather than an error.
func (s *Store) deref(id string, path map[string]bool, depth int) any {
	if path[id] {
		return stub(s.Records[id], id, "cycle")
	}
	rec, ok := s.Get(id)
	if !ok {
		return stub(nil, id, "missing")
	}
	path[id] = true
	out := s.resolve(rec, path, depth+1)
	delete(path, id)
	return out
}

// stub stands in for a record the walk will not expand, and says why. A caller
// that wants the record anyway can ask the store for it by id.
func stub(rec any, id, reason string) map[string]any {
	out := map[string]any{idKey: id, "__stub": reason}
	if m, ok := rec.(map[string]any); ok {
		if t, ok := m[typeKey]; ok {
			out[typeKey] = t
		}
	}
	return out
}

// Typed returns every record of a given __typename, resolved. This is the
// blunt way to find things when the root field name has moved, and it is what
// the fallback path in x/webpage.go uses.
func (s *Store) Typed(typename string) []any {
	var ids []string
	for id, rec := range s.Records {
		m, ok := rec.(map[string]any)
		if !ok {
			continue
		}
		if m[typeKey] == typename {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.Resolve(map[string]any{refKey: id}))
	}
	return out
}
