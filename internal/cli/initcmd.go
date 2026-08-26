package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/config"
)

const boxTemplate = `apiVersion: bastion/v1alpha1
kind: Box

metadata:
  name: %s

# Point Bastion at an existing GCE VM (attached mode).
provider:
  name: gcp
  mode: attached
  project: my-gcp-project      # ← your project ID
  zone: us-west1-a             # ← the VM's zone
  instance: my-devbox          # ← the VM's instance name

# IAP + OS Login is the private-by-default connection path.
connection:
  type: iap
  osLogin: true

# Host convergence (validated now, applied from milestone B).
host:
  packages:
    - git
    - tmux
    - jq
#  shell:
#    prompt: yourname   # PS1 shows yourname@<host> instead of the OS Login
#                       # username; cosmetic only — auth and whoami unchanged
#  features:
#    - uses: docker
#    - uses: github-cli
#    - uses: claude-code
#  files:
#    - source: files/tmux.conf
#      target: ~/.tmux.conf
#      mode: replace
#      permissions: "0600"

# Container services (validated now, applied from milestone C).
#services:
#  dashboard:
#    image: ghcr.io/example/dashboard:1.4.2
#    endpoints:
#      web:
#        containerPort: 3000
#        visibility: private
`

var nameCleanRe = regexp.MustCompile(`[^a-z0-9-]+`)

func (a *App) initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [path]",
		Short: "Scaffold a box definition (never overwrites existing files)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			abs, err := filepath.Abs(target)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(abs, 0o755); err != nil {
				return err
			}
			file := filepath.Join(abs, config.BoxFileName)
			if _, err := os.Stat(file); err == nil {
				return fmt.Errorf("%s already exists; refusing to overwrite", file)
			}

			name := boxNameFor(abs)
			if err := os.WriteFile(file, []byte(fmt.Sprintf(boxTemplate, name)), 0o644); err != nil {
				return err
			}
			for _, sub := range []string{"files", "features", "scripts"} {
				if err := os.MkdirAll(filepath.Join(abs, sub), 0o755); err != nil {
					return err
				}
			}

			u := a.ui()
			if u.json {
				return u.emit(map[string]string{"name": name, "file": file})
			}
			fmt.Fprintf(u.out, "Created %s (box %q)\n\n", file, name)
			fmt.Fprintf(u.out, "Next steps:\n")
			fmt.Fprintf(u.out, "  1. Edit provider.project, provider.zone, and provider.instance\n")
			fmt.Fprintf(u.out, "  2. bastion validate --config %s\n", abs)
			fmt.Fprintf(u.out, "  3. bastion box adopt %s --config %s\n", name, abs)
			fmt.Fprintf(u.out, "  4. bastion status %s\n", name)
			return nil
		},
	}
}

// boxNameFor derives a DNS-label box name from the directory name.
func boxNameFor(dir string) string {
	name := strings.ToLower(filepath.Base(dir))
	name = nameCleanRe.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if len(name) > 63 {
		name = name[:63]
	}
	if name == "" || name == "." {
		return "devbox"
	}
	return name
}
