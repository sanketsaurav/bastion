package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/engine"
	"github.com/sanketsaurav/bastion/internal/lockfile"
	"github.com/sanketsaurav/bastion/internal/provider/gcp"
	"github.com/sanketsaurav/bastion/internal/registry"
	"github.com/sanketsaurav/bastion/internal/xdg"
)

// lockBox takes the local per-box operation lock for mutating commands.
func (a *App) lockBox(boxID string) (*lockfile.Lock, error) {
	stateDir, err := xdg.StateDir()
	if err != nil {
		return nil, err
	}
	return lockfile.Acquire(stateDir+"/locks", boxID)
}

// confirm asks an interactive yes/no question on the terminal.
func (a *App) confirm(prompt string) (bool, error) {
	if f, ok := a.stdin.(*os.File); !ok || f == nil || !isTerminal(f) {
		return false, errors.New("interactive confirmation required; rerun with --yes to approve non-destructive changes")
	}
	fmt.Fprintf(a.stderr, "%s [y/N]: ", prompt)
	line, err := bufio.NewReader(a.stdin).ReadString('\n')
	if err != nil {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// confirmExact requires the user to type an exact phrase — the durable-data
// guard, deliberately stronger than --yes (SPEC.md §9.3).
func (a *App) confirmExact(prompt, expected string) (bool, error) {
	if f, ok := a.stdin.(*os.File); !ok || f == nil || !isTerminal(f) {
		return false, fmt.Errorf("deleting data requires --confirm %s (or an interactive terminal)", expected)
	}
	fmt.Fprintf(a.stderr, "%s\nType %q to confirm: ", prompt, expected)
	line, err := bufio.NewReader(a.stdin).ReadString('\n')
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(line) == expected, nil
}

func (a *App) applyCmd() *cobra.Command {
	var healthTimeout time.Duration
	cmd := &cobra.Command{
		Use:   "apply [box]",
		Short: "Converge the box toward its declared configuration",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			in := engineInput(res)
			lock, err := a.lockBox(in.BoxID)
			if err != nil {
				return err
			}
			defer lock.Release()

			client := a.clientFor(res)
			ctx := cmd.Context()
			inst, err := client.Describe(ctx)
			if err != nil {
				return err
			}
			if !inst.Running() {
				return fmt.Errorf("apply requires a running box (currently %s); run `bastion up %s`", inst.Status, in.BoxID)
			}
			plan, facts, err := a.computePlan(ctx, client, in)
			if err != nil {
				return err
			}
			u := a.ui()
			if !plan.Changes() {
				if u.json {
					return u.emit(map[string]any{"plan": plan, "applied": []string{}})
				}
				renderPlan(u, plan)
				return nil
			}
			if !u.json {
				renderPlan(u, plan)
			}
			if !a.flags.yes {
				ok, err := a.confirm(fmt.Sprintf("Apply these %d actions to %s?", len(plan.Actions), in.BoxID))
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("apply cancelled")
				}
			}
			result, err := a.runApply(ctx, client, in, plan, facts, healthTimeout)
			if u.json {
				emitErr := u.emit(map[string]any{"plan": plan, "result": result})
				if err != nil {
					return err
				}
				return emitErr
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(u.out, "%s Applied %d action(s) to %s.\n", u.paint(ansiGreen, "✓"), len(result.Completed), in.BoxID)
			return nil
		},
	}
	cmd.Flags().DurationVar(&healthTimeout, "health-timeout", 2*time.Minute, "how long to wait for service health checks")
	return cmd
}

// runApply resolves secrets, generates the apply program, and streams it.
func (a *App) runApply(ctx context.Context, client *gcp.Client, in *engine.Input, plan *engine.Plan,
	facts *engine.Facts, healthTimeout time.Duration) (*engine.ApplyResult, error) {

	if plan.RootNeeded() && facts != nil && !facts.SudoOK {
		return nil, errors.New("this plan requires root actions but non-interactive sudo is unavailable on the box; " +
			"grant NOPASSWD sudo to your OS Login user or remove root-requiring configuration")
	}
	var secretValues map[string]string
	for _, act := range plan.Actions {
		if act.Kind == engine.KindSecret {
			var err error
			if secretValues, err = engine.ResolveSecrets(in.Box, a.getenv); err != nil {
				return nil, err
			}
			break
		}
	}
	u := a.ui()

	// Heartbeat: long steps (a Docker install runs minutes) tick with elapsed
	// time so silence never looks like a hang.
	var mu sync.Mutex
	var curID string
	var curStart time.Time
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				mu.Lock()
				if curID != "" {
					u.progressf("  %s %s (%ds elapsed)", u.paint(ansiDim, "…"), curID, int(time.Since(curStart).Seconds()))
				}
				mu.Unlock()
			}
		}
	}()
	setCurrent := func(id string) {
		mu.Lock()
		curID = id
		curStart = time.Now()
		mu.Unlock()
	}

	hooks := engine.ApplyHooks{
		OnStart: func(id string) {
			setCurrent(id)
			u.progressf("→ %s", id)
		},
		OnDone: func(id, status string) {
			setCurrent("")
			if status == "ok" {
				u.progressf("  %s %s", u.paint(ansiGreen, "✓"), id)
			} else {
				u.progressf("  %s %s", u.paint(ansiRed, "✗"), id)
			}
		},
		OnLog: func(id, line string) { u.debugf("    %s", line) },
	}
	return engine.ApplyPlan(ctx, client, in, plan, engine.ApplyOptions{
		HealthTimeout: healthTimeout,
		SecretValues:  secretValues,
	}, hooks)
}

// convergeAfterUp runs the apply step inside `bastion up`.
func (a *App) convergeAfterUp(ctx context.Context, client *gcp.Client, res *registry.Resolution, healthTimeout time.Duration) error {
	in := engineInput(res)
	plan, facts, err := a.computePlan(ctx, client, in)
	if err != nil {
		return err
	}
	u := a.ui()
	if !plan.Changes() {
		u.progressf("Configuration is up to date.")
		return nil
	}
	if !u.json {
		renderPlan(u, plan)
	}
	if plan.HasDestructive() && !a.flags.yes {
		ok, err := a.confirm("This plan removes resources (see above). Continue?")
		if err != nil || !ok {
			if err == nil {
				err = errors.New("up cancelled before destructive actions; rerun with --yes or use `bastion apply`")
			}
			return err
		}
	}
	result, err := a.runApply(ctx, client, in, plan, facts, healthTimeout)
	if err != nil {
		return err
	}
	u.progressf("Applied %d action(s).", len(result.Completed))
	return nil
}
