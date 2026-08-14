package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/execx"
	"github.com/sanketsaurav/bastion/internal/provider/gcp"
	"github.com/sanketsaurav/bastion/internal/registry"
)

// Service commands operate only on bastion-owned containers, addressed by the
// deterministic container name bastion-<box>-<service> (SPEC.md §9.1).

func (a *App) serviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Operate declared container services",
	}
	cmd.AddCommand(
		a.serviceListCmd(),
		a.serviceStatusCmd(),
		a.serviceLogsCmd(),
		a.serviceLifecycleCmd("start", "Start a stopped service container"),
		a.serviceLifecycleCmd("stop", "Stop a service container (operational state; the next up/apply restores it)"),
		a.serviceLifecycleCmd("restart", "Restart a service container"),
		a.serviceExecCmd(),
		a.serviceUpdateCmd(),
	)
	return cmd
}

// resolveService resolves [box] <service> argument shapes and verifies the
// service is declared.
func (a *App) resolveService(args []string) (*registry.Resolution, string, error) {
	var boxArgs []string
	var svc string
	switch len(args) {
	case 1:
		svc = args[0]
	case 2:
		boxArgs, svc = args[:1], args[1]
	default:
		return nil, "", errors.New("usage: bastion service <cmd> [box] <service>")
	}
	res, err := a.resolveBox(boxArgs)
	if err != nil {
		return nil, "", err
	}
	if _, ok := res.Loaded.Box.Services[svc]; !ok {
		return nil, "", fmt.Errorf("service %q is not declared in box %q (declared: %s)",
			svc, res.Loaded.Box.Metadata.Name, strings.Join(serviceNames(res), ", "))
	}
	return res, svc, nil
}

func serviceNames(res *registry.Resolution) []string {
	var names []string
	for name := range res.Loaded.Box.Services {
		names = append(names, name)
	}
	if len(names) == 0 {
		names = []string{"none"}
	}
	return names
}

// docker runs a docker CLI invocation on the box and returns captured output.
func (a *App) docker(ctx context.Context, client *gcp.Client, args ...string) (execx.Result, error) {
	argv := client.ExecArgv(append([]string{"sudo", "-n", "docker"}, args...), false)
	return a.runner.Run(ctx, argv)
}

func dockerErr(res execx.Result, what string) error {
	detail := strings.TrimSpace(string(res.Stderr))
	if detail == "" {
		detail = strings.TrimSpace(string(res.Stdout))
	}
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}
	return fmt.Errorf("%s failed (exit %d): %s", what, res.ExitCode, detail)
}

type serviceRow struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Status  string `json:"status,omitempty"`
	Image   string `json:"image,omitempty"`
	Enabled bool   `json:"enabled"`
}

func (a *App) serviceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [box]",
		Short: "List declared services and their container state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			box := res.Loaded.Box
			if len(box.Services) == 0 {
				fmt.Fprintln(a.stdout, "No services declared.")
				return nil
			}
			client := a.clientFor(res)
			in := engineInput(res)

			out, err := a.docker(cmd.Context(), client, "ps", "-a",
				"--filter", "label=bastion.box-id="+in.BoxID,
				"--format", "{{.Names}}\t{{.State}}\t{{.Status}}\t{{.Image}}")
			if err != nil {
				return err
			}
			if out.ExitCode != 0 {
				return dockerErr(out, "listing containers")
			}
			observed := map[string]serviceRow{}
			for _, line := range strings.Split(strings.TrimSpace(string(out.Stdout)), "\n") {
				parts := strings.SplitN(line, "\t", 4)
				if len(parts) < 4 {
					continue
				}
				name := strings.TrimPrefix(parts[0], "bastion-"+in.BoxID+"-")
				observed[name] = serviceRow{Name: name, State: parts[1], Status: parts[2], Image: parts[3]}
			}
			var rows []serviceRow
			for _, name := range sortedServiceNames(res) {
				svc := box.Services[name]
				row, ok := observed[name]
				if !ok {
					row = serviceRow{Name: name, State: "absent"}
				}
				row.Enabled = svc.IsEnabled()
				rows = append(rows, row)
				delete(observed, name)
			}
			for name, row := range observed {
				row.Name = name + " (undeclared)"
				rows = append(rows, row)
			}
			u := a.ui()
			if u.json {
				return u.emit(rows)
			}
			for _, row := range rows {
				state := row.State
				switch state {
				case "running":
					state = u.paint(ansiGreen, state)
				case "absent":
					state = u.paint(ansiDim, state)
				default:
					state = u.paint(ansiYellow, state)
				}
				note := ""
				if !row.Enabled {
					note = "  " + u.paint(ansiDim, "(disabled)")
				}
				fmt.Fprintf(u.out, "%-20s %-10s %-30s %s%s\n", row.Name, state, row.Status, row.Image, note)
			}
			return nil
		},
	}
}

