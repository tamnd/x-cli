package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/x-cli/x"
)

// cmd_trends.go is `x trends` and `x places`, the two commands over surface 5
// (spec 3003 doc 05).
//
// They are a pair because neither is much use alone: trends takes a woeid and
// nobody knows a woeid, so places is how you find one.

func trendCommands() []kit.Command {
	return []kit.Command{newTrendsCmd(), newPlacesCmd()}
}

func newTrendsCmd() kit.Command {
	return kit.Command{
		Use:   "trends [woeid]",
		Short: "What is trending in a place (default worldwide)",
		Args:  kit.MaximumNArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			woeid := int64(x.WorldwideWOEID)
			if len(args) == 1 {
				n, err := a.resolveWOEID(args[0])
				if err != nil {
					return err
				}
				woeid = n
			}
			a.target = strconv.FormatInt(woeid, 10)
			sp := a.progress("fetching trends")
			trends, err := a.engine().Trends(a.ctx(), woeid, a.limit)
			sp.stop()
			if err != nil {
				return a.done(err)
			}
			out, err := a.out()
			if err != nil {
				return a.done(err)
			}
			for _, t := range trends {
				if err := out.Emit(trendRow(t)); err != nil {
					return a.done(err)
				}
			}
			return a.done(out.Flush())
		},
	}
}

// resolveWOEID takes the number, and also takes a place name, because
// `x trends tokyo` is what somebody who has just read `x places` will type and
// making them paste the digits back is a small unkindness. The name goes through
// the directory, which x caches for a week, so it costs one request ever.
//
// An ambiguous name is a usage error naming the candidates rather than a pick.
// "springfield" is eight towns and choosing one for the user would be the tool
// answering a question it was not asked.
func (a *App) resolveWOEID(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n <= 0 {
			return 0, errs.Usage("a woeid is a positive number, and %q is not one; run `x places` to find one", s)
		}
		return n, nil
	}
	places, err := a.engine().Places(a.ctx(), s, "", "", 0)
	if err != nil {
		return 0, err
	}
	// An exact name beats a substring, so "york" is York and not New York, and
	// a country beats a town of the same name.
	var exact []*x.Place
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
func placeChoices(places []*x.Place) string {
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

func newPlacesCmd() kit.Command {
	var country, placeType string
	return kit.Command{
		Use:     "places [query]",
		Aliases: []string{"woeids"},
		Short:   "The places X has trends for, and their woeids",
		Args:    kit.MaximumNArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.StringVar(&country, "country", "", "only this country, by name or two-letter code")
			f.StringVar(&placeType, "type", "", "only this kind of place: town, country, supername")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			a.target = query
			sp := a.progress("fetching the place directory")
			places, err := a.engine().Places(a.ctx(), query, country, placeType, a.limit)
			sp.stop()
			if err != nil {
				return a.done(err)
			}
			if len(places) == 0 {
				// Nothing matched is not a failure of the read, and it is the one
				// case where saying so is more useful than an empty table: the
				// directory is 467 places and a typo is the likely reason.
				return a.done(errNoResults)
			}
			out, err := a.out()
			if err != nil {
				return a.done(err)
			}
			for _, p := range places {
				if err := out.Emit(placeRow(p)); err != nil {
					return a.done(err)
				}
			}
			return a.done(out.Flush())
		},
	}
}
