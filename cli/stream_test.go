package cli

import (
	"strings"
	"testing"

	"github.com/tamnd/x-cli/x"
)

// The sample warning tells you how to get a walk in time order instead. The
// advice is only advice if it names something the caller has not already done:
// `x replies jack --guest` routes to tier 0 on purpose, so it prints this
// warning with the guest tier already on, and "pass --guest" there is the tool
// answering a question nobody asked.
func TestTheSampleWarningDoesNotAdviseAFlagThatIsAlreadySet(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  x.Config
		want string
	}{
		{"tier 0", x.Config{}, "pass --guest"},
		{"--guest", x.Config{AllowGuest: true}, "x auth import"},
		{"--tier guest", x.Config{Tier: "guest"}, "x auth import"},
		{"session", x.Config{AuthToken: "a", CT0: "b"}, "no deeper read"},
		{"--tier syndication", x.Config{Tier: "syndication"}, "drop --tier syndication"},
		{"--tier web", x.Config{Tier: "web"}, "drop --tier web"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := (&App{cfg: c.cfg}).sampleFix()
			if !strings.Contains(got, c.want) {
				t.Errorf("got %q, want it to mention %q", got, c.want)
			}
			if c.name != "tier 0" && strings.Contains(got, "--guest") {
				t.Errorf("got %q, which tells a caller past tier 0 to pass --guest", got)
			}
		})
	}
}

// A session beats a forced low tier in the config but not on the command line:
// somebody who said --tier syndication asked for tier 0 and gets told how to
// unask, not how to log in again.
func TestAForcedTierOwnsTheAdvice(t *testing.T) {
	got := (&App{cfg: x.Config{AuthToken: "a", CT0: "b", Tier: "syndication"}}).sampleFix()
	if !strings.Contains(got, "drop --tier syndication") {
		t.Errorf("got %q, want the forced tier named", got)
	}
}
