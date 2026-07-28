package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/x-cli/x"
)

// defaultDiscoverBudget caps an unbounded `x discover` so a deep walk has a sane
// stop even when the user does not pass -n. `x crawl` has its own --max default.
const defaultDiscoverBudget = 500

// dataCommands returns the graph and local-store workflow: edges, graph, rdf,
// discover, crawl, queue, db, query, export. The store lives at a fixed path under the data dir (App.StorePath); it
// is not the generic kit --db sink.
func dataCommands() []kit.Command {
	return []kit.Command{
		newDiscoverCmd(),
		newEdgesCmd(),
		newGraphCmd(),
		newRDFCmd(),
		newQueryCmd(),
		newCrawlCmd(),
		newQueueCmd(),
		newDBCmd(),
		newExportCmd(),
	}
}

// parseSeeds classifies each positional argument as a tweet or a user seed.
func parseSeeds(args []string) ([]x.Seed, error) {
	seeds := make([]x.Seed, 0, len(args))
	for _, s := range args {
		sd, err := x.ParseSeed(s)
		if err != nil {
			return nil, errs.Usage("%s", err.Error())
		}
		seeds = append(seeds, sd)
	}
	return seeds, nil
}

// followHelp is the shared --follow flag help, drawn from the hop catalogue so
// the names a user can type live in one place (x.HopHelp).
var followHelp = "hops to follow: " + x.HopHelp()

func newDiscoverCmd() kit.Command {
	var depth, fanout, budget int
	var follow string
	var store bool
	return kit.Command{
		Use:     "discover <seed>...",
		Aliases: []string{"walk"},
		Short:   "Breadth-first walk of the graph linked from a tweet or user",
		Long: "discover starts at one or more tweets or users and follows their links\n" +
			"outward, hop by hop, streaming every node it reaches. Choose what to follow\n" +
			"with --follow (a preset like content/thread/engagement/network, or a list of\n" +
			"hops), how far with --depth, and how wide per hop with --fanout. The walk\n" +
			"stays on Tier 0 by default; engagement and network hops need a session.\n" +
			"--budget caps what the walk spends upstream, counted in requests rather than\n" +
			"nodes, and a walk that stops early says how many nodes it left unexpanded.\n" +
			"Add --store to also persist nodes and edges into the local store.",
		Args: kit.MinimumNArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.IntVar(&depth, "depth", 1, "how many hops to follow from each seed")
			f.IntVar(&fanout, "fanout", 25, "max neighbors to pull per hop (0 = unlimited)")
			f.StringVar(&follow, "follow", "content", followHelp)
			f.IntVar(&budget, "budget", 0, "stop after spending this many upstream requests (0 = no cap)")
			f.BoolVar(&store, "store", false, "also persist discovered nodes and edges into the local store")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			hops, err := x.ParseHops(follow)
			if err != nil {
				return errs.Usage("%s", err.Error())
			}
			seeds, err := parseSeeds(args)
			if err != nil {
				return err
			}
			out, err := a.out()
			if err != nil {
				return err
			}
			var st *x.Store
			if store {
				st, err = a.openStore()
				if err != nil {
					return err
				}
				defer func() { _ = st.Close() }()
			}
			nodes := a.limit
			if nodes <= 0 {
				nodes = defaultDiscoverBudget
			}
			sp := a.progress("discovering")
			defer sp.stop()
			opts := x.WalkOptions{
				Depth:  depth,
				Max:    nodes,
				Fanout: fanout,
				Hops:   hops,
				Budget: budget,
				Note:   func(s string) { sp.stop(); a.logf("note: %s", s) },
				Left: func(n int, why string) {
					sp.stop()
					a.logf("stopped early (%s): %d nodes left unexpanded", why, n)
				},
			}
			if st != nil {
				opts.OnHop = func(src, dst string, e x.Hop, m *x.Meta) {
					if edge, ok := x.HopEdge(e, src, dst, m); ok {
						_ = st.PutEdges([]x.Edge{edge})
					}
				}
			}
			n := 0
			err = a.engine().Walk(a.ctx(), seeds, opts, func(nd *x.Node) error {
				sp.stop()
				if st != nil {
					_ = st.UpsertNode(nd)
				}
				if e := out.Emit(nodeRow(nd)); e != nil {
					return e
				}
				n++
				return nil
			})
			if e := out.Flush(); e != nil && err == nil {
				err = e
			}
			if err != nil {
				return mapErr(err)
			}
			if n == 0 {
				return mapErr(errNoResults)
			}
			return nil
		},
	}
}

