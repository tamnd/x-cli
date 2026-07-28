package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/x-cli/x"
)

// cmd_tiers.go prints the three tables the spec is built on: what each tier
// reads, which surface answers which question, and what each surface costs
// (spec 3003 doc 01 sections 7, 9, 10 and doc 05 section 6).
//
// They come out of x.Surfaces, x.Tiers and x.Routes, which is the same data the
// client routes on. Printing them from a second copy would give us two tables
// that agree until the day they do not.

func tableCommands() []kit.Command {
	return []kit.Command{
		newTiersCmd(),
		newRoutesCmd(),
		newSurfacesCmd(),
	}
}

func newTiersCmd() kit.Command {
	return kit.Command{
		Use:     "tiers",
		Aliases: []string{"info"},
		Short:   "Show what each credential tier reads, and which ones you have",
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			cfg := a.config()
			out, err := a.out()
			if err != nil {
				return err
			}
			cols := []string{"tier", "credential", "status", "reads"}
			for _, t := range x.Tiers {
				n := strconv.Itoa(t.N)
				st := tierStatus(cfg, t.N)
				if err := out.Emit(Row{
					Cols: cols,
					Vals: []string{n, t.Credential, st, t.Reads},
					Value: map[string]any{
						"tier": t.N, "credential": t.Credential, "status": st, "reads": t.Reads,
					},
				}); err != nil {
					return err
				}
			}
			return out.Flush()
		},
	}
}

// tierStatus is a statement about this machine, not about X. Tier 0 is always
// on because it needs nothing; the other two say what is missing and how to get
// it rather than just saying no.
func tierStatus(cfg x.Config, tier int) string {
	switch tier {
	case 0:
		return "ok"
	case 1:
		if cfg.AllowGuest || cfg.Tier == "guest" || cfg.Tier == "session" {
			return "ok"
		}
		return "off (pass --guest)"
	default:
		if cfg.HasSession() {
			return "ok"
		}
		return "not set (x auth import)"
	}
}

func newRoutesCmd() kit.Command {
	var only string
	return kit.Command{
		Use:   "routes",
		Short: "Show which surface answers which question, at each tier",
		Flags: func(f *kit.FlagSet) {
			f.StringVar(&only, "tier", "", "only questions this tier can answer (0, 1, or 2)")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			want := -1
			if only != "" {
				n, err := strconv.Atoi(only)
				if err != nil || n < 0 || n > 2 {
					// Not "--tier takes ...": kit title-cases the first token, and
				// a flag name is one word that should not come back capitalised.
				return errs.Usage("there are three tiers, 0, 1 and 2, and %q is not one of them", only)
				}
				want = n
			}
			out, err := a.out()
			if err != nil {
				return err
			}
			cols := []string{"question", "tier0", "tier1", "tier2"}
			n := 0
			for _, r := range x.Routes {
				if want >= 0 && !servesAt(r, want) {
					continue
				}
				n++
				if err := out.Emit(Row{
					Cols: cols,
					Vals: []string{r.Question, dash(r.Tier0), dash(r.Tier1), dash(r.Tier2)},
					Value: map[string]any{
						"question": r.Question,
						"tier0":    r.Tier0,
						"tier1":    r.Tier1,
						"tier2":    r.Tier2,
					},
				}); err != nil {
					return err
				}
			}
			if err := out.Flush(); err != nil {
				return err
			}
			if n == 0 {
				return mapErr(errNoResults)
			}
			return nil
		},
	}
}

// servesAt reports whether a tier answers a question at all. A caller asking
// for tier 0 wants the rows that work with no credential, so a row served only
// higher up is not one of them.
func servesAt(r x.Route, tier int) bool {
	switch tier {
	case 0:
		return r.Tier0 != ""
	case 1:
		return r.Tier1 != ""
	default:
		return r.Tier2 != ""
	}
}

func newSurfacesCmd() kit.Command {
	return kit.Command{
		Use:   "surfaces",
		Short: "Show every public route into X, with its tier, limit, and cache life",
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			out, err := a.out()
			if err != nil {
				return err
			}
			cols := []string{"n", "surface", "host", "tier", "limit", "cache", "note"}
			for _, s := range x.Surfaces {
				if err := out.Emit(Row{
					Cols: cols,
					Vals: []string{
						strconv.Itoa(s.N), s.Name, s.Host, strconv.Itoa(s.Tier),
						limitText(s), s.CacheTTL, s.Note,
					},
					Value: map[string]any{
						"n": s.N, "surface": s.Name, "host": s.Host, "tier": s.Tier,
						"limit": s.Limit, "window": s.Window, "headers": s.Headers,
						"cache": s.CacheTTL, "note": s.Note,
					},
				}); err != nil {
					return err
				}
			}
			return out.Flush()
		},
	}
}

// limitText folds the count and its window into one cell, because a limit with
// no window is half a fact.
func limitText(s x.Surface) string {
	if s.Window == "" {
		return s.Limit
	}
	return fmt.Sprintf("%s / %s", s.Limit, s.Window)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
