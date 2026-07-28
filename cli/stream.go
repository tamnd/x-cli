package cli

import (
	"errors"
	"strings"

	"github.com/tamnd/any-cli/kit/render"
	"github.com/tamnd/x-cli/x"
)

// joinArgs reassembles a multi-word query the user typed without quotes.
func joinArgs(args []string) string { return strings.Join(args, " ") }

// needSession returns a need-auth error when no user session is configured.
func (a *App) needSession(action string) error {
	if a.config().HasSession() {
		return nil
	}
	return x.ErrNeedUser(action + " needs your own session, run `x auth import`")
}

// needGraphQL returns a need-auth error when no GraphQL tier is available.
func (a *App) needGraphQL(action string) error {
	cfg := a.config()
	if cfg.HasSession() || cfg.AllowGuest || cfg.Tier == "guest" || cfg.Tier == "session" {
		return nil
	}
	return x.ErrNeedAuth(action + " needs the GraphQL tier: pass --guest, or run `x auth import`")
}

// sampleFix names the way out of a ranked selection, and there is not always
// one.
//
// The walk in time order is a GraphQL operation, so "pass --guest" is the right
// advice at tier 0 and noise to somebody who already passed it. That case is
// real: a replies read routes to tier 0 on purpose even with a guest token,
// because guest UserTweetsAndReplies answers 200 with an empty envelope. Telling
// that caller to pass the flag they just passed reads as the tool not listening.
func (a *App) sampleFix() string {
	cfg := a.config()
	switch {
	case cfg.Tier == "syndication" || cfg.Tier == "web":
		return "drop --tier " + cfg.Tier + " to walk it in time order"
	case cfg.HasSession():
		return "there is no deeper read to fall back on here"
	case cfg.AllowGuest || cfg.Tier == "guest":
		return "run `x auth import` to walk it in time order"
	default:
		return "pass --guest to walk it in time order"
	}
}

// errStop unwinds an emit callback once the row limit is hit; it is swallowed
// by the stream helpers and never surfaces to the user.
var errStop = errors.New("stop")

// streamTweets runs a producer that emits *x.Tweet, renders each as a row, and
// stops at --limit. It returns errNoResults when the producer yielded nothing.
// Reads no longer tee into a store; the crawl command owns store population.
func (a *App) streamTweets(run func(emit func(*x.Tweet) error) error) error {
	out, err := a.out()
	if err != nil {
		return err
	}
	err = a.streamInto(out, run)
	if e := out.Flush(); e != nil && err == nil {
		err = e
	}
	return err
}

// streamInto is the same thing against a renderer the caller owns and flushes.
// x get reads several references in one run, and they belong in one document.
func (a *App) streamInto(out *render.Renderer, run func(emit func(*x.Tweet) error) error) error {
	sp := a.progress("fetching tweets")
	defer sp.stop()
	n := 0
	err := run(func(t *x.Tweet) error {
		if t == nil {
			return nil
		}
		sp.stop() // clear the spinner before the first row reaches stdout
		a.warnMissed(t.Meta)
		if t.Sample {
			// The rows carry `sample`, but the caveat is the kind that has to
			// arrive before somebody reads the dates off the top of the list.
			a.warnOnce("X returned a ranked selection from the whole account, not the most recent tweets; " + a.sampleFix())
		}
		if e := out.Emit(tweetRow(t)); e != nil {
			return e
		}
		n++
		if a.limit > 0 && n >= a.limit {
			return errStop
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

// streamUsers is the user-list analogue of streamTweets.
func (a *App) streamUsers(run func(emit func(*x.User) error) error) error {
	out, err := a.out()
	if err != nil {
		return err
	}
	sp := a.progress("fetching accounts")
	defer sp.stop()
	n := 0
	err = run(func(u *x.User) error {
		if u == nil {
			return nil
		}
		sp.stop() // clear the spinner before the first row reaches stdout
		if e := out.Emit(userRow(u)); e != nil {
			return e
		}
		n++
		if a.limit > 0 && n >= a.limit {
			return errStop
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

// emitOne renders a single row and flushes.
func (a *App) emitOne(r Row) error {
	out, err := a.out()
	if err != nil {
		return err
	}
	if err := out.Emit(r); err != nil {
		return err
	}
	return out.Flush()
}

// tweetRef parses a positional tweet reference to a canonical ID.
func tweetRef(s string) (string, error) { return x.ParseTweetRef(s) }

// userRef parses a positional user reference. forceID treats a numeric value as
// an account id rather than a handle.
func userRef(s string, forceID bool) (string, bool, error) { return x.ParseUserRef(s, forceID) }
