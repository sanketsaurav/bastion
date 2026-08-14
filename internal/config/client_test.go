package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientConfigMissingFileIsDefault(t *testing.T) {
	c, err := LoadClientConfig(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.APIVersion != APIVersion || c.Kind != KindClientConfig {
		t.Errorf("default = %+v", c)
	}
	if c.Boxes == nil {
		t.Error("Boxes map must be initialized")
	}
}

func TestClientConfigRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nested", "config.yaml")
	c := NewClientConfig()
	c.CurrentBox = "agents"
	c.Boxes["agents"] = "/home/user/boxes/agents"
	if err := c.Save(file); err != nil {
		t.Fatal(err)
	}
	back, err := LoadClientConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	if back.CurrentBox != "agents" || back.Boxes["agents"] != "/home/user/boxes/agents" {
		t.Errorf("round trip = %+v", back)
	}
}

func TestClientConfigStrictParsing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.yaml")
	doc := "apiVersion: bastion/v1alpha1\nkind: ClientConfig\nboxez: {}\n"
	if err := os.WriteFile(file, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClientConfig(file); err == nil || !strings.Contains(err.Error(), "boxez") {
		t.Fatalf("expected an unknown-field error, got: %v", err)
	}
}

func TestBoxSchemaGenerates(t *testing.T) {
	data, err := BoxSchema()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"secretRef", "pullPolicy", "containerPort", "unless-stopped"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("schema missing %q", want)
		}
	}
}
