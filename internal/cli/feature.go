package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/engine"
)

func (a *App) featureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feature",
		Short: "Operate installed host features",
	}
	cmd.AddCommand(a.featureRemoveCmd())
	return cmd
}

// featureRemoveCmd removes a user-level builtin's installed payload and its
// state marker. Apt-based features are refused with the manual command —
// their packages may be shared, so removal is not bastion's call. A feature
// the definition still declares is refused too: apply never uninstalls, and
// an imperative command must not fight declared state.
func (a *App) featureRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [box] <feature>",
		Short: "Remove a user-level feature installed by bastion (configuration and credentials are kept)",
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

			def, known := engine.Builtins[name]
			if !known {
				for _, feat := range hostFeatures(box) {
					if feat.Local() && engine.LocalFeatureNameOf(feat.Uses) == name {
						return fmt.Errorf("local feature %q applies user-defined changes; bastion cannot invert them — undeclare it and remove its effects yourself", name)
					}
				}
				return fmt.Errorf("unknown feature %q; removable features: %s", name, strings.Join(removableBuiltins(), ", "))
			}
			for _, feat := range hostFeatures(box) {
				if feat.Uses == name {
					return fmt.Errorf("feature %q is still declared in the definition; remove the declaration first — the next apply would just reinstall it", name)
				}
			}
			if len(def.RemovePaths) == 0 {
				return fmt.Errorf("feature %q is apt-managed and its packages may be shared; bastion will not remove it — do it yourself: %s", name, def.RemoveHint)
			}

			if !a.flags.yes {
				targets := make([]string, 0, len(def.RemovePaths))
				for _, p := range def.RemovePaths {
					targets = append(targets, "~/"+p)
				}
				prompt := fmt.Sprintf("This deletes %s on box %q", strings.Join(targets, ", "), in.BoxID)
				if def.RemoveKeeps != "" {
					prompt += ", keeping " + def.RemoveKeeps
				}
				ok, err := a.confirm(prompt + ". Continue?")
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("feature removal cancelled")
				}
			}

			lock, err := a.lockBox(in.BoxID)
			if err != nil {
				return err
			}
			defer lock.Release()

			client := a.clientFor(res)
			script := engine.FeatureRemoveScript(in, def)
			out, err := a.runner.Run(cmd.Context(), client.ExecArgv([]string{"sh", "-c", script}, false))
			if err != nil {
				return err
			}
			if out.ExitCode != 0 {
				detail := strings.TrimSpace(string(out.Stderr))
				if detail == "" {
					detail = strings.TrimSpace(string(out.Stdout))
				}
				return fmt.Errorf("removing feature %q failed: %s", name, detail)
			}
			u := a.ui()
			if u.json {
				return u.emit(map[string]any{"feature": name, "removed": true, "deleted": def.RemovePaths, "kept": def.RemoveKeeps})
			}
			fmt.Fprintf(u.out, "%s removed feature %s\n", u.paint(ansiGreen, "✓"), name)
			for _, p := range def.RemovePaths {
				fmt.Fprintf(u.out, "  deleted ~/%s\n", p)
			}
			if def.RemoveKeeps != "" {
				fmt.Fprintf(u.out, "  kept    %s\n", def.RemoveKeeps)
			}
			return nil
		},
	}
}

func hostFeatures(box *config.Box) []config.Feature {
	if box.Host == nil {
		return nil
	}
	return box.Host.Features
}

// removableBuiltins lists the builtins `feature remove` can act on.
func removableBuiltins() []string {
	var names []string
	for name, def := range engine.Builtins {
		if len(def.RemovePaths) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
