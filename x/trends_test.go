package x

import (
	"strings"
	"testing"
	"time"
)

// trends_test.go covers surface 5 against the two captures taken live on
// 2026-07-28: the United States trend list and the whole woeid directory.

func TestTrendsCarryTheirPlaceAndTheirRank(t *testing.T) {
	src := trendsPlaceURL(23424977)
	got, err := decodeTrends([]byte(capture(t, "s5_trends_us.json.gz")), 23424977, src, 0)
	if err != nil {
		t.Fatalf("decodeTrends: %v", err)
	}
	if len(got) != 47 {
		t.Fatalf("got %d trends, want the 47 in the capture", len(got))
	}
	for i, tr := range got {
		if tr.Rank != i+1 {
			t.Errorf("trend %d has rank %d, want %d: the rank is the position and nothing else carries it", i, tr.Rank, i+1)
		}
		if tr.WOEID != 23424977 || tr.PlaceName != "United States" {
			t.Errorf("trend %q says it is from %d/%q, want 23424977/United States", tr.Name, tr.WOEID, tr.PlaceName)
		}
		if tr.Name == "" {
			t.Errorf("trend %d came back with no name", i)
		}
		if tr.Kind != KindTrend {
			t.Errorf("trend %q is kind %q, want %q", tr.Name, tr.Kind, KindTrend)
		}
		if len(tr.Surfaces) != 1 || tr.Surfaces[0] != "s5" {
			t.Errorf("trend %q says it came from %v, want [s5]", tr.Name, tr.Surfaces)
		}
		if tr.Tier != 0 {
			t.Errorf("trend %q says tier %d; surface 5 answers on the public bearer alone", tr.Name, tr.Tier)
		}
	}

	// as_of is when X computed the list, and the capture says so to the second.
	want := time.Date(2026, 7, 28, 10, 18, 53, 0, time.UTC)
	if !got[0].AsOf.Equal(want) {
		t.Errorf("as_of is %v, want %v", got[0].AsOf, want)
	}

	// The id has to carry the place. #Trending in Tokyo and #Trending in London
	// are two facts, and an id that was just the name would fold them into one.
	first := got[0]
	if first.ID != TrendID(23424977, first.Name) || !strings.HasPrefix(first.ID, "23424977/") {
		t.Errorf("trend id is %q, want it prefixed with its woeid", first.ID)
	}
	if TrendID(1, "Andromeda") == TrendID(23424977, "Andromeda") {
		t.Error("the same trend in two places got one id")
	}
}

// Every trend in every capture taken came back with tweet_volume null: 294 of
// them, over six places, on the day this was written. So the field has to be
// absent rather than zero, and this is the test that says the pointer is doing
// its job rather than being decoration.
func TestATrendWithNoVolumeSaysNothingRatherThanZero(t *testing.T) {
	got, err := decodeTrends([]byte(capture(t, "s5_trends_us.json.gz")), 23424977, "", 0)
	if err != nil {
		t.Fatalf("decodeTrends: %v", err)
	}
	for _, tr := range got {
		if tr.Volume != nil {
			// Not a failure. If X starts publishing volumes again this test
			// should say so out loud rather than turn red.
			t.Logf("X published a volume for %q: %d", tr.Name, *tr.Volume)
		}
	}

	// And the decoder does read one when it is there, which is the half a
	// capture of all-nulls cannot prove.
	live := []byte(`[{"trends":[{"name":"a","tweet_volume":48210},{"name":"b","tweet_volume":null}],
	  "as_of":"2026-07-28T10:18:53Z","locations":[{"name":"Nowhere","woeid":7}]}]`)
	got, err = decodeTrends(live, 7, "", 0)
	if err != nil {
		t.Fatalf("decodeTrends: %v", err)
	}
	if got[0].Volume == nil || *got[0].Volume != 48210 {
		t.Errorf("a published volume came back as %v, want 48210", got[0].Volume)
	}
	if got[1].Volume != nil {
		t.Errorf("a null volume came back as %d, want absent", *got[1].Volume)
	}
}

