package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/x-cli/x"
)

// cmd_identity.go is the offline plane (spec 3003 doc 05 section 5). None of
// these three commands opens a socket, reads the cache, or looks at a session,
// which is what makes them usable inside a shell loop over a file of pasted
// links. They take any number of references so the loop can be one process.

func identityCommands() []kit.Command {
	return []kit.Command{newURLCmd(), newURICmd(), newClassifyCmd()}
}

func newURLCmd() kit.Command {
	return kit.Command{
		Use:   "url <ref>...",
		Short: "Print the canonical x.com address for a reference (offline)",
		Long: "url canonicalises anything you can paste: a bare id, a handle, a status URL " +
			"with tracking parameters, a nitter link. It makes no network call.",
		Args: kit.MinimumNArgs(1),
		Run: func(ctx context.Context, args []string) error {
			return eachRef(args, func(ref, kind, id string) (string, error) {
				u, err := x.Locate(kind, id)
				if err != nil {
					return "", fmt.Errorf("no url for %s: %s", ref, err)
				}
				return u, nil
			})
		},
	}
}

func newURICmd() kit.Command {
	return kit.Command{
		Use:   "uri <ref>...",
		Short: "Print the x:// address for a reference (offline)",
		Long: "uri is the stable name of a node. The same tweet read anonymously and read " +
			"with a session has one uri, which is what lets the graph plane join them.",
		Args: kit.MinimumNArgs(1),
		Run: func(ctx context.Context, args []string) error {
			return eachRef(args, func(ref, kind, id string) (string, error) {
				if kind == x.KindSearch {
					return "", fmt.Errorf("a search is a query rather than a node, so %s has no uri", ref)
				}
				return x.URI(kind, id), nil
			})
		},
	}
}

func newClassifyCmd() kit.Command {
	return kit.Command{
		Use:   "classify <ref>...",
		Short: "Say what a reference points at, and where (offline)",
		Args:  kit.MinimumNArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			out, err := a.out()
			if err != nil {
				return err
			}
			for _, ref := range args {
				kind, id, err := x.Classify(ref)
				if err != nil {
					return errs.Usage("%s", err.Error())
				}
				uri := ""
				if kind != x.KindSearch {
					uri = x.URI(kind, id)
				}
				// A kind with no page of its own leaves the url column empty
				// rather than inventing an address for it.
				url, _ := x.Locate(kind, id)
				cols := []string{"kind", "id", "uri", "url"}
				vals := []string{kind, id, uri, url}
				if err := out.Emit(Row{Cols: cols, Vals: vals, Value: map[string]any{
					"kind": kind, "id": id, "uri": uri, "url": url,
				}}); err != nil {
					return err
				}
			}
			return out.Flush()
		},
	}
}

// eachRef classifies every argument and prints one line each. A reference that
// does not classify is a usage error, because the caller passed something that
// is not a reference, and a loop over a file wants to know which line.
func eachRef(refs []string, render func(ref, kind, id string) (string, error)) error {
	lines := make([]string, 0, len(refs))
	for _, ref := range refs {
		kind, id, err := x.Classify(ref)
		if err != nil {
			return errs.Usage("%s", err.Error())
		}
		s, err := render(ref, kind, id)
		if err != nil {
			return errs.Usage("%s", err.Error())
		}
		lines = append(lines, s)
	}
	for _, s := range lines {
		if _, err := fmt.Fprintln(os.Stdout, s); err != nil {
			return err
		}
	}
	return nil
}
