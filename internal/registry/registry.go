// Package registry manages local box registrations and resolves which box
// definition a command should operate on (SPEC.md §5.2).
package registry

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/xdg"
)

// Registry is the client configuration plus its storage location.
type Registry struct {
	File   string
	Client *config.ClientConfig
}

// Open loads the registry from the client config directory, creating an empty
// in-memory registry when no file exists yet.
func Open() (*Registry, error) {
	dir, err := xdg.ConfigDir()
	if err != nil {
		return nil, err
	}
	file := filepath.Join(dir, "config.yaml")
	client, err := config.LoadClientConfig(file)
	if err != nil {
		return nil, err
	}
	return &Registry{File: file, Client: client}, nil
}

// Save persists the registry.
func (r *Registry) Save() error { return r.Client.Save(r.File) }

// Adopt validates the definition at path and registers it under name. The
// caller saves. Adoption never mutates the VM.
func (r *Registry) Adopt(name, path string) (*config.Loaded, error) {
	loaded, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if loaded.Box.Metadata.Name != name {
		return nil, fmt.Errorf("definition at %s is named %q, not %q", loaded.File, loaded.Box.Metadata.Name, name)
	}
	if existing, ok := r.Client.Boxes[name]; ok && existing != loaded.Dir {
		return nil, fmt.Errorf("box %q is already registered at %s; run `bastion box forget %s` first", name, existing, name)
	}
	r.Client.Boxes[name] = loaded.Dir
	return loaded, nil
}

// Use selects the current box. The caller saves.
func (r *Registry) Use(name string) error {
	if _, ok := r.Client.Boxes[name]; !ok {
		return unknownBoxError(name, r.Names())
	}
	r.Client.CurrentBox = name
	return nil
}

// Forget removes a registration (never the definition or the VM) and reports
// whether it existed. The caller saves.
func (r *Registry) Forget(name string) bool {
	if _, ok := r.Client.Boxes[name]; !ok {
		return false
	}
	delete(r.Client.Boxes, name)
	if r.Client.CurrentBox == name {
		r.Client.CurrentBox = ""
	}
	return true
}

// Entry is one registered box.
type Entry struct {
	Name    string
	Path    string
	Current bool
}

// List returns registrations sorted by name.
func (r *Registry) List() []Entry {
	entries := make([]Entry, 0, len(r.Client.Boxes))
	for name, path := range r.Client.Boxes {
		entries = append(entries, Entry{Name: name, Path: path, Current: name == r.Client.CurrentBox})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// Names returns registered box names sorted.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.Client.Boxes))
	for name := range r.Client.Boxes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