func TestTheTrendLimitCutsTheListAndKeepsTheOrder(t *testing.T) {
	got, err := decodeTrends([]byte(capture(t, "s5_trends_us.json.gz")), 23424977, "", 5)
	if err != nil {
		t.Fatalf("decodeTrends: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d trends, want 5", len(got))
	}
	if got[0].Rank != 1 || got[4].Rank != 5 {
		t.Errorf("a truncated list has ranks %d..%d, want 1..5", got[0].Rank, got[4].Rank)
	}
}

// The place a trend is from comes out of the answer, not out of the request, so
// the day X redirects a town to its country the record says which one it got.
func TestTheAnsweringPlaceWinsOverTheRequestedOne(t *testing.T) {
	raw := []byte(`[{"trends":[{"name":"a"}],"locations":[{"name":"Japan","woeid":23424856}]}]`)
	got, err := decodeTrends(raw, 1118370, "", 0)
	if err != nil {
		t.Fatalf("decodeTrends: %v", err)
	}
	if got[0].WOEID != 23424856 || got[0].PlaceName != "Japan" {
		t.Errorf("got %d/%q, want the place X answered for", got[0].WOEID, got[0].PlaceName)
	}
	// And with no locations at all the request is all there is to go on.
	got, err = decodeTrends([]byte(`[{"trends":[{"name":"a"}]}]`), 42, "", 0)
	if err != nil {
		t.Fatalf("decodeTrends: %v", err)
	}
	if got[0].WOEID != 42 || got[0].PlaceName != "woeid 42" {
		t.Errorf("got %d/%q, want the woeid asked for", got[0].WOEID, got[0].PlaceName)
	}
}

