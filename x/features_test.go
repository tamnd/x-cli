package x

import (
	"encoding/json"
	"testing"
)

// The two rejection shapes X has been seen to use. Both are real: the first is
// what a redeploy produces, the second is what a stale query id produces when
// the operation itself moved on.
func TestMissingFeaturesReadsBothRejectionShapes(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
		want []string
	}{
		{
			"cannot be null",
			`{"errors":[{"code":366,"message":"The following features cannot be null: rweb_video_screen_enabled, payments_enabled"}]}`,
			[]string{"rweb_video_screen_enabled", "payments_enabled"},
		},
		{
			"missing variable",
			`{"errors":[{"code":"GRAPHQL_VALIDATION_FAILED","message":"missing variable: xyz_enabled"}]}`,
			[]string{"xyz_enabled"},
		},
	} {
		got := missingFeatures(&HTTPError{Status: 400, Body: c.body})
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}

func TestMissingFeaturesIgnoresEverythingElse(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		body string
	}{
		{"a 404", &HTTPError{Status: 404, Body: `{"errors":[{"message":"missing variable: a_enabled"}]}`}, ""},
		{"a rate limit", &HTTPError{Status: 429}, ""},
		{"a message about something else", &HTTPError{Status: 400,
			Body: `{"errors":[{"message":"Rate limit exceeded"}]}`}, ""},
		{"a prose sentence past the marker", &HTTPError{Status: 400,
			Body: `{"errors":[{"message":"missing variable: see the docs at x.com"}]}`}, ""},
	} {
		if got := missingFeatures(c.err); len(got) != 0 {
			t.Errorf("%s healed %v, want nothing", c.name, got)
		}
	}
}

func TestHealAddsAFlagOnceAndSendsItAsFalse(t *testing.T) {
	g := &GraphQL{}
	if !g.heal([]string{"a_enabled", "b_enabled"}) {
		t.Fatal("the first rejection healed nothing")
	}
	// The same rejection twice is a real failure, not another round.
	if g.heal([]string{"a_enabled"}) {
		t.Error("the same flag healed twice")
	}
	if !g.heal([]string{"c_enabled"}) {
		t.Error("a new flag did not heal")
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(g.features()), &m); err != nil {
		t.Fatalf("the healed blob is not JSON: %v", err)
	}
	for _, n := range []string{"a_enabled", "b_enabled", "c_enabled"} {
		v, ok := m[n]
		if !ok {
			t.Errorf("%s missing from the blob", n)
		} else if v != false {
			t.Errorf("%s = %v, want false: a flag the tool has never heard of gates a feature it does not ask for", n, v)
		}
	}
	// Healing must not lose what X already told us to send.
	if m["view_counts_everywhere_api_enabled"] != true {
		t.Error("the healed blob dropped a shipped flag")
	}
}

func TestHealNeverOverwritesAShippedFlag(t *testing.T) {
	got := withFeatures(`{"a":true}`, map[string]bool{"a": true, "b": true})
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	if m["a"] != true {
		t.Errorf("a = %v, want the shipped true kept", m["a"])
	}
	if m["b"] != false {
		t.Errorf("b = %v, want false", m["b"])
	}
}
