package embed

import (
	"encoding/json"
	"fmt"
	"strings"
)

// syndication.twitter.com is a Next.js app, so its pages carry the whole
// server render as plain JSON in a __NEXT_DATA__ script. That is the easiest
// plane in this package and the only one that needs no parser of our own.
//
// It is also the only anonymous route that returns a user's tweets in bulk.
// The timeline page hands back around a hundred entries with full text,
// entities, engagement counts and the author profile, with no credential.

const nextDataOpen = `<script id="__NEXT_DATA__" type="application/json">`

// NextData returns the __NEXT_DATA__ payload of a syndication page.
func NextData(doc string) (map[string]any, error) {
	i := strings.Index(doc, nextDataOpen)
	if i < 0 {
		return nil, fmt.Errorf("%w: no __NEXT_DATA__ script", ErrNoPayload)
	}
	rest := doc[i+len(nextDataOpen):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		return nil, fmt.Errorf("%w: unterminated __NEXT_DATA__ script", ErrNoPayload)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(rest[:j]), &out); err != nil {
		return nil, fmt.Errorf("__NEXT_DATA__: %w", err)
	}
	return out, nil
}

// PageProps returns props.pageProps, which is where everything a syndication
// page knows actually lives.
func PageProps(next map[string]any) (map[string]any, bool) {
	props, ok := next["props"].(map[string]any)
	if !ok {
		return nil, false
	}
	pp, ok := props["pageProps"].(map[string]any)
	return pp, ok
}

// TimelineEntries returns the timeline entries of a syndication timeline page,
// each one an entry object with a type, an entry_id and a content.
func TimelineEntries(next map[string]any) ([]any, error) {
	pp, ok := PageProps(next)
	if !ok {
		return nil, fmt.Errorf("%w: no props.pageProps", ErrNoPayload)
	}
	tl, ok := pp["timeline"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: no pageProps.timeline", ErrNoPayload)
	}
	entries, ok := tl["entries"].([]any)
	if !ok {
		// An account with nothing to show renders the page with no entries
		// key at all, which is a real answer rather than a broken page.
		return nil, nil
	}
	return entries, nil
}

// Dig walks a path of object keys and returns what it finds. It exists so the
// callers of this package can pull one field out of a deep payload without
// four levels of comma-ok.
func Dig(v any, path ...string) (any, bool) {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return v, true
}

// MustDig is Dig for a caller that is about to type-check the result anyway,
// so a missing key and a key holding nil are the same answer.
func MustDig(v any, path ...string) any {
	got, _ := Dig(v, path...)
	return got
}

// DigStr is Dig for a string field.
func DigStr(v any, path ...string) (string, bool) {
	got, ok := Dig(v, path...)
	if !ok {
		return "", false
	}
	s, ok := got.(string)
	return s, ok
}
