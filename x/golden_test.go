package x

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// golden_test.go is the golden suite from spec 3003 doc 06 section 5.2: every
// extractor parses its fixture and the whole record it produced is compared
// against a committed JSON file.
//
// The rest of the suite asserts the fields somebody thought to assert. This one
// asserts everything, which is a different job: it is how a change nobody
// predicted shows up. A parser that starts filling `place` from the wrong node,
// a decoder that quietly stops setting `lang`, an entity offset that shifts by
// one, none of those have a test written for them and all of them are a diff
// here.
//
// It matters more in this project than in most, because doc 01 is a page of
// claims about somebody else's website and every one of them will eventually
// stop being true. The golden is the thing that says which one, and when.
//
// `go test ./x/ -update` rewrites them. The workflow after `x capture` is:
// recapture, run -update, read the diff. Counters will have moved and that is
// not news; a field that went from a value to absent is.
func TestGoldens(t *testing.T) {
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			got := marshalGolden(t, c.parse(t))
			path := filepath.Join("testdata", "golden", c.name+".json")
			if *update {
				writeGolden(t, path, got)
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("no golden for %s: %v; run go test ./x/ -update", c.name, err)
			}
			if string(want) != got {
				t.Errorf("%s no longer parses to its golden.\n%s\nif the change is right, run go test ./x/ -update",
					c.name, firstDiff(string(want), got))
			}
		})
	}
}

// A golden that nothing writes is a test that passes by having no opinion, so
// the directory has to hold exactly the files the cases produce. A fixture
// dropped from the list without its golden being deleted leaves a file that
// looks like coverage and is not.
func TestEveryGoldenHasACase(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "golden"))
	if err != nil {
		t.Fatalf("no golden directory: %v; run go test ./x/ -update", err)
	}
	want := map[string]bool{}
	for _, c := range goldenCases() {
		want[c.name+".json"] = true
	}
	for _, e := range entries {
		if !want[e.Name()] {
			t.Errorf("testdata/golden/%s belongs to no case, so nothing compares against it", e.Name())
		}
	}
	if len(entries) != len(want) {
		t.Errorf("%d goldens for %d cases", len(entries), len(want))
	}
}

type goldenCase struct {
	name  string
	parse func(*testing.T) any
}

// goldenCases is every extractor against every fixture that exercises it. The
// records come out of the decoders directly rather than through the engine,
// because a golden that needed a request in front of it could not run offline.
func goldenCases() []goldenCase {
	return []goldenCase{
		{"s1_tweet_20", func(t *testing.T) any {
			return synTweetFromCapture(t, "s1_tweet_20.json.gz", "20")
		}},
		{"s1_reply_with_parent", func(t *testing.T) any {
			return synTweetFromCapture(t, "s1_reply_with_parent.json.gz", "1903142823316049977")
		}},
		{"s3_oembed_20", func(t *testing.T) any {
			return decodeGolden(t, "s3_oembed_20.json.gz")
		}},
		{"s3_oembed_media", func(t *testing.T) any {
			return decodeGolden(t, "s3_oembed_media.json.gz")
		}},
		{"s4_user_nasa", func(t *testing.T) any {
			return decodeS4User(t)
		}},
		{"s4_usertweets_nasa", func(t *testing.T) any {
			var out []*Tweet
			for _, o := range tweetResults([]byte(capture(t, "s4_usertweets_nasa.json.gz"))) {
				var r gqlTweetResult
				if err := json.Unmarshal(o, &r); err != nil {
					t.Fatalf("usertweets: %v", err)
				}
				if tw := r.build(); tw != nil {
					out = append(out, tw)
				}
			}
			return out
		}},
		{"s4_space_running", func(t *testing.T) any {
			return spaceGolden(t, "s4_space_1dRJZEpyjlNGB.json.gz", "1dRJZEpyjlNGB")
		}},
		{"s4_space_ended", func(t *testing.T) any {
			return spaceGolden(t, "s4_space_1MnxnMDeQLeJO.json.gz", "1MnxnMDeQLeJO")
		}},
		{"s5_trends_us", func(t *testing.T) any {
			ts, err := decodeTrends([]byte(capture(t, "s5_trends_us.json.gz")),
				23424977, trendsPlaceURL(23424977), 0)
			if err != nil {
				t.Fatalf("trends: %v", err)
			}
			return ts
		}},
		{"s5_places", func(t *testing.T) any {
			ps, err := decodePlaces([]byte(capture(t, "s5_places.json.gz")), "", "", "", 0)
			if err != nil {
				t.Fatalf("places: %v", err)
			}
			return ps
		}},
		{"s8_status_20", func(t *testing.T) any {
			return tweetPageGolden(t, "status_20.html.gz", "20")
		}},
		{"s8_status_media", func(t *testing.T) any {
			return tweetPageGolden(t, "status_media.html.gz", "2081860978694594863")
		}},
		{"s8_status_reply", func(t *testing.T) any {
			return tweetPageGolden(t, "status_reply.html.gz", "1903142823316049977")
		}},
		{"s8_profile_jack", func(t *testing.T) any {
			return userPageGolden(t, "profile_jack.html.gz", "jack")
		}},
		{"s8_profile_nasa", func(t *testing.T) any {
			return userPageGolden(t, "profile_nasa.html.gz", "nasa")
		}},
		{"s2_timeline_jack", func(t *testing.T) any {
			return widget(t, "s2_timeline_jack.html.gz")
		}},
		{"s2_timeline_nasa", func(t *testing.T) any {
			return widget(t, "s2_timeline_nasa.html.gz")
		}},
	}
}