func TestPromotedIsPresenceNotShape(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want bool
	}{
		{`null`, false},
		{``, false},
		{`{"advertiser_id":"1"}`, true},
	} {
		if got := isPromoted([]byte(c.raw)); got != c.want {
			t.Errorf("isPromoted(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

func TestThePlaceDirectoryDecodesWhole(t *testing.T) {
	got, err := decodePlaces([]byte(capture(t, "s5_places.json.gz")), "", "", "", 0)
	if err != nil {
		t.Fatalf("decodePlaces: %v", err)
	}
	if len(got) != 467 {
		t.Fatalf("got %d places, want the 467 in the capture", len(got))
	}
	// Worldwide is the default woeid, so it goes first whatever the sort would
	// otherwise do with a place that has no country.
	if got[0].WOEID != WorldwideWOEID || got[0].Name != "Worldwide" {
		t.Errorf("the list starts with %d/%q, want Worldwide", got[0].WOEID, got[0].Name)
	}
	if got[0].ParentID != 0 {
		t.Errorf("Worldwide has parent %d, want 0: it is the top", got[0].ParentID)
	}
	for _, p := range got {
		if p.WOEID == 0 || p.Name == "" {
			t.Errorf("place %+v came back without an id or a name", p)
		}
		if p.ID != itoa64(p.WOEID) {
			t.Errorf("place %q has id %q, want its woeid", p.Name, p.ID)
		}
		if p.Kind != KindPlace || p.Tier != 0 {
			t.Errorf("place %q is kind %q at tier %d, want place at tier 0", p.Name, p.Kind, p.Tier)
		}
		// A place is a geotag, not a page, so there is no URL to state and
		// stating one would be an address that 404s.
		if p.URL != "" {
			t.Errorf("place %q claims the URL %q", p.Name, p.URL)
		}
	}
}

// Two different place types both come back named "Unknown", with codes 9 and 22.
// The code is the only thing that tells them apart, which is why it is modeled
// alongside the name rather than instead of it.
func TestTwoPlaceTypesShareTheNameUnknown(t *testing.T) {
	got, err := decodePlaces([]byte(capture(t, "s5_places.json.gz")), "", "", "unknown", 0)
	if err != nil {
		t.Fatalf("decodePlaces: %v", err)
	}
	codes := map[int]bool{}
	for _, p := range got {
		codes[p.PlaceTypeCode] = true
	}
	if len(codes) < 2 {
		t.Errorf("the Unknown places have %d distinct codes, want at least 2", len(codes))
	}
}

func TestThePlaceFilters(t *testing.T) {
	raw := []byte(capture(t, "s5_places.json.gz"))
	for _, c := range []struct {
		name                 string
		query, country, kind string
		wantSome             bool
		wantEvery            func(*Place) bool
	}{
		{name: "a country name matches its towns too", query: "japan", wantSome: true,
			wantEvery: func(p *Place) bool {
				return strings.EqualFold(p.Country, "Japan") || strings.EqualFold(p.Name, "Japan")
			}},
		{name: "the two-letter code works", country: "US", wantSome: true,
			wantEvery: func(p *Place) bool { return p.CountryCode == "US" }},
		{name: "and so does the country name", country: "United States", wantSome: true,
			wantEvery: func(p *Place) bool { return p.CountryCode == "US" }},
		{name: "type narrows to one kind", kind: "country", wantSome: true,
			wantEvery: func(p *Place) bool { return p.PlaceType == "Country" }},
		{name: "the filters compose", query: "york", country: "US", wantSome: true,
			wantEvery: func(p *Place) bool {
				return p.CountryCode == "US" && strings.Contains(strings.ToLower(p.Name), "york")
			}},
		{name: "and a miss is a miss", query: "atlantis"},
	} {
		got, err := decodePlaces(raw, c.query, c.country, c.kind, 0)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if c.wantSome && len(got) == 0 {
			t.Errorf("%s: matched nothing", c.name)
		}
		if !c.wantSome && len(got) > 0 {
			t.Errorf("%s: matched %d places, want none", c.name, len(got))
		}
		for _, p := range got {
			if c.wantEvery != nil && !c.wantEvery(p) {
				t.Errorf("%s: %q (%s) does not belong in the result", c.name, p.Name, p.Country)
			}
		}
	}
}

// The directory is a list of places to read, so it comes out sorted by country
// then name. X returns it in Yahoo!'s woeid allocation order, which is an
// artifact of 2008.
func TestThePlaceDirectoryIsSorted(t *testing.T) {
	got, err := decodePlaces([]byte(capture(t, "s5_places.json.gz")), "", "", "", 0)
	if err != nil {
		t.Fatalf("decodePlaces: %v", err)
	}
	for i := 2; i < len(got); i++ {
		a, b := got[i-1], got[i]
		if a.Country > b.Country || (a.Country == b.Country && a.Name > b.Name) {
			t.Fatalf("%q/%q comes before %q/%q", a.Country, a.Name, b.Country, b.Name)
		}
	}
}

func TestSurfaceFiveIsTierZero(t *testing.T) {
	for _, s := range Surfaces {
		if s.N != 5 {
			continue
		}
		if s.Tier != 0 {
			t.Errorf("surface 5 is filed at tier %d; it answers on the public web bearer, and a guest token only shrinks its budget from 180 to 15", s.Tier)
		}
	}
	// The header set is the whole claim: a bearer, and no guest token.
	h := appHeaders()
	if !strings.HasPrefix(h.Get("Authorization"), "Bearer ") {
		t.Errorf("surface 5 sends Authorization %q, want the public bearer", h.Get("Authorization"))
	}
	if h.Get("x-guest-token") != "" {
		t.Error("surface 5 attached a guest token, which is what cuts the budget by twelve times")
	}
	for _, q := range []string{"trends", "places"} {
		if tier, ok := LowestTier(q); !ok || tier != 0 {
			t.Errorf("%q resolves to tier %d (ok=%v), want tier 0", q, tier, ok)
		}
	}
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
