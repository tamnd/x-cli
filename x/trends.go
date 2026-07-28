package x

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/any-cli/kit/errs"
)

// trends.go is surface 5, the two routes left over from the REST v1.1 API
// (spec 3003 doc 01 section 5).
//
//	GET https://api.x.com/1.1/trends/place.json?id={woeid}
//	GET https://api.x.com/1.1/trends/available.json
//
// Everything else under /1.1/ that was once public now needs a session or is
// gone: statuses/show, users/show, search/tweets and statuses/user_timeline all
// answer 401 or 404. Those are recorded in the doc so nobody re-discovers them.
//
// The one correction the live probe forced is about what these cost. The spec
// filed them under Tier 1 and said 15 requests per fifteen minutes, which is
// what X reports when the request carries a guest token. Sent with the public
// web bearer alone, the same two routes answer with a much larger budget:
//
//	trends/place.json      180 / 15 min   (15 with a guest token)
//	trends/available.json   75 / 15 min
//
// Measured side by side against the same IP, alternating requests: two separate
// buckets with separate resets, and the guest one is twelve times smaller. So
// attaching a guest token here would be paying a credential to get less. x sends
// the bearer and nothing else, which also moves trends and places down to Tier
// 0, since the bearer is the public constant the browser ships rather than
// anyone's credential.

// trendsPlaceURL is the surface 5 request for one place's trends.
func trendsPlaceURL(woeid int64) string {
	return "https://api.x.com/1.1/trends/place.json?id=" + strconv.FormatInt(woeid, 10)
}

// trendsAvailableURL is the surface 5 request for the place directory.
const trendsAvailableURL = "https://api.x.com/1.1/trends/available.json"

// WorldwideWOEID is the whole world, and the default when nobody names a place.
// It is a Yahoo! Where On Earth ID, an identifier scheme whose owner shut down
// in 2019; X kept the numbers.
const WorldwideWOEID = 1

// trendsResp is the wire shape of trends/place.json. X wraps the answer in a
// one-element array, which is a REST-era habit rather than a promise of more.
type trendsResp struct {
	Trends []wireTrend `json:"trends"`
	AsOf   string      `json:"as_of"`

	// CreatedAt is named so the unread-keys test has something to match, and is
	// not carried onto the record. X sent as_of 2026-07-28T10:18:53Z with
	// created_at 2026-07-27T03:46:04Z on the same answer, a day and a half
	// apart, and nothing about the payload says what the older one is the
	// creation of. A field on a record has to mean something.
	CreatedAt string `json:"created_at"`

	Locations []wireLocation `json:"locations"`
}

type wireTrend struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// PromotedContent is an object when X sold the slot and null otherwise. Only
	// its presence is read, because what is in it is the advertiser's business.
	PromotedContent json.RawMessage `json:"promoted_content"`
	Query           string          `json:"query"`
	TweetVolume     *int            `json:"tweet_volume"`
}

type wireLocation struct {
	Name  string `json:"name"`
	WOEID int64  `json:"woeid"`
}

// wirePlace is one entry of trends/available.json.
type wirePlace struct {
	Name      string        `json:"name"`
	PlaceType wirePlaceType `json:"placeType"`

	// URL is http://where.yahooapis.com/v1/place/{woeid}, and it is not carried
	// onto the record. Yahoo! shut that service down in 2019, so it is an
	// address that resolves to nothing, and a record that published it would be
	// handing the caller a dead link with a straight face.
	URL string `json:"url"`

	ParentID    int64   `json:"parentid"`
	Country     string  `json:"country"`
	WOEID       int64   `json:"woeid"`
	CountryCode *string `json:"countryCode"`
}

type wirePlaceType struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

// Trends reads what is trending in one place. limit <= 0 means all of them.
func Trends(ctx context.Context, c *Client, woeid int64, limit int) ([]*Trend, error) {
	u := trendsPlaceURL(woeid)
	b, err := c.Do(ctx, Req{
		URL:      u,
		Endpoint: "v11.trends.place",
		Header:   appHeaders(),
		CacheTTL: ttlTrends,
	})
	if err != nil {
		// X answers an unknown woeid with the same 404 and the same "that page
		// does not exist" it gives a route that was never there, so the id is
		// what the message has to name.
		if nf := asNotFound(err, "place", strconv.FormatInt(woeid, 10)); nf != nil {
			return nil, nf
		}
		return nil, err
	}
	return decodeTrends(b, woeid, u, limit)
}

