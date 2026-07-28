//go:build live

package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// baseline_live_test.go is spec 3003 doc 06 section 5.6: the acceptance
// criterion from doc 00 section 11, run as a test.
//
//	go test -tags live ./cli/
//
// It builds the binary and runs it, rather than calling the engine, because the
// criterion is about what a person gets when they type the command. Exit codes,
// the tier printed on the record, and the wording of the refusal are all things
// only the command line has.
//
// HOME is a fresh empty directory for every case, not --data-dir. That is
// deliberate and it is the whole reason this test is worth its runtime: the
// defect in doc 06 section 4 was that the session store was read from the home
// directory whatever --data-dir said, so a run the caller believed was anonymous
// used their cookies and reported capabilities they did not have. A test that
// isolated with the flag would have passed against the bug.
func TestLiveAnonymousBaseline(t *testing.T) {
	bin := buildX(t)
	for _, c := range []struct {
		name string
		args []string
		want int
		is   func(*testing.T, string)
	}{
		{
			name: "a tweet",
			args: []string{"tweet", "20", "--tier", "0", "-o", "json"},
			want: 0,
			is: func(t *testing.T, out string) {
				r := decodeRecord(t, out)
				wantTier(t, r, 0)
				if r["text"] != "just setting up my twttr" {
					t.Errorf("tweet 20 came back as %q", r["text"])
				}
			},
		},
		{
			name: "a profile",
			args: []string{"user", "jack", "--tier", "0", "-o", "json"},
			want: 0,
			is: func(t *testing.T, out string) {
				r := decodeRecord(t, out)
				wantTier(t, r, 0)
				if r["username"] != "jack" {
					t.Errorf("asked for @jack and got %v", r["username"])
				}
				if r["description"] == nil {
					t.Error("no bio on an anonymous profile read")
				}
			},
		},
		{
			name: "a timeline, marked for what it is",
			args: []string{"timeline", "jack", "--tier", "0", "-n", "5", "-o", "json"},
			want: 0,
			is: func(t *testing.T, out string) {
				rows := decodeRows(t, out)
				if len(rows) == 0 {
					t.Fatal("no rows")
				}
				for _, r := range rows {
					wantTier(t, r, 0)
					if r["id"] == nil || r["text"] == nil {
						t.Errorf("an empty row: %v", r)
					}
				}
				// Sample or not is X's call, not ours. What the criterion asks
				// is that the record says which, so the flag being absent is
				// only a pass when the rows really were walked.
				t.Logf("%d rows, sample=%v", len(rows), rows[0]["sample"])
			},
		},
		{
			name: "search, which is the one that has to refuse",
			args: []string{"search", "golang"},
			want: 4, // NeedAuth
			is: func(t *testing.T, out string) {
				// The refusal has to name the tier the reader needs, because
				// "not available" sends somebody looking for a bug and "needs
				// tier 2" sends them to `x auth import`.
				if !strings.Contains(strings.ToLower(out), "tier 2") {
					t.Errorf("the refusal does not name tier 2: %s", out)
				}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, code := runX(t, bin, c.args...)
			if code != c.want {
				t.Fatalf("exit %d, wanted %d\nstdout: %s\nstderr: %s", code, c.want, stdout, stderr)
			}
			// The two streams are checked apart, not together. A warning on
			// stdout would be a record a pipe cannot parse, and X hands out
			// rate-limit warnings often enough that reading the run's output as
			// one blob would have quietly hidden that.
			out := stdout
			if c.want != 0 {
				out = stderr
			}
			c.is(t, out)
		})
	}
}

// buildX builds the binary once for the whole matrix, into the test's own
// temporary directory. Never into the working tree.
func buildX(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "x")
	out, err := exec.Command("go", "build", "-o", bin, "../cmd/x").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// runX runs the binary with a home directory that has nothing in it, and an
// environment with no session in it either, so the only thing that can make a
// read succeed is the read.
func runX(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	home := t.TempDir()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"X_AUTH_TOKEN=", "X_CT0=", "X_ALLOW_GUEST=", "X_DATA_DIR=",
	)
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running x: %v\n%s", err, stderr.String())
	}
	return stdout.String(), stderr.String(), code
}

// decodeRecord takes the one record a single-subject read emits. `-o json`
// frames it as a one-element array, because the renderer does not have a
// separate shape for "exactly one", and a caller piping into jq would rather
// have one shape than two.
func decodeRecord(t *testing.T, out string) map[string]any {
	t.Helper()
	rows := decodeRows(t, out)
	if len(rows) != 1 {
		t.Fatalf("wanted one record and got %d:\n%s", len(rows), out)
	}
	return rows[0]
}

// decodeRows takes either a JSON array or a stream of one object per line,
// because which one a command emits is the command's business and this test is
// asking about the records, not the framing.
func decodeRows(t *testing.T, out string) []map[string]any {
	t.Helper()
	out = strings.TrimSpace(out)
	if strings.HasPrefix(out, "[") {
		var rows []map[string]any
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			t.Fatalf("not a JSON array: %v\n%s", err, out)
		}
		return rows
	}
	var rows []map[string]any
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("not a JSON record: %v\n%s", err, line)
		}
		rows = append(rows, r)
	}
	return rows
}

func wantTier(t *testing.T, r map[string]any, want float64) {
	t.Helper()
	if got, ok := r["tier"].(float64); !ok || got != want {
		t.Errorf("record says tier %v, wanted %v", r["tier"], want)
	}
}
