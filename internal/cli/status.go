package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/provider"
)

type statusReport struct {
	Box        string             `json:"box"`
	Definition string             `json:"definition"`
	Source     string             `json:"source"`
	Provider   statusProvider     `json:"provider"`
	Connection string             `json:"connection"`
	Instance   *provider.Instance `json:"instance"`
}

type statusProvider struct {
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Project  string `json:"project"`
	Zone     string `json:"zone"`
	Instance string `json:"instance"`
}

func (a *App) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [box]",
		Short: "Show provider and instance state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			box := res.Loaded.Box
			inst, err := a.clientFor(res).Describe(cmd.Context())
			if err != nil {
				return err
			}

			u := a.ui()
			if u.json {
				return u.emit(statusReport{
					Box:        box.Metadata.Name,
					Definition: res.Loaded.File,
					Source:     res.Source,
					Provider: statusProvider{
						Name:     box.Provider.Name,
						Mode:     box.Provider.Mode,
						Project:  box.Provider.Project,
						Zone:     box.Provider.Zone,
						Instance: box.Provider.Instance,
					},
					Connection: box.Connection.Type,
					Instance:   inst,
				})
			}

			external := inst.ExternalIP
			if external == "" {
				external = "(none)"
			}
			rows := [][2]string{
				{"Box", fmt.Sprintf("%s  (%s/%s, %s)", box.Metadata.Name, box.Provider.Name, box.Provider.Mode, box.Connection.Type)},
				{"Definition", fmt.Sprintf("%s  (%s)", res.Loaded.File, res.Source)},
				{"Instance", fmt.Sprintf("%s — project %s, zone %s", box.Provider.Instance, box.Provider.Project, inst.Zone)},
				{"Status", u.statusPaint(inst.Status)},
				{"Machine", inst.MachineType},
				{"Internal IP", valueOr(inst.InternalIP, "(none)")},
				{"External IP", external},
			}
			if inst.LastStart != "" {
				rows = append(rows, [2]string{"Last start", inst.LastStart})
			}
			if inst.LastStop != "" {
				rows = append(rows, [2]string{"Last stop", inst.LastStop})
			}
			u.printKV(rows)
			return nil
		},
	}
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
