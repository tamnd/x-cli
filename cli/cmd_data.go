package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tamnd/x-cli/x"
)

// addDataCommands wires the local-store workflow: crawl, queue, db, export.
func addDataCommands(root *cobra.Command, a *App) {
	root.AddCommand(
		a.cmdCrawl(),
		a.cmdQueue(),
		a.cmdDB(),
		a.cmdExport(),
	)
}

// openStore opens the SQLite store named by --db (required for the data group).
func (a *App) openStore() (*x.Store, error) {
	path := a.config().Store
	if path == "" {
		return nil, fmt.Errorf("this command needs a store: pass --db <file.db>")
	}
	return x.OpenStore(path)
}

func (a *App) cmdCrawl() *cobra.Command {
	var depth, max int
	c := &cobra.Command{
		Use:     "crawl <seed>...",
		Short:   "Breadth-first crawl of users into the local store",
		GroupID: "data",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
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
	c.Flags().IntVar(&depth, "depth", 1, "how many mention-hops to follow")
	c.Flags().IntVar(&max, "max", 200, "stop after this many stored tweets")
	return c
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

func (a *App) cmdQueue() *cobra.Command {
	c := &cobra.Command{
		Use:     "queue",
		Short:   "Show or clear the crawl queue",
		GroupID: "data",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			counts, err := st.QueueCounts()
			if err != nil {
				return err
			}
			return a.printKV(counts)
		},
	}
	clear := &cobra.Command{
		Use:   "clear",
		Short: "Empty the crawl queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			if err := st.ClearQueue(); err != nil {
				return err
			}
			a.logf("queue cleared")
			return nil
		},
	}
	c.AddCommand(clear)
	return c
}

func (a *App) cmdDB() *cobra.Command {
	c := &cobra.Command{
		Use:     "db",
		Short:   "Query and inspect the local store",
		GroupID: "data",
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "stats",
			Short: "Row counts per table",
			RunE: func(cmd *cobra.Command, args []string) error {
				st, err := a.openStore()
				if err != nil {
					return err
				}
				defer st.Close()
				s, err := st.Stats()
				if err != nil {
					return err
				}
				return a.printKV(s)
			},
		},
		&cobra.Command{
			Use:   "query <sql>",
			Short: "Run a read-only SQL query",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				st, err := a.openStore()
				if err != nil {
					return err
				}
				defer st.Close()
				return a.runQuery(st, joinArgs(args))
			},
		},
	)
	return c
}

// runQuery streams arbitrary SQL rows through the output formatter.
func (a *App) runQuery(st *x.Store, sql string) error {
	rows, err := st.DB().Query(sql)
	if err != nil {
		return err
	}
	defer rows.Close()
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

func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func (a *App) cmdExport() *cobra.Command {
	return &cobra.Command{
		Use:     "export <user> <out-dir>",
		Short:   "Render a stored user's tweets as Markdown",
		GroupID: "data",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
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

var _ = strings.TrimSpace
