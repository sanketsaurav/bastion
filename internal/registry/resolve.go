package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sanketsaurav/bastion/internal/config"
)

// Request carries every input that participates in box resolution.
type Request struct {
	ConfigFlag string // --config
	EnvConfig  string // $BASTION_CONFIG
	Name       string // positional argument or --box
	CWD        string
}

// Resolution is a resolved and loaded box definition plus where it came from.
type Resolution struct {
	Loaded *config.Loaded
	Source string
}

const (
	SourceFlag     = "--config"
	SourceEnv      = "BASTION_CONFIG"
	SourceCWD      = "current directory"
	SourceRegistry = "registry"
	SourceCurrent  = "current box"
)

// Resolve selects a box definition following SPEC.md §5.2. Ambiguity — the
// same name resolving to two different definitions — is always an error;
// Bastion never silently chooses.
func (r *Registry) Resolve(req Request) (*Resolution, error) {
	if req.ConfigFlag != "" {
		return loadChecked(req.ConfigFlag, req.Name, SourceFlag)
	}
	if req.EnvConfig != "" {
		return loadChecked(req.EnvConfig, req.Name, SourceEnv)
	}

	cwdFile := filepath.Join(req.CWD, config.BoxFileName)
	if req.Name != "" {
		// A definition in the current directory counts only when its own
		// metadata.name matches the requested name; a broken or unrelated
		// bastion.yaml must not shadow a registered box.
		var cwdLoaded *config.Loaded
		if fileExists(cwdFile) {
			if l, err := config.Load(cwdFile); err == nil && l.Box.Metadata.Name == req.Name {
				cwdLoaded = l
			}
		}
		regPath, registered := r.Client.Boxes[req.Name]
		if cwdLoaded != nil && registered && filepath.Clean(regPath) != filepath.Clean(cwdLoaded.Dir) {
			return nil, fmt.Errorf(
				"box %q is ambiguous: %s (current directory) and %s (registry); pass --config to choose one",
				req.Name, cwdLoaded.Dir, regPath)
		}
		if cwdLoaded != nil {
			return &Resolution{Loaded: cwdLoaded, Source: SourceCWD}, nil
		}
		if registered {
			loaded, err := config.Load(regPath)
			if err != nil {
				return nil, err
			}
			if loaded.Box.Metadata.Name != req.Name {
				return nil, fmt.Errorf(
					"registered box %q now has metadata.name %q at %s; re-adopt it under its new name",
					req.Name, loaded.Box.Metadata.Name, loaded.File)
			}
			return &Resolution{Loaded: loaded, Source: SourceRegistry}, nil
		}
		return nil, unknownBoxError(req.Name, r.Names())
	}

	if fileExists(cwdFile) {
		loaded, err := config.Load(cwdFile)
		if err != nil {
			return nil, err
		}
		return &Resolution{Loaded: loaded, Source: SourceCWD}, nil
	}
	if cur := r.Client.CurrentBox; cur != "" {
		path, ok := r.Client.Boxes[cur]
		if !ok {
			return nil, fmt.Errorf("current box %q is not registered; run `bastion box use` or `bastion box adopt`", cur)
		}
		loaded, err := config.Load(path)
		if err != nil {
			return nil, err
		}
		return &Resolution{Loaded: loaded, Source: SourceCurrent}, nil
	}
	return nil, errors.New(
		"no box selected: pass a box name, run inside a directory containing bastion.yaml, " +
			"or pick a current box with `bastion box use` (create one with `bastion init`)")
}

func loadChecked(path, name, source string) (*Resolution, error) {
	loaded, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if name != "" && loaded.Box.Metadata.Name != name {
		return nil, fmt.Errorf("definition at %s is named %q, not %q", loaded.File, loaded.Box.Metadata.Name, name)
	}
	return &Resolution{Loaded: loaded, Source: source}, nil
}

func unknownBoxError(name string, known []string) error {
	if len(known) == 0 {
		return fmt.Errorf("unknown box %q: no boxes are registered yet (see `bastion box adopt`)", name)
	}
	return fmt.Errorf("unknown box %q: registered boxes: %s", name, strings.Join(known, ", "))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
