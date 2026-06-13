package cli

import (
	"errors"
	"strings"

	"github.com/tamnd/x-cli/x"
)

// joinArgs reassembles a multi-word query the user typed without quotes.
func joinArgs(args []string) string { return strings.Join(args, " ") }

// needSession returns a need-auth error when no user session is configured.
func (a *App) needSession(action string) error {
	if a.config().HasSession() {
		return nil
	}
	return x.ErrNeedUser(action + " needs your own session — run `x auth import`")
}

// needGraphQL returns a need-auth error when no GraphQL tier is available.
func (a *App) needGraphQL(action string) error {
	cfg := a.config()
	if cfg.HasSession() || cfg.AllowGuest || cfg.Tier == "guest" || cfg.Tier == "session" {
		return nil
	}
	return x.ErrNeedAuth(action + " needs the GraphQL tier — pass --guest, or run `x auth import`")
}

// errStop unwinds an emit callback once the row limit is hit; it is swallowed
// by the stream helpers and never surfaces to the user.
var errStop = errors.New("stop")

// teeTweets returns a best-effort sink that upserts every emitted tweet (its
// author and media too) into the --db store, plus a closer. When --db is unset
// it is a no-op, so any read doubles as a crawl simply by adding --db.
func (a *App) teeTweets() (func(*x.Tweet), func()) {
	if a.db == "" {
		return func(*x.Tweet) {}, func() {}
	}
	st, err := x.OpenStore(a.db)
	if err != nil {
		a.logf("store: %v", err)
		return func(*x.Tweet) {}, func() {}
	}
	sink := func(t *x.Tweet) {
		if t == nil {
			return
		}
		if t.Author != nil {
			_ = st.UpsertUser(t.Author)
		}
		_ = st.UpsertTweet(t)
		for _, m := range t.Media {
			_ = st.UpsertMedia(t.ID, m)
		}
	}
	return sink, func() { _ = st.Close() }
}

// teeUsers is the user-list analogue of teeTweets.
func (a *App) teeUsers() (func(*x.User), func()) {
	if a.db == "" {
		return func(*x.User) {}, func() {}
	}
	st, err := x.OpenStore(a.db)
	if err != nil {
		a.logf("store: %v", err)
		return func(*x.User) {}, func() {}
	}
	return func(u *x.User) {
			if u != nil {
				_ = st.UpsertUser(u)
			}
		}, func() {
			_ = st.Close()
		}
}

// streamTweets runs a producer that emits *x.Tweet, renders each as a row, and
// stops at --limit. It returns errNoResults when the producer yielded nothing.
func (a *App) streamTweets(run func(emit func(*x.Tweet) error) error) error {
	out, err := a.out()
	if err != nil {
		return err
	}
	persist, closeStore := a.teeTweets()
	defer closeStore()
	n := 0
	err = run(func(t *x.Tweet) error {
		if t == nil {
			return nil
		}
		persist(t)
		if e := out.Emit(tweetRow(t)); e != nil {
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

// streamUsers is the user-list analogue of streamTweets.
func (a *App) streamUsers(run func(emit func(*x.User) error) error) error {
	out, err := a.out()
	if err != nil {
		return err
	}
	persist, closeStore := a.teeUsers()
	defer closeStore()
	n := 0
	err = run(func(u *x.User) error {
		if u == nil {
			return nil
		}
		persist(u)
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
