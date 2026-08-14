package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/version"
)

func (a *App) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the bastion version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			u := a.ui()
			if u.json {
				return u.emit(map[string]string{
					"version": version.Version,
					"os":      runtime.GOOS,
					"arch":    runtime.GOARCH,
				})
			}
			fmt.Fprintf(u.out, "bastion %s (%s/%s)\n", version.Version, runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
