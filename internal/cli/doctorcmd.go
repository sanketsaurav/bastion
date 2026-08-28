package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/doctor"
)

func (a *App) doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [box]",
		Short: "Diagnose the local environment and box connectivity",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			sp := a.ui().spin("Running checks", "")
			results := doctor.Run(cmd.Context(), doctor.Deps{
				Runner:   a.runner,
				LookPath: a.lookPath,
				Client:   a.clientFor(res),
				Box:      res.Loaded.Box,
			})

			sp.erase()
			u := a.ui()
			if u.json {
				if err := u.emit(results); err != nil {
					return err
				}
			} else {
				width := 0
				for _, r := range results {
					if len(r.Name) > width {
						width = len(r.Name)
					}
				}
				for _, r := range results {
					var glyph string
					switch r.Status {
					case doctor.OK:
						glyph = u.paint(ansiGreen, "✓")
					case doctor.Warn:
						glyph = u.paint(ansiYellow, "!")
					case doctor.Fail:
						glyph = u.paint(ansiRed, "✗")
					case doctor.Skip:
						glyph = u.paint(ansiDim, "-")
					}
					fmt.Fprintf(u.out, "%s %-*s  %s\n", glyph, width, r.Name, r.Detail)
					if r.Hint != "" {
						fmt.Fprintf(u.out, "  %-*s  %s\n", width, "", u.paint(ansiDim, "↳ "+r.Hint))
					}
				}
			}
			if doctor.Failed(results) {
				return exitWithCode(1)
			}
			return nil
		},
	}
}
