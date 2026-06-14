package x_test

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/x-cli/x"
)

// These tests exercise the kit driver wiring without any network: the blank
// import registers the domain, and Mint/Links/Resolve are pure string and
// reflection work over the registry.

func TestDomainInfo(t *testing.T) {
	info := x.Domain{}.Info()
	if info.Scheme != "x" {
		t.Errorf("scheme = %q, want x", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != "x.com" {
		t.Errorf("hosts = %v, want x.com first", info.Hosts)
	}
}

func TestClassifyLocate(t *testing.T) {
	d := x.Domain{}

	typ, id, err := d.Classify("https://x.com/jack/status/20")
	if err != nil || typ != "status" || id != "20" {
		t.Fatalf("Classify(status) = %q/%q/%v", typ, id, err)
	}
	loc, err := d.Locate("status", "20")
	if err != nil || loc != x.TweetURL("", "20") {
		t.Errorf("Locate(status) = %q/%v", loc, err)
	}

	typ, id, err = d.Classify("https://x.com/jack")
	if err != nil || typ != "user" || id != "jack" {
		t.Fatalf("Classify(user) = %q/%q/%v", typ, id, err)
	}
	loc, err = d.Locate("user", "jack")
	if err != nil || loc != x.UserURL("jack") {
		t.Errorf("Locate(user) = %q/%v", loc, err)
	}

	if _, _, err := d.Classify("  "); err == nil {
		t.Error("Classify(blank) = nil error, want error")
	}
}

func TestHostMintLinksResolve(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Domain("x"); !ok {
		t.Fatal("x not mounted on host")
	}

	tw := &x.Tweet{
		ID:             "1500",
		ConversationID: "1490",
		ReplyTo:        "1490",
		ReplyToUser:    "12",
	}
	minted, err := h.Mint(tw)
	if err != nil || minted.String() != "x://status/1500" {
		t.Errorf("Mint = %q/%v", minted.String(), err)
	}

	got := map[string]bool{}
	for _, u := range h.Links(tw) {
		got[u.String()] = true
	}
	for _, want := range []string{"x://status/1490", "x://user/12"} {
		if !got[want] {
			t.Errorf("Links missing %q (got %v)", want, got)
		}
	}

	usr := &x.User{ID: "12", Username: "jack", PinnedTweet: "1500"}
	mu, err := h.Mint(usr)
	if err != nil || mu.String() != "x://user/12" {
		t.Errorf("Mint(user) = %q/%v", mu.String(), err)
	}

	// A bare numeric resolves to a tweet; a @handle URL to a user.
	u, err := h.ResolveOn("x", "20")
	if err != nil || u.String() != "x://status/20" {
		t.Errorf("ResolveOn(bare) = %q/%v", u.String(), err)
	}
	u, err = h.Resolve("https://twitter.com/jack")
	if err != nil || u.String() != "x://user/jack" {
		t.Errorf("Resolve(url) = %q/%v", u.String(), err)
	}
}
