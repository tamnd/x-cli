package x

import (
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// unmodeled.go answers one question about a captured response: which keys did X
// send that this version does not read?
//
// It is the mechanism behind the build-failing test in spec 3003 doc 06 section
// 5.3. A tool that decodes into a struct is silent about everything the struct
// does not name, so an upstream addition looks exactly like an upstream that
// never changed. Comparing the raw JSON against the target type turns that
// silence into a list, and the list into a red build.
//
// It works off the type rather than the decoded value on purpose. A field that
// came back empty is still a field this version reads, and a value-driven walk
// cannot tell the two apart.

// unmodeled lists the JSON paths in raw that dst's type does not read, sorted.
// Array elements collapse to a single [] step, because a hundred tweets in a
// timeline missing the same key is one finding, not a hundred.
func unmodeled(raw []byte, dst any) []string {
	found := map[string]bool{}
	walkUnmodeled(raw, reflect.TypeOf(dst), "", found)
	out := make([]string, 0, len(found))
	for p := range found {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func walkUnmodeled(raw []byte, t reflect.Type, path string, found map[string]bool) {
	if t == nil || len(raw) == 0 {
		return
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	// A RawMessage, an any, or a map keyed by string with raw values is the
	// shape that keeps whatever it is given. Nothing under it is unread.
	if t == rawMessageType || t.Kind() == reflect.Interface {
		return
	}

	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return // []byte, which is a value and not a shape
		}
		var elems []json.RawMessage
		if json.Unmarshal(raw, &elems) != nil {
			return
		}
		for _, e := range elems {
			walkUnmodeled(e, t.Elem(), path+"[]", found)
		}
	case reflect.Map:
		// Every key of a map is read by definition; only the values have a shape.
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return
		}
		for _, v := range obj {
			walkUnmodeled(v, t.Elem(), path+".{}", found)
		}
	case reflect.Struct:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			return // not an object, so there are no keys to miss
		}
		fields := jsonFields(t)
		for k, v := range obj {
			f, ok := fields[k]
			if !ok {
				found[join(path, k)] = true
				continue
			}
			walkUnmodeled(v, f, join(path, k), found)
		}
	}
}

// join is a dotted path with no leading dot, so a top-level miss reads as
// "screen_name" rather than ".screen_name".
func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

// jsonFields maps the wire keys a struct reads to their types, following
// embedded structs the way encoding/json does. A field tagged "-" is not read
// from the wire and so is not in the map.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	collectJSONFields(t, out)
	return out
}

func collectJSONFields(t reflect.Type, out map[string]reflect.Type) {
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			et := f.Type
			for et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				collectJSONFields(et, out)
				continue
			}
		}
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f.Type
	}
}

// unmodeledReport renders the paths for a test failure, capped, because a
// wholesale upstream reshape produces a list nobody reads past the first screen.
func unmodeledReport(paths []string) string {
	const max = 40
	shown := paths
	suffix := ""
	if len(shown) > max {
		shown = shown[:max]
		suffix = "\n  ... and " + strconv.Itoa(len(paths)-max) + " more"
	}
	return "  " + strings.Join(shown, "\n  ") + suffix
}
