// Package execx abstracts process execution so that command construction can
// be tested without spawning real processes.
package execx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Result captures a completed captured-output execution. A non-zero exit code
// is not an error at this layer; callers decide what it means.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner executes commands. Run captures output; Interactive attaches the
// current terminal and returns the child's exit code; RunStream feeds stdin to
// the child and delivers stdout line by line while capturing stderr.
type Runner interface {
	Run(ctx context.Context, argv []string) (Result, error)
	Interactive(ctx context.Context, argv []string) (int, error)
	RunStream(ctx context.Context, argv []string, stdin []byte, onLine func(string)) (Result, error)
}

// Local runs commands on this machine.
type Local struct{}

// command builds a captured-output command in its own process group so that
// cancellation reaches grandchildren too: the transport wraps ssh (gcloud →
// ssh), and interrupting only the wrapper orphans the live connection — the
// remote runner then finishes the whole plan unmonitored (observed live,
// SPEC.md §14). Interrupt rather than SIGKILL so ssh and gcloud tear down
// cleanly; escalation happens after WaitDelay.
func command(ctx context.Context, argv []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
			return cmd.Process.Signal(os.Interrupt)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// interactiveCommand keeps the child in this process group: an interactive
// ssh needs the terminal's foreground group for tty access, and the terminal
// already delivers Ctrl-C to that whole group itself.
func interactiveCommand(ctx context.Context, argv []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func (Local) Run(ctx context.Context, argv []string) (Result, error) {
	if len(argv) == 0 {
		return Result{}, errors.New("execx: empty argv")
	}
	cmd := command(ctx, argv)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		return res, fmt.Errorf("running %s: %w", argv[0], err)
	}
	return res, nil
}

// RunStream executes argv with stdin supplied, invoking onLine for every
// stdout line as it arrives. Stderr is captured. Used for remote scripts that
// stream progress events.
func (Local) RunStream(ctx context.Context, argv []string, stdin []byte, onLine func(string)) (Result, error) {
	if len(argv) == 0 {
		return Result{}, errors.New("execx: empty argv")
	}
	cmd := command(ctx, argv)
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("running %s: %w", argv[0], err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
	scanErr := scanner.Err()
	err = cmd.Wait()
	res := Result{Stderr: stderr.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		return res, fmt.Errorf("running %s: %w", argv[0], err)
	}
	if scanErr != nil {
		return res, fmt.Errorf("reading %s output: %w", argv[0], scanErr)
	}
	return res, nil
}

func (Local) Interactive(ctx context.Context, argv []string) (int, error) {
	if len(argv) == 0 {
		return 1, errors.New("execx: empty argv")
	}
	cmd := interactiveCommand(ctx, argv)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), nil
	default:
		return 1, fmt.Errorf("running %s: %w", argv[0], err)
	}
}
