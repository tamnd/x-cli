package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/x-cli/x"
)

// dataCommands returns the local-store workflow: crawl, queue, db, export. The
// store lives at a fixed path under the data dir (App.StorePath); it is not the
// generic kit --db sink.
func dataCommands() []kit.Command {
	return []kit.Command{
		newCrawlCmd(),
		newQueueCmd(),
		newDBCmd(),
		newExportCmd(),
	}
}

func newCrawlCmd() kit.Command {
	var depth, max int
	return kit.Command{
		Use:   "crawl <seed>...",
		Short: "Breadth-first crawl of users into the local store",
		Args:  kit.MinimumNArgs(1),
		Write: true,
		Flags: func(f *kit.FlagSet) {
			f.IntVar(&depth, "depth", 1, "how many mention-hops to follow")
			f.IntVar(&max, "max", 200, "stop after this many stored tweets")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			eng := a.engine()
			for _, s := range args {
				ref, _, err := userRef(s, false)
				if err != nil {
					return err
				}
				_ = st.Enqueue(ref, "user", depth)
			}
			stored := 0
			for stored < max {
				item, ok, err := st.NextPending()
				if err != nil {
					return err
				}
				if !ok {
					break
				}
				prio := remainingDepth(st, item.Target)
				a.logf("crawl @%s", item.Target)
				o := x.TimelineOpts{Limit: 0}
				err = eng.Timeline(a.ctx(), item.Target, false, o, func(t *x.Tweet) error {
					if e := st.UpsertTweet(t); e != nil {
						return e
					}
					stored++
					if prio > 1 {
						for _, m := range t.Entities.Mentions {
							_ = st.Enqueue(m, "user", prio-1)
						}
					}
					if stored >= max {
						return errStop
					}
					return nil
				})
				_ = st.MarkDone(item.Target)
				if err != nil && err != errStop {
					a.logf("  warn: %v", err)
				}
				if err == errStop {
					break
				}
			}
			a.logf("stored %d tweets", stored)
			return nil
		},
	}
}

// remainingDepth reads the queued priority (used as a depth counter) for target.
func remainingDepth(st *x.Store, target string) int {
	var p int
	_ = st.DB().QueryRow(`SELECT priority FROM queue WHERE url=?`, target).Scan(&p)
	if p < 1 {
		p = 1
	}
	return p
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
