package cli

import (
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit/errs"
	"github.com/tamnd/x-cli/x"
)

// get is the front door, so what matters is that every kind the classifier can
// return has a decided answer: a reader, or an exit code 7 that says why not.
// A kind that fell through to an unclassified error would be exit 1, which
// tells a caller looping over a file of links nothing at all.
func TestEveryKindHasAnAnswer(t *testing.T) {
	readable := map[string]bool{
		x.KindTweet: true, x.KindCard: true, x.KindNote: true, x.KindPoll: true,
		x.KindUser: true, x.KindConversation: true, x.KindHashtag: true,
		x.KindCashtag: true, x.KindSearch: true, x.KindList: true,
		x.KindSpace: true,
	}
	for _, kind := range x.Kinds {
		if readable[kind] {
			continue
		}
		err := noReaderFor(kind)
		if got := errs.ExitCode(err); got != 7 {
			t.Errorf("%s: exit %d, want 7 (err %v)", kind, got, err)
		}
		if strings.Contains(err.Error(), "%!") {
			t.Errorf("%s: the message did not format: %s", kind, err)
		}
	}
}

// A kind get can read must never reach noReaderFor, which is the same list
// stated from the other side: if a case is deleted from read's switch, this
// catches it only in company with the live checks, so it stays a reminder of
// which kinds the dispatch owns.
func TestNoReaderNamesTheKind(t *testing.T) {
	err := noReaderFor(x.KindBroadcast)
	if !strings.Contains(err.Error(), "broadcast") {
		t.Errorf("the message should name the kind, got %q", err)
	}
	// The three that are not pages get their own reason rather than the
	// not-built-yet one, because no milestone is going to change them.
	for _, kind := range []string{x.KindMedia, x.KindLink, x.KindPlace} {
		if strings.Contains(noReaderFor(kind).Error(), "yet") {
			t.Errorf("%s reads as unbuilt, and it is not", kind)
		}
	}
}

// media takes a tweet or a profile. Everything else is the caller's mistake and
// gets exit 2, not a request.
func TestMediaRefTakesATweetOrAProfile(t *testing.T) {
	for _, c := range []struct {
		arg      string
		byID     bool
		wantKind string
		wantRef  string
		wantExit int
	}{
		{arg: "2081860978694594863", wantKind: x.KindTweet, wantRef: "2081860978694594863"},
		{arg: "https://x.com/NASA/status/20", wantKind: x.KindTweet, wantRef: "20"},
		{arg: "@NASA", wantKind: x.KindUser, wantRef: "nasa"},
		{arg: "nasa", wantKind: x.KindUser, wantRef: "nasa"},
		{arg: "https://x.com/nasa/media", wantKind: x.KindUser, wantRef: "nasa"},
		// --id is the escape hatch: a bare number is a tweet id everywhere else.
		{arg: "11348282", byID: true, wantKind: x.KindUser, wantRef: "11348282"},
		{arg: "#nasa", wantExit: 2},
		{arg: "https://example.com/x", wantExit: 2},
		{arg: "", wantExit: 2},
	} {
		kind, ref, err := tweetOrUserRef("media", c.arg, c.byID)
		if got := errs.ExitCode(err); got != c.wantExit {
			t.Errorf("%q: exit %d, want %d (err %v)", c.arg, got, c.wantExit, err)
			continue
		}
		if c.wantExit != 0 {
			continue
		}
		if kind != c.wantKind || ref != c.wantRef {
			t.Errorf("%q: got %s/%s, want %s/%s", c.arg, kind, ref, c.wantKind, c.wantRef)
		}
	}
}
