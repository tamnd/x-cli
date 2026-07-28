package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/x-cli/x"
)

// cmd_rdf.go writes the graph as RDF (spec 3003 doc 04 section 4, doc 05).
//
// It is `x graph` in somebody else's vocabulary. The document is x's own shape;
// this is the same thing said in schema.org, so it can be loaded into a triple
// store beside data that has nothing to do with X and still join.
//
// It writes bytes rather than records, so the record flags do not apply: -o and
// --template have nothing to format when the output is already a serialisation
// with a syntax of its own.

func newRDFCmd() kit.Command {
	var format string
	var provenance bool
	return kit.Command{
		Use:   "rdf <ref>...",
		Short: "Write the graph a record asserts as RDF",
		Long: "rdf reads each reference and writes the same graph `x graph` prints, in RDF: " +
			"schema.org wherever a term exists and the x: namespace where none does. X " +
			"publishes schema.org microdata on its own pages, so this vocabulary is X's " +
			"rather than this tool's invention, which is what makes the output checkable " +
			"against the source.\n\n" +
			"Four serializations. `nq` carries the URL each claim was read from as the graph " +
			"name, which is how provenance survives a merge, and `jsonld` carries it as a " +
			"named graph per source. `nt` and `ttl` have nowhere to put it, so " +
			"--provenance adds reified statements; it is off by default because reification " +
			"outnumbers the data four to one.",
		Args: kit.MinimumNArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.StringVar(&format, "format", "ttl", "serialization: "+strings.Join(x.RDFFormats, ", "))
			f.BoolVar(&provenance, "provenance", false, "carry the source of every claim, reified, in nt and ttl")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			if err := a.rawOutput("rdf"); err != nil {
				return err
			}
			var recs []any
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
			}
			doc := x.Graph(recs...)
			out, err := x.WriteRDF(format, x.Triples(doc), x.RDFOptions{Provenance: provenance})
			if err != nil {
				return errs.Usage("%s", err.Error())
			}
			_, err = io.WriteString(os.Stdout, out)
			return err
		},
	}
}
