package x

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite fields_gen.go from the fixtures")

// TestFieldSurfaces measures which surface fills which field by running every
// committed fixture through the decoder that would read it, and fails when
// fields_gen.go disagrees. `go test ./x/ -update` rewrites it.
//
// Measured rather than declared, because a hand-written table of which surface
// carries what is exactly the kind of claim doc 01 exists to stop trusting. The
// cost is that the table says nothing about a surface with no fixture, which is
// why the generated file names the fixtures it was built from.
func TestFieldSurfaces(t *testing.T) {
	got := measureSurfaces(t)
	if *update {
		if err := os.WriteFile("fields_gen.go", renderFieldsGen(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("wrote fields_gen.go")
		return
	}
	for _, kind := range FieldKinds {
		for name, want := range got[kind] {
			if have := fieldSurfaces[kind][name]; !sameInts(have, want) {
				t.Errorf("%s.%s is filled by %v, fields_gen.go says %v; run go test ./x/ -update",
					kind, name, want, have)
			}
		}
		for name, have := range fieldSurfaces[kind] {
			if _, ok := got[kind][name]; !ok {
				t.Errorf("%s.%s claims %v and no fixture fills it; run go test ./x/ -update",
					kind, name, have)
			}
		}
	}
}

// measureSurfaces decodes every fixture and records, per kind and field, the
// surfaces that produced a non-empty value.
func measureSurfaces(t *testing.T) map[string]map[string][]int {
	t.Helper()
	out := map[string]map[string][]int{KindTweet: {}, KindUser: {}}
	// Only census names, so the envelope stays out of the table. url and id are
	// filled by the fetch on every surface alike, and saying so would pad every
	// row with a fact about the tool rather than about X.
	census := map[string]map[string]bool{}
	for _, kind := range FieldKinds {
		census[kind] = map[string]bool{}
		for _, f := range Fields(kind) {
			census[kind][f.Name] = true
		}
	}
	note := func(kind string, n int, rec any) {
		if rec == nil {
			return
		}
		for _, f := range filled(rec) {
			if !census[kind][f] {
				continue
			}
			s := out[kind][f]
			if !containsInt(s, n) {
				out[kind][f] = insertInt(s, n)
			}
		}
	}
	noteTweet := func(n int, tw *Tweet) {
		if tw == nil {
			return
		}
		note(KindTweet, n, tw)
		note(KindUser, n, tw.Author)
		noteNested(out, n, tw.Quoted, tw.Retweeted, note)
	}

	// Surface 1, the syndication tweet.
	var st synTweet
	if err := json.Unmarshal([]byte(capture(t, "s1_tweet_20.json.gz")), &st); err != nil {
		t.Fatal(err)
	}
	noteTweet(1, st.toTweet())

	// Surface 2, the syndication profile timeline.
	raw, ok := extractNextData([]byte(capture(t, "s2_timeline_nasa.html.gz")))
	if !ok {
		t.Fatal("no __NEXT_DATA__ in the s2 fixture")
	}
	for _, tw := range timelineTweets(t, raw) {
		noteTweet(2, tw)
	}
	if u, ok := profileFromTimeline(raw, ""); ok {
		note(KindUser, 2, u)
	}

	// Surface 4, guest GraphQL.
	for _, o := range userResults([]byte(capture(t, "s4_user_nasa.json.gz"))) {
		var ur gqlUserResult
		if json.Unmarshal(o, &ur) == nil {
			note(KindUser, 4, ur.toUser())
		}
	}
	for _, o := range tweetResults([]byte(capture(t, "s4_usertweets_nasa.json.gz"))) {
		var r gqlTweetResult
		if json.Unmarshal(o, &r) == nil {
			noteTweet(4, r.build())
		}
	}

	// Surface 8, an x.com page. The status page carries the tweet, the profile
	// page the user, and both are read out of the same embedded data planes.
	if p, err := ParsePage(StatusPageURL("20"), capture(t, "status_20.html.gz")); err == nil {
		if tw, err := p.TweetFromPage("20"); err == nil {
			noteTweet(8, tw)
		}
	}
	if p, err := ParsePage("https://x.com/nasa", capture(t, "profile_nasa.html.gz")); err == nil {
		if u, err := p.UserFromPage("nasa"); err == nil {
			note(KindUser, 8, u)
		}
	}
	return out
}

// noteNested records a quoted or retweeted tweet, which is a whole record of
// the same kind and so fills the same fields.
func noteNested(out map[string]map[string][]int, n int, quoted, retweeted *Tweet, note func(string, int, any)) {
	for _, tw := range []*Tweet{quoted, retweeted} {
		if tw == nil {
			continue
		}
		note(KindTweet, n, tw)
		note(KindUser, n, tw.Author)
	}
}

func timelineTweets(t *testing.T, raw json.RawMessage) []*Tweet {
	t.Helper()
	var data struct {
		Props struct {
			PageProps struct {
				Timeline struct {
					Entries []struct {
						Content struct {
							Tweet *legacyTweet `json:"tweet"`
						} `json:"content"`
					} `json:"entries"`
				} `json:"timeline"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	var out []*Tweet
	for _, e := range data.Props.PageProps.Timeline.Entries {
		if e.Content.Tweet != nil && e.Content.Tweet.IDStr != "" {
			out = append(out, e.Content.Tweet.toTweet(nil, ""))
		}
	}
	if len(out) == 0 {
		t.Fatal("the s2 fixture decoded to no tweets")
	}
	return out
}

// filled is the census names a record actually carries a value for. It goes
// through JSON because every census field is omitempty or omitzero, so a key
// that survives marshaling is a key the decoder filled. Metrics is flattened to
// match censusFields and `via`.
func filled(rec any) []string {
	b, err := json.Marshal(rec)
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	var out []string
	for k, v := range m {
		if k == "metrics" {
			var inner map[string]json.RawMessage
			if json.Unmarshal(v, &inner) == nil {
				for ik := range inner {
					out = append(out, ik)
				}
			}
			continue
		}
		if isEmptyJSON(v) {
			continue
		}
		out = append(out, k)
	}
	return out
}

// isEmptyJSON catches what omitempty does not: an all-zero struct still
// marshals to {}, and counting that as filled would have every record claiming
// every surface fills its entities.
func isEmptyJSON(v json.RawMessage) bool {
	s := strings.TrimSpace(string(v))
	return s == "" || s == "{}" || s == "[]" || s == "null" || s == `""` || s == "0" || s == "false"
}

func renderFieldsGen(got map[string]map[string][]int) []byte {
	var b strings.Builder
	b.WriteString(`// Code generated by go test ./x/ -update. DO NOT EDIT.

package x

// fieldSurfaces is which surface has been observed to fill which field, measured
// by decoding the committed fixtures in testdata: the syndication tweet and
// profile timeline, the guest GraphQL profile and timeline, and the x.com status
// and profile pages.
//
// It is evidence, not a promise. A field with no surfaces is one no fixture has
// shown filled, which usually means the plane that carries it is not built yet,
// and a surface with no fixture (3, 5, 6, 7) never appears here at all. Surface
// 7 is surface 4 with a session, so it fills at least what 4 does.
var fieldSurfaces = map[string]map[string][]int{
`)
	for _, kind := range FieldKinds {
		fmt.Fprintf(&b, "\t%q: {\n", kind)
		var names []string
		for n := range got[kind] {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "\t\t%q: {%s},\n", n, joinInts(got[kind][n]))
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

func joinInts(ns []int) string {
	var parts []string
	for _, n := range ns {
		parts = append(parts, fmt.Sprint(n))
	}
	return strings.Join(parts, ", ")
}

func containsInt(s []int, n int) bool {
	for _, v := range s {
		if v == n {
			return true
		}
	}
	return false
}

func insertInt(s []int, n int) []int {
	s = append(s, n)
	sort.Ints(s)
	return s
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFieldsCensus checks the shape of the census rather than its contents: the
// contents are fields_gen.go, which has its own test above.
func TestFieldsCensus(t *testing.T) {
	if Fields("org") != nil {
		t.Error("Fields accepted a kind that has no record type")
	}
	for _, kind := range FieldKinds {
		fields := Fields(kind)
		if len(fields) == 0 {
			t.Fatalf("%s has no fields", kind)
		}
		seen := map[string]bool{}
		for _, f := range fields {
			if f.Name == "" || f.Type == "" {
				t.Errorf("%s has a row with no name or no type: %+v", kind, f)
			}
			if seen[f.Name] {
				t.Errorf("%s lists %s twice", kind, f.Name)
			}
			seen[f.Name] = true
		}
		// The envelope is filled by the fetch, not by a decoder, so it is not a
		// question about a surface and does not belong in the table.
		for _, envelope := range []string{"kind", "id", "uri", "url", "sources", "surfaces", "via"} {
			if seen[envelope] {
				t.Errorf("%s lists the envelope field %s", kind, envelope)
			}
		}
		// Metrics is expanded in place, so the flat names are there and the
		// struct itself is not.
		if seen["metrics"] {
			t.Errorf("%s lists metrics as a row instead of expanding it", kind)
		}
		if !seen["likes"] {
			t.Errorf("%s does not list likes, so metrics was not expanded", kind)
		}
	}
}

func TestFieldTier(t *testing.T) {
	for _, c := range []struct {
		surfaces  []int
		tier      int
		reachable bool
	}{
		{surfaces: nil, reachable: false},
		{surfaces: []int{1, 2, 8}, tier: 0, reachable: true},
		{surfaces: []int{4}, tier: 1, reachable: true},
		{surfaces: []int{7}, tier: 2, reachable: true},
		// Cheapest wins, whatever order they arrive in.
		{surfaces: []int{7, 4, 1}, tier: 0, reachable: true},
	} {
		tier, reachable := Field{Surfaces: c.surfaces}.Tier()
		if tier != c.tier || reachable != c.reachable {
			t.Errorf("Field{%v}.Tier() = %d, %v; want %d, %v",
				c.surfaces, tier, reachable, c.tier, c.reachable)
		}
	}
}

func TestSurfacesUpTo(t *testing.T) {
	all := []int{1, 2, 4, 7, 8}
	for tier, want := range map[int][]int{
		0: {1, 2, 8},
		1: {1, 2, 4, 8},
		2: {1, 2, 4, 7, 8},
	} {
		if got := SurfacesUpTo(all, tier); !sameInts(got, want) {
			t.Errorf("SurfacesUpTo(%v, %d) = %v, want %v", all, tier, got, want)
		}
	}
	if got := SurfacesUpTo(nil, 2); got != nil {
		t.Errorf("SurfacesUpTo(nil, 2) = %v, want nil", got)
	}
}
