package x

import (
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// capture.go writes a live response into a fixture file (spec 3003 doc 05
// section 6, doc 06 section 5.1).
//
// It is the reason no fixture in testdata is hand-written. A shape somebody
// typed out is a shape that agrees with the parser by construction and with X
// by luck, and the whole tier-0 claim in this tool rests on fixtures that are
// what X actually sent. When a surface changes, the fixture is recaptured and
// the golden diff shows what moved.
//
// The requests here are the reader's own, down to the headers, because a page
// fetched with a different Accept header is a different page.

// Recording is one captured response, on its way to a file.
type Recording struct {
	// Surface is the plane it came off, 1 through 8.
	Surface int
	// Name is the fixture file, gzipped, under the capture directory.
	Name string
	URL  string
	Body []byte
	// Err is why this surface produced nothing. A capture of a tweet asks
	// several surfaces and some of them are allowed to say no: an account with
	// no session cannot read surface 7, and that is a fact about the run rather
	// than a failure of the capture.
	Err error
}

// Written reports whether this recording has bytes to write.
func (r Recording) Written() bool { return r.Err == nil && len(r.Body) > 0 }

// Capture reads every surface that answers for a reference and returns what
// each one said. Nothing is written to disk; Save does that, so a caller can
// look at the result first.
func (e *Engine) Capture(ctx context.Context, ref string) ([]Recording, error) {
	kind, id, err := Classify(ref)
	if err != nil {
		return nil, err
	}
	recipes, err := captureRecipes(kind, id)
	if err != nil {
		return nil, err
	}
	out := make([]Recording, 0, len(recipes))
	for _, c := range recipes {
		out = append(out, e.record(ctx, c))
	}
	return out, nil
}

// recipe is one surface to record and the file it lands in. The request is the
// reader's own, built by the same function the reader calls.
type recipe struct {
	surface int
	name    string
	req     Req
}

// captureRecipes is what a capture would ask for, without asking. Keeping it
// apart from the fetching is what lets a test check the file names against the
// fixtures in testdata offline, which is the check that matters: a capture
// writing a name nothing loads refreshes nothing while looking like it worked.
func captureRecipes(kind, id string) ([]recipe, error) {
	switch kind {
	case KindTweet:
		return []recipe{
			{1, "s1_tweet_" + id + ".json.gz",
				Req{URL: synTweetURL(id), Endpoint: "syndication.tweet"}},
			{3, "s3_oembed_" + id + ".json.gz",
				Req{URL: OEmbedURL(TweetURL("i", id)), Endpoint: "oembed"}},
			{8, "status_" + id + ".html.gz",
				webPageReq(StatusPageURL(id))},
		}, nil
	case KindUser:
		handle := strings.ToLower(id)
		return []recipe{
			{2, "s2_timeline_" + handle + ".html.gz",
				Req{URL: synProfileURL(handle), Endpoint: "syndication.profile"}},
			{8, "profile_" + handle + ".html.gz",
				webPageReq(UserURL(handle))},
		}, nil
	}
	return nil, Unsupported("capturing a "+kind,
		"capture records the surfaces that serve a tweet or a profile, and nothing serves a "+kind+" on its own")
}

// record makes one request with the cache off. A capture is a recording of what
// the surface says now, and answering it out of a file this tool wrote earlier
// would make the whole exercise circular.
func (e *Engine) record(ctx context.Context, c recipe) Recording {
	rec := Recording{Surface: c.surface, Name: c.name, URL: c.req.URL}
	c.req.CacheTTL = 0
	b, err := e.c.Do(ctx, c.req)
	if err != nil {
		rec.Err = err
		return rec
	}
	rec.Body = b
	return rec
}

// Save writes one recording, gzipped, into dir. It returns the path so a caller
// can print it, because the point of the command is telling somebody which file
// to look at next.
func (r Recording) Save(dir string) (string, error) {
	if !r.Written() {
		return "", fmt.Errorf("nothing to save for %s", r.Name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, r.Name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	if _, err := gz.Write(r.Body); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	// Close for real here rather than leaving it to the defer: a fixture that
	// lost its last block on a full disk should be an error, not a file the
	// next test run reads as the truth.
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}
