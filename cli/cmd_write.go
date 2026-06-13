package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/tamnd/x-cli/x"
)

// addWriteCommands wires the actions that act on the user's own account. Every
// one needs an imported session (`x auth import`) and respects --dry-run/--yes.
func addWriteCommands(root *cobra.Command, a *App) {
	root.AddCommand(
		a.cmdPost(),
		a.cmdReply(),
		a.cmdDelete(),
		a.cmdLike(),
		a.cmdRetweet(),
		a.cmdBookmark(),
		a.cmdFollow(),
		a.cmdMute(),
		a.cmdBlock(),
		a.cmdDM(),
	)
}

// confirm asks for a y/N unless --yes; on a non-tty it requires --yes.
func (a *App) confirm(action string) error {
	if a.yes {
		return nil
	}
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("%s needs confirmation: re-run with --yes", action)
	}
	fmt.Fprintf(os.Stderr, "%s? [y/N] ", action)
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	if s := strings.ToLower(strings.TrimSpace(line)); s == "y" || s == "yes" {
		return nil
	}
	return fmt.Errorf("cancelled")
}

// resolveID turns a user reference into a numeric account id.
func (a *App) resolveID(ref string) (string, error) {
	r, isID, err := userRef(ref, true)
	if err != nil {
		return "", err
	}
	if isID {
		return r, nil
	}
	u, err := a.engine().User(a.ctx(), r, false)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

func (a *App) cmdPost() *cobra.Command {
	var quote, replyTo, replySettings string
	c := &cobra.Command{
		Use:     "post <text>",
		Aliases: []string{"tweet-create"},
		Short:   "Post a tweet from your account",
		GroupID: "write",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in := x.NewTweet{Text: joinArgs(args), Quote: quote, ReplyTo: replyTo, ReplySettings: replySettings}
			if a.dryRun {
				return a.emitOne(rawRow(fmt.Sprintf("POST tweet: %q reply_to=%s quote=%s", in.Text, in.ReplyTo, in.Quote)))
			}
			if err := a.confirm("post this tweet"); err != nil {
				return err
			}
			t, err := a.engine().GraphQL().CreateTweet(a.ctx(), in)
			if err != nil {
				return err
			}
			return a.emitOne(tweetRow(t))
		},
	}
	c.Flags().StringVar(&quote, "quote", "", "tweet ref to quote")
	c.Flags().StringVar(&replyTo, "reply-to", "", "tweet ref to reply to")
	c.Flags().StringVar(&replySettings, "reply-settings", "", "everyone|following|mentionedUsers")
	return c
}

func (a *App) cmdReply() *cobra.Command {
	return &cobra.Command{
		Use:     "reply <ref> <text>",
		Short:   "Reply to a tweet",
		GroupID: "write",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			in := x.NewTweet{Text: joinArgs(args[1:]), ReplyTo: id}
			if a.dryRun {
				return a.emitOne(rawRow(fmt.Sprintf("REPLY to %s: %q", id, in.Text)))
			}
			if err := a.confirm("post this reply"); err != nil {
				return err
			}
			t, err := a.engine().GraphQL().CreateTweet(a.ctx(), in)
			if err != nil {
				return err
			}
			return a.emitOne(tweetRow(t))
		},
	}
}

func (a *App) cmdDelete() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <ref>",
		Short:   "Delete one of your tweets",
		GroupID: "write",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			if a.dryRun {
				return a.emitOne(rawRow("DELETE tweet " + id))
			}
			if err := a.confirm("delete tweet " + id); err != nil {
				return err
			}
			if err := a.engine().GraphQL().DeleteTweet(a.ctx(), id); err != nil {
				return err
			}
			a.logf("deleted %s", id)
			return nil
		},
	}
}

