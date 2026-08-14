package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/registry"
)

func (a *App) boxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "box",
		Short: "Manage box registrations",
	}
	cmd.AddCommand(a.boxAdoptCmd(), a.boxListCmd(), a.boxUseCmd(), a.boxForgetCmd())
	return cmd
}

func (a *App) boxAdoptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "adopt <name>",
		Short: "Register an existing box definition (never mutates the VM)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			path := a.flags.config
			if path == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				path = cwd
			}
			reg, err := registry.Open()
			if err != nil {
				return err
			}
			loaded, err := reg.Adopt(name, path)
			if err != nil {
				return err
			}
			madeCurrent := false
			if reg.Client.CurrentBox == "" {
				reg.Client.CurrentBox = name
				madeCurrent = true
			}
			if err := reg.Save(); err != nil {
				return err
			}
			u := a.ui()
			if u.json {
				return u.emit(map[string]any{"name": name, "path": loaded.Dir, "current": madeCurrent})
			}
			fmt.Fprintf(u.out, "%s adopted %s → %s\n", u.paint(ansiGreen, "✓"), name, loaded.Dir)
			if madeCurrent {
				fmt.Fprintf(u.out, "%s is now the current box\n", name)
			}
			return nil
		},
	}
}

type boxRow struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Current bool   `json:"current"`
	Valid   bool   `json:"valid"`
	Error   string `json:"error,omitempty"`
}

func (a *App) boxListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered boxes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.Open()
			if err != nil {
				return err
			}
			var rows []boxRow
			for _, e := range reg.List() {
				row := boxRow{Name: e.Name, Path: e.Path, Current: e.Current, Valid: true}
				if _, err := config.Load(e.Path); err != nil {
					row.Valid = false
					row.Error = "definition failed to load; run `bastion validate " + e.Name + "`"
				}
				rows = append(rows, row)
			}
			u := a.ui()
			if u.json {
				if rows == nil {
					rows = []boxRow{}
				}
				return u.emit(rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(u.out, "No boxes registered. Create one with `bastion init` and register it with `bastion box adopt`.")
				return nil
			}
			for _, row := range rows {
				marker := " "
				if row.Current {
					marker = u.paint(ansiBold, "*")
				}
				note := ""
				if !row.Valid {
					note = "  " + u.paint(ansiRed, "(invalid)")
				}
				fmt.Fprintf(u.out, "%s %-20s %s%s\n", marker, row.Name, row.Path, note)
			}
			return nil
		},
	}
}

func (a *App) boxUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Select the current box",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.Open()
			if err != nil {
				return err
			}
			if err := reg.Use(args[0]); err != nil {
				return err
			}
			if err := reg.Save(); err != nil {
				return err
			}
			u := a.ui()
			if u.json {
				return u.emit(map[string]string{"current": args[0]})
			}
			fmt.Fprintf(u.out, "%s is now the current box\n", args[0])
			return nil
		},
	}
}

func (a *App) boxForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <name>",
		Short: "Remove a local registration (the definition and VM are untouched)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.Open()
			if err != nil {
				return err
			}
			if !reg.Forget(args[0]) {
				return fmt.Errorf("box %q is not registered", args[0])
			}
			if err := reg.Save(); err != nil {
				return err
			}
			u := a.ui()
			if u.json {
				return u.emit(map[string]string{"forgotten": args[0]})
			}
			fmt.Fprintf(u.out, "Forgot %s (local registration only; the VM was not touched)\n", args[0])
			return nil
		},
	}
}
