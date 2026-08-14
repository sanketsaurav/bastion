// Package xdg resolves Bastion's local directories. Bastion follows the XDG
// base directory convention on every platform, including macOS (SPEC.md Δ4).
package xdg

import (
	"os"
	"path/filepath"
)

func resolve(env string, fallback ...string) (string, error) {
	if v := os.Getenv(env); v != "" {
		return filepath.Join(v, "bastion"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	parts := append([]string{home}, fallback...)
	parts = append(parts, "bastion")
	return filepath.Join(parts...), nil
}

// ConfigDir holds the client config and default box definitions.
func ConfigDir() (string, error) { return resolve("XDG_CONFIG_HOME", ".config") }

// StateDir holds local operational state. It is a disposable cache and index,
// never the sole source of truth.
func StateDir() (string, error) { return resolve("XDG_STATE_HOME", ".local", "state") }

// CacheDir holds downloaded artifacts such as remote-runner binaries.
func CacheDir() (string, error) { return resolve("XDG_CACHE_HOME", ".cache") }
