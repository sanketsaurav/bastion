package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/registry"
)

type validateReport struct {
	Valid  bool     `json:"valid"`
	Name   string   `json:"name,omitempty"`
	File   string   `json:"file,omitempty"`
	Issues []string `json:"issues,omitempty"`
}

func (a *App) validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [box|path]",
		Short: "Validate a box definition without contacting GCP",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u := a.ui()
			res, err := a.resolveForValidate(args)
			if err != nil {
				report := validateReport{Valid: false}
				var verr *config.ValidationError
				if errors.As(err, &verr) {
					report.File = verr.File
					report.Issues = verr.Issues
				} else {
					report.Issues = []string{err.Error()}
				}
				if u.json {
					if err := u.emit(report); err != nil {
						return err
					}
					return exitWithCode(1)
				}
				fmt.Fprintf(u.out, "%s invalid box definition\n", u.paint(ansiRed, "✗"))
				for _, issue := range report.Issues {
					fmt.Fprintf(u.out, "  - %s\n", issue)
				}
				return exitWithCode(1)
			}

			box := res.Loaded.Box
			if u.json {
				return u.emit(validateReport{Valid: true, Name: box.Metadata.Name, File: res.Loaded.File})
			}
			fmt.Fprintf(u.out, "%s %s is a valid Box definition\n", u.paint(ansiGreen, "✓"), box.Metadata.Name)
			rows := [][2]string{{"File", res.Loaded.File}}
			if box.Host != nil {
				rows = append(rows,
					[2]string{"Packages", fmt.Sprintf("%d", len(box.Host.Packages))},
					[2]string{"Features", fmt.Sprintf("%d", len(box.Host.Features))},
					[2]string{"Files", fmt.Sprintf("%d", len(box.Host.Files))},
				)
			}
			rows = append(rows,
				[2]string{"Services", fmt.Sprintf("%d", len(box.Services))},
				[2]string{"Volumes", fmt.Sprintf("%d", len(box.Volumes))},
				[2]string{"Secrets", fmt.Sprintf("%d", len(box.Secrets))},
			)
			u.printKV(rows)
			return nil
		},
	}
}

// resolveForValidate accepts either a box name or a filesystem path as the
// positional argument — validate is the one command where a bare path is the
// common case (`bastion validate ./examples/agents`).
func (a *App) resolveForValidate(args []string) (*registry.Resolution, error) {
	if len(args) == 1 && a.flags.config == "" {
		arg := args[0]
		looksLikePath := strings.ContainsRune(arg, os.PathSeparator) || arg == "." || arg == ".."
		if _, err := os.Stat(arg); err == nil && (looksLikePath || !isRegisteredName(arg)) {
			loaded, err := config.Load(arg)
			if err != nil {
				return nil, err
			}
			return &registry.Resolution{Loaded: loaded, Source: registry.SourceFlag}, nil
		}
	}
	return a.resolveBox(args)
}

func isRegisteredName(name string) bool {
	reg, err := registry.Open()
	if err != nil {
		return false
	}
	_, ok := reg.Client.Boxes[name]
	return ok
}
