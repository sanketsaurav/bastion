package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanketsaurav/bastion/internal/config"
)

func writeBoxDir(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	doc := fmt.Sprintf(`apiVersion: bastion/v1alpha1
kind: Box
metadata:
  name: %s
provider:
  name: gcp
  project: proj
  zone: us-west1-a
  instance: vm
`, name)
	if err := os.WriteFile(filepath.Join(dir, "bastion.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	return &Registry{
		File:   filepath.Join(t.TempDir(), "config.yaml"),
		Client: config.NewClientConfig(),
	}
}

func TestOpenUsesXDGConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	reg, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "bastion", "config.yaml")
	if reg.File != want {
		t.Errorf("registry file = %q, want %q", reg.File, want)
	}
}

func TestAdoptUseForget(t *testing.T) {
	reg := testRegistry(t)
	dir := writeBoxDir(t, "agents")

	if _, err := reg.Adopt("agents", dir); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := reg.Use("agents"); err != nil {
		t.Fatal(err)
	}
	if reg.Client.CurrentBox != "agents" {
		t.Errorf("current = %q", reg.Client.CurrentBox)
	}

	back, err := config.LoadClientConfig(reg.File)
	if err != nil {
		t.Fatal(err)
	}
	if back.Boxes["agents"] != dir {
		t.Errorf("persisted path = %q, want %q", back.Boxes["agents"], dir)
	}

	if !reg.Forget("agents") {
		t.Error("Forget should report the entry existed")
	}
	if reg.Client.CurrentBox != "" {
		t.Error("Forget must clear a matching currentBox")
	}
	if reg.Forget("agents") {
		t.Error("second Forget should report absence")
	}
}

func TestAdoptNameMismatch(t *testing.T) {
	reg := testRegistry(t)
	dir := writeBoxDir(t, "agents")
	if _, err := reg.Adopt("other", dir); err == nil || !strings.Contains(err.Error(), `named "agents"`) {
		t.Fatalf("expected a name-mismatch error, got: %v", err)
	}
}

func TestAdoptConflictingPath(t *testing.T) {
	reg := testRegistry(t)
	dirA := writeBoxDir(t, "agents")
	dirB := writeBoxDir(t, "agents")
	if _, err := reg.Adopt("agents", dirA); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Adopt("agents", dirB); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected an already-registered error, got: %v", err)
	}
}

func TestUseUnknown(t *testing.T) {
	reg := testRegistry(t)
	if err := reg.Use("ghost"); err == nil {
		t.Fatal("expected an error for an unknown box")
	}
}
