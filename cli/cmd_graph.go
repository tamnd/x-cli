package cli

import (
	"context"
	"sort"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/x-cli/x"
)

// cmd_graph.go prints a whole graph as one value (spec 3003 doc 05, `x graph`).
//
// `x get` prints a record and `x edges` prints claims, and a caller who wants
// both runs two commands and joins them by hand on the URI. This is the join
// done once: every claim the reads assert and every node those claims address,
// in a single document, which is the shape a graph tool wants to be handed.
//
// It walks nothing. The nodes it can show a record for are the ones the reads
// happened to carry, which for a tweet is the tweet, its author and anything it
// quoted; the rest are addressed and unread, which the document says plainly by
// leaving their record empty rather than by omitting them.

func newGraphCmd() kit.Command {
	return kit.Command{
		Use:   "graph <ref>...",
		Short: "Nodes and edges together, as one document",
		Long: "graph reads each reference and prints one document holding both halves of the " +
			"graph: the claims those records make, and every node the claims address. Nodes " +
			"the read carried whole come with their record; nodes that were only named come " +
			"with just an address, because a mention is a claim about an account nobody has " +
			"fetched.\n\n" +
			"The document is the point, so `-o json` is the format it is for. On a terminal " +
			"you get its shape summarized; use `x edges` to read the claims one per line and " +
			"`x get` to read a record.",
		Args: kit.MinimumNArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			out, err := a.out()
			if err != nil {
				return err
			}
			var recs []any
			var subjects []string
			for _, ref := range args {
				kind, id, err := x.Classify(ref)
				if err != nil {
					return errs.Usage("%s", err.Error())
				}
				a.target = ref
				rec, err := a.record(kind, id)
				if err != nil {
					return a.done(err)
				}
				recs = append(recs, rec)
				subjects = append(subjects, x.URI(kind, id))
			}
			doc := x.Graph(recs...)
			if len(doc.Nodes) == 0 {
				return mapErr(errNoResults)
			}
			if err := out.Emit(graphRow(doc, subjects)); err != nil {
				return err
			}
			return out.Flush()
		},
	}
}

// graphRow summarizes a document for the row formats while carrying the whole
// thing as the value. A document is one value and a table row is a line, so the
// line says how big the graph is, how much of it was actually read, and which
// predicates it uses; json and --template still get every node and every edge.
func graphRow(d x.Document, subjects []string) Row {
	read, preds := 0, map[string]bool{}
	for _, n := range d.Nodes {
		if n.Record != nil {
			read++
		}
	}
	for _, e := range d.Edges {
		preds[string(e.Predicate)] = true
	}
	names := make([]string, 0, len(preds))
	for p := range preds {
		names = append(names, p)
	}
	sort.Strings(names)
	return Row{
		Cols: []string{"graph", "nodes", "read", "edges", "predicates"},
		Vals: []string{
			strings.Join(subjects, " "),
			itoa(len(d.Nodes)), itoa(read), itoa(len(d.Edges)), strings.Join(names, " "),
		},
		Value: d,
	}
}
