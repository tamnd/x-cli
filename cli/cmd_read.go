package cli

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/x-cli/x"
)

// readCommands returns the tweet- and query-centric reads.
func readCommands() []kit.Command {
	return []kit.Command{
		newTweetCmd(),
		newTimelineCmd(),
		newRepliesCmd(),
		newMediaCmd(),
		newThreadCmd(),
		newSearchCmd(),
		newQuotesCmd(),
		newMentionsCmd(),
		newHomeCmd(),
		newBookmarksCmd(),
		newPollCmd(),
		newCountsCmd(),
		newListCmd(),
	}
}

func newTweetCmd() kit.Command {
	return kit.Command{
		Use:   "tweet <ref>",
		Short: "Show a single tweet",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			t, err := a.engine().Tweet(a.ctx(), id)
			if err != nil {
				return mapErr(err)
			}
			return mapErr(a.emitOne(tweetRow(t)))
		},
	}
}

func newTimelineCmd() kit.Command {
	var withReplies, mediaOnly, byID bool
	return kit.Command{
		Use:     "timeline <user>",
		Aliases: []string{"tweets"},
		Short:   "A user's tweets (Tier 0 recent window; deeper with --guest/session)",
		Args:    kit.ExactArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.BoolVar(&withReplies, "replies", false, "include replies")
			f.BoolVar(&mediaOnly, "media", false, "only tweets with media")
			f.BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			ref, isID, err := userRef(args[0], byID)
			if err != nil {
				return err
			}
			o := x.TimelineOpts{Replies: withReplies, Media: mediaOnly, Limit: a.limit}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Timeline(a.ctx(), ref, isID, o, emit)
			}))
		},
	}
}

func newRepliesCmd() kit.Command {
	var byID bool
	return kit.Command{
		Use:   "replies <user>",
		Short: "A user's tweets including replies",
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
			o := x.TimelineOpts{Replies: true, Limit: a.limit}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Timeline(a.ctx(), ref, isID, o, emit)
			}))
		},
	}
}

func newMediaCmd() kit.Command {
	var byID bool
	return kit.Command{
		Use:   "media <user>",
		Short: "Media attached to a user's tweets",
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
				return mapErr(err)
			}
			if n == 0 {
				return mapErr(errNoResults)
			}
			return nil
		},
	}
}

func newThreadCmd() kit.Command {
	return kit.Command{
		Use:   "thread <ref>",
		Short: "A conversation thread around a tweet",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Thread(a.ctx(), id, a.limit, emit)
			}))
		},
	}
}

func newSearchCmd() kit.Command {
	var product string
	return kit.Command{
		Use:   "search <query>",
		Short: "Search tweets (needs --guest or your session)",
		Args:  kit.MinimumNArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.StringVar(&product, "product", "Latest", "Top|Latest|People|Photos|Videos")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			q := x.SearchQuery{Raw: joinArgs(args), Product: product, Limit: a.limit}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Search(a.ctx(), q, emit)
			}))
		},
	}
}

func newQuotesCmd() kit.Command {
	return kit.Command{
		Use:   "quotes <ref>",
		Short: "Quote tweets of a tweet (search-backed)",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			q := x.SearchQuery{Raw: "quoted_tweet_id:" + id, Product: "Latest", Limit: a.limit}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Search(a.ctx(), q, emit)
			}))
		},
	}
}

func newMentionsCmd() kit.Command {
	return kit.Command{
		Use:   "mentions <user>",
		Short: "Tweets mentioning a user (search-backed)",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			ref, _, err := userRef(args[0], false)
			if err != nil {
				return err
			}
			q := x.SearchQuery{Raw: "@" + ref, Product: "Latest", Limit: a.limit}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Search(a.ctx(), q, emit)
			}))
		},
	}
}

func newHomeCmd() kit.Command {
	return kit.Command{
		Use:   "home",
		Short: "Your reverse-chron home timeline (session only)",
		Args:  kit.NoArgs,
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			if err := a.needSession("home"); err != nil {
				return mapErr(err)
			}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().GraphQL().Home(a.ctx(), a.limit, emit)
			}))
		},
	}
}

func newBookmarksCmd() kit.Command {
	return kit.Command{
		Use:   "bookmarks",
		Short: "Your bookmarks (session only)",
		Args:  kit.NoArgs,
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			if err := a.needSession("bookmarks"); err != nil {
				return mapErr(err)
			}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().GraphQL().Bookmarks(a.ctx(), a.limit, emit)
			}))
		},
	}
}

func newPollCmd() kit.Command {
	return kit.Command{
		Use:   "poll <ref>",
		Short: "Show a tweet's poll options and tallies",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			t, err := a.engine().Tweet(a.ctx(), id)
			if err != nil {
				return mapErr(err)
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

func newCountsCmd() kit.Command {
	var product string
	return kit.Command{
		Use:   "counts <query>",
		Short: "Per-day tweet counts for a search (client-side buckets)",
		Args:  kit.MinimumNArgs(1),
		Flags: func(f *kit.FlagSet) {
			f.StringVar(&product, "product", "Latest", "Top|Latest")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
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
				return mapErr(err)
			}
			if len(days) == 0 {
				return mapErr(errNoResults)
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
}

func newListCmd() kit.Command {
	return kit.Command{
		Use:   "list <list-id>",
		Short: "Tweets in an X List (needs --guest or your session)",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			if err := a.needGraphQL("listing tweets"); err != nil {
				return mapErr(err)
			}
			return mapErr(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().GraphQL().ListTweets(a.ctx(), args[0], a.limit, emit)
			}))
		},
	}
}
