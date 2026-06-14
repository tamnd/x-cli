package cli

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

// spinner is a minimal progress indicator for the slow network reads. It writes
// only to stderr, and only when stderr is an interactive terminal, so it never
// lands a byte on stdout — the data a command prints — and a pipeline like
// `x timeline nasa | jq` stays clean. It also waits a short beat before its
// first frame, so a command that finishes quickly never paints anything.
//
// stop() clears the line and is idempotent. A caller stops it right before the
// first row reaches stdout, so the spinner never overlaps rendered output even
// when stdout and stderr share one terminal.
type spinner struct {
	msg   string
	stopc chan struct{}
	done  chan struct{}
	once  sync.Once
	on    bool
}

// spinnerFrames is the braille dot cycle; spinnerDelay is how long a command may
// run before the spinner appears, and spinnerTick the frame interval.
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

const (
	spinnerDelay = 120 * time.Millisecond
	spinnerTick  = 100 * time.Millisecond
)

// progress starts a stderr spinner labeled msg and returns it. It paints only
// when stderr is a terminal and --quiet is off; otherwise the returned spinner
// is inert and stop() does nothing.
func (a *App) progress(msg string) *spinner {
	return newSpinner(msg, !a.quiet && isatty.IsTerminal(os.Stderr.Fd()))
}

func newSpinner(msg string, enabled bool) *spinner {
	s := &spinner{msg: msg, stopc: make(chan struct{}), done: make(chan struct{})}
	if !enabled {
		close(s.done)
		return s
	}
	s.on = true
	go s.run()
	return s
}

func (s *spinner) run() {
	defer close(s.done)
	timer := time.NewTimer(spinnerDelay)
	defer timer.Stop()
	select {
	case <-s.stopc:
		return // stopped before the delay: never paint
	case <-timer.C:
	}
	ticker := time.NewTicker(spinnerTick)
	defer ticker.Stop()
	i := 0
	paint := func() {
		fmt.Fprintf(os.Stderr, "\r%c %s", spinnerFrames[i%len(spinnerFrames)], s.msg)
		i++
	}
	paint()
	for {
		select {
		case <-s.stopc:
			return
		case <-ticker.C:
			paint()
		}
	}
}

// stop halts the spinner and clears its line. It waits for the paint goroutine
// to settle before clearing, so no stale frame survives, and is safe to call
// any number of times.
func (s *spinner) stop() {
	s.once.Do(func() {
		close(s.stopc)
		<-s.done
		if s.on {
			fmt.Fprint(os.Stderr, "\r\033[K") // carriage return, clear to end of line
		}
	})
}