func decodeGolden(t *testing.T, fixture string) *OEmbed {
	t.Helper()
	o, err := decodeOEmbed([]byte(capture(t, fixture)))
	if err != nil {
		t.Fatalf("%s: %v", fixture, err)
	}
	return o
}

func spaceGolden(t *testing.T, fixture, id string) *Space {
	t.Helper()
	s, err := parseSpace([]byte(capture(t, fixture)), id)
	if err != nil {
		t.Fatalf("%s: %v", fixture, err)
	}
	return s
}

// The surface-8 goldens are the records the page produced, not the Page. A Page
// still holds the Relay store it was parsed out of, and pinning that would
// commit a second, larger, unindented copy of a fixture that is already in the
// tree: a diff nobody can read, over bytes nobody extracted. What is worth
// pinning is what came out the other end.
func tweetPageGolden(t *testing.T, fixture, id string) any {
	t.Helper()
	p, err := ParsePage(StatusPageURL(id), capture(t, fixture))
	if err != nil {
		t.Fatalf("%s: %v", fixture, err)
	}
	tw, err := p.TweetFromPage(id)
	if err != nil {
		t.Fatalf("%s: %v", fixture, err)
	}
	// Postings too: the JSON-LD plane is a separate extractor over the same
	// bytes, and on a reply it is the only one that sees the parent.
	return struct {
		Tweet    *Tweet   `json:"tweet"`
		Postings []*Tweet `json:"postings,omitempty"`
	}{tw, p.Postings()}
}

func userPageGolden(t *testing.T, fixture, handle string) *User {
	t.Helper()
	p, err := ParsePage(UserURL(handle), capture(t, fixture))
	if err != nil {
		t.Fatalf("%s: %v", fixture, err)
	}
	u, err := p.UserFromPage(handle)
	if err != nil {
		t.Fatalf("%s: %v", fixture, err)
	}
	return u
}

// marshalGolden renders a record the way a caller sees it, which is what makes
// the file worth reading in a review: json tags, omitempty, field order, all of
// it. Indented, because a golden nobody can read is a golden nobody checks.
func marshalGolden(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b) + "\n"
}

func writeGolden(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// firstDiff reports the first line that moved, with a little context. A whole
// record printed twice is thousands of lines and tells a reader nothing; the
// line number and the pair is the thing they actually want.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		a, b := at(w, i), at(g, i)
		if a == b {
			continue
		}
		var b2 strings.Builder
		for j := max(0, i-2); j < i; j++ {
			b2.WriteString("  " + at(w, j) + "\n")
		}
		b2.WriteString("- " + a + "\n")
		b2.WriteString("+ " + b + "\n")
		return "line " + strconv.Itoa(i+1) + ":\n" + b2.String()
	}
	return "the files differ only in their trailing bytes"
}

func at(ss []string, i int) string {
	if i < len(ss) {
		return ss[i]
	}
	return ""
}
