package cli

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/x-cli/x"
)

// ops_test.go guards issue #18. `x mcp` listed no tools and `x serve` answered
// 404 to everything, because every command is a kit escape hatch and an escape
// hatch is a command and nothing else. The reads are now also registered as
// operations, and these tests fail if that registration goes away or starts
// taking the command line back off the hand-written commands.

func TestServeAndMCPHaveTheReads(t *testing.T) {
	got := map[string]bool{}
	for _, op := range New().Ops() {
		got[op.Meta().Name] = true
	}
	if len(got) == 0 {
		t.Fatal("no operations registered, so serve has no routes and mcp lists no tools")
	}
	// The spread matters more than the count: a single tweet read, a read that
	// takes a user, a search, a graph read, and one off the trends surface.
	for _, want := range []string{"tweet", "user", "timeline", "thread", "search", "edges", "graph", "trends"} {
		if !got[want] {
			t.Errorf("no %q operation, so neither serve nor mcp answers it", want)
		}
	}
}

func TestTheOperationsDoNotTakeTheCommandLine(t *testing.T) {
	ops := map[string]bool{}
	for _, op := range New().Ops() {
		if !op.Meta().NoCLI {
			t.Errorf("operation %q would shadow the hand-written command of the same name", op.Meta().Name)
		}
		ops[op.Meta().Name] = true
	}
	// The commands are still the hand-written ones. `x tweet` renders a curated
	// table of the fields somebody wants; the reflected operation would be
	// thirty columns with JSON in the cells.
	cmds := map[string]bool{}
	for _, group := range [][]kit.Command{
		readCommands(), entityCommands(), trendCommands(), dataCommands(),
		identityCommands(), tableCommands(), metaCommands(),
	} {
		for _, c := range group {
			cmds[cmdName(c)] = true
		}
	}
	for want := range ops {
		if !cmds[want] {
			t.Errorf("operation %q has no command of its own, so NoCLI took the verb away entirely", want)
		}
	}
}

// TestTheHostTakesTheSameOperations is the other half: in an ant host there are
// no hand-written commands, so the same set has to land on the command line.
func TestTheHostTakesTheSameOperations(t *testing.T) {
	standalone := map[string]bool{}
	for _, op := range New().Ops() {
		standalone[op.Meta().Name] = true
	}
	host := kit.New(x.Domain{}.Info().Identity)
	x.Domain{}.Register(host)
	got := map[string]bool{}
	for _, op := range host.Ops() {
		got[op.Meta().Name] = true
		if op.Meta().NoCLI {
			t.Errorf("operation %q is off the command line in a host, where the operations are the command line", op.Meta().Name)
		}
	}
	for name := range standalone {
		if !got[name] {
			t.Errorf("the host is missing %q, so the two surfaces disagree about what x reads", name)
		}
	}
}

// cmdName is the verb out of a Use string ("timeline <user> [--flags]").
func cmdName(c kit.Command) string {
	for i, r := range c.Use {
		if r == ' ' {
			return c.Use[:i]
		}
	}
	return c.Use
}
