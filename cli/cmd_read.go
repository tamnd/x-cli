package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/any-cli/kit/render"
	"github.com/tamnd/x-cli/x"
)

// readCommands returns the tweet- and query-centric reads.
func readCommands() []kit.Command {
	return []kit.Command{
		newGetCmd(),
		newTweetCmd(),
		newTimelineCmd(),
		newRepliesCmd(),
		newMediaCmd(),
		newEmbedCmd(),
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
			a.target = id
			sp := a.progress("fetching tweet")
			t, err := a.engine().Tweet(a.ctx(), id)
			sp.stop()
			if err != nil {
				return a.done(err)
			}
			return a.done(a.emitOne(tweetRow(t)))
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
			a.target = ref
			o := x.TimelineOpts{Replies: withReplies, Media: mediaOnly, Limit: a.limit}
			return a.done(a.streamTweets(func(emit func(*x.Tweet) error) error {
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
			a.target = ref
			o := x.TimelineOpts{Replies: true, Limit: a.limit}
			return a.done(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().Timeline(a.ctx(), ref, isID, o, emit)
			}))
		},
	}
}

// newMediaCmd reads the pictures and video (spec 3003 doc 05, `x media`).
//
// It takes a tweet or a profile, because both are things people have in hand: a
// link to one post whose photo they want, or an account whose pictures they
// want. With --download it stops being a record command and becomes a byte one,
// printing the paths it wrote.
//
// The size and the variant are decided here rather than taken from whatever URL
// a plane happened to record, which is the whole point of x.MediaURL: a photo
// URL carries its size in it, and the default has to be the original or a
// download quietly saves a thumbnail.
func newMediaCmd() kit.Command {
	var byID, tab bool
	var download, size, variant string
	return kit.Command{
		Use:   "media <ref>",
		Short: "Media on a tweet, or on a user's tweets",
		Args:  kit.ExactArgs(1),
		Write: true, // --download writes files
		Flags: func(f *kit.FlagSet) {
			f.BoolVar(&byID, "id", false, "treat the argument as a numeric user id")
			f.BoolVar(&tab, "tab", false, "read the profile's media tab (needs --guest or your session)")
			f.StringVar(&download, "download", "", "save the bytes to this directory")
			f.StringVar(&size, "size", x.DefaultMediaSize, "photo size: thumb|small|medium|large|orig")
			f.StringVar(&variant, "variant", "", "video rendition, by resolution or bitrate (default: the best mp4)")
		},
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			if download != "" {
				if err := a.rawOutput("media --download"); err != nil {
					return err
				}
			}
			if err := x.CheckSize(size); err != nil {
				return errs.Usage("%s", err.Error())
			}
			kind, ref, err := mediaRef(args[0], byID)
			if err != nil {
				return err
			}
			if tab && kind != x.KindUser {
				return errs.Usage("--tab reads a profile's media tab, and %s is a tweet", args[0])
			}
			a.target = ref

			// One producer for the three shapes, so the download path and the
			// record path read the same thing.
			walk := func(emit func(*x.Tweet) error) error {
				switch {
				case kind == x.KindTweet:
					t, err := a.engine().Tweet(a.ctx(), ref)
					if err != nil {
						return err
					}
					return emit(t)
				case tab:
					return a.engine().MediaTab(a.ctx(), ref, byID, a.limit, emit)
				default:
					o := x.TimelineOpts{Media: true, Limit: a.limit}
					return a.engine().Timeline(a.ctx(), ref, byID, o, emit)
				}
			}
			if download != "" {
				return mapErr(a.downloadMedia(walk, download, size, variant))
			}
			return a.done(a.streamMedia(walk, size, variant))
		},
	}
}

// mediaRef classifies the argument. --id is the escape hatch for a numeric user
// id, which would otherwise read as a tweet id, because that is what a bare run
// of digits means everywhere else in the tool.
func mediaRef(arg string, byID bool) (kind, ref string, err error) {
	if byID {
		r, _, err := userRef(arg, true)
		return x.KindUser, r, err
	}
	kind, ref, err = x.Classify(arg)
	if err != nil {
		return "", "", errs.Usage("%s", err.Error())
	}
	switch kind {
	case x.KindTweet, x.KindUser:
		return kind, ref, nil
	}
	return "", "", errs.Usage("media reads a tweet or a profile, and %s is a %s", arg, kind)
}

