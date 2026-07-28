package cli

import (
	"context"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/any-cli/kit/render"
	"github.com/tamnd/x-cli/x"
)

// cmd_edges.go prints the graph a record asserts (spec 3003 doc 04 section 2).
//
// It is one read and no walking. `x discover` follows links outward and costs a
// request per node; this fetches the references you named and prints what those
// records already say, which for a tweet read with no credential is five or six
// edges for one request. It is the command for seeing what a surface is worth
// before spending a budget on a crawl.

func newEdgesCmd() kit.Command {
	var conflicts bool
	return kit.Command{
		Use:   "edges <ref>...",
		Short: "Show the graph edges a record asserts",
		Long: "edges reads each reference and prints the claims that record makes about other " +
			"nodes: who wrote it, what it replies to, who it mentions, what it links to, what " +
			"media it carries. Every edge names the URL it was read from and the tier that " +
			"cost, so a graph built from mixed reads can be filtered by how much you trust it.\n\n" +
			"Pass --conflicts to print only the disagreements: two sources that cannot both be " +
			"right about a claim that admits one answer. Nothing is resolved for you.",
		Args: kit.MinimumNArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.BoolVar(&conflicts, "conflicts", false, "print only the claims two sources disagree about")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			out, err := a.out()
			if err != nil {
				return err
			}
			var all []x.Edge
			for _, ref := range args {
				kind, id, err := x.Classify(ref)
				if err != nil {
					return errs.Usage("%s", err.Error())
				}
				a.target = ref
				rec, err := a.record(kind, id)
				if err != nil {
					_ = out.Flush()
					return a.done(err)
				}
				all = append(all, x.Edges(rec)...)
			}
			all = x.MergeEdges(all)
			if conflicts {
				return emitConflicts(out, all)
			}
			for _, e := range all {
				if err := out.Emit(edgeRow(e)); err != nil {
					return err
				}
			}
			return out.Flush()
		},
	}
}

// emitConflicts prints one row per edge in each disagreement, with a marker on
// the one that wins on provenance. Both sides are printed, because a command
// whose whole purpose is to surface a contradiction cannot quietly pick a side.
func emitConflicts(out *render.Renderer, es []x.Edge) error {
	cs := x.Conflicts(es)
	if len(cs) == 0 {
		return out.Flush()
	}
	for _, c := range cs {
		win := c.Winner()
		for _, e := range c.Edges {
			r := edgeRow(e)
			mark := ""
			if e == win {
				mark = "wins"
			}
			r.Cols = append([]string{"verdict"}, r.Cols...)
			r.Vals = append([]string{mark}, r.Vals...)
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	return out.Flush()
}

// record fetches whatever a reference points at, as the record rather than as a
// rendered row. It reads the kinds that are one object; a hashtag or a search is
// a query rather than a node and has no edges of its own to assert.
func (a *App) record(kind, id string) (any, error) {
	e := a.engine()
	switch kind {
	case x.KindTweet, x.KindCard, x.KindNote, x.KindPoll:
		sp := a.progress("fetching tweet")
		t, err := e.Tweet(a.ctx(), id)
		sp.stop()
		return t, err
	case x.KindUser:
		sp := a.progress("fetching profile")
		u, err := e.User(a.ctx(), id, false)
		sp.stop()
		return u, err
	case x.KindSpace:
		sp := a.progress("fetching space")
		s, err := e.Space(a.ctx(), id)
		sp.stop()
		return s, err
	}
	return nil, errs.Unsupported("a %s asserts no edges of its own, so there is nothing to read", kind)
}

// edgeRow renders one edge. The triple leads and the provenance follows, because
// the triple is what a reader came for and the provenance is what they check
// when they doubt it.
func edgeRow(e x.Edge) Row {
	return Row{
		Cols: []string{"from", "predicate", "to", "tier", "surface", "source"},
		Vals: []string{
			e.From, string(e.Predicate), e.To,
			itoa(e.Tier), x.SurfaceCode(e.Surface), e.Source,
		},
		Value: e,
	}
}
