package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/audit"
)

// auditCmd runs the hardening checks: a short list of high-value findings
// (attached cloud identity, world-open firewall rules, boot security,
// self-applying updates, exposed listeners, password auth), each with its
// exact remediation. Read-only; provider checks run even when the box is
// stopped.
func (a *App) auditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "audit [box]",
		Short: "Check the box's hardening (read-only) and print exact fixes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			client := a.clientFor(res)
			ctx := cmd.Context()
			reachable := false
			if inst, err := client.Describe(ctx); err == nil && inst.Running() {
				reachable = client.CheckSSH(ctx) == nil
			}
			results := audit.Run(ctx, audit.Deps{
				Cloud:          client,
				Session:        client,
				Box:            res.Loaded.Box,
				GuestReachable: reachable,
			})

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
					case audit.OK:
						glyph = u.paint(ansiGreen, "✓")
					case audit.Warn:
						glyph = u.paint(ansiYellow, "!")
					case audit.Fail:
						glyph = u.paint(ansiRed, "✗")
					case audit.Skip:
						glyph = u.paint(ansiDim, "-")
					}
					fmt.Fprintf(u.out, "%s %-*s  %s\n", glyph, width, r.Name, r.Detail)
					if r.Hint != "" {
						fmt.Fprintf(u.out, "  %-*s  %s\n", width, "", u.paint(ansiDim, "↳ "+r.Hint))
					}
				}
			}
			if audit.Failed(results) {
				return exitWithCode(1)
			}
			return nil
		},
	}
}
