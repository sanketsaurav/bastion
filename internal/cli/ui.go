package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
)

// ui renders command output. Data goes to out; progress and diagnostics go to
// err so that --json keeps stdout machine-clean.
type ui struct {
	out, err io.Writer
	json     bool
	color    bool
	verbose  bool
}

func (a *App) ui() *ui {
	color := !a.flags.noColor && a.getenv("NO_COLOR") == "" && isTerminal(a.stdout)
	return &ui{out: a.stdout, err: a.stderr, json: a.flags.json, color: color, verbose: a.flags.verbose}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (u *ui) paint(code, s string) string {
	if !u.color {
		return s
	}
	return code + s + ansiReset
}

func (u *ui) statusPaint(status string) string {
	switch status {
	case "RUNNING":
		return u.paint(ansiGreen, status)
	case "TERMINATED", "SUSPENDED":
		return u.paint(ansiYellow, status)
	default:
		return u.paint(ansiYellow, status)
	}
}

// emit writes v as indented JSON to stdout.
func (u *ui) emit(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(u.out, string(data))
	return nil
}

// printKV renders aligned key/value rows to stdout.
func (u *ui) printKV(rows [][2]string) {
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		fmt.Fprintf(u.out, "%-*s  %s\n", width+1, r[0]+":", r[1])
	}
}

// progressf reports command progress on stderr.
func (u *ui) progressf(format string, args ...any) {
	fmt.Fprintf(u.err, format+"\n", args...)
}

// spinnerFrames animate in-progress steps on a terminal.
var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner is one in-progress step: on a terminal it animates in place
// (with elapsed time once a step drags) and resolves to a final line;
// elsewhere it degrades to plain lines so logs and --verbose streams stay
// append-only. Methods are nil-safe so call sites can hold an optional one.
type spinner struct {
	u        *ui
	label    string
	start    time.Time
	stop     chan struct{}
	wg       sync.WaitGroup
	once     sync.Once
	animated bool
}

// spinAnimated reports whether steps animate for this ui.
func (u *ui) spinAnimated() bool { return !u.json && !u.verbose && isTerminal(u.err) }

// spin starts a step. label is what animates; static is printed instead on
// non-terminals ("" = print nothing there).
func (u *ui) spin(label, static string) *spinner {
	s := &spinner{u: u, label: label, start: time.Now(), stop: make(chan struct{}), animated: u.spinAnimated()}
	if !s.animated {
		if static != "" {
			u.progressf("%s", static)
		}
		return s
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for i := 0; ; i++ {
			select {
			case <-s.stop:
				return
			case <-tick.C:
				elapsed := ""
				if d := time.Since(s.start); d >= 3*time.Second {
					elapsed = fmt.Sprintf(" (%ds)", int(d.Seconds()))
				}
				fmt.Fprintf(s.u.err, "\r\x1b[2K%s %s…%s",
					s.u.paint(ansiDim, spinnerFrames[i%len(spinnerFrames)]), s.label, elapsed)
			}
		}
	}()
	return s
}

// done resolves the step: the animated line is replaced by final (or erased
// when final is ""); on non-terminals final is printed as-is.
func (s *spinner) done(final string) {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if s.animated {
			close(s.stop)
			s.wg.Wait()
			fmt.Fprint(s.u.err, "\r\x1b[2K")
		}
		if final != "" {
			fmt.Fprintln(s.u.err, final)
		}
	})
}

func (s *spinner) ok(text string) {
	if s != nil {
		s.done(s.u.paint(ansiGreen, "✓") + " " + text)
	}
}

func (s *spinner) fail(text string) {
	if s != nil {
		s.done(s.u.paint(ansiRed, "✗") + " " + text)
	}
}

func (s *spinner) erase() { s.done("") }

// debugf reports verbose-only detail on stderr.
func (u *ui) debugf(format string, args ...any) {
	if u.verbose {
		fmt.Fprintf(u.err, format+"\n", args...)
	}
}
