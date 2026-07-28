package x

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// doctor.go probes each surface live and reports what answers today (spec 3003
// doc 05, `x doctor`).
//
// It probes the routes rather than the features, which is why it works at M0
// with half the commands still unbuilt: the question it answers is whether the
// surface is up and what it costs, not whether we have got round to using it.
// Every claim in doc 01 was measured once, in November, by hand. This is the
// same measurement, on demand, by the tool.

// Probe is one surface's answer.
type Probe struct {
	Surface int    `json:"surface"`
	Name    string `json:"name"`
	Status  string `json:"status"` // ok, skip, or fail
	Millis  int    `json:"millis,omitempty"`
	Note    string `json:"note,omitempty"`
	Err     string `json:"error,omitempty"`
}

const (
	probeOK   = "ok"
	probeSkip = "skip"
	probeFail = "fail"
)

// Doctor probes every surface in order and returns one row each. It never
// returns an error: a surface that is down is the answer, not a failure of the
// command.
//
// Surface 2 is the expensive one. It is 30 requests per 15 minutes for the whole
// syndication timeline budget, and the probe spends one of them, which is worth
// saying out loud rather than hiding in a comment.
func (e *Engine) Doctor(ctx context.Context) []Probe {
	out := make([]Probe, 0, len(Surfaces))

	// Surface 8 goes first, because its answer feeds two later probes: the
	// deploy sha it reports, and a pbs.twimg.com URL to test surface 6 with.
	page, pageProbe := e.probePage(ctx)

	out = append(out, e.probeSyndicationTweet(ctx))
	out = append(out, e.probeSyndicationTimeline(ctx))
	out = append(out, e.probeOEmbed(ctx))
	out = append(out, e.probeGuestGraphQL(ctx))
	out = append(out, e.probeGuestV11(ctx))
	out = append(out, e.probeMedia(ctx, page))
	out = append(out, e.probeSessionGraphQL(ctx))
	out = append(out, pageProbe)
	return out
}

// probeTweet is the first tweet on X, which is public, ancient, and never going
// to be deleted. Every probe that needs an id uses it.
const probeTweet = "20"

// probeHandle is the account that posted it.
const probeHandle = "jack"

func (e *Engine) probeSyndicationTweet(ctx context.Context) Probe {
	p := Probe{Surface: 1, Name: "syndication tweet"}
	start := time.Now()
	t, err := TweetByID(ctx, e.c, probeTweet)
	p.Millis = msSince(start)
	switch {
	case err != nil:
		p.Status, p.Err = probeFail, err.Error()
	case t == nil || t.ID == "":
		p.Status, p.Err = probeFail, "answered without a tweet in it"
	default:
		p.Status = probeOK
		p.Note = e.limitNote("syndication.tweet")
	}
	return p
}

func (e *Engine) probeSyndicationTimeline(ctx context.Context) Probe {
	p := Probe{Surface: 2, Name: "syndication timeline"}
	start := time.Now()
	tweets, err := ProfileTimeline(ctx, e.c, probeHandle, 1)
	p.Millis = msSince(start)
	switch {
	case err != nil:
		p.Status, p.Err = probeFail, err.Error()
	case len(tweets) == 0:
		p.Status, p.Err = probeFail, "answered with an empty timeline"
	default:
		p.Status = probeOK
		p.Note = joinNotes(fmt.Sprintf("%d tweets", len(tweets)), e.limitNote("syndication.profile"))
	}
	return p
}

func (e *Engine) probeOEmbed(ctx context.Context) Probe {
	p := Probe{Surface: 3, Name: "oembed"}
	start := time.Now()
	o, err := FetchOEmbed(ctx, e.c, probeTweet)
	p.Millis = msSince(start)
	switch {
	case err != nil:
		p.Status, p.Err = probeFail, err.Error()
	case !strings.Contains(o.HTML, "blockquote"):
		p.Status, p.Err = probeFail, "answered without the embed html"
	case o.Text == "" || o.Handle == "":
		// The bytes arrived and plane F got nothing out of them, which is the
		// shape change worth hearing about before a read quietly comes back thin.
		p.Status, p.Err = probeFail, "the blockquote parsed to no text or no author"
	default:
		p.Status = probeOK
		p.Note = joinNotes("blockquote html, @"+o.Handle, e.limitNote("oembed"))
	}
	return p
}

func (e *Engine) probeGuestGraphQL(ctx context.Context) Probe {
	p := Probe{Surface: 4, Name: "guest graphql"}
	if !e.canGraphQL() {
		p.Status, p.Note = probeSkip, "no guest tier: pass --guest"
		return p
	}
	if e.cfg.HasSession() {
		p.Status, p.Note = probeSkip, "a session is configured, so this reads as surface 7"
		return p
	}
	start := time.Now()
	t, err := e.g.TweetByID(ctx, probeTweet)
	p.Millis = msSince(start)
	switch {
	case err != nil:
		p.Status, p.Err = probeFail, err.Error()
		if isTierWall(err) {
			p.Note = "TweetResultByRestId left the guest allowlist"
		}
	case t == nil || t.ID == "":
		p.Status, p.Err = probeFail, "answered without a tweet in it"
	default:
		p.Status = probeOK
		p.Note = joinNotes("TweetResultByRestId", e.limitNote("graphql.TweetResultByRestId"))
	}
	return p
}

