package x

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"
)

// thread_test.go covers the upward walk against two live captures that happen
// to form a chain: 1903142823316049977 replies to 1903136743634723031, which
// replies to 20, which replies to nothing.

// synFixtures answers out of testdata, keyed by the id asked for, and records
// what was asked for. The URLs are constants in the production code, which is
// correct (there is one of each and neither is configurable), so the seam is the
// transport.
//
// tweet is surface 1, keyed by the id in the query. page is a status page, keyed
// by the id at the end of the path. They are separate maps because one tweet has
// both and they are different bytes.
type synFixtures struct {
	tweet map[string]string
	page  map[string]string
	got   []string
	asked []string
}

func (s *synFixtures) RoundTrip(r *http.Request) (*http.Response, error) {
	if id := r.URL.Query().Get("id"); id != "" || !strings.Contains(r.URL.Path, "/status/") {
		s.got = append(s.got, id)
		return reply(r, s.tweet[id], "application/json", `{"error":"not found"}`)
	}
	id := path.Base(r.URL.Path)
	s.asked = append(s.asked, id)
	return reply(r, s.page[id], "text/html", "<html><body>gone</body></html>")
}

func reply(r *http.Request, body, ctype, missing string) (*http.Response, error) {
	code, status := 200, "200 OK"
	if body == "" {
		code, status, body = 404, "404 Not Found", missing
	}
	return &http.Response{
		StatusCode: code,
		Status:     status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{ctype}},
		Request:    r,
	}, nil
}

func fixtureCfg(t *testing.T) Config {
	t.Helper()
	return Config{Timeout: 5 * time.Second, NoCache: true, Paths: Paths{Cache: t.TempDir()}}
}

func fixtureClient(t *testing.T, tweets map[string]string) (*Client, *synFixtures) {
	t.Helper()
	c := NewClient(fixtureCfg(t))
	f := &synFixtures{tweet: tweets}
	c.hc.Transport = f
	return c, f
}

func fixtureEngine(t *testing.T, f *synFixtures) *Engine {
	t.Helper()
	e := NewEngine(fixtureCfg(t))
	e.c.hc.Transport = f
	return e
}

func chainFixtures(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"1903142823316049977": capture(t, "s1_reply_with_parent.json.gz"),
		"20":                  capture(t, "s1_tweet_20.json.gz"),
	}
}

func TestParentChainReadsRootFirst(t *testing.T) {
	c, f := fixtureClient(t, chainFixtures(t))
	got, err := ParentChain(context.Background(), c, "1903142823316049977", 0)
	if err != nil {
		t.Fatalf("ParentChain: %v", err)
	}
	want := []string{"20", "1903136743634723031", "1903142823316049977"}
	if len(got) != len(want) {
		t.Fatalf("walked %d tweets, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d is %s, want %s: a thread reads from the root down", i, got[i].ID, id)
		}
	}

	// The halving. A fetch of a reply hands back the reply and its parent, so a
	// chain of three costs two requests and not three. If X stops inlining the
	// parent this goes to three and the walk still works, which is why the walk
	// checks for a nil parent rather than assuming one.
	if len(f.got) != 2 {
		t.Errorf("walked a chain of 3 in %d requests (%v), want 2", len(f.got), f.got)
	}
	if f.got[0] != "1903142823316049977" || f.got[1] != "20" {
		t.Errorf("asked for %v, want the reply and then its grandparent", f.got)
	}
}

// The parent arrives inside the child's answer, so it is stamped with the
// child's URL. That is where it came from, and saying otherwise would be the
// record claiming a request nobody made.
func TestAnInlinedParentIsStampedWithTheRequestThatCarriedIt(t *testing.T) {
	c, _ := fixtureClient(t, chainFixtures(t))
	got, err := ParentChain(context.Background(), c, "1903142823316049977", 0)
	if err != nil {
		t.Fatalf("ParentChain: %v", err)
	}
	parent := got[1]
	if len(parent.Sources) != 1 || !strings.Contains(parent.Sources[0], "id=1903142823316049977") {
		t.Errorf("the parent says it came from %v, want the child's request", parent.Sources)
	}
	for _, tw := range got {
		if tw.Kind != KindTweet || tw.Tier != 0 {
			t.Errorf("%s is kind %q at tier %d, want tweet at tier 0", tw.ID, tw.Kind, tw.Tier)
		}
		if len(tw.Surfaces) != 1 || tw.Surfaces[0] != "s1" {
			t.Errorf("%s says it came from %v, want [s1]", tw.ID, tw.Surfaces)
		}
	}
}

// The limit counts from the tweet asked for, because the root is not known
// until the walk is over, and a caller who says -n 2 wants the two nearest.
func TestTheChainLimitCountsFromTheTweet(t *testing.T) {
	c, f := fixtureClient(t, chainFixtures(t))
	got, err := ParentChain(context.Background(), c, "1903142823316049977", 2)
	if err != nil {
		t.Fatalf("ParentChain: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tweets, want 2", len(got))
	}
	if got[0].ID != "1903136743634723031" || got[1].ID != "1903142823316049977" {
		t.Errorf("got %s then %s, want the parent then the tweet", got[0].ID, got[1].ID)
	}
	if len(f.got) != 1 {
		t.Errorf("a two-tweet chain cost %d requests (%v), want 1", len(f.got), f.got)
	}
}

