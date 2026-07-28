package x

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestACaptureLandsOnTheFixtureItRefreshes is the point of the command. `x
// capture 20` is supposed to rewrite the fixture the tests already read, and a
// capture that writes s8_status_20.html.gz next to a test loading
// status_20.html.gz refreshes nothing while looking like it worked.
func TestACaptureLandsOnTheFixtureItRefreshes(t *testing.T) {
	names := map[string]bool{}
	for _, kind := range []struct{ kind, id string }{{KindTweet, "20"}, {KindUser, "jack"}} {
		recipes, err := captureRecipes(kind.kind, kind.id)
		if err != nil {
			t.Fatalf("recipes for %s: %v", kind.kind, err)
		}
		for _, c := range recipes {
			names[c.name] = true
		}
	}
	for _, want := range []string{
		"s1_tweet_20.json.gz",
		"s3_oembed_20.json.gz",
		"status_20.html.gz",
		"s2_timeline_jack.html.gz",
		"profile_jack.html.gz",
	} {
		if !names[want] {
			t.Errorf("no capture writes %s (got %v)", want, names)
		}
		if _, err := os.Stat(filepath.Join("testdata", want)); err != nil {
			t.Errorf("%s is not a fixture in testdata, so the name is drifting: %v", want, err)
		}
	}
}

// TestACaptureAsksForWhatTheReaderAsksFor guards the other half. A page fetched
// with different headers is a different page, so the capture sends the reader's
// own request rather than one that looks close enough.
func TestACaptureAsksForWhatTheReaderAsksFor(t *testing.T) {
	recipes, err := captureRecipes(KindTweet, "20")
	if err != nil {
		t.Fatalf("recipes: %v", err)
	}
	var syn, web string
	for _, c := range recipes {
		switch c.surface {
		case 1:
			syn = c.req.URL
		case 8:
			web = c.req.URL
		}
	}
	if syn != synTweetURL("20") {
		t.Errorf("surface 1 capture asks for %q, the reader asks for %q", syn, synTweetURL("20"))
	}
	if web != StatusPageURL("20") {
		t.Errorf("surface 8 capture asks for %q, the reader asks for %q", web, StatusPageURL("20"))
	}
}

func TestCaptureRefusesAKindNothingServes(t *testing.T) {
	_, err := captureRecipes(KindHashtag, "golang")
	if err == nil {
		t.Fatal("capturing a hashtag should be unsupported, not silently empty")
	}
	if !strings.Contains(err.Error(), "hashtag") {
		t.Errorf("the error should name the kind, got %q", err)
	}
}

func TestSaveWritesGzipTheLoaderCanRead(t *testing.T) {
	dir := t.TempDir()
	r := Recording{Surface: 1, Name: "s1_tweet_20.json.gz", Body: []byte(`{"id_str":"20"}`)}
	path, err := r.Save(dir)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("the fixture is not gzip, so no test can read it: %v", err)
	}
	got, _ := io.ReadAll(gz)
	if !bytes.Equal(got, r.Body) {
		t.Errorf("round trip changed the bytes: %q", got)
	}
}

// TestNothingToSaveIsNotAnEmptyFile: a surface that refused is a row saying so,
// never a zero-byte fixture that the next test run would take for the truth.
func TestNothingToSaveIsNotAnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	r := Recording{Surface: 2, Name: "s2_timeline_jack.html.gz", Err: io.EOF}
	if _, err := r.Save(dir); err == nil {
		t.Fatal("saving a failed recording should be an error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("a failed recording wrote %d files", len(entries))
	}
}
