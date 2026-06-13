package cli

import (
	"github.com/spf13/cobra"
	"github.com/tamnd/x-cli/x"
)

// addEntityCommands wires the profile/user-graph reads.
func addEntityCommands(root *cobra.Command, a *App) {
	root.AddCommand(
		a.cmdUser(),
		a.cmdFollowers(),
		a.cmdFollowing(),
		a.cmdRetweeters(),
		a.cmdLikers(),
		a.cmdLikes(),
	)
}

func (a *App) cmdUser() *cobra.Command {
	var byID bool
	c := &cobra.Command{
		Use:     "user <user>",
		Aliases: []string{"profile"},
		Short:   "Show a profile",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			u, err := a.engine().User(a.ctx(), ref, isID)
			if err != nil {
				return err
			}
			return a.emitOne(userRow(u))
		},
	}
	c.Flags().BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
	return c
}

// userListCmd builds a followers/following-style command sharing one shape.
func (a *App) userListCmd(use, short string, run func(e *x.Engine, ref string, isID bool, emit func(*x.User) error) error) *cobra.Command {
	var byID bool
	c := &cobra.Command{
		Use:     use,
		Short:   short,
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			e := a.engine()
			return a.streamUsers(func(emit func(*x.User) error) error {
				return run(e, ref, isID, emit)
			})
		},
	}
	c.Flags().BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
	return c
}

func (a *App) cmdFollowers() *cobra.Command {
	return a.userListCmd("followers <user>", "Accounts following a user (needs --guest or session)",
		func(e *x.Engine, ref string, isID bool, emit func(*x.User) error) error {
			return e.Followers(a.ctx(), ref, isID, a.limit, emit)
		})
}

func (a *App) cmdFollowing() *cobra.Command {
	return a.userListCmd("following <user>", "Accounts a user follows (needs --guest or session)",
		func(e *x.Engine, ref string, isID bool, emit func(*x.User) error) error {
			return e.Following(a.ctx(), ref, isID, a.limit, emit)
		})
}

func (a *App) cmdLikes() *cobra.Command {
	var byID bool
	c := &cobra.Command{
		Use:     "likes <user>",
		Short:   "Tweets a user has liked (needs --guest or session)",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Likes(a.ctx(), ref, isID, a.limit, emit)
			})
		},
	}
	c.Flags().BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
	return c
}

func (a *App) cmdRetweeters() *cobra.Command {
	return &cobra.Command{
		Use:     "retweeters <ref>",
		Short:   "Accounts that retweeted a tweet (needs --guest or session)",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			return a.streamUsers(func(emit func(*x.User) error) error {
				return a.engine().Retweeters(a.ctx(), id, a.limit, emit)
			})
		},
	}
}

func (a *App) cmdLikers() *cobra.Command {
	return &cobra.Command{
		Use:     "likers <ref>",
		Short:   "Accounts that liked a tweet (needs --guest or session)",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			return a.streamUsers(func(emit func(*x.User) error) error {
				return a.engine().Likers(a.ctx(), id, a.limit, emit)
			})
		},
	}
}
