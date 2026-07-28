package cli

import (
	"context"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/any-cli/kit/render"
	"github.com/tamnd/x-cli/x"
)

// cmd_get.go is the front door (spec 3003 doc 05 section 69).
//
// get classifies whatever you paste and reads it with the command that fits, so
// `x timeline nasa -o url | xargs -n1 x get` works across kinds and a file of
// mixed links does not need sorting first. The classifier is the offline one in
// x/identity.go, so the dispatch costs nothing and agrees with x classify.
//
// Every reference in one run shares one renderer, which is the reason this does
// not just loop over the other commands: three refs under -o json are one array
// rather than three.

func newGetCmd() kit.Command {
	return kit.Command{
		Use:   "get <ref>...",
		Short: "Read whatever a reference points at",
		Long: "get reads each reference with the command that fits it: a tweet id or a status " +
			"link reads the tweet, a handle reads the profile, a hashtag or a search link runs " +
			"the search, a list link reads the list. Run x classify to see what a reference is.",
		Args: kit.MinimumNArgs(1),
		Run: func(ctx context.Context, args []string) error {
			a := appFromCtx(ctx)
			out, err := a.out()
			if err != nil {
				return err
			}
			shape := ""
			for _, ref := range args {
				kind, id, err := x.Classify(ref)
				if err != nil {
					return errs.Usage("%s", err.Error())
				}
				a.target = ref
				// A table has one header, so a run that switches from tweets to
				// profiles starts a second table rather than printing profiles
				// under the tweet columns. json and jsonl take mixed records in
				// one document, and the readable list view labels every record
				// itself, so both of those stay in one renderer.
				if s := rowShape(kind); shape != "" && s != shape && a.columnar() {
					if err := out.Flush(); err != nil {
						return err
					}
					if out, err = a.out(); err != nil {
						return err
					}
				}
				shape = rowShape(kind)
				if err := a.read(out, kind, id); err != nil {
					// Flush first: the refs that did work have already been
					// emitted, and a buffering format would otherwise drop them.
					_ = out.Flush()
					return a.done(err)
				}
			}
			return out.Flush()
		},
	}
}

// read is the dispatch. It renders into a renderer the caller owns rather than
// making its own, so one run is one document however many references it read.
func (a *App) read(out *render.Renderer, kind, id string) error {
	e := a.engine()
	switch kind {
	case x.KindTweet, x.KindCard, x.KindNote:
		// A card and a note are parts of a tweet rather than records with their
		// own address, so the tweet that carries them is the answer.
		sp := a.progress("fetching tweet")
		t, err := e.Tweet(a.ctx(), id)
		sp.stop()
		if err != nil {
			return err
		}
		return out.Emit(tweetRow(t))
	case x.KindPoll:
		sp := a.progress("fetching tweet")
		t, err := e.Tweet(a.ctx(), id)
		sp.stop()
		if err != nil {
			return err
		}
		return emitPoll(out, t, id)
	case x.KindUser:
		sp := a.progress("fetching profile")
		u, err := e.User(a.ctx(), id, false)
		sp.stop()
		if err != nil {
			return err
		}
		a.warnMissed(u.Meta)
		return out.Emit(userRow(u))
	case x.KindConversation:
		return a.streamInto(out, func(emit func(*x.Tweet) error) error {
			return e.Thread(a.ctx(), id, a.limit, emit)
		})
	case x.KindHashtag:
		return a.searchInto(out, "#"+id)
	case x.KindCashtag:
		return a.searchInto(out, "$"+id)
	case x.KindSearch:
		return a.searchInto(out, id)
	case x.KindList:
		if err := a.needGraphQL("listing tweets"); err != nil {
			return err
		}
		return a.streamInto(out, func(emit func(*x.Tweet) error) error {
			return e.GraphQL().ListTweets(a.ctx(), id, a.limit, emit)
		})
	}
	return noReaderFor(kind)
}

// rowShape is which row builder a kind ends up at, which is the thing a table
// header has to agree with. Kinds that read as a list of tweets share one shape
// however different they are as references.
func rowShape(kind string) string {
	switch kind {
	case x.KindUser:
		return "user"
	case x.KindPoll:
		return "poll"
	}
	return "tweet"
}

// columnar reports whether this run's format has one header for the whole
// document, which is what makes a change of record shape mid-run a problem.
func (a *App) columnar() bool {
	switch a.format() {
	case render.Table, render.CSV, render.TSV:
		return true
	}
	return false
}

// searchInto runs a search into the caller's renderer. Latest rather than Top,
// because a hashtag typed at a tool is a question about now.
func (a *App) searchInto(out *render.Renderer, q string) error {
	return a.streamInto(out, func(emit func(*x.Tweet) error) error {
		return a.engine().Search(a.ctx(),
			x.SearchQuery{Raw: q, Product: "Latest", Limit: a.limit}, emit)
	})
}

// noReaderFor is exit code 7 for a kind get cannot read, and the message says
// which of the two reasons it is.
//
// A media key, a link and a place are not pages: nothing fetches them on their
// own at any tier, which is what code 7 means. A space, a broadcast, a community
// and a trend are real records that x has not built a reader for yet, so the
// message says that rather than blaming X for it. Both are 7 because no
// credential changes either one, which is the distinction the taxonomy draws
// between 7 and 4.
func noReaderFor(kind string) error {
	switch kind {
	case x.KindMedia:
		return errs.Unsupported("a media key is not fetchable on its own, so read the tweet that posted it")
	case x.KindLink:
		return errs.Unsupported("that is a link off X rather than a node on it, so there is nothing to read")
	case x.KindPlace:
		return errs.Unsupported("a place is a geotag rather than a page, so there is nothing to read")
	}
	return errs.Unsupported("x has no reader for a %s yet", kind)
}
