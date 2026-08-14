package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sanketsaurav/bastion/internal/config"
)

// ResolveSecrets resolves every declared secret's value from its source.
// Called only at apply time — plans never resolve values (SPEC.md §9.4).
// The returned map must never be logged, serialized, or embedded in digests.
func ResolveSecrets(box *config.Box, getenv func(string) string) (map[string]string, error) {
	values := map[string]string{}
	for _, name := range sortedKeys(box.Secrets) {
		src := box.Secrets[name].Source
		var value string
		switch {
		case src.Environment != "":
			value = getenv(src.Environment)
			if value == "" {
				return nil, fmt.Errorf("secret %q: environment variable %s is not set (export it before applying)", name, src.Environment)
			}
		case src.File != "":
			path := src.File
			if strings.HasPrefix(path, "~/") {
				home, err := os.UserHomeDir()
				if err != nil {
					return nil, err
				}
				path = filepath.Join(home, path[2:])
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("secret %q: %w", name, err)
			}
			value = strings.TrimRight(string(data), "\r\n")
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("secret %q: multi-line values are not supported for environment secrets", name)
		}
		values[name] = value
	}
	return values, nil
}

// BuildSecretEnvFile renders the env file mounted for one service:
// KEY=value lines for every secretRef in its environment, sorted.
func BuildSecretEnvFile(box *config.Box, svc string, values map[string]string) ([]byte, error) {
	s := box.Services[svc]
	var b strings.Builder
	for _, key := range sortedKeys(s.Environment) {
		v := s.Environment[key]
		if v.IsLiteral {
			continue
		}
		value, ok := values[v.SecretRef]
		if !ok {
			return nil, fmt.Errorf("service %q references unresolved secret %q", svc, v.SecretRef)
		}
		fmt.Fprintf(&b, "%s=%s\n", key, value)
	}
	return []byte(b.String()), nil
}
