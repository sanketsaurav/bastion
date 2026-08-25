package engine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sanketsaurav/bastion/internal/shellquote"
)

// LocalFeatureMeta is the strict feature.yaml contract (SPEC.md §8.4).
type LocalFeatureMeta struct {
	Name         string `yaml:"name" json:"name"`
	Version      string `yaml:"version" json:"version"`
	RequiresRoot bool   `yaml:"requiresRoot,omitempty" json:"requiresRoot,omitempty"`
}

// localFeatureName derives the feature name from its `uses: ./…` path.
func localFeatureName(uses string) string {
	return path.Base(path.Clean(strings.TrimPrefix(uses, "./")))
}

// LocalFeatureNameOf is localFeatureName for callers outside the engine.
func LocalFeatureNameOf(uses string) string { return localFeatureName(uses) }

// localFeatureRemoteDir returns a shell expression (already quoted) for the
// feature's remote source directory — $HOME must expand, the rest must not.
func localFeatureRemoteDir(boxID, name string) string {
	return `"$HOME"/` + shellquote.Quote(".cache/bastion/features/"+boxID+"/"+name)
}

// LoadLocalFeature reads and validates a local feature directory: strict
// feature.yaml, executable check and apply entry points.
func LoadLocalFeature(boxDir, uses string) (*LocalFeatureMeta, string, error) {
	rel := filepath.Clean(strings.TrimPrefix(uses, "./"))
	dir := filepath.Join(boxDir, rel)
	metaPath := filepath.Join(dir, "feature.yaml")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, dir, fmt.Errorf("local feature %s: missing feature.yaml", uses)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var meta LocalFeatureMeta
	if err := dec.Decode(&meta); err != nil {
		return nil, dir, fmt.Errorf("local feature %s: parsing feature.yaml: %w", uses, err)
	}
	if meta.Name == "" {
		meta.Name = localFeatureName(uses)
	}
	if meta.Name != localFeatureName(uses) {
		return nil, dir, fmt.Errorf("local feature %s: feature.yaml name %q must match directory name %q",
			uses, meta.Name, localFeatureName(uses))
	}
	if meta.Version == "" {
		meta.Version = "0"
	}
	for _, entry := range []string{"check", "apply"} {
		info, err := os.Stat(filepath.Join(dir, entry))
		if err != nil {
			return nil, dir, fmt.Errorf("local feature %s: missing %s executable", uses, entry)
		}
		if info.Mode()&0o100 == 0 {
			return nil, dir, fmt.Errorf("local feature %s: %s is not executable (chmod +x it)", uses, entry)
		}
	}
	return &meta, dir, nil
}

// PackLocalFeature builds a deterministic tar.gz of the feature directory so
// the source digest is stable across machines and runs.
func PackLocalFeature(dir string) ([]byte, string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Strings(files)

	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	tw := tar.NewWriter(gz)
	for _, p := range files {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil, "", err
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, "", err
		}
		mode := int64(0o644)
		if info.Mode()&0o100 != 0 {
			mode = 0o755
		}
		hdr := &tar.Header{
			Name: filepath.ToSlash(rel),
			Mode: mode,
			Size: info.Size(),
			// Zero times keep the archive — and therefore the source
			// digest — deterministic.
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, "", err
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, "", err
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return nil, "", err
		}
		f.Close()
	}
	if err := tw.Close(); err != nil {
		return nil, "", err
	}
	if err := gz.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), sha256hex(buf.Bytes()), nil
}

// localFeatureInputs renders the validated `with` map as the JSON input file
// the feature's executables receive (never shell-interpolated).
func localFeatureInputs(with map[string]any) ([]byte, string) {
	if with == nil {
		with = map[string]any{}
	}
	data, err := json.Marshal(with)
	if err != nil {
		data = []byte("{}")
	}
	return data, sha256hex(data)
}
