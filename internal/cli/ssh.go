package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func (a *App) sshCmd() *cobra.Command {
	return &cobra.Command{
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
			argv := a.clientFor(res).SSHArgv(extra)
			a.ui().debugf("+ %s", strings.Join(argv, " "))
			code, err := a.runner.Interactive(cmd.Context(), argv)
			if err != nil {
				return err
			}
			return exitWithCode(code)
		},
	}
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
