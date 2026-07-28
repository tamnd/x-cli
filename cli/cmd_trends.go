package cli

import (
	"context"
	"strconv"

	"github.com/tamnd/any-cli/kit"
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

// resolveWOEID is the engine's, so a place name resolves the same way here as it
// does over HTTP and MCP.
func (a *App) resolveWOEID(s string) (int64, error) {
	return a.engine().ResolveWOEID(a.ctx(), s)
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
