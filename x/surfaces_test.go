package x

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The tables are documentation the binary prints, so the thing worth testing is
// that they stay internally honest: every surface a route names exists, every
// route is served somewhere, and a tier never claims a surface it cannot reach.

var surfaceRef = regexp.MustCompile(`surface (\d+)`)

func TestRoutesOnlyNameSurfacesThatExist(t *testing.T) {
	byN := map[int]Surface{}
	for _, s := range Surfaces {
		byN[s.N] = s
	}
	for _, r := range Routes {
		for tier, cell := range []string{r.Tier0, r.Tier1, r.Tier2} {
			for _, m := range surfaceRef.FindAllStringSubmatch(cell, -1) {
				n, _ := strconv.Atoi(m[1])
				s, ok := byN[n]
				if !ok {
					t.Errorf("%q at tier %d names surface %d, which is not in the table", r.Question, tier, n)
					continue
				}
				if s.Tier > tier {
					t.Errorf("%q claims surface %d at tier %d, but that surface needs tier %d",
						r.Question, n, tier, s.Tier)
				}
			}
		}
	}
}

// A row that answers nowhere is a row that should not be in the table. That is
// what exit code 7 is for, and it is decided by the command, not by a blank line
// here.
func TestEveryRouteIsServedSomewhere(t *testing.T) {
	for _, r := range Routes {
		if r.Tier0 == "" && r.Tier1 == "" && r.Tier2 == "" {
			t.Errorf("%q is served at no tier at all", r.Question)
		}
	}
}

// Tiers do not go backwards. If tier 0 answers a question, tier 1 and 2 answer
// it too, because they can always fall back down. The client depends on this:
// it resolves to the best tier available and falls back, never up.
func TestATierNeverLosesWhatALowerTierHas(t *testing.T) {
	for _, r := range Routes {
		if r.Tier0 != "" && r.Tier1 == "" {
			t.Errorf("%q: tier 0 answers but tier 1 does not", r.Question)
		}
		if r.Tier1 != "" && r.Tier2 == "" {
			t.Errorf("%q: tier 1 answers but tier 2 does not", r.Question)
		}
	}
}

func TestLowestTierMatchesTheTable(t *testing.T) {
	for _, r := range Routes {
		want := 2
		switch {
		case r.Tier0 != "":
			want = 0
		case r.Tier1 != "":
			want = 1
		}
		got, ok := LowestTier(r.Question)
		if !ok || got != want {
			t.Errorf("LowestTier(%q) = %d, %v; want %d, true", r.Question, got, ok, want)
		}
	}
	if _, ok := LowestTier("send a direct message"); ok {
		t.Error("LowestTier answered for a question the table does not have")
	}
}

func TestSurfacesAreNumberedTheWayTheDocsNumberThem(t *testing.T) {
	for i, s := range Surfaces {
		if s.N != i+1 {
			t.Errorf("surface %d is numbered %d, and the docs number them 1 through 8", i+1, s.N)
		}
		if s.Tier < 0 || s.Tier > 2 {
			t.Errorf("surface %d sits at tier %d, and there are only three tiers", s.N, s.Tier)
		}
		if s.Host == "" || s.Name == "" || s.CacheTTL == "" {
			t.Errorf("surface %d has an empty column: %+v", s.N, s)
		}
		// A limit with no window is half a fact, and the reverse is worse.
		if (s.Window == "") != strings.Contains(s.Limit, "none") {
			t.Errorf("surface %d has limit %q and window %q, which do not agree", s.N, s.Limit, s.Window)
		}
	}
	if len(Tiers) != 3 {
		t.Errorf("there are %d tiers in the table, want 3", len(Tiers))
	}
}
