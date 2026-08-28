package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/provider"
	"github.com/sanketsaurav/bastion/internal/provider/gcp"
)

const (
	statusPollInterval = 3 * time.Second
	statusPollTimeout  = 5 * time.Minute
	sshPollInterval    = 5 * time.Second
)

func (a *App) upCmd() *cobra.Command {
	var noWait bool
	var noApply bool
	var waitTimeout time.Duration
	var healthTimeout time.Duration
	cmd := &cobra.Command{
		Use:   "up [box]",
		Short: "Start the box, wait until it is reachable, and converge it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			name := res.Loaded.Box.Metadata.Name
			lock, err := a.lockBox(name)
			if err != nil {
				return err
			}
			defer lock.Release()
			client := a.clientFor(res)
			u := a.ui()
			ctx := cmd.Context()
			started := time.Now()

			inst, err := client.Describe(ctx)
			if err != nil {
				return err
			}
			if inst.Transitional() {
				u.progressf("Instance is %s; waiting for it to settle…", inst.Status)
				if inst, err = waitForStable(ctx, client); err != nil {
					return err
				}
			}
			var startSp *spinner
			switch {
			case inst.Running():
				u.progressf("%s is already running.", name)
			case inst.Suspended():
				startSp = u.spin("Resuming "+name, "Resuming "+name+"…")
				if err := client.Resume(ctx); err != nil {
					startSp.fail("could not resume " + name)
					return err
				}
			case inst.Stopped():
				startSp = u.spin("Starting "+name, "Starting "+name+"…")
				if err := client.Start(ctx); err != nil {
					startSp.fail("could not start " + name)
					return err
				}
			default:
				return fmt.Errorf("instance is in unexpected status %q; check the GCP console", inst.Status)
			}
			if inst, err = waitForRunning(ctx, client); err != nil {
				startSp.fail(name + " did not reach RUNNING")
				return err
			}
			startSp.ok(name + " started")

			sshReady := false
			if !noWait {
				sshSp := u.spin("Waiting for SSH", "Waiting for SSH…")
				if err := waitForSSH(ctx, client, u, waitTimeout); err != nil {
					sshSp.fail("SSH did not become ready")
					return err
				}
				sshSp.ok("SSH ready")
				sshReady = true
			}

			// The product contract: up means running, reachable, AND
			// configured with services healthy (SPEC.md §1).
			converged := false
			if sshReady && !noApply {
				if err := a.convergeAfterUp(ctx, client, res, healthTimeout); err != nil {
					return err
				}
				converged = true
			}

			if u.json {
				return u.emit(map[string]any{
					"box":             name,
					"status":          inst.Status,
					"sshReady":        sshReady,
					"converged":       converged,
					"durationSeconds": int(time.Since(started).Seconds()),
				})
			}
			switch {
			case converged:
				fmt.Fprintf(u.out, "%s %s is running, reachable, and configured (%.0fs)\n", u.paint(ansiGreen, "✓"), name, time.Since(started).Seconds())
				fmt.Fprintf(u.out, "  connect with: bastion ssh %s\n", name)
			case sshReady:
				fmt.Fprintf(u.out, "%s %s is running and reachable (apply skipped)\n", u.paint(ansiGreen, "✓"), name)
			default:
				fmt.Fprintf(u.out, "%s %s is running (SSH wait skipped)\n", u.paint(ansiGreen, "✓"), name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "do not wait for SSH readiness (implies --no-apply)")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "start without converging configuration")
	cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 3*time.Minute, "how long to wait for SSH readiness")
	cmd.Flags().DurationVar(&healthTimeout, "health-timeout", 2*time.Minute, "how long to wait for service health checks")
	return cmd
}

func (a *App) downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down [box]",
		Short: "Stop compute; workspace and service data are retained",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			name := res.Loaded.Box.Metadata.Name
			client := a.clientFor(res)
			u := a.ui()
			ctx := cmd.Context()

			inst, err := client.Describe(ctx)
			if err != nil {
				return err
			}
			if inst.Transitional() {
				u.progressf("Instance is %s; waiting for it to settle…", inst.Status)
				if inst, err = waitForStable(ctx, client); err != nil {
					return err
				}
			}
			if inst.Stopped() {
				if u.json {
					return u.emit(map[string]any{"box": name, "status": inst.Status})
				}
				fmt.Fprintf(u.out, "%s is already stopped.\n", name)
				return nil
			}
			stopSp := u.spin("Stopping "+name, "Stopping "+name+"…")
			if err := client.Stop(ctx); err != nil {
				stopSp.fail("could not stop " + name)
				return err
			}
			if inst, err = waitForStatus(ctx, client, func(i *provider.Instance) bool { return i.Stopped() }); err != nil {
				stopSp.fail(name + " did not stop cleanly")
				return err
			}
			stopSp.erase()
			if u.json {
				return u.emit(map[string]any{"box": name, "status": inst.Status})
			}
			fmt.Fprintf(u.out, "%s %s stopped\n", u.paint(ansiGreen, "✓"), name)
			fmt.Fprintln(u.out, "  Disks are retained and continue to accrue storage charges; data persists.")
			return nil
		},
	}
}

func waitForRunning(ctx context.Context, client *gcp.Client) (*provider.Instance, error) {
	return waitForStatus(ctx, client, func(i *provider.Instance) bool { return i.Running() })
}

func waitForStable(ctx context.Context, client *gcp.Client) (*provider.Instance, error) {
	return waitForStatus(ctx, client, func(i *provider.Instance) bool { return !i.Transitional() })
}

func waitForStatus(ctx context.Context, client *gcp.Client, done func(*provider.Instance) bool) (*provider.Instance, error) {
	deadline := time.Now().Add(statusPollTimeout)
	for {
		inst, err := client.Describe(ctx)
		if err != nil {
			return nil, err
		}
		if done(inst) {
			return inst, nil
		}
		if time.Now().After(deadline) {
			return inst, fmt.Errorf("timed out after %s waiting for the instance (currently %s)", statusPollTimeout, inst.Status)
		}
		if err := sleepCtx(ctx, statusPollInterval); err != nil {
			return inst, err
		}
	}
}

func waitForSSH(ctx context.Context, client *gcp.Client, u *ui, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for attempt := 1; ; attempt++ {
		err := client.CheckSSH(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		u.debugf("ssh attempt %d: %v", attempt, err)
		if time.Now().After(deadline) {
			return fmt.Errorf("SSH did not become ready within %s (last error: %v); run `bastion doctor` to diagnose", timeout, lastErr)
		}
		if err := sleepCtx(ctx, sshPollInterval); err != nil {
			return err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
