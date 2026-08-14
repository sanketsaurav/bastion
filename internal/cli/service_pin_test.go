package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pinFixture = `apiVersion: bastion/v1alpha1
kind: Box
metadata:
  name: dev
provider:
  name: gcp
  project: p
  zone: z
  instance: vm
services:
  web:
    image: docker.io/library/nginx:1.27-alpine
`

func writePinFixture(t *testing.T, doc string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "bastion.yaml")
	if err := os.WriteFile(file, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestPinImageInConfig(t *testing.T) {
	file := writePinFixture(t, pinFixture)
	old := "docker.io/library/nginx:1.27-alpine"
	pinned := "docker.io/library/nginx@sha256:00112233445566778899001122334455667788990011223344556677889900aa"

	if err := pinImageInConfig(file, old, pinned); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), pinned) || strings.Contains(string(data), old) {
		t.Errorf("pin not applied:\n%s", data)
	}
}

func TestPinImageInConfigAmbiguous(t *testing.T) {
	doc := strings.Replace(pinFixture, "services:", `services:
  other:
    image: docker.io/library/nginx:1.27-alpine
`, 1)
	file := writePinFixture(t, doc)
	err := pinImageInConfig(file, "docker.io/library/nginx:1.27-alpine", "docker.io/library/nginx@sha256:aa")
	if err == nil || !strings.Contains(err.Error(), "manually") {
		t.Fatalf("ambiguous references must refuse the edit, got: %v", err)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), "nginx:1.27-alpine") {
		t.Error("file must be untouched after a refused edit")
	}
}

func TestPinImageInConfigRestoresOnInvalid(t *testing.T) {
	file := writePinFixture(t, pinFixture)
	// A "pin" that would corrupt the YAML structure must be rolled back.
	err := pinImageInConfig(file, "docker.io/library/nginx:1.27-alpine", "x: [broken\nyaml: {")
	if err == nil {
		t.Fatal("expected a validation failure")
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), "nginx:1.27-alpine") {
		t.Error("original content must be restored after a failed validation")
	}
}

func TestPinImageMissing(t *testing.T) {
	file := writePinFixture(t, pinFixture)
	if err := pinImageInConfig(file, "ghcr.io/absent:1", "ghcr.io/absent@sha256:aa"); err == nil {
		t.Fatal("expected a not-found error")
	}
}
