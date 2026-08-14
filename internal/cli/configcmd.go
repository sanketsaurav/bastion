package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/xdg"
)

func (a *App) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect client configuration",
	}
	cmd.AddCommand(a.configPathsCmd(), a.configSchemaCmd())
	return cmd
}

func (a *App) configPathsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "paths",
		Short: "Show resolved configuration, state, and cache locations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := xdg.ConfigDir()
			if err != nil {
				return err
			}
			stateDir, err := xdg.StateDir()
			if err != nil {
				return err
			}
			cacheDir, err := xdg.CacheDir()
			if err != nil {
				return err
			}
			paths := map[string]string{
				"configDir":    configDir,
				"clientConfig": filepath.Join(configDir, "config.yaml"),
				"boxesDir":     filepath.Join(configDir, "boxes"),
				"stateDir":     stateDir,
				"cacheDir":     cacheDir,
			}
			u := a.ui()
			if u.json {
				return u.emit(paths)
			}
			u.printKV([][2]string{
				{"Config dir", paths["configDir"]},
				{"Client config", paths["clientConfig"]},
				{"Boxes dir", paths["boxesDir"]},
				{"State dir", paths["stateDir"]},
				{"Cache dir", paths["cacheDir"]},
			})
			return nil
		},
	}
}

func (a *App) configSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for kind: Box",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := config.BoxSchema()
			if err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, string(data))
			return nil
		},
	}
}