func newCrawlCmd() kit.Command {
	var depth, max, fanout, budget int
	var follow string
	return kit.Command{
		Use:   "crawl <seed>...",
		Short: "Breadth-first crawl of the graph into the local store",
		Long: "crawl is discover that persists: it walks the graph from each seed and writes\n" +
			"every node and edge into the local store under the data dir, marking the\n" +
			"frontier in the queue as it goes. Use the same --follow/--depth/--fanout knobs\n" +
			"as discover; --max bounds how many nodes it stores and --budget how many\n" +
			"requests it spends getting them. Inspect the result with\n" +
			"`x db stats`, `x db query`, and `x queue`.",
		Args:  kit.MinimumNArgs(1),
		Write: true,
		Flags: func(f *kit.FlagSet) {
			f.IntVar(&depth, "depth", 1, "how many hops to follow from each seed")
			f.IntVar(&max, "max", 200, "stop after storing this many nodes")
			f.IntVar(&fanout, "fanout", 25, "max neighbors to pull per hop (0 = unlimited)")
			f.IntVar(&budget, "budget", 0, "stop after spending this many upstream requests (0 = no cap)")
			f.StringVar(&follow, "follow", "content", followHelp)
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			hops, err := x.ParseHops(follow)
			if err != nil {
				return errs.Usage("%s", err.Error())
			}
			seeds, err := parseSeeds(args)
			if err != nil {
				return err
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			stored, left := 0, 0
			opts := x.WalkOptions{
				Depth:  depth,
				Max:    max,
				Fanout: fanout,
				Hops:   hops,
				Budget: budget,
				Note:   func(s string) { a.logf("note: %s", s) },
				Left:   func(n int, why string) { left = n; a.logf("stopped early (%s)", why) },
				OnHop: func(src, dst string, e x.Hop, m *x.Meta) {
					if edge, ok := x.HopEdge(e, src, dst, m); ok {
						_ = st.PutEdges([]x.Edge{edge})
					}
					_ = st.Enqueue(dst, string(e.Target()), 0)
				},
			}
			err = a.engine().Walk(a.ctx(), seeds, opts, func(n *x.Node) error {
				if e := st.UpsertNode(n); e != nil {
					return e
				}
				_ = st.MarkDone(n.Endpoint())
				stored++
				a.logf("[%d] %s %s", n.Depth, n.Kind, n.Endpoint())
				return nil
			})
			if err != nil {
				return mapErr(err)
			}
			if left > 0 {
				a.logf("stored %d nodes, %d left unexpanded", stored, left)
				return nil
			}
			a.logf("stored %d nodes", stored)
			return nil
		},
	}
}

// newQueryCmd is `x query <sql>` (spec 3003 doc 05 section 4), the same read as
// `x db query` under the name the spec gives it. The store is a graph and SQL is
// how you ask a graph a question, so the question does not deserve to be two
// words deep. `x db` keeps its copy because the rest of `x db` is store
// housekeeping and query belongs beside stats.
func newQueryCmd() kit.Command {
	return kit.Command{
		Use:   "query <sql>",
		Short: "Run a read-only SQL query against the local store",
		Long: "query runs SQL against the store a crawl filled in, and never touches the " +
			"network: crawl once and the graph is yours. The two tables are `nodes` (uri, " +
			"kind, id, tier, record, captured) and `edges` (from_uri, predicate, to_uri, " +
			"source, tier, captured), where the source is part of the key so two surfaces " +
			"asserting the same claim stay two rows.",
		Args: kit.MinimumNArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			return mapErr(a.runQuery(st, joinArgs(args)))
		},
	}
}

