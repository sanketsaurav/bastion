package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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

// debugf reports verbose-only detail on stderr.
func (u *ui) debugf(format string, args ...any) {
	if u.verbose {
		fmt.Fprintf(u.err, format+"\n", args...)
	}
}
