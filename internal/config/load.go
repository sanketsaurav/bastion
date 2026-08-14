package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// BoxFileName is the canonical box definition filename.
const BoxFileName = "bastion.yaml"

// Loaded is a parsed, normalized, and validated box definition together with
// its location. Dir anchors relative references (files/, features/).
type Loaded struct {
	Box  *Box
	File string
	Dir  string
}

// Load reads a box definition from path — either the bastion.yaml itself or a
// directory containing one.
func Load(path string) (*Loaded, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("box definition not found at %s", abs)
	}
	file := abs
	if info.IsDir() {
		file = filepath.Join(abs, BoxFileName)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("box definition not found at %s", file)
	}
	box, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	dir := filepath.Dir(file)
	if issues := ValidateBox(box, dir); len(issues) > 0 {
		return nil, &ValidationError{File: file, Issues: issues}
	}
	return &Loaded{Box: box, File: file, Dir: dir}, nil
}

// Parse strictly decodes and normalizes a Box document. Unknown fields are
// errors. Semantic validation is separate (ValidateBox).
func Parse(data []byte) (*Box, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var box Box
	if err := dec.Decode(&box); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty configuration document")
		}
		return nil, fmt.Errorf("parsing configuration: %w", err)
	}
	box.Normalize()
	return &box, nil
}

// ValidationError aggregates every semantic problem found in a definition so
// users fix them in one pass.
type ValidationError struct {
	File   string
	Issues []string
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "invalid box definition %s:", e.File)
	for _, issue := range e.Issues {
		fmt.Fprintf(&b, "\n  - %s", issue)
	}
	return b.String()
}
