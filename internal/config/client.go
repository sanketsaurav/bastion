package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ClientConfig is the local client configuration (kind: ClientConfig). It
// holds registrations and preferences — never box desired state.
type ClientConfig struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	CurrentBox string            `yaml:"currentBox,omitempty" json:"currentBox,omitempty"`
	Boxes      map[string]string `yaml:"boxes,omitempty" json:"boxes,omitempty"`
	Output     OutputPrefs       `yaml:"output,omitempty" json:"output,omitempty"`
}

type OutputPrefs struct {
	Color string `yaml:"color,omitempty" json:"color,omitempty"`
}

// NewClientConfig returns an empty, well-formed client configuration.
func NewClientConfig() *ClientConfig {
	return &ClientConfig{APIVersion: APIVersion, Kind: KindClientConfig, Boxes: map[string]string{}}
}

// LoadClientConfig reads the client configuration, returning a fresh default
// when the file does not exist yet.
func LoadClientConfig(file string) (*ClientConfig, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return NewClientConfig(), nil
	}
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c ClientConfig
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: parsing client configuration: %w", file, err)
	}
	if c.APIVersion != APIVersion {
		return nil, fmt.Errorf("%s: apiVersion: expected %q, got %q", file, APIVersion, c.APIVersion)
	}
	if c.Kind != KindClientConfig {
		return nil, fmt.Errorf("%s: kind: expected %q, got %q", file, KindClientConfig, c.Kind)
	}
	if c.Boxes == nil {
		c.Boxes = map[string]string{}
	}
	return &c, nil
}

// Save writes the client configuration atomically (temp file + rename).
func (c *ClientConfig) Save(file string) error {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
