package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/engine"
	"github.com/sanketsaurav/bastion/internal/provider/gcp"
	"github.com/sanketsaurav/bastion/internal/registry"
	"github.com/sanketsaurav/bastion/internal/version"
)

func engineInput(res *registry.Resolution) *engine.Input {
	return &engine.Input{
		Box:     res.Loaded.Box,
		Dir:     res.Loaded.Dir,
		BoxID:   engine.BoxID(res.Loaded.Box),
		Version: version.Version,
	}
}

// computePlan inspects the guest and diffs it against the definition.
func (a *App) computePlan(ctx context.Context, client *gcp.Client, in *engine.Input) (*engine.Plan, *engine.Facts, error) {
	u := a.ui()
	u.progressf("Inspecting %s…", in.BoxID)
	facts, raw, err := engine.Inspect(ctx, client, in)
	if err != nil {
		if u.verbose {
			for _, line := range raw {
				u.debugf("inspect: %s", line)
			}
		}
		return nil, nil, err
	}
	plan, err := engine.BuildPlan(in, facts)
	if err != nil {
		return nil, facts, err
	}
	return plan, facts, nil
}

type planReport struct {
	Plan           *engine.Plan `json:"plan"`
	InstanceStatus string       `json:"instanceStatus"`
}

func (a *App) planCmd() *cobra.Command {
	var detailedExit bool
	var rotateSecrets bool
	cmd := &cobra.Command{
		Use:   "plan [box]",
		Short: "Show what apply would change (read-only)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			client := a.clientFor(res)
			in := engineInput(res)
			in.RotateSecrets = rotateSecrets
			u := a.ui()
			ctx := cmd.Context()

			inst, err := client.Describe(ctx)
			if err != nil {
				return err
			}
			var plan *engine.Plan
			if inst.Running() {
				plan, _, err = a.computePlan(ctx, client, in)
				if err != nil {
					return err
				}
			} else {
				// Plan never starts a stopped box (SPEC.md §10).
				plan = &engine.Plan{BoxID: in.BoxID, GuestUnknown: true, InstanceStatus: inst.Status}
				plan.Actions = append(plan.Actions, engine.Action{
					ID: "instance:start", Kind: engine.KindInstanceStart,
					Summary: fmt.Sprintf("start instance %s (currently %s)", res.Loaded.Box.Provider.Instance, inst.Status),
				})
				plan.Notes = append(plan.Notes,
					"the box is not running, so guest state is unknown; run `bastion up` to start it and converge")
			}
			plan.InstanceStatus = inst.Status

			if u.json {
				if err := u.emit(planReport{Plan: plan, InstanceStatus: inst.Status}); err != nil {
					return err
				}
			} else {
				renderPlan(u, plan)
			}
			if detailedExit && plan.Changes() {
				return exitWithCode(2)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&detailedExit, "detailed-exitcode", false, "exit 0 = no changes, 2 = changes proposed, 1 = error")
	cmd.Flags().BoolVar(&rotateSecrets, "rotate-secrets", false,
		"preview a secret rotation: the secret rewrites and container replacements apply --rotate-secrets would run")
	return cmd
}

func renderPlan(u *ui, plan *engine.Plan) {
	if !plan.Changes() && !plan.GuestUnknown {
		fmt.Fprintf(u.out, "%s No changes. %s matches its definition.\n", u.paint(ansiGreen, "✓"), plan.BoxID)
		renderPlanFootnotes(u, plan)
		return
	}
	word := "actions"
	if len(plan.Actions) == 1 {
		word = "action"
	}
	fmt.Fprintf(u.out, "Plan for %s: %d %s\n", u.paint(ansiBold, plan.BoxID), len(plan.Actions), word)
	for i, act := range plan.Actions {
		tags := ""
		if act.RequiresRoot {
			tags += "  " + u.paint(ansiDim, "[root]")
		}
		if act.LocalCode {
			tags += "  " + u.paint(ansiYellow, "[local executable]")
		}
		marker := "~"
		if act.Destructive {
			marker = u.paint(ansiRed, "−")
			tags += "  " + u.paint(ansiRed, "[destructive]")
		}
		fmt.Fprintf(u.out, "  %s %2d. %s%s\n", marker, i+1, act.Summary, tags)
		for _, d := range act.Detail {
			fmt.Fprintf(u.out, "        %s\n", u.paint(ansiDim, d))
		}
	}
	renderPlanFootnotes(u, plan)
}

func renderPlanFootnotes(u *ui, plan *engine.Plan) {
	for _, w := range plan.Warnings {
		fmt.Fprintf(u.out, "%s %s\n", u.paint(ansiYellow, "!"), w)
	}
	for _, n := range plan.Notes {
		fmt.Fprintf(u.out, "%s %s\n", u.paint(ansiDim, "·"), n)
	}
}