func sortedServiceNames(res *registry.Resolution) []string {
	names := make([]string, 0, len(res.Loaded.Box.Services))
	for name := range res.Loaded.Box.Services {
		names = append(names, name)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

func (a *App) serviceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [box] <service>",
		Short: "Show one service's container state in detail",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, svc, err := a.resolveService(args)
			if err != nil {
				return err
			}
			client := a.clientFor(res)
			in := engineInput(res)
			format := "{{.State.Status}}\t{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}\t" +
				"{{.Config.Image}}\t{{.State.StartedAt}}\t{{.RestartCount}}\t" +
				`{{index .Config.Labels "bastion.config-digest"}}`
			out, err := a.docker(cmd.Context(), client, "inspect", "--format", format,
				"bastion-"+in.BoxID+"-"+svc)
			if err != nil {
				return err
			}
			u := a.ui()
			if out.ExitCode != 0 {
				if u.json {
					return u.emit(map[string]any{"service": svc, "state": "absent"})
				}
				fmt.Fprintf(u.out, "%s is declared but has no container yet; run `bastion apply`\n", svc)
				return nil
			}
			parts := strings.SplitN(strings.TrimSpace(string(out.Stdout)), "\t", 6)
			for len(parts) < 6 {
				parts = append(parts, "")
			}
			if u.json {
				return u.emit(map[string]any{
					"service": svc, "state": parts[0], "health": parts[1],
					"image": parts[2], "startedAt": parts[3], "restarts": parts[4],
					"configDigest": parts[5],
				})
			}
			u.printKV([][2]string{
				{"Service", svc},
				{"State", parts[0]},
				{"Health", parts[1]},
				{"Image", parts[2]},
				{"Started", parts[3]},
				{"Restarts", parts[4]},
				{"Config digest", shortDigest(parts[5])},
			})
			return nil
		},
	}
}

func shortDigest(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}

func (a *App) serviceLogsCmd() *cobra.Command {
	var follow bool
	var tail int
	cmd := &cobra.Command{
		Use:   "logs [box] <service>",
		Short: "Show a service's container logs",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, svc, err := a.resolveService(args)
			if err != nil {
				return err
			}
			client := a.clientFor(res)
			in := engineInput(res)
			dockerArgs := []string{"sudo", "-n", "docker", "logs"}
			if follow {
				dockerArgs = append(dockerArgs, "--follow")
			}
			if tail > 0 {
				dockerArgs = append(dockerArgs, "--tail", strconv.Itoa(tail))
			}
			dockerArgs = append(dockerArgs, "bastion-"+in.BoxID+"-"+svc)
			code, err := a.runner.Interactive(cmd.Context(), client.ExecArgv(dockerArgs, false))
			if err != nil {
				return err
			}
			return exitWithCode(code)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream new log lines until interrupted")
	cmd.Flags().IntVar(&tail, "tail", 0, "show only the last N lines")
	return cmd
}

func (a *App) serviceLifecycleCmd(verb, short string) *cobra.Command {
	return &cobra.Command{
		Use:   verb + " [box] <service>",
		Short: short,
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, svc, err := a.resolveService(args)
			if err != nil {
				return err
			}
			client := a.clientFor(res)
			in := engineInput(res)
			out, err := a.docker(cmd.Context(), client, verb, "bastion-"+in.BoxID+"-"+svc)
			if err != nil {
				return err
			}
			if out.ExitCode != 0 {
				return dockerErr(out, verb+" "+svc)
			}
			u := a.ui()
			if u.json {
				return u.emit(map[string]string{"service": svc, "action": verb})
			}
			past := map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}[verb]
			fmt.Fprintf(u.out, "%s %s %s\n", u.paint(ansiGreen, "✓"), svc, past)
			if verb == "stop" {
				fmt.Fprintln(u.out, "  This is operational state; the next `bastion up` or `bastion apply` restores the declared state.")
			}
			return nil
		},
	}
}

