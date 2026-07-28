package embed

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// fixture reads a capture from testdata, transparently ungzipping the big ones.
//
// Every file under testdata came off the live site through x capture. None of
// them are hand-written, because a hand-written fixture only ever proves that
// the parser agrees with whoever wrote the fixture.
func fixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	var r io.Reader = f
	if filepath.Ext(name) == ".gz" {
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatalf("gunzip %s: %v", name, err)
		}
		defer gz.Close()
		r = gz
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