func decodeTrends(b []byte, woeid int64, src string, limit int) ([]*Trend, error) {
	var resp []trendsResp
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("parse trends response: %w", err)
	}
	if len(resp) == 0 {
		return nil, fmt.Errorf("trends answered with no place at all")
	}
	r := resp[0]
	place, at := r.Name(woeid), parseTrendTime(r.AsOf)
	out := make([]*Trend, 0, len(r.Trends))
	for i, w := range r.Trends {
		if limit > 0 && len(out) == limit {
			break
		}
		t := &Trend{
			Name:      w.Name,
			Query:     w.Query,
			SearchURL: w.URL,
			// Rank is not in the payload. It is the position in the array, which
			// is the ranking, and it is the only part of a trend that survives
			// being stored: a name with no rank says a thing was trending
			// somewhere in a list of fifty.
			Rank:      i + 1,
			WOEID:     r.WOEID(woeid),
			PlaceName: place,
			AsOf:      at,
			Volume:    w.TweetVolume,
			Promoted:  isPromoted(w.PromotedContent),
		}
		t.Identify(KindTrend, TrendID(t.WOEID, t.Name))
		t.Stamp(5, src)
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("nothing is trending in %s, X says", place)
	}
	return out, nil
}

// parseTrendTime reads the as_of stamp. Surface 5 sends RFC 3339 here, unlike
// every other v1.1 timestamp, which used the "Mon Jan 02 ..." shape. A stamp
// that will not parse comes back zero rather than as an error: the trends are
// the answer, and losing all fifty of them over a changed date format would be
// the wrong trade.
func parseTrendTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Name is the place X named in the answer, falling back to the woeid asked for
// when X did not say. It matters because a trend's place is half of what the
// trend means, and the request only carries a number.
func (r trendsResp) Name(woeid int64) string {
	if len(r.Locations) > 0 && r.Locations[0].Name != "" {
		return r.Locations[0].Name
	}
	return "woeid " + strconv.FormatInt(woeid, 10)
}

// WOEID is the place X answered for, which is the one asked for on every capture
// so far. Reading it back rather than echoing the request is what would catch
// the day X redirects a town to its country.
func (r trendsResp) WOEID(asked int64) int64 {
	if len(r.Locations) > 0 && r.Locations[0].WOEID != 0 {
		return r.Locations[0].WOEID
	}
	return asked
}

// isPromoted reports whether X sold this slot. promoted_content is null on every
// trend of every capture taken, which is what an unauthenticated read of a
// public endpoint would be expected to see.
func isPromoted(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null"
}

// TrendID is a trend's id: the woeid, then the name lowercased. The same name
// trending in two places is two different facts, and an id that was just the
// name would fold them into one node.
func TrendID(woeid int64, name string) string {
	return strconv.FormatInt(woeid, 10) + "/" + strings.ToLower(name)
}

// Places reads the woeid directory, which is every place X will accept an id
// for. It is about 88 KB, it changes on the order of months, and x caches it for
// a week rather than shipping a copy that would start rotting on release day.
//
// query filters by name or country, case-insensitively. country and placeType
// narrow further; either may be empty.
func Places(ctx context.Context, c *Client, query, country, placeType string, limit int) ([]*Place, error) {
	b, err := c.Do(ctx, Req{
		URL:      trendsAvailableURL,
		Endpoint: "v11.trends.available",
		Header:   appHeaders(),
		CacheTTL: ttlPlaces,
	})
	if err != nil {
		return nil, err
	}
	return decodePlaces(b, query, country, placeType, limit)
}