func (a *App) serviceExecCmd() *cobra.Command {
	var tty bool
	cmd := &cobra.Command{
		Use:   "exec [box] <service> -- <command> [args...]",
		Short: "Run a command inside a service container",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			before, remote := splitAtDash(cmd, args)
			if len(remote) == 0 {
				return errors.New("usage: bastion service exec [box] <service> -- <command> [args...]")
			}
			res, svc, err := a.resolveService(before)
			if err != nil {
				return err
			}
			client := a.clientFor(res)
			in := engineInput(res)
			dockerArgs := []string{"sudo", "-n", "docker", "exec", "-i"}
			if tty {
				dockerArgs = append(dockerArgs, "-t")
			}
			dockerArgs = append(append(dockerArgs, "bastion-"+in.BoxID+"-"+svc), remote...)
			argv := client.ExecArgv(dockerArgs, false)
			if tty {
				// A pseudo-terminal must exist on both hops: ssh -t for the
				// SSH leg, docker exec -t for the container leg.
				argv = client.ExecArgvTTY(dockerArgs, false)
			}
			code, err := a.runner.Interactive(cmd.Context(), argv)
			if err != nil {
				return err
			}
			return exitWithCode(code)
		},
	}
	cmd.Flags().BoolVarP(&tty, "tty", "t", false, "allocate a pseudo-terminal (interactive shells)")
	return cmd
}