// probeGuestV11 tests surface 5 the way the readers use it: the public web
// bearer and no guest token. Attaching one is what the name of this surface used
// to say, and it is what cuts the budget from 180 requests per fifteen minutes
// to 15, so the probe would be measuring a route x deliberately does not take.
func (e *Engine) probeGuestV11(ctx context.Context) Probe {
	p := Probe{Surface: 5, Name: "app-only v1.1"}
	start := time.Now()
	b, err := e.c.Do(ctx, Req{
		URL:      trendsAvailableURL,
		Endpoint: "v11.trends.available",
		Header:   appHeaders(),
		CacheTTL: ttlPlaces,
	})
	p.Millis = msSince(start)
	switch {
	case err != nil:
		p.Status, p.Err = probeFail, err.Error()
	case !strings.HasPrefix(strings.TrimSpace(string(b)), "["):
		p.Status, p.Err = probeFail, "answered with something that is not the place list"
	default:
		p.Status = probeOK
		p.Note = joinNotes("trends/available", e.limitNote("v11.trends.available"))
	}
	return p
}

// probeMedia tests the CDN with whatever image the status page pointed at, so
// the probe is a URL X published rather than one this file guessed.
func (e *Engine) probeMedia(ctx context.Context, page *Page) Probe {
	p := Probe{Surface: 6, Name: "media cdn"}
	u := ""
	if page != nil && page.Head != nil {
		u = page.Head.Property["og:image"]
	}
	if u == "" {
		p.Status, p.Note = probeSkip, "surface 8 gave no image url to test with"
		return p
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		p.Status, p.Err = probeFail, err.Error()
		return p
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Range", "bytes=0-0") // a byte is enough to know it is there
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	p.Millis = msSince(start)
	if err != nil {
		p.Status, p.Err = probeFail, err.Error()
		return p
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		p.Status, p.Err = probeFail, fmt.Sprintf("http %d", resp.StatusCode)
		return p
	}
	p.Status = probeOK
	p.Note = hostOf(u)
	return p
}

func (e *Engine) probeSessionGraphQL(ctx context.Context) Probe {
	p := Probe{Surface: 7, Name: "session graphql"}
	if !e.cfg.HasSession() {
		p.Status, p.Note = probeSkip, "no session configured"
		return p
	}
	start := time.Now()
	u, err := e.g.UserByName(ctx, probeHandle)
	p.Millis = msSince(start)
	switch {
	case err != nil:
		p.Status, p.Err = probeFail, err.Error()
	case u == nil || u.Username == "":
		p.Status, p.Err = probeFail, "answered without a profile in it"
	default:
		p.Status = probeOK
		p.Note = joinNotes("UserByScreenName", e.limitNote("graphql.UserByScreenName"))
	}
	return p
}

// probePage reads the status page and reports the deploy sha, which is the one
// request that answers "did they ship something" when a payload shape changes.
func (e *Engine) probePage(ctx context.Context) (*Page, Probe) {
	p := Probe{Surface: 8, Name: "x.com html"}
	start := time.Now()
	page, err := e.c.FetchPage(ctx, StatusPageURL(probeTweet))
	p.Millis = msSince(start)
	if err != nil {
		p.Status, p.Err = probeFail, err.Error()
		return nil, p
	}
	p.Status = probeOK
	planes := []string{}
	if page.Store != nil {
		planes = append(planes, "relay store")
	}
	if len(page.Items) > 0 {
		planes = append(planes, fmt.Sprintf("%d microdata items", len(page.Items)))
	}
	note := strings.Join(planes, ", ")
	if v := appVersion(page); v != "" {
		note = joinNotes("app-version "+v, note)
	}
	p.Note = note
	return page, p
}

// appVersion pulls the deploy sha out of the page head.
func appVersion(p *Page) string {
	if p == nil || p.Head == nil {
		return ""
	}
	for _, k := range []string{"app-version", "twitter:app-version"} {
		if v := p.Head.Name[k]; v != "" {
			return v
		}
		if v := p.Head.Property[k]; v != "" {
			return v
		}
	}
	return ""
}

// limitNote turns a bucket into the "30 per 15m, 8 left" half of a probe line.
// An endpoint X publishes no headers for says so, because "no rate headers" and
// "we have not asked yet" are different facts.
func (e *Engine) limitNote(endpoint string) string {
	for _, b := range e.c.Buckets() {
		if b.Endpoint != endpoint {
			continue
		}
		if b.Limit == 0 {
			return fmt.Sprintf("%d left", b.Remaining)
		}
		return fmt.Sprintf("%d of %d left", b.Remaining, b.Limit)
	}
	return "no rate headers"
}

func isTierWall(err error) bool {
	var na *NeedAuthError
	return errors.As(err, &na)
}

func joinNotes(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, ", ")
}

func msSince(t time.Time) int { return int(time.Since(t).Milliseconds()) }
