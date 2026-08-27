package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/banner"
	"github.com/sanketsaurav/bastion/internal/registry"
)

func (a *App) sshCmd() *cobra.Command {
	var noBanner bool
	cmd := &cobra.Command{
		Use:   "ssh [box] [-- ssh-args...]",
		Short: "Open an interactive SSH session (IAP by default)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			boxArgs, extra := splitAtDash(cmd, args)
			if len(boxArgs) > 1 {
				return errors.New("unexpected arguments; pass options for the underlying ssh after --")
			}
			res, err := a.resolveBox(boxArgs)
			if err != nil {
				return err
			}
			if !noBanner && len(extra) == 0 {
				a.printBanner(res)
			}
			argv := a.clientFor(res).SSHArgv(extra)
			a.ui().debugf("+ %s", strings.Join(argv, " "))
			code, err := a.runner.Interactive(cmd.Context(), argv)
			if err != nil {
				return err
			}
			return exitWithCode(code)
		},
	}
	cmd.Flags().BoolVar(&noBanner, "no-banner", false, "skip the box nameplate before the session")
	return cmd
}

// printBanner writes the box nameplate before an interactive session: the
// name in half-block art with a stable per-box accent color, then where the
// box lives and its public URLs. Everything comes from local configuration,
// so the session starts without any extra round trip. Decoration goes to
// stderr, only when it is a terminal.
func (a *App) printBanner(res *registry.Resolution) {
	box := res.Loaded.Box
	if box.Host != nil && box.Host.Shell != nil && box.Host.Shell.Banner == "off" {
		return
	}
	if !isTerminal(a.stderr) {
		return
	}
	u := a.ui()
	name := box.Metadata.Name
	accent := fmt.Sprintf("\x1b[38;5;%dm", banner.Color(name))
	fmt.Fprintln(a.stderr)
	for _, row := range banner.Art(name) {
		if u.color {
			fmt.Fprintf(a.stderr, "  %s%s%s\n", accent, row, ansiReset)
		} else {
			fmt.Fprintf(a.stderr, "  %s\n", row)
		}
	}
	fmt.Fprintf(a.stderr, "\n  %s\n", u.paint(ansiDim,
		fmt.Sprintf("%s/%s · %s", box.Provider.Project, box.Provider.Zone, box.Provider.Instance)))
	for _, pe := range box.PublicEndpoints() {
		fmt.Fprintf(a.stderr, "  %s\n", u.paint(ansiDim, "https://"+pe.Hostname))
	}
	fmt.Fprintln(a.stderr)
}

func (a *App) execCmd() *cobra.Command {
	var shell bool
	cmd := &cobra.Command{
		Use:   "exec [box] -- <command> [args...]",
		Short: "Run a command on the box, forwarding argv verbatim",
		Long: "Run a command on the box. The argument vector is forwarded without shell\n" +
			"interpretation; pass --shell to explicitly request remote shell evaluation.\n" +
			"The remote exit code is preserved.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			boxArgs, remote := splitAtDash(cmd, args)
			if len(remote) == 0 {
				return errors.New("usage: bastion exec [box] -- <command> [args...]")
			}
			if len(boxArgs) > 1 {
				return fmt.Errorf("at most one box name may precede --, got %d arguments", len(boxArgs))
			}
			res, err := a.resolveBox(boxArgs)
			if err != nil {
				return err
			}
			argv := a.clientFor(res).ExecArgv(remote, shell)
			a.ui().debugf("+ %s", strings.Join(argv, " "))
			code, err := a.runner.Interactive(cmd.Context(), argv)
			if err != nil {
				return err
			}
			return exitWithCode(code)
		},
	}
	cmd.Flags().BoolVar(&shell, "shell", false, "join arguments and let the remote shell evaluate them")
	return cmd
}

// splitAtDash separates positional args from everything after --.
func splitAtDash(cmd *cobra.Command, args []string) (before, after []string) {
	dash := cmd.ArgsLenAtDash()
	if dash < 0 {
		return args, nil
	}
	return args[:dash], args[dash:]
}