func decodePlaces(b []byte, query, country, placeType string, limit int) ([]*Place, error) {
	var wire []wirePlace
	if err := json.Unmarshal(b, &wire); err != nil {
		return nil, fmt.Errorf("parse place directory: %w", err)
	}
	out := make([]*Place, 0, len(wire))
	for _, w := range wire {
		if !matchPlace(w, query, country, placeType) {
			continue
		}
		p := &Place{
			Name:          w.Name,
			WOEID:         w.WOEID,
			Country:       w.Country,
			PlaceType:     w.PlaceType.Name,
			PlaceTypeCode: w.PlaceType.Code,
			ParentID:      w.ParentID,
		}
		if w.CountryCode != nil {
			p.CountryCode = *w.CountryCode
		}
		p.Identify(KindPlace, strconv.FormatInt(w.WOEID, 10))
		p.Stamp(5, trendsAvailableURL)
		out = append(out, p)
	}
	// X returns the directory in no order anyone can rely on, with Worldwide
	// first and the rest by woeid, which is a Yahoo! allocation order. Sorting
	// by country then name is the order a person reading a list of places
	// expects, and Worldwide belongs at the top of it because it is the default.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.WOEID == WorldwideWOEID) != (b.WOEID == WorldwideWOEID) {
			return a.WOEID == WorldwideWOEID
		}
		if a.Country != b.Country {
			return a.Country < b.Country
		}
		return a.Name < b.Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// matchPlace is the filter. query matches a name or a country, so `x places
// japan` finds both the country and Osaka; country and type are exact against
// either the code or the name, so --country US and --country "United States"
// both work and neither needs the user to know which X publishes.
func matchPlace(w wirePlace, query, country, placeType string) bool {
	if query != "" {
		q := strings.ToLower(query)
		if !strings.Contains(strings.ToLower(w.Name), q) &&
			!strings.Contains(strings.ToLower(w.Country), q) {
			return false
		}
	}
	if country != "" {
		cc := ""
		if w.CountryCode != nil {
			cc = *w.CountryCode
		}
		if !strings.EqualFold(cc, country) && !strings.EqualFold(w.Country, country) {
			return false
		}
	}
	if placeType != "" && !strings.EqualFold(w.PlaceType.Name, placeType) {
		return false
	}
	return true
}

// appHeaders is the whole credential for surface 5: the public web bearer, the
// same constant every browser sends and the same one ensureGuest uses to mint a
// guest token. No guest token goes with it, deliberately, because attaching one
// cuts the budget from 180 requests per fifteen minutes to 15.
//
// Without it X answers 400 "Bad Authentication data", so it is not optional; it
// is just not a credential belonging to anybody.
func appHeaders() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+bearerForHeader())
	h.Set("Accept", "*/*")
	return h
}

// ResolveWOEID takes the number, and also takes a place name, because
// `x trends tokyo` is what somebody who has just read `x places` will type and
// making them paste the digits back is a small unkindness. The name goes through
// the directory, which x caches for a week, so it costs one request ever.
//
// An ambiguous name is a usage error naming the candidates rather than a pick.
// "springfield" is eight towns and choosing one for the user would be the tool
// answering a question it was not asked.
//
// It lives on the engine rather than in the command because the HTTP and MCP
// surfaces take the same argument, and a place name that resolves on the command
// line and not over MCP would be the same tool disagreeing with itself.
func (e *Engine) ResolveWOEID(ctx context.Context, s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return WorldwideWOEID, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n <= 0 {
			return 0, errs.Usage("a woeid is a positive number, and %q is not one; run `x places` to find one", s)
		}
		return n, nil
	}
	places, err := e.Places(ctx, s, "", "", 0)
	if err != nil {
		return 0, err
	}
	// An exact name beats a substring, so "york" is York and not New York, and
	// a country beats a town of the same name.
	var exact []*Place
	for _, p := range places {
		if strings.EqualFold(p.Name, s) {
			exact = append(exact, p)
		}
	}
	if len(exact) > 0 {
		places = exact
	}
	switch {
	case len(places) == 0:
		return 0, errs.Usage("X has no trends for %q; run `x places` to see the ones it has", s)
	case len(places) == 1:
		return places[0].WOEID, nil
	}
	return 0, errs.Usage("%q is %d places (%s); pass the woeid you meant, or run `x places %s`",
		s, len(places), placeChoices(places), s)
}

// placeChoices renders the candidates for the ambiguity message, capped, because
// a query matching forty places produces a message nobody reads to the end.
func placeChoices(places []*Place) string {
	const max = 4
	var parts []string
	for _, p := range places[:min(max, len(places))] {
		label := p.Name
		if p.Country != "" && !strings.EqualFold(p.Country, p.Name) {
			label += ", " + p.Country
		}
		parts = append(parts, label+" "+strconv.FormatInt(p.WOEID, 10))
	}
	if len(places) > max {
		parts = append(parts, "and "+strconv.Itoa(len(places)-max)+" more")
	}
	return strings.Join(parts, "; ")
}