// streamMedia emits one record per media item, with the URL resolved to the
// size and variant asked for.
func (a *App) streamMedia(walk func(func(*x.Tweet) error) error, size, variant string) error {
	out, err := a.out()
	if err != nil {
		return err
	}
	sp := a.progress("fetching media")
	defer sp.stop()
	n := 0
	err = walk(func(t *x.Tweet) error {
		for _, m := range t.Media {
			u, err := x.MediaURL(m, size, variant)
			if err != nil {
				// One item that cannot answer the request, such as a photo in a
				// tweet read with --variant, is not the whole read failing.
				a.logf("warn: %s: %v", t.ID, err)
				continue
			}
			sp.stop()
			if e := out.Emit(mediaRow(m, u)); e != nil {
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
	if err != nil && !errors.Is(err, errStop) {
		return err
	}
	if n == 0 {
		return errNoResults
	}
	return nil
}

// downloadMedia saves the bytes and prints the path of each file, one per line.
//
// The paths are on stdout as plain lines rather than as records: the command
// wrote bytes, and a list of files is what a shell wants back from it. Warnings
// go to stderr, so one dead URL in a timeline does not abort the rest.
func (a *App) downloadMedia(walk func(func(*x.Tweet) error) error, dir, size, variant string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sp := a.progress("downloading")
	defer sp.stop()
	n := 0
	err := walk(func(t *x.Tweet) error {
		for i, m := range t.Media {
			u, err := x.MediaURL(m, size, variant)
			if err != nil {
				a.logf("warn: %s: %v", t.ID, err)
				continue
			}
			dst := filepath.Join(dir, fmt.Sprintf("%s-%d%s", t.ID, i+1, extOf(u)))
			if err := downloadFile(a.ctx(), u, dst); err != nil {
				a.logf("warn: %s: %v", dst, err)
				continue
			}
			n++
			sp.stop()
			if _, e := fmt.Fprintln(os.Stdout, dst); e != nil {
				return e
			}
			if a.limit > 0 && n >= a.limit {
				return errStop
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return err
	}
	if n == 0 {
		return errNoResults
	}
	return nil
}

// newEmbedCmd prints the oEmbed blockquote (spec 3003 doc 02 section 6).
//
// It is a byte command: the bytes X returns are the answer, because the point of
// the surface is that you paste them into a page. Reformatting them into a
// record would be this tool deciding it knows better than the thing it asked.
//
// The parsed fields plane F reads out of the same blockquote are on the tweet
// record, which is where `x tweet 20 -o json` already shows them.
func newEmbedCmd() kit.Command {
	return kit.Command{
		Use:   "embed <ref>",
		Short: "Print a tweet's oEmbed blockquote, verbatim",
		Args:  kit.ExactArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			if err := a.rawOutput("embed"); err != nil {
				return err
			}
			id, err := tweetRef(args[0])
			if err != nil {
				return err
			}
			a.target = id
			sp := a.progress("fetching embed")
			o, err := a.engine().OEmbed(a.ctx(), id)
			sp.stop()
			if err != nil {
				return mapErr(err)
			}
			_, err = io.WriteString(os.Stdout, o.HTML)
			return err
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
			a.target = id
			return a.done(a.streamTweets(func(emit func(*x.Tweet) error) error {
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
			a.target = q.Raw
			return a.done(a.streamTweets(func(emit func(*x.Tweet) error) error {
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
			a.target = id
			q := x.SearchQuery{Raw: "quoted_tweet_id:" + id, Product: "Latest", Limit: a.limit}
			return a.done(a.streamTweets(func(emit func(*x.Tweet) error) error {
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
			a.target = ref
			q := x.SearchQuery{Raw: "@" + ref, Product: "Latest", Limit: a.limit}
			return a.done(a.streamTweets(func(emit func(*x.Tweet) error) error {
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
				return a.done(err)
			}
			return a.done(a.streamTweets(func(emit func(*x.Tweet) error) error {
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
				return a.done(err)
			}
			return a.done(a.streamTweets(func(emit func(*x.Tweet) error) error {
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
			a.target = id
			sp := a.progress("fetching tweet")
			t, err := a.engine().Tweet(a.ctx(), id)
			sp.stop()
			if err != nil {
				return a.done(err)
			}
			out, err := a.out()
			if err != nil {
				return err
			}
			if err := emitPoll(out, t, id); err != nil {
				return a.done(err)
			}
			return out.Flush()
		},
	}
}

// emitPoll renders a tweet's poll, one row per option. x poll and x get both
// want it, and a tweet with no poll is the same answer either way.
func emitPoll(out *render.Renderer, t *x.Tweet, id string) error {
	if t.Poll == nil || len(t.Poll.Options) == 0 {
		return fmt.Errorf("tweet %s has no poll", id)
	}
	for _, o := range t.Poll.Options {
		if err := out.Emit(pollOptionRow(t.Poll, o)); err != nil {
			return err
		}
	}
	return nil
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
			a.target = q.Raw
			sp := a.progress("counting")
			days := map[string]int{}
			err := a.engine().Search(a.ctx(), q, func(t *x.Tweet) error {
				key := t.CreatedAt.UTC().Format("2006-01-02")
				if t.CreatedAt.IsZero() {
					key = "undated"
				}
				days[key]++
				return nil
			})
			sp.stop()
			if err != nil {
				return a.done(err)
			}
			if len(days) == 0 {
				return a.done(errNoResults)
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
				return a.done(err)
			}
			return a.done(a.streamTweets(func(emit func(*x.Tweet) error) error {
				return a.engine().GraphQL().ListTweets(a.ctx(), args[0], a.limit, emit)
			}))
		},
	}
}
