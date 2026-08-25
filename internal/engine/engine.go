package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sanketsaurav/bastion/internal/execx"
)

// Session is how the engine reaches a box: pipe a generated program to
// `bash -s` and stream its stdout. *gcp.Client satisfies it.
type Session interface {
	RunScript(ctx context.Context, script []byte, onLine func(string)) (execx.Result, error)
}

// Inspect runs the read-only inspection program and parses observed facts.
// The raw lines are returned for --verbose diagnostics.
func Inspect(ctx context.Context, s Session, in *Input) (*Facts, []string, error) {
	script := GenInspectScript(in)
	var lines []string
	res, err := s.RunScript(ctx, script, func(l string) { lines = append(lines, l) })
	if err != nil {
		return nil, lines, err
	}
	if res.ExitCode != 0 {
		return nil, lines, fmt.Errorf("guest inspection failed (exit %d): %s", res.ExitCode, stderrTail(res.Stderr))
	}
	facts, err := ParseFacts(lines)
	if err != nil {
		return nil, lines, err
	}
	return facts, lines, nil
}

// ApplyPlan generates and runs the apply program for a plan, streaming
// progress through hooks and interpreting the run's outcome.
func ApplyPlan(ctx context.Context, s Session, in *Input, plan *Plan, opts ApplyOptions, hooks ApplyHooks) (*ApplyResult, error) {
	script, err := GenApplyScript(in, plan, opts)
	if err != nil {
		return nil, err
	}
	parser := newEventParser(plan, hooks)
	res, runErr := s.RunScript(ctx, script, parser.line)
	result := &parser.result
	if ctx.Err() != nil {
		return result, fmt.Errorf("apply interrupted; completed actions are recorded on the box, the in-flight action may be partially applied, and the remote runner can keep running until the dropped connection is noticed — resume with `bastion plan %s`, then `bastion apply %s`", in.BoxID, in.BoxID)
	}
	if runErr != nil {
		return result, runErr
	}
	switch {
	case result.LockHeld:
		held := "another apply holds the remote lock for this box"
		if result.LockAgeSecs > 0 {
			held = fmt.Sprintf("another apply has held the remote lock for this box for %s",
				(time.Duration(result.LockAgeSecs) * time.Second).Round(time.Second))
		}
		return result, fmt.Errorf("%s; if it is still running, wait and retry — if it was interrupted, remove the lock with `bastion exec %s -- rmdir .cache/bastion/locks/%s` (stale locks are taken over after an hour)", held, in.BoxID, in.BoxID)
	case result.Failed != "":
		msg := fmt.Sprintf("action %q failed", result.Failed)
		if len(result.FailedLogs) > 0 {
			tail := result.FailedLogs
			if len(tail) > 8 {
				tail = tail[len(tail)-8:]
			}
			msg += ":\n    " + strings.Join(tail, "\n    ")
		}
		return result, fmt.Errorf("%s\n  earlier actions remain applied; fix the cause and rerun `bastion apply`", msg)
	case res.ExitCode != 0:
		return result, fmt.Errorf("apply transport failed (exit %d): %s", res.ExitCode, stderrTail(res.Stderr))
	case !result.Done:
		return result, fmt.Errorf("apply output ended unexpectedly; rerun `bastion plan` to see the current state")
	}
	return result, nil
}

func stderrTail(stderr []byte) string {
	lines := strings.Split(strings.TrimSpace(string(stderr)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if len(l) > 300 {
				l = l[:300] + "…"
			}
			return l
		}
	}
	return "no error output"
}
