package cli

import (
	"context"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/x-cli/x"
)

// entityCommands returns the profile/user-graph reads.
func entityCommands() []kit.Command {
	return []kit.Command{
		newUserCmd(),
		newFollowersCmd(),
		newFollowingCmd(),
		newRetweetersCmd(),
		newLikersCmd(),
		newLikesCmd(),
	}
}

func newUserCmd() kit.Command {
	var byID bool
	return kit.Command{
		Use:     "user <user>",
		Aliases: []string{"profile"},
		Short:   "Show a profile",
		Args:    kit.ExactArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			sp := a.progress("fetching profile")
			u, err := a.engine().User(a.ctx(), ref, isID)
			sp.stop()
			if err != nil {
				return mapErr(err)
			}
			return mapErr(a.emitOne(userRow(u)))
		},
	}
}

// userListCmd builds a followers/following-style command sharing one shape.
func userListCmd(use, short string, run func(a *App, e *x.Engine, ref string, isID bool, emit func(*x.User) error) error) kit.Command {
	var byID bool
	return kit.Command{
		Use:   use,
		Short: short,
		Args:  kit.ExactArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			e := a.engine()
			return mapErr(a.streamUsers(func(emit func(*x.User) error) error {
				return run(a, e, ref, isID, emit)
			}))
		},
	}
}

func newFollowersCmd() kit.Command {
	return userListCmd("followers <user>", "Accounts following a user (needs --guest or session)",
		func(a *App, e *x.Engine, ref string, isID bool, emit func(*x.User) error) error {
			return e.Followers(a.ctx(), ref, isID, a.limit, emit)
		})
}

func newFollowingCmd() kit.Command {
	return userListCmd("following <user>", "Accounts a user follows (needs --guest or session)",
		func(a *App, e *x.Engine, ref string, isID bool, emit func(*x.User) error) error {
			return e.Following(a.ctx(), ref, isID, a.limit, emit)
		})
}

func newLikesCmd() kit.Command {
	var byID bool
	return kit.Command{
		Use:   "likes <user>",
		Short: "Tweets a user has liked (needs --guest or session)",
		Args:  kit.ExactArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Likes(a.ctx(), ref, isID, a.limit, emit)
			}))
		},
	}
}

func newRetweetersCmd() kit.Command {
	return kit.Command{
		Use:   "retweeters <ref>",
		Short: "Accounts that retweeted a tweet (needs --guest or session)",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			return mapErr(a.streamUsers(func(emit func(*x.User) error) error {
				return a.engine().Retweeters(a.ctx(), id, a.limit, emit)
			}))
		},
	}
}

func newLikersCmd() kit.Command {
	return kit.Command{
		Use:   "likers <ref>",
		Short: "Accounts that liked a tweet (needs --guest or session)",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			return mapErr(a.streamUsers(func(emit func(*x.User) error) error {
				return a.engine().Likers(a.ctx(), id, a.limit, emit)
			}))
		},
	}
}
