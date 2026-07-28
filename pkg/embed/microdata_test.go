package embed

import (
	"testing"
)

func TestMicrodataSimple(t *testing.T) {
	const doc = `<div itemscope itemtype="https://schema.org/Person">
		<meta itemprop="name" content="jack">
		<a itemprop="url" href="https://x.com/jack">link</a>
		<span itemprop="note">  spaced  </span>
		<div itemprop="address" itemscope itemtype="https://schema.org/PostalAddress">
			<meta itemprop="addressLocality" content="somewhere">
		</div>
	</div>`
	items, err := Microdata(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d top items, want 1", len(items))
	}
	p := items[0]
	if p.Type != "Person" {
		t.Errorf("type = %q, want Person", p.Type)
	}
	if got := p.Str("name"); got != "jack" {
		t.Errorf("name = %q", got)
	}
	if got := p.Str("url"); got != "https://x.com/jack" {
		t.Errorf("url = %q, want the href", got)
	}
	if got := p.Str("note"); got != "spaced" {
		t.Errorf("note = %q, want the trimmed text", got)
	}
	addr := p.Item("address")
	if addr == nil || addr.Str("addressLocality") != "somewhere" {
		t.Errorf("nested address did not come through: %#v", addr)
	}
}

// A scope without an itemprop is its own item even when it sits inside
// another one, which is how X marks the focal tweet of a status page.
func TestMicrodataNestedScopeWithoutProp(t *testing.T) {
	const doc = `<div itemscope itemtype="https://schema.org/Collection">
		<meta itemprop="name" content="outer">
		<article itemscope itemtype="https://schema.org/SocialMediaPosting">
			<meta itemprop="identifier" content="20">
		</article>
	</div>`
	items, _ := Microdata(doc)
	if len(items) != 2 {
		t.Fatalf("got %d top items, want 2", len(items))
	}
	if _, ok := items[0].Props["identifier"]; ok {
		t.Error("the inner posting was absorbed into the collection")
	}
	if got := items[1].Str("identifier"); got != "20" {
		t.Errorf("inner identifier = %q, want 20", got)
	}
}

// An itemprop scope with nothing above it is still real data. The profile
// timeline arrives this way, streamed in after the page element closed.
func TestMicrodataOrphanProp(t *testing.T) {
	const doc = `<article itemscope itemprop="hasPart" itemtype="https://schema.org/SocialMediaPosting">
		<meta itemprop="identifier" content="7">
	</article>`
	items, _ := Microdata(doc)
	if len(items) != 1 {
		t.Fatalf("got %d top items, want 1", len(items))
	}
	if got := items[0].Str("identifier"); got != "7" {
		t.Errorf("identifier = %q", got)
	}
}

func TestMicrodataCounter(t *testing.T) {
	const doc = `<article itemscope itemtype="https://schema.org/SocialMediaPosting">
		<div itemprop="interactionStatistic" itemscope itemtype="https://schema.org/InteractionCounter">
			<meta itemprop="interactionType" content="https://schema.org/LikeAction">
			<meta itemprop="userInteractionCount" content="12">
		</div>
	</article>`
	items, _ := Microdata(doc)
	got, ok := items[0].Counter("interactionStatistic", "LikeAction")
	if !ok || got != "12" {
		t.Errorf("likes = %q %v, want 12 true", got, ok)
	}
	// A counter X did not send is absent, not zero. Doc 03 keeps that
	// distinction all the way out to the JSON.
	if _, ok := items[0].Counter("interactionStatistic", "ViewAction"); ok {
		t.Error("a missing counter reported as present")
	}
}

// The status page, captured live on 2026-07-28. Four tweets, the focal one
// plus three replies, with every engagement number and no credential.
func TestMicrodataStatusPage(t *testing.T) {
	items, err := Microdata(fixture(t, "sp2.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	posts := Find(items, "SocialMediaPosting")
	if len(posts) != 4 {
		t.Fatalf("got %d postings, want 4", len(posts))
	}

	var focal *Item
	for _, p := range posts {
		if p.Str("identifier") == "20" {
			focal = p
		}
	}
	if focal == nil {
		t.Fatal("the focal tweet is not in the microdata")
	}
	if got := focal.Str("articleBody"); got != "just setting up my twttr" {
		t.Errorf("text = %q", got)
	}
	if got := focal.Str("commentCount"); got != "17964" {
		t.Errorf("replies = %q, want 17964", got)
	}
	if got := focal.Str("dateCreated"); got != "2006-03-21T20:50:14.000Z" {
		t.Errorf("dateCreated = %q", got)
	}
	for _, c := range []struct{ action, want string }{
		{"LikeAction", "307403"},
		{"ShareAction", "124855"},
	} {
		got, ok := focal.Counter("interactionStatistic", c.action)
		if !ok || got != c.want {
			t.Errorf("%s = %q %v, want %s", c.action, got, ok, c.want)
		}
	}
	// X does not report views for a tweet this old, and the Relay store on
	// the same page agrees, so absent is the honest answer here.
	if _, ok := focal.Counter("interactionStatistic", "ViewAction"); ok {
		t.Error("views reported for tweet 20, which X leaves null")
	}

	author := focal.Item("author")
	if author == nil {
		t.Fatal("no author on the focal tweet")
	}
	if got := author.Str("alternateName"); got != "jack" {
		t.Errorf("author handle = %q", got)
	}
	if got := author.Str("identifier"); got != "12" {
		t.Errorf("author id = %q, want 12", got)
	}
	if got, ok := author.Counter("interactionStatistic", "FollowAction"); !ok || got != "10548148" {
		t.Errorf("followers = %q %v", got, ok)
	}
}

// The profile page streams its timeline in late, so this is the test that
// fails if the orphan handling regresses.
func TestMicrodataProfilePage(t *testing.T) {
	items, err := Microdata(fixture(t, "prof.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(Find(items, "ProfilePage")) != 1 {
		t.Error("no ProfilePage item")
	}
	posts := Find(items, "SocialMediaPosting")
	if len(posts) != 9 {
		t.Fatalf("got %d timeline postings, want 9", len(posts))
	}
	for _, p := range posts {
		if p.Str("identifier") == "" {
			t.Errorf("posting with no identifier: %#v", p.Props)
		}
		if p.Item("author") == nil {
			t.Errorf("posting %s has no author", p.Str("identifier"))
		}
	}
}
