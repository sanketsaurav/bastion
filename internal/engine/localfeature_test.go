package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackLocalFeatureDeterministic(t *testing.T) {
	in := fixtureInput(t)
	dir := filepath.Join(in.Dir, "features", "mytool")
	_, digest1, err := PackLocalFeature(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, digest2, err := PackLocalFeature(dir)
	if err != nil {
		t.Fatal(err)
	}
	if digest1 != digest2 {
		t.Error("source digest must be deterministic")
	}
	// Content change must change the digest.
	if err := os.WriteFile(filepath.Join(dir, "apply"), []byte("#!/bin/sh\necho changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, digest3, err := PackLocalFeature(dir)
	if err != nil {
		t.Fatal(err)
	}
	if digest3 == digest1 {
		t.Error("digest must change when source changes")
	}
}

func TestLoadLocalFeatureValidation(t *testing.T) {
	in := fixtureInput(t)

	meta, _, err := LoadLocalFeature(in.Dir, "./features/mytool")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Name != "mytool" || meta.Version != "2" {
		t.Errorf("meta = %+v", meta)
	}

	t.Run("missing check executable", func(t *testing.T) {
		bad := filepath.Join(in.Dir, "features", "broken")
		if err := os.MkdirAll(bad, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bad, "feature.yaml"), []byte("name: broken\nversion: \"1\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadLocalFeature(in.Dir, "./features/broken"); err == nil || !strings.Contains(err.Error(), "check") {
			t.Fatalf("expected missing-check error, got: %v", err)
		}
	})

	t.Run("name mismatch", func(t *testing.T) {
		bad := filepath.Join(in.Dir, "features", "misnamed")
		if err := os.MkdirAll(bad, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bad, "feature.yaml"), []byte("name: other\nversion: \"1\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadLocalFeature(in.Dir, "./features/misnamed"); err == nil || !strings.Contains(err.Error(), "match") {
			t.Fatalf("expected name-mismatch error, got: %v", err)
		}
	})

	t.Run("unknown feature.yaml fields are errors", func(t *testing.T) {
		bad := filepath.Join(in.Dir, "features", "extra")
		if err := os.MkdirAll(bad, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bad, "feature.yaml"), []byte("name: extra\nversion: \"1\"\nbogus: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadLocalFeature(in.Dir, "./features/extra"); err == nil || !strings.Contains(err.Error(), "bogus") {
			t.Fatalf("expected strict-parse error, got: %v", err)
		}
	})
}

func TestBuiltinOptionValidation(t *testing.T) {
	def := Builtins["mise"]
	if err := validateOptions(def, map[string]any{"version": "2025.1.0"}); err != nil {
		t.Errorf("version option must be allowed: %v", err)
	}
	if err := validateOptions(def, map[string]any{"channel": "x"}); err == nil {
		t.Error("unknown options must be rejected")
	}
	if _, err := versionOpt(map[string]any{"version": "$(reboot)"}); err == nil {
		t.Error("shell metacharacters in version must be rejected")
	}
	if _, err := versionOpt(map[string]any{"version": 42}); err == nil {
		t.Error("non-string version must be rejected")
	}
}

func TestAllBuiltinsRenderApply(t *testing.T) {
	for name, def := range Builtins {
		body, err := def.ApplyBash(nil)
		if err != nil || body == "" {
			t.Errorf("builtin %s must render an apply body: %v", name, err)
		}
		if def.CheckBash == "" {
			t.Errorf("builtin %s must have a check", name)
		}
	}
}
