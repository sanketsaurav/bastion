package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type endpointRow struct {
	Service       string `json:"service"`
	Endpoint      string `json:"endpoint"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
	Visibility    string `json:"visibility"`
	VMPort        int    `json:"vmPort,omitempty"`
	Access        string `json:"access"`
}

func (a *App) endpointCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "endpoint",
		Short: "Inspect declared service endpoints",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list [box]",
		Short: "List every declared endpoint and how to reach it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := a.resolveBox(args)
			if err != nil {
				return err
			}
			box := res.Loaded.Box
			var rows []endpointRow
			for _, svc := range sortedServiceNames(res) {
				s := box.Services[svc]
				for _, en := range sortedStrings(s.Endpoints) {
					ep := s.Endpoints[en]
					row := endpointRow{
						Service: svc, Endpoint: en,
						ContainerPort: ep.ContainerPort, Protocol: ep.Protocol, Visibility: ep.Visibility,
					}
					switch ep.Visibility {
					case "private":
						row.VMPort = ep.EffectiveVMPort()
						row.Access = fmt.Sprintf("bastion port %s %s:%s", box.Metadata.Name, svc, en)
					default:
						row.Access = fmt.Sprintf("%s:%d on the box network", svc, ep.ContainerPort)
					}
					rows = append(rows, row)
				}
			}
			u := a.ui()
			if u.json {
				if rows == nil {
					rows = []endpointRow{}
				}
				return u.emit(rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(u.out, "No endpoints declared.")
				return nil
			}
			for _, row := range rows {
				fmt.Fprintf(u.out, "%-28s %-9s %-8s %s\n",
					row.Service+":"+row.Endpoint,
					row.Visibility,
					fmt.Sprintf("%s/%d", row.Protocol, row.ContainerPort),
					u.paint(ansiDim, row.Access))
			}
			return nil
		},
	})
	return cmd
}

func sortedStrings[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