func newQueueCmd() kit.Command {
	return kit.Command{
		Use:   "queue",
		Short: "Show or clear the crawl queue",
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			counts, err := st.QueueCounts()
			if err != nil {
				return err
			}
			return a.printKV(counts)
		},
		Sub: []kit.Command{
			{
				Use:   "clear",
				Short: "Empty the crawl queue",
				Write: true,
				Run: func(ctx context.Context, args []string) error {
					a := appFromCtx(ctx)
					st, err := a.openStore()
					if err != nil {
						return err
					}
					defer func() { _ = st.Close() }()
					if err := st.ClearQueue(); err != nil {
						return err
					}
					a.logf("queue cleared")
					return nil
				},
			},
		},
	}
}

func newDBCmd() kit.Command {
	return kit.Command{
		Use:   "db",
		Short: "Query and inspect the local store",
		Sub: []kit.Command{
			{
				Use:   "stats",
				Short: "Row counts per table",
				Run: func(ctx context.Context, args []string) error {
					a := appFromCtx(ctx)
					st, err := a.openStore()
					if err != nil {
						return err
					}
					defer func() { _ = st.Close() }()
					s, err := st.Stats()
					if err != nil {
						return err
					}
					return a.printKV(s)
				},
			},
			{
				Use:   "query <sql>",
				Short: "Run a read-only SQL query",
				Args:  kit.MinimumNArgs(1),
				Run: func(ctx context.Context, args []string) error {
					a := appFromCtx(ctx)
					st, err := a.openStore()
					if err != nil {
						return err
					}
					defer func() { _ = st.Close() }()
					return mapErr(a.runQuery(st, joinArgs(args)))
				},
			},
		},
	}
}

// runQuery streams arbitrary SQL rows through the output formatter.
func (a *App) runQuery(st *x.Store, sql string) error {
	rows, err := st.DB().Query(sql)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	out, err := a.out()
	if err != nil {
		return err
	}
	n := 0
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		vals := make([]string, len(cols))
		obj := map[string]any{}
		for i, v := range raw {
			vals[i] = cellString(v)
			obj[cols[i]] = v
		}
		if err := out.Emit(Row{Cols: cols, Vals: vals, Value: obj}); err != nil {
			return err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := out.Flush(); err != nil {
		return err
	}
	if n == 0 {
		return errNoResults
	}
	return nil
}

// cellString renders a scanned SQL value for a table/csv cell. It collapses
// newlines and tabs to spaces so a stored tweet body stays on one row; the full
// untouched value still rides in the Row's Value for json and template output.
func cellString(v any) string {
	var s string
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		s = string(t)
	default:
		s = fmt.Sprintf("%v", t)
	}
	return strings.NewReplacer("\n", " ", "\t", " ").Replace(s)
}

func newExportCmd() kit.Command {
	return kit.Command{
		Use:   "export <user> <out-dir>",
		Short: "Render a stored user's tweets as Markdown",
		Args:  kit.ExactArgs(2),
		Write: true,
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			ref, _, err := userRef(args[0], false)
			if err != nil {
				return err
			}
			n, err := x.Export(st, ref, args[1])
			if err != nil {
				return err
			}
			a.logf("wrote %d tweets to %s", n, args[1])
			return nil
		},
	}
}

// printKV renders a small map as sorted key/value rows.
func (a *App) printKV(m map[string]int) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out, err := a.out()
	if err != nil {
		return err
	}
	for _, k := range keys {
		r := Row{
			Cols:  []string{"key", "value"},
			Vals:  []string{k, fmt.Sprintf("%d", m[k])},
			Value: map[string]any{"key": k, "value": m[k]},
		}
		if err := out.Emit(r); err != nil {
			return err
		}
	}
	return out.Flush()
}
