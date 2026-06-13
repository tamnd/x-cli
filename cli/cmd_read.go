package cli

import (
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/tamnd/x-cli/x"
)

// addReadCommands wires the tweet- and query-centric reads.
func addReadCommands(root *cobra.Command, a *App) {
	root.AddCommand(
		a.cmdTweet(),
		a.cmdTimeline(),
		a.cmdReplies(),
		a.cmdMedia(),
		a.cmdThread(),
		a.cmdSearch(),
		a.cmdQuotes(),
		a.cmdMentions(),
		a.cmdHome(),
		a.cmdBookmarks(),
		a.cmdPoll(),
		a.cmdCounts(),
		a.cmdList(),
	)
}

func (a *App) cmdTweet() *cobra.Command {
	return &cobra.Command{
		Use:     "tweet <ref>",
		Short:   "Show a single tweet",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			t, err := a.engine().Tweet(a.ctx(), id)
			if err != nil {
				return err
			}
			return a.emitOne(tweetRow(t))
		},
	}
}

func (a *App) cmdTimeline() *cobra.Command {
	var withReplies, mediaOnly, byID bool
	c := &cobra.Command{
		Use:     "timeline <user>",
		Aliases: []string{"tweets"},
		Short:   "A user's tweets (Tier 0 recent window; deeper with --guest/session)",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			o := x.TimelineOpts{Replies: withReplies, Media: mediaOnly, Limit: a.limit}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Timeline(a.ctx(), ref, isID, o, emit)
			})
		},
	}
	c.Flags().BoolVar(&withReplies, "replies", false, "include replies")
	c.Flags().BoolVar(&mediaOnly, "media", false, "only tweets with media")
	c.Flags().BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
	return c
}

func (a *App) cmdReplies() *cobra.Command {
	var byID bool
	c := &cobra.Command{
		Use:     "replies <user>",
		Short:   "A user's tweets including replies",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			o := x.TimelineOpts{Replies: true, Limit: a.limit}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Timeline(a.ctx(), ref, isID, o, emit)
			})
		},
	}
	c.Flags().BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
	return c
}

func (a *App) cmdMedia() *cobra.Command {
	var byID bool
	c := &cobra.Command{
		Use:     "media <user>",
		Short:   "Media attached to a user's tweets",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			out, err := a.out()
			if err != nil {
				return err
			}
			n := 0
			o := x.TimelineOpts{Media: true, Limit: a.limit}
			err = a.engine().Timeline(a.ctx(), ref, isID, o, func(t *x.Tweet) error {
				for _, m := range t.Media {
					if e := out.Emit(mediaRow(m)); e != nil {
						return e
					}
					n++
					if a.limit > 0 && n >= a.limit {
						return errStop
					}
				}
				return nil
			})
			if e := out.Flush(); e != nil && err == nil {
				err = e
			}
			if err != nil && err != errStop {
				return err
			}
			if n == 0 {
				return errNoResults
			}
			return nil
		},
	}
	c.Flags().BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
	return c
}

func (a *App) cmdThread() *cobra.Command {
	return &cobra.Command{
		Use:     "thread <ref>",
		Short:   "A conversation thread around a tweet",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Thread(a.ctx(), id, a.limit, emit)
			})
		},
	}
}

func (a *App) cmdSearch() *cobra.Command {
	var product string
	c := &cobra.Command{
		Use:     "search <query>",
		Short:   "Search tweets (needs --guest or your session)",
		GroupID: "read",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := x.SearchQuery{Raw: joinArgs(args), Product: product, Limit: a.limit}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Search(a.ctx(), q, emit)
			})
		},
	}
	c.Flags().StringVar(&product, "product", "Latest", "Top|Latest|People|Photos|Videos")
	return c
}

func (a *App) cmdQuotes() *cobra.Command {
	return &cobra.Command{
		Use:     "quotes <ref>",
		Short:   "Quote tweets of a tweet (search-backed)",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			q := x.SearchQuery{Raw: "quoted_tweet_id:" + id, Product: "Latest", Limit: a.limit}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Search(a.ctx(), q, emit)
			})
		},
	}
}

func (a *App) cmdMentions() *cobra.Command {
	return &cobra.Command{
		Use:     "mentions <user>",
		Short:   "Tweets mentioning a user (search-backed)",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, _, err := userRef(args[0], false)
			if err != nil {
				return err
			}
			q := x.SearchQuery{Raw: "@" + ref, Product: "Latest", Limit: a.limit}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Search(a.ctx(), q, emit)
			})
		},
	}
}

func (a *App) cmdHome() *cobra.Command {
	return &cobra.Command{
		Use:     "home",
		Short:   "Your reverse-chron home timeline (session only)",
		GroupID: "read",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.needSession("home"); err != nil {
				return err
			}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().GraphQL().Home(a.ctx(), a.limit, emit)
			})
		},
	}
}

func (a *App) cmdBookmarks() *cobra.Command {
	return &cobra.Command{
		Use:     "bookmarks",
		Short:   "Your bookmarks (session only)",
		GroupID: "read",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.needSession("bookmarks"); err != nil {
				return err
			}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().GraphQL().Bookmarks(a.ctx(), a.limit, emit)
			})
		},
	}
}

func (a *App) cmdPoll() *cobra.Command {
	return &cobra.Command{
		Use:     "poll <ref>",
		Short:   "Show a tweet's poll options and tallies",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			t, err := a.engine().Tweet(a.ctx(), id)
			if err != nil {
				return err
			}
			if t.Poll == nil || len(t.Poll.Options) == 0 {
				return fmt.Errorf("tweet %s has no poll", id)
			}
			out, err := a.out()
			if err != nil {
				return err
			}
			for _, o := range t.Poll.Options {
				if e := out.Emit(pollOptionRow(t.Poll, o)); e != nil {
					return e
				}
			}
			return out.Flush()
		},
	}
}

func (a *App) cmdCounts() *cobra.Command {
	var product string
	c := &cobra.Command{
		Use:     "counts <query>",
		Short:   "Per-day tweet counts for a search (client-side buckets)",
		GroupID: "read",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := x.SearchQuery{Raw: joinArgs(args), Product: product, Limit: a.limit}
			days := map[string]int{}
			err := a.engine().Search(a.ctx(), q, func(t *x.Tweet) error {
				key := t.CreatedAt.UTC().Format("2006-01-02")
				if t.CreatedAt.IsZero() {
					key = "undated"
				}
				days[key]++
				return nil
			})
			if err != nil {
				return err
			}
			if len(days) == 0 {
				return errNoResults
			}
			keys := make([]string, 0, len(days))
			for k := range days {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			out, err := a.out()
			if err != nil {
				return err
			}
			for _, k := range keys {
				day, _ := time.Parse("2006-01-02", k)
				b := x.Bucket{Start: day, End: day.AddDate(0, 0, 1), Count: days[k]}
				if e := out.Emit(bucketRow(b)); e != nil {
					return e
				}
			}
			return out.Flush()
		},
	}
	c.Flags().StringVar(&product, "product", "Latest", "Top|Latest")
	return c
}

func (a *App) cmdList() *cobra.Command {
	return &cobra.Command{
		Use:     "list <list-id>",
		Short:   "Tweets in an X List (needs --guest or your session)",
		GroupID: "read",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.needGraphQL("listing tweets"); err != nil {
				return err
			}
			return a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().GraphQL().ListTweets(a.ctx(), args[0], a.limit, emit)
			})
		},
	}
}
