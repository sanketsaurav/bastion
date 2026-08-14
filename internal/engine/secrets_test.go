package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSecrets(t *testing.T) {
	in := fixtureInput(t)
	getenv := func(k string) string {
		if k == "TEST_TOKEN" {
			return "hunter2"
		}
		return ""
	}
	values, err := ResolveSecrets(in.Box, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if values["token"] != "hunter2" {
		t.Errorf("values = %v", values)
	}
}

func TestResolveSecretsMissingEnv(t *testing.T) {
	in := fixtureInput(t)
	_, err := ResolveSecrets(in.Box, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "TEST_TOKEN") {
		t.Fatalf("expected missing-env error naming the variable, got: %v", err)
	}
}

func TestResolveSecretsFromFile(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "token")
	if err := os.WriteFile(secretFile, []byte("filesecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := fixtureInput(t)
	sec := in.Box.Secrets["token"]
	sec.Source.Environment = ""
	sec.Source.File = secretFile
	in.Box.Secrets["token"] = sec

	values, err := ResolveSecrets(in.Box, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if values["token"] != "filesecret" {
		t.Errorf("trailing newline must be trimmed, got %q", values["token"])
	}
}

func TestResolveSecretsRejectsMultiline(t *testing.T) {
	in := fixtureInput(t)
	_, err := ResolveSecrets(in.Box, func(k string) string { return "a\nb" })
	if err == nil || !strings.Contains(err.Error(), "multi-line") {
		t.Fatalf("expected multi-line rejection, got: %v", err)
	}
}

func TestBuildSecretEnvFile(t *testing.T) {
	in := fixtureInput(t)
	content, err := BuildSecretEnvFile(in.Box, "web", map[string]string{"token": "hunter2"})
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "API_TOKEN=hunter2\n" {
		t.Errorf("env file = %q", content)
	}
}