// tweetToggleCmd builds like/retweet/bookmark with a shared --undo shape.
func (a *App) tweetToggleCmd(use, verb string, do func(g *x.GraphQL, id string, undo bool) error) *cobra.Command {
	var undo bool
	c := &cobra.Command{
		Use:     use,
		Short:   verb + " a tweet (use --undo to reverse)",
		GroupID: "write",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			act := verb
			if undo {
				act = "un-" + verb
			}
			if a.dryRun {
				return a.emitOne(rawRow(strings.ToUpper(act) + " " + id))
			}
			if err := do(a.engine().GraphQL(), id, undo); err != nil {
				return err
			}
			a.logf("%s %s", act, id)
			return nil
		},
	}
	c.Flags().BoolVar(&undo, "undo", false, "reverse the action")
	return c
}

func (a *App) cmdLike() *cobra.Command {
	return a.tweetToggleCmd("like <ref>", "like", func(g *x.GraphQL, id string, undo bool) error { return g.Like(a.ctx(), id, undo) })
}
func (a *App) cmdRetweet() *cobra.Command {
	c := a.tweetToggleCmd("retweet <ref>", "retweet", func(g *x.GraphQL, id string, undo bool) error { return g.Retweet(a.ctx(), id, undo) })
	c.Aliases = []string{"rt"}
	return c
}
func (a *App) cmdBookmark() *cobra.Command {
	return a.tweetToggleCmd("bookmark <ref>", "bookmark", func(g *x.GraphQL, id string, undo bool) error { return g.Bookmark(a.ctx(), id, undo) })
}

// userToggleCmd builds follow/mute/block with a shared --undo shape.
func (a *App) userToggleCmd(use, verb string, do func(g *x.GraphQL, userID string, undo bool) error) *cobra.Command {
	var undo bool
	c := &cobra.Command{
		Use:     use,
		Short:   verb + " a user (use --undo to reverse)",
		GroupID: "write",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			act := verb
			if undo {
				act = "un-" + verb
			}
			if a.dryRun {
				return a.emitOne(rawRow(strings.ToUpper(act) + " " + args[0]))
			}
			uid, err := a.resolveID(args[0])
			if err != nil {
				return err
			}
			if err := do(a.engine().GraphQL(), uid, undo); err != nil {
				return err
			}
			a.logf("%s %s", act, args[0])
			return nil
		},
	}
	c.Flags().BoolVar(&undo, "undo", false, "reverse the action")
	return c
}

func (a *App) cmdFollow() *cobra.Command {
	return a.userToggleCmd("follow <user>", "follow", func(g *x.GraphQL, uid string, undo bool) error { return g.Follow(a.ctx(), uid, undo) })
}
func (a *App) cmdMute() *cobra.Command {
	return a.userToggleCmd("mute <user>", "mute", func(g *x.GraphQL, uid string, undo bool) error { return g.Mute(a.ctx(), uid, undo) })
}
func (a *App) cmdBlock() *cobra.Command {
	return a.userToggleCmd("block <user>", "block", func(g *x.GraphQL, uid string, undo bool) error { return g.Block(a.ctx(), uid, undo) })
}

func (a *App) cmdDM() *cobra.Command {
	return &cobra.Command{
		Use:     "dm <user> <text>",
		Short:   "Send a direct message",
		GroupID: "write",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := joinArgs(args[1:])
			if a.dryRun {
				return a.emitOne(rawRow(fmt.Sprintf("DM %s: %q", args[0], text)))
			}
			if err := a.confirm("send this DM"); err != nil {
				return err
			}
			uid, err := a.resolveID(args[0])
			if err != nil {
				return err
			}
			if err := a.engine().GraphQL().SendDM(a.ctx(), uid, text); err != nil {
				return err
			}
			a.logf("sent DM to %s", args[0])
			return nil
		},
	}
}

// rawRow wraps a plain string for -o raw / table single-cell display.
func rawRow(s string) Row {
	return Row{Cols: []string{"action"}, Vals: []string{s}, Value: s}
}
