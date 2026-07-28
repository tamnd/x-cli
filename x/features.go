package x

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// features.go is the self-heal for X's feature blob (spec 3003 doc 06 section
// 3.3).
//
// Every GraphQL read carries a JSON object of feature flags, and X adds to it
// whenever it ships something. A request missing a flag X now requires is
// rejected outright, so a tool that ships the blob as a constant breaks on X's
// deploy schedule rather than its own.
//
// The rejection names the flags it wanted. So read them, send them back as
// false, and retry, once. False is the right guess: a flag the tool has never
// heard of gates a feature it does not ask for.
//
// Once, and only once, for the same reason the guest token re-mint is once: a
// retry loop against a rejection is a way to get an IP blocked.

// heal records the features X asked for and reports whether any were new. A
// second rejection naming the same flags is a real failure, not another round.
func (g *GraphQL) heal(names []string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	added := false
	for _, n := range names {
		if g.healed[n] {
			continue
		}
		if g.healed == nil {
			g.healed = map[string]bool{}
		}
		g.healed[n] = true
		added = true
	}
	return added
}

// missingFeatures pulls the flag names out of a rejection, or nothing when the
// failure was about something else.
func missingFeatures(err error) []string {
	he, ok := err.(*HTTPError)
	if !ok || (he.Status != 400 && he.Status != 422) {
		return nil
	}
	var body struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal([]byte(he.Body), &body) != nil {
		return nil
	}
	var out []string
	for _, e := range body.Errors {
		out = append(out, featureNames(e.Message)...)
	}
	return out
}

// featureNames reads the two shapes X states a missing flag in:
//
//	The following features cannot be null: a, b
//	missing variable: a
//
// Anything past the marker that does not look like a flag name is dropped, so a
// message that changes shape heals nothing rather than adding junk to the blob.
func featureNames(msg string) []string {
	var rest string
	for _, marker := range []string{"cannot be null:", "missing variables:", "missing variable:"} {
		if i := strings.Index(msg, marker); i >= 0 {
			rest = msg[i+len(marker):]
			break
		}
	}
	if rest == "" {
		return nil
	}
	var out []string
	for _, f := range strings.FieldsFunc(rest, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '"' || r == '\''
	}) {
		if isFlagName(f) {
			out = append(out, f)
		}
	}
	// A message naming dozens of flags is a message this code has misread.
	if len(out) > 32 {
		return nil
	}
	return out
}

// isFlagName is deliberately strict. Every flag X has ever named is snake_case,
// and the alternative to requiring the underscore is a message like "missing
// variable: see the docs" adding `see`, `the` and `docs` to the blob.
func isFlagName(s string) bool {
	if len(s) < 3 || len(s) > 96 || !strings.Contains(s, "_") {
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

// withFeatures folds the healed flags into a features blob.
func withFeatures(blob string, healed map[string]bool) string {
	if len(healed) == 0 {
		return blob
	}
	var m map[string]any
	if json.Unmarshal([]byte(blob), &m) != nil {
		return blob
	}
	for name := range healed {
		if _, taken := m[name]; !taken {
			m[name] = false
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return blob
	}
	return string(b)
}

// verbosef writes a note about the tool's own workings to stderr: a re-minted
// guest token, a feature flag X has added since the last release. Never data,
// which belongs on stdout in whatever shape -o asked for.
func (c Config) verbosef(format string, args ...any) {
	if c.Verbose > 0 {
		fmt.Fprintf(os.Stderr, "x: "+format+"\n", args...)
	}
}
