package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/registry"
	"github.com/sanketsaurav/bastion/internal/xdg"
)

// sshConfigCmd generates (and optionally installs) a ~/.ssh/config Host
// block for a box, so every standard SSH tool — IDE remotes, scp, rsync —
// can reach it by name over the same transport bastion uses. The block is
// delimited by markers and replaced as a unit; nothing outside it is ever
// touched.
func (a *App) sshConfigCmd() *cobra.Command {
	var install, remove bool
	cmd := &cobra.Command{
		Use:   "ssh-config [box]",
		Short: "Generate a ~/.ssh/config entry so any SSH tool can reach the box",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if install && remove {
				return fmt.Errorf("--install and --remove are mutually exclusive")
			}
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			boxID := res.Loaded.Box.Metadata.Name
			path, err := sshConfigPath()
			if err != nil {
				return err
			}
			u := a.ui()

			if remove {
				changed, err := spliceSSHConfig(path, boxID, "")
				if err != nil {
					return err
				}
				if u.json {
					return u.emit(map[string]any{"host": boxID, "removed": changed})
				}
				if changed {
					fmt.Fprintf(u.out, "%s removed Host %s from %s\n", u.paint(ansiGreen, "✓"), boxID, path)
				} else {
					fmt.Fprintf(u.out, "no managed block for %s in %s\n", boxID, path)
				}
				return nil
			}

			stanza, err := a.buildSSHStanza(cmd, res)
			if err != nil {
				return err
			}
			if !install {
				if u.json {
					return u.emit(map[string]any{"host": boxID, "stanza": stanza})
				}
				fmt.Fprintln(u.out, stanza)
				return nil
			}
			if _, err := spliceSSHConfig(path, boxID, stanza); err != nil {
				return err
			}
			if u.json {
				return u.emit(map[string]any{"host": boxID, "installed": true, "path": path})
			}
			fmt.Fprintf(u.out, "%s installed Host %s in %s\n", u.paint(ansiGreen, "✓"), boxID, path)
			fmt.Fprintf(u.out, "  connect with: ssh %s (works in any SSH tool)\n", boxID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&install, "install", false, "write or update the managed block in ~/.ssh/config")
	cmd.Flags().BoolVar(&remove, "remove", false, "delete the managed block from ~/.ssh/config")
	return cmd
}

func sshConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// buildSSHStanza renders the Host block for the box's transport. IAP boxes
// need the OS Login username and the instance ID, each one gcloud call.
func (a *App) buildSSHStanza(cmd *cobra.Command, res *registry.Resolution) (string, error) {
	box := res.Loaded.Box
	boxID := box.Metadata.Name
	conn := box.Connection

	var b strings.Builder
	fmt.Fprintf(&b, "# bastion:%s — managed by `bastion ssh-config`; edits inside are overwritten\n", boxID)
	fmt.Fprintf(&b, "Host %s\n", boxID)

	if conn.Type == "direct" {
		fmt.Fprintf(&b, "  HostName %s\n", conn.Host)
		if conn.User != "" {
			fmt.Fprintf(&b, "  User %s\n", conn.User)
		}
		if conn.IdentityFile != "" {
			fmt.Fprintf(&b, "  IdentityFile %s\n", conn.IdentityFile)
		}
	} else {
		client := a.clientFor(res)
		ctx := cmd.Context()
		user, err := client.OSLoginUser(ctx)
		if err != nil {
			return "", err
		}
		inst, err := client.Describe(ctx)
		if err != nil {
			return "", err
		}
		project, zone, instance := client.Instance()
		fmt.Fprintf(&b, "  User %s\n", user)
		b.WriteString("  IdentityFile ~/.ssh/google_compute_engine\n")
		b.WriteString("  IdentitiesOnly yes\n")
		fmt.Fprintf(&b, "  ProxyCommand gcloud compute start-iap-tunnel %s %%p --listen-on-stdin --project %s --zone %s --verbosity=error\n",
			instance, project, zone)
		b.WriteString("  UserKnownHostsFile ~/.ssh/google_compute_known_hosts\n")
		fmt.Fprintf(&b, "  HostKeyAlias compute.%s\n", inst.ID)
		b.WriteString("  CheckHostIP no\n")

		home, err := os.UserHomeDir()
		if err == nil {
			if _, statErr := os.Stat(filepath.Join(home, ".ssh", "google_compute_engine")); statErr != nil {
				fmt.Fprintf(a.stderr, "note: ~/.ssh/google_compute_engine does not exist yet; run `bastion ssh %s` once to create and register it\n", boxID)
			}
		}
	}

	if conn.ForwardSSHAgent {
		b.WriteString("  ForwardAgent yes\n")
	}
	b.WriteString("  ServerAliveInterval 30\n")
	if conn.UseMultiplex() && !conn.ForwardSSHAgent {
		if stateDir, err := xdg.StateDir(); err == nil {
			b.WriteString("  ControlMaster auto\n")
			fmt.Fprintf(&b, "  ControlPath %s\n", filepath.Join(stateDir, "mux", boxID+".sock"))
			b.WriteString("  ControlPersist 600\n")
		}
	}
	fmt.Fprintf(&b, "# end bastion:%s", boxID)
	return b.String(), nil
}

// spliceSSHConfig replaces the box's managed block in the file (stanza ""
// deletes it; a missing block is appended). Everything outside the markers
// is preserved byte for byte; the write is atomic and keeps 0600.
func spliceSSHConfig(path, boxID, stanza string) (changed bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}

	begin := "# bastion:" + boxID
	end := "# end bastion:" + boxID
	isBegin := func(line string) bool {
		return line == begin || strings.HasPrefix(line, begin+" ")
	}

	lines := strings.Split(string(data), "\n")
	var out []string
	found := false
	for i := 0; i < len(lines); {
		if !isBegin(lines[i]) {
			out = append(out, lines[i])
			i++
			continue
		}
		endIdx := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimRight(lines[j], " ") == end {
				endIdx = j
				break
			}
		}
		if endIdx == -1 {
			// Never guess at block extent: eating lines past a lost end
			// marker would destroy unrelated configuration.
			return false, fmt.Errorf("the managed block for %q in %s has no %q end marker; repair the file by hand first", boxID, path, end)
		}
		found = true
		if stanza != "" {
			out = append(out, strings.Split(stanza, "\n")...)
			stanza = "" // replace the first occurrence; drop any duplicates
		} else if len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1] // also drop the blank line we insert above blocks
		}
		i = endIdx + 1
	}
	if stanza != "" {
		for len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, strings.Split(stanza, "\n")...)
		out = append(out, "")
		found = true
	}

	content := strings.Join(out, "\n")
	if content == string(data) {
		return false, nil
	}
	tmp := path + ".bastion-tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return found, nil
}
