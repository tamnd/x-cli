package cli

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/x-cli/x"
)

// The failure record from spec 3003 doc 03 section 11. The exit code and the
// record have to agree, because a caller reading the stream and a caller reading
// $? are asking the same question.
func TestFailureRecordMatchesTheExitCode(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		code int
		want func(x.Failure) string // "" when the record is right
	}{
		{
			name: "no results",
			err:  errNoResults,
			code: 3,
		},
		{
			name: "the tier wall says which tier clears it",
			err:  x.NeedTier("search", 2),
			code: 4,
			want: func(f x.Failure) string {
				if f.NeedTier == nil || *f.NeedTier != 2 {
					return "need_tier should be 2"
				}
				return ""
			},
		},
		{
			name: "rate limited names the surface",
			err:  &x.RateLimitedError{Msg: "slow down", Endpoint: "syndication.tweet"},
			code: 5,
			want: func(f x.Failure) string {
				if f.Surface != "syndication.tweet" {
					return "surface should name the bucket that emptied"
				}
				return ""
			},
		},
		{
			name: "not found",
			err:  &x.NotFoundError{Kind: "tweet", Ref: "20"},
			code: 6,
		},
		{
			// 7 is the one that must not carry need_tier: there is no
			// credential to go and get.
			name: "unsupported has nothing to go and get",
			err:  x.Unsupported("dms", "X has no read surface for them"),
			code: 7,
			want: func(f x.Failure) string {
				if f.NeedTier != nil {
					return "need_tier should be absent, nothing unlocks this"
				}
				return ""
			},
		},
		{
			name: "network",
			err:  &x.NetworkError{Msg: "cannot resolve x.com"},
			code: 8,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := &App{}
			f := a.failure("20", c.err)
			if f.Kind != "error" {
				t.Errorf("kind = %q, want error", f.Kind)
			}
			if f.Target != "20" {
				t.Errorf("target = %q, want 20", f.Target)
			}
			if f.Code != c.code {
				t.Errorf("record code = %d, want %d", f.Code, c.code)
			}
			if got := errs.ExitCode(mapErr(c.err)); got != c.code {
				t.Errorf("exit code = %d but the record says %d", got, c.code)
			}
			if c.want != nil {
				if msg := c.want(f); msg != "" {
					t.Errorf("%s: %+v", msg, f)
				}
			}
		})
	}
}

// The record says which tier was tried, which is not the tier that would have
// worked. Failure keeps those two apart and need_tier is the one to act on.
func TestFailureRecordsTheTierThatWasTried(t *testing.T) {
	a := &App{cfg: x.Config{AllowGuest: true}}
	f := a.failure("search", x.NeedTier("search", 2))
	if f.Tier != 1 {
		t.Errorf("tier = %d, want 1, the tier this run could use", f.Tier)
	}
	if f.NeedTier == nil || *f.NeedTier != 2 {
		t.Errorf("need_tier = %v, want 2, the tier that would have worked", f.NeedTier)
	}
}

func TestTierNum(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  x.Config
		want int
	}{
		{"nothing", x.Config{}, 0},
		{"guest opt-in", x.Config{AllowGuest: true}, 1},
		{"a session", x.Config{AuthToken: "a", CT0: "b"}, 2},
		{"forced down from a session", x.Config{AuthToken: "a", CT0: "b", Tier: "syndication"}, 0},
		{"forced up without one", x.Config{Tier: "session"}, 2},
	} {
		if got := c.cfg.TierNum(); got != c.want {
			t.Errorf("%s: TierNum() = %d, want %d", c.name, got, c.want)
		}
	}
}

// The record goes to stdout on jsonl and nowhere else. On json it would land
// after the array closed, on csv it would be a row with the wrong columns, and
// the human formats already print something better on stderr.
func TestFailureRecordOnlyOnJSONL(t *testing.T) {
	for _, c := range []struct {
		format string
		isTTY  bool
		want   bool
	}{
		{format: "", isTTY: false, want: true}, // piped, so jsonl
		{format: "jsonl", want: true},
		{format: "", isTTY: true},
		{format: "json"},
		{format: "csv"},
		{format: "table"},
	} {
		a := &App{st: &kit.State{Output: kit.OutputOptions{Format: c.format, IsTTY: c.isTTY}}}
		if got := a.format() == "jsonl"; got != c.want {
			t.Errorf("format %q tty %v: emits record = %v, want %v", c.format, c.isTTY, got, c.want)
		}
	}
}

// A read that worked has nothing to report, and fail must not invent a record
// for it.
func TestFailPassesNilThrough(t *testing.T) {
	a := &App{}
	if err := a.fail("20", nil); err != nil {
		t.Errorf("fail(nil) = %v, want nil", err)
	}
	if err := a.done(nil); err != nil {
		t.Errorf("done(nil) = %v, want nil", err)
	}
}
