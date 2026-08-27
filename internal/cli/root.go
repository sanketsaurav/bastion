// Package cli implements the bastion command tree.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/execx"
	"github.com/sanketsaurav/bastion/internal/provider/gcp"
	"github.com/sanketsaurav/bastion/internal/registry"
	"github.com/sanketsaurav/bastion/internal/version"
	"github.com/sanketsaurav/bastion/internal/xdg"
)

// App carries the process-level collaborators so commands stay testable.
type App struct {
	flags    globalFlags
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	runner   execx.Runner
	getenv   func(string) string
	lookPath func(string) (string, error)
}

type globalFlags struct {
	config  string
	box     string
	json    bool
	noColor bool
	yes     bool
	verbose bool
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	app := &App{
		stdin:    os.Stdin,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		runner:   execx.Local{},
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := app.Root().ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var code *exitCodeError
	if errors.As(err, &code) {
		return code.code
	}
	fmt.Fprintf(os.Stderr, "bastion: %v\n", err)
	return 1
}

// exitCodeError requests a specific exit code without printing anything —
// used to propagate remote exit codes from ssh/exec.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func exitWithCode(code int) error {
	if code == 0 {
		return nil
	}
	if code < 0 {
		code = 1
	}
	return &exitCodeError{code: code}
}

// Root builds the command tree.
func (a *App) Root() *cobra.Command {
	root := &cobra.Command{
		Use:   "bastion",
		Short: "Operate a personal cloud development box from your terminal",
		Long: "Bastion is a local, config-driven CLI for operating a personal Linux\n" +
			"development box on Google Compute Engine. Your terminal is the control\n" +
			"plane: no hosted service, no daemon on the VM.",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVar(&a.flags.config, "config", "", "path to a box definition (file or directory)")
	pf.StringVar(&a.flags.box, "box", "", "registered box name")
	pf.BoolVar(&a.flags.json, "json", false, "emit structured JSON on stdout")
	pf.BoolVar(&a.flags.noColor, "no-color", false, "disable colored output")
	pf.BoolVar(&a.flags.yes, "yes", false, "approve non-data-destructive prompts")
	pf.BoolVarP(&a.flags.verbose, "verbose", "v", false, "include diagnostic detail")

	root.AddCommand(
		a.initCmd(),
		a.validateCmd(),
		a.configCmd(),
		a.boxCmd(),
		a.statusCmd(),
		a.planCmd(),
		a.applyCmd(),
		a.upCmd(),
		a.downCmd(),
		a.sshCmd(),
		a.sshConfigCmd(),
		a.execCmd(),
		a.portCmd(),
		a.serviceCmd(),
		a.endpointCmd(),
		a.featureCmd(),
		a.volumeCmd(),
		a.doctorCmd(),
		a.versionCmd(),
	)
	return root
}

// resolveBox resolves the target box from flags and an optional positional
// name (SPEC.md §5.2).
func (a *App) resolveBox(positional []string) (*registry.Resolution, error) {
	name := a.flags.box
	if len(positional) > 0 && positional[0] != "" {
		if name != "" && name != positional[0] {
			return nil, fmt.Errorf("conflicting box selection: positional %q vs --box %q", positional[0], name)
		}
		name = positional[0]
	}
	reg, err := registry.Open()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return reg.Resolve(registry.Request{
		ConfigFlag: a.flags.config,
		EnvConfig:  a.getenv("BASTION_CONFIG"),
		Name:       name,
		CWD:        cwd,
	})
}

// clientFor builds the provider client for a resolved box.
func (a *App) clientFor(res *registry.Resolution) *gcp.Client {
	c := gcp.FromBox(res.Loaded.Box, a.runner)
	// Connection multiplexing (SPEC.md Δ14): the per-box control socket
	// lives under the state dir. Any failure to prepare it just means the
	// ordinary one-connection-per-command path.
	if res.Loaded.Box.Connection.UseMultiplex() {
		if stateDir, err := xdg.StateDir(); err == nil {
			sock := filepath.Join(stateDir, "mux", res.Loaded.Box.Metadata.Name+".sock")
			if os.MkdirAll(filepath.Dir(sock), 0o700) == nil {
				c.SetMuxSocket(sock)
			}
		}
	}
	return c
}
