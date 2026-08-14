package xdg

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	dir, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join("/custom/config", "bastion") {
		t.Errorf("ConfigDir = %q", dir)
	}
}

func TestDefaultsUnderHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	config, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(config, filepath.Join(".config", "bastion")) {
		t.Errorf("ConfigDir = %q", config)
	}
	state, err := StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(state, filepath.Join(".local", "state", "bastion")) {
		t.Errorf("StateDir = %q", state)
	}
	cache, err := CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cache, filepath.Join(".cache", "bastion")) {
		t.Errorf("CacheDir = %q", cache)
	}
}