// A tweet that replies to nothing is a chain of one. That is the answer, not an
// error, and `x thread 20` printing tweet 20 is correct.
func TestATweetWithNoParentIsAChainOfOne(t *testing.T) {
	c, _ := fixtureClient(t, chainFixtures(t))
	got, err := ParentChain(context.Background(), c, "20", 0)
	if err != nil {
		t.Fatalf("ParentChain: %v", err)
	}
	if len(got) != 1 || got[0].ID != "20" {
		t.Fatalf("got %d tweets, want just tweet 20", len(got))
	}
}

// A chain that breaks partway up keeps what it read. A deleted ancestor is
// common: somebody removed their reply and the rest of the conversation is
// still a conversation.
func TestABrokenChainKeepsWhatItRead(t *testing.T) {
	// Only the first hop is available, so the walk finds the reply and its
	// inlined parent, then asks for tweet 20 and gets a 404.
	c, _ := fixtureClient(t, map[string]string{
		"1903142823316049977": capture(t, "s1_reply_with_parent.json.gz"),
	})
	got, err := ParentChain(context.Background(), c, "1903142823316049977", 0)
	if len(got) != 2 {
		t.Fatalf("got %d tweets, want the two the first request carried", len(got))
	}
	if got[0].ID != "1903136743634723031" || got[1].ID != "1903142823316049977" {
		t.Errorf("got %s then %s, want root-first order even on a broken chain", got[0].ID, got[1].ID)
	}
	var pe *PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("got err %v, want a PartialError saying the walk stopped", err)
	}
	if pe.Got != 2 || pe.Kind != "ancestors" {
		t.Errorf("got %d %s, want 2 ancestors", pe.Got, pe.Kind)
	}
}

// The first tweet failing is a plain failure. "Stopped after 0 ancestors" says
// less than "tweet not found" does.
func TestAChainThatNeverStartsIsJustAnError(t *testing.T) {
	c, _ := fixtureClient(t, map[string]string{})
	got, err := ParentChain(context.Background(), c, "20", 0)
	if len(got) != 0 {
		t.Errorf("got %d tweets from nothing", len(got))
	}
	var pe *PartialError
	if errors.As(err, &pe) {
		t.Errorf("got %v, want the underlying error rather than a partial", err)
	}
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Errorf("got %T (%v), want a NotFoundError", err, err)
	}
}

// A thread is the upward walk and then the replies X renders, and those two
// overlap: the status page for a reply shows the conversation above it as well
// as the answers below. The capture of 1903142823316049977 carries its parent
// 1903136743634723031 on the page, and the parent is also the second tweet in
// the chain, so without a dedupe the thread prints it twice. It did.
func TestAThreadSaysEachTweetOnce(t *testing.T) {
	f := &synFixtures{
		tweet: chainFixtures(t),
		page:  map[string]string{"1903142823316049977": capture(t, "status_reply.html.gz")},
	}

	// The overlap has to be real or this test passes by saying nothing.
	p, err := ParsePage(StatusPageURL("1903142823316049977"), f.page["1903142823316049977"])
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if !hasTweet(p.Postings(), "1903136743634723031") {
		t.Fatal("the page capture no longer renders the parent, so this test is not testing the overlap")
	}

	var got []string
	if err := fixtureEngine(t, f).Thread(context.Background(), "1903142823316049977", 0, func(tw *Tweet) error {
		got = append(got, tw.ID)
		return nil
	}); err != nil {
		t.Fatalf("Thread: %v", err)
	}

	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] {
			t.Errorf("%s appears twice in %v", id, got)
		}
		seen[id] = true
	}
	// Deduping must not cost the order: the ancestors still lead, root first.
	for i, id := range []string{"20", "1903136743634723031", "1903142823316049977"} {
		if i >= len(got) || got[i] != id {
			t.Fatalf("thread reads %v, want the chain first", got)
		}
	}
	if len(got) <= 3 {
		t.Errorf("thread is %v, want the rendered replies after the chain", got)
	}
}

func hasTweet(ts []*Tweet, id string) bool {
	for _, t := range ts {
		if t.ID == id {
			return true
		}
	}
	return false
}

func TestSplitFocalOnAPageIsTheSameSplitTheEngineUses(t *testing.T) {
	focal, replies := pageReplies(statusPage(t).Postings(), "20")
	if focal == nil {
		t.Fatal("no focal tweet on the status page for tweet 20")
	}
	if len(replies) == 0 {
		t.Fatal("no replies on the status page for tweet 20")
	}
	// Whatever the page order was, the replies are replies and the focal tweet
	// is not among them.
	for _, r := range replies {
		if r.ID == focal.ID {
			t.Errorf("%s is both the tweet and a reply to it", r.ID)
		}
	}
}
