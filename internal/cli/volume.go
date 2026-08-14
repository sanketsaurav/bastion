package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/shellquote"
)

func (a *App) volumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage declared volume data",
	}
	cmd.AddCommand(a.volumeDeleteCmd())
	return cmd
}

// volumeDeleteCmd deletes one volume's data. Durable-data deletion always
// requires a confirmation naming the volume — `--yes` alone is never enough
// (SPEC.md §9.3, §12.4 of the original draft).
func (a *App) volumeDeleteCmd() *cobra.Command {
	var confirmName string
	cmd := &cobra.Command{
		Use:   "delete [box] <volume> --confirm <volume>",
		Short: "Delete a volume's data (separately confirmed by name)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var boxArgs []string
			name := args[len(args)-1]
			if len(args) == 2 {
				boxArgs = args[:1]
			}
			res, err := a.resolveBox(boxArgs)
			if err != nil {
				return err
			}
			box := res.Loaded.Box
			in := engineInput(res)

			// Refuse while a declared, enabled service still mounts it.
			for svc, s := range box.Services {
				if !s.IsEnabled() {
					continue
				}
				for _, m := range s.Mounts {
					if m.Volume == name {
						return fmt.Errorf("volume %q is mounted by service %q; remove the mount (or the service) and apply first", name, svc)
					}
				}
			}

			ephemeral := false
			if vol, declared := box.Volumes[name]; declared {
				ephemeral = vol.Persistence == "ephemeral"
			}
			if !dnsLabel(name) {
				return fmt.Errorf("%q is not a valid volume name", name)
			}

			if confirmName != name {
				if confirmName != "" {
					return fmt.Errorf("--confirm %q does not match volume %q", confirmName, name)
				}
				ok, err := a.confirmExact(
					fmt.Sprintf("This permanently deletes all data of volume %q on box %q.", name, in.BoxID), name)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("confirmation did not match; nothing was deleted")
				}
			}

			lock, err := a.lockBox(in.BoxID)
			if err != nil {
				return err
			}
			defer lock.Release()

			client := a.clientFor(res)
			var remote []string
			if ephemeral {
				remote = []string{"sudo", "-n", "docker", "volume", "rm", "bastion-" + in.BoxID + "-" + name}
			} else {
				// The path is assembled from the validated data root and a
				// validated DNS-label name — never from free-form input
				// (SPEC.md §11: no broad or unresolved deletion targets).
				dir := box.Workspace.DataRoot + "/volumes/" + name
				remote = []string{"sudo", "-n", "sh", "-c",
					"[ -d " + shellquote.Quote(dir) + " ] && rm -rf " + shellquote.Quote(dir) + " || { echo 'no such volume directory'; exit 1; }"}
			}
			out, err := a.runner.Run(cmd.Context(), client.ExecArgv(remote, false))
			if err != nil {
				return err
			}
			if out.ExitCode != 0 {
				detail := strings.TrimSpace(string(out.Stderr))
				if detail == "" {
					detail = strings.TrimSpace(string(out.Stdout))
				}
				return fmt.Errorf("deleting volume %q failed: %s", name, detail)
			}
			u := a.ui()
			if u.json {
				return u.emit(map[string]any{"volume": name, "deleted": true, "ephemeral": ephemeral})
			}
			fmt.Fprintf(u.out, "%s deleted volume %s\n", u.paint(ansiGreen, "✓"), name)
			if _, declared := box.Volumes[name]; declared {
				fmt.Fprintln(u.out, "  The volume is still declared; the next apply will recreate it empty. Remove the declaration to retire it.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&confirmName, "confirm", "", "non-interactive confirmation: must equal the volume name")
	return cmd
}

func dnsLabel(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' && i > 0 && i < len(s)-1
		if !ok {
			return false
		}
	}
	return true
}
