package cli

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// A byte command writes what X sent. The record flags have nothing to act on
// there, and taking them silently would be the tool agreeing with a question it
// did not answer, so they are a usage error (spec 3003 doc 05 section 1).
func TestRawOutputRejectsTheRecordFlags(t *testing.T) {
	for _, c := range []struct {
		name string
		out  kit.OutputOptions
		want int // the exit code, 0 for accepted
	}{
		{name: "no flags", want: 0},
		{name: "auto is not a choice the user made", out: kit.OutputOptions{Format: "auto"}},
		{name: "a tty makes no difference", out: kit.OutputOptions{IsTTY: true}},
		{name: "-o json", out: kit.OutputOptions{Format: "json"}, want: 2},
		{name: "-o table", out: kit.OutputOptions{Format: "table"}, want: 2},
		{name: "--template", out: kit.OutputOptions{Template: "{{.text}}"}, want: 2},
		{name: "--fields", out: kit.OutputOptions{Fields: []string{"text"}}, want: 2},
	} {
		a := &App{st: &kit.State{Output: c.out}}
		err := a.rawOutput("embed")
		if got := errs.ExitCode(err); got != c.want {
			t.Errorf("%s: exit %d, want %d (err %v)", c.name, got, c.want, err)
		}
	}
}

// The defensive fallback has no resolved state, and a command that still runs
// should write its bytes rather than refuse.
func TestRawOutputWithNoState(t *testing.T) {
	if err := (&App{}).rawOutput("embed"); err != nil {
		t.Errorf("rawOutput with no state = %v, want nil", err)
	}
}