func (a *App) serviceUpdateCmd() *cobra.Command {
	var pin bool
	cmd := &cobra.Command{
		Use:   "update [box] <service>",
		Short: "Pull a newer image for a mutable tag and redeploy after confirmation",
		Long: "Pull the current content of the service's image reference, show what\n" +
			"changed, and redeploy after confirmation. With --pin, the resolved digest\n" +
			"is written back into the box definition so the update never becomes\n" +
			"silent drift.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, svc, err := a.resolveService(args)
			if err != nil {
				return err
			}
			box := res.Loaded.Box
			client := a.clientFor(res)
			in := engineInput(res)
			ctx := cmd.Context()
			u := a.ui()
			image := box.Services[svc].Image
			cname := "bastion-" + in.BoxID + "-" + svc

			before, err := a.docker(ctx, client, "image", "inspect", "--format", "{{.Id}}", image)
			if err != nil {
				return err
			}
			beforeID := strings.TrimSpace(string(before.Stdout))

			u.progressf("Pulling %s…", image)
			pull, err := a.docker(ctx, client, "pull", "--quiet", image)
			if err != nil {
				return err
			}
			if pull.ExitCode != 0 {
				return dockerErr(pull, "pulling "+image)
			}
			after, err := a.docker(ctx, client, "image", "inspect", "--format", "{{.Id}}", image)
			if err != nil {
				return err
			}
			afterID := strings.TrimSpace(string(after.Stdout))
			if beforeID == afterID && before.ExitCode == 0 {
				if pin {
					return a.pinService(ctx, client, res, svc, image)
				}
				if u.json {
					return u.emit(map[string]any{"service": svc, "updated": false, "image": image})
				}
				fmt.Fprintf(u.out, "%s is already running the newest %s\n", svc, image)
				return nil
			}
			fmt.Fprintf(u.out, "New image for %s:\n  old: %s\n  new: %s\n", svc, shortDigest(strings.TrimPrefix(beforeID, "sha256:")), shortDigest(strings.TrimPrefix(afterID, "sha256:")))
			fmt.Fprintln(u.out, "Consider pinning this digest in the box definition to avoid drift.")
			if !a.flags.yes {
				ok, err := a.confirm("Redeploy " + svc + " with the new image?")
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("update cancelled (the new image is pulled but not deployed)")
				}
			}
			composePath := "/var/lib/bastion/services/" + in.BoxID + "/" + svc + "/compose.yaml"
			up, err := a.docker(ctx, client, "compose", "-p", "bastion-"+in.BoxID+"-"+svc, "-f", composePath, "up", "-d")
			if err != nil {
				return err
			}
			if up.ExitCode != 0 {
				return dockerErr(up, "redeploying "+svc)
			}
			if !u.json {
				fmt.Fprintf(u.out, "%s %s redeployed (%s)\n", u.paint(ansiGreen, "✓"), svc, cname)
			}
			if pin {
				return a.pinService(ctx, client, res, svc, image)
			}
			if u.json {
				return u.emit(map[string]any{"service": svc, "updated": true, "image": image})
			}
			fmt.Fprintln(u.out, "  Rerun with --pin to write the resolved digest into the box definition.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&pin, "pin", false, "write the resolved image digest back into the box definition")
	return cmd
}

// pinService resolves the image's repo digest and writes it into the box
// definition — configuration changes always live in the definition, never as
// hidden drift (SPEC.md §4, §9.2).
func (a *App) pinService(ctx context.Context, client *gcp.Client, res *registry.Resolution, svc, image string) error {
	u := a.ui()
	out, err := a.docker(ctx, client, "image", "inspect", "--format", "{{index .RepoDigests 0}}", image)
	if err != nil {
		return err
	}
	if out.ExitCode != 0 {
		return dockerErr(out, "resolving digest for "+image)
	}
	pinned := strings.TrimSpace(string(out.Stdout))
	if pinned == "" || !strings.Contains(pinned, "@sha256:") {
		return fmt.Errorf("could not resolve a repo digest for %s", image)
	}
	if pinned == image {
		if u.json {
			return u.emit(map[string]any{"service": svc, "pinned": image, "changed": false})
		}
		fmt.Fprintf(u.out, "%s already pins %s\n", svc, image)
		return nil
	}
	if err := pinImageInConfig(res.Loaded.File, image, pinned); err != nil {
		return err
	}
	if u.json {
		return u.emit(map[string]any{"service": svc, "pinned": pinned, "changed": true})
	}
	fmt.Fprintf(u.out, "%s pinned %s\n    → %s\n", u.paint(ansiGreen, "✓"), svc, pinned)
	fmt.Fprintln(u.out, "  The definition changed; the next plan/apply will redeploy with the pinned reference.")
	return nil
}

// pinImageInConfig rewrites one image reference in a definition file. The old
// reference must occur exactly once — otherwise the edit is ambiguous and the
// user is told to pin manually. The rewritten file must still load; on any
// failure the original bytes are restored.
func pinImageInConfig(file, oldImage, newImage string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	switch count := strings.Count(string(data), oldImage); count {
	case 1:
	case 0:
		return fmt.Errorf("image reference %q not found in %s; pin it manually", oldImage, file)
	default:
		return fmt.Errorf("image reference %q appears %d times in %s; pin it manually to avoid an ambiguous edit", oldImage, count, file)
	}
	updated := strings.Replace(string(data), oldImage, newImage, 1)
	if err := os.WriteFile(file, []byte(updated), 0o644); err != nil {
		return err
	}
	if _, err := config.Load(file); err != nil {
		_ = os.WriteFile(file, data, 0o644)
		return fmt.Errorf("pinned definition failed to validate (original restored): %w", err)
	}
	return nil
}
