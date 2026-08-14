package execx

import (
	"context"
	"fmt"
	"strings"
)

// Rule matches an argv and supplies a canned outcome for Fake.
type Rule struct {
	Match       func(argv []string) bool
	Result      Result
	Err         error
	Interactive int
}

// Prefix builds a matcher for argv beginning with the given words.
func Prefix(words ...string) func(argv []string) bool {
	return func(argv []string) bool {
		if len(argv) < len(words) {
			return false
		}
		for i, w := range words {
			if argv[i] != w {
				return false
			}
		}
		return true
	}
}

// Fake replays canned results and records every call. The zero value rejects
// all commands, which makes unexpected executions loud in tests.
type Fake struct {
	Rules  []Rule
	Calls  [][]string
	Stdins []string
}

func (f *Fake) find(argv []string) (*Rule, error) {
	for i := range f.Rules {
		if f.Rules[i].Match(argv) {
			return &f.Rules[i], nil
		}
	}
	return nil, fmt.Errorf("execx.Fake: unexpected command: %s", strings.Join(argv, " "))
}

func (f *Fake) Run(_ context.Context, argv []string) (Result, error) {
	f.Calls = append(f.Calls, argv)
	r, err := f.find(argv)
	if err != nil {
		return Result{}, err
	}
	return r.Result, r.Err
}

func (f *Fake) Interactive(_ context.Context, argv []string) (int, error) {
	f.Calls = append(f.Calls, argv)
	r, err := f.find(argv)
	if err != nil {
		return 1, err
	}
	return r.Interactive, r.Err
}

// RunStream replays the rule's Result.Stdout through onLine, line by line,
// and records the stdin it was given.
func (f *Fake) RunStream(_ context.Context, argv []string, stdin []byte, onLine func(string)) (Result, error) {
	f.Calls = append(f.Calls, argv)
	f.Stdins = append(f.Stdins, string(stdin))
	r, err := f.find(argv)
	if err != nil {
		return Result{}, err
	}
	for _, line := range strings.Split(strings.TrimRight(string(r.Result.Stdout), "\n"), "\n") {
		if line != "" || len(r.Result.Stdout) > 0 {
			onLine(line)
		}
	}
	return Result{ExitCode: r.Result.ExitCode, Stderr: r.Result.Stderr}, r.Err
}
