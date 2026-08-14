package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"
)

// ByteSize is a human-readable size such as "10MiB" or "1GiB".
type ByteSize struct {
	bytes int64
	raw   string
}

var sizeRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([A-Za-z]*)$`)

var sizeUnits = map[string]float64{
	"":    1,
	"b":   1,
	"kb":  1e3,
	"kib": 1 << 10,
	"mb":  1e6,
	"mib": 1 << 20,
	"gb":  1e9,
	"gib": 1 << 30,
	"tb":  1e12,
	"tib": 1 << 40,
}

// ParseByteSize parses a size string such as "512KiB", "10MiB", or "1GiB".
func ParseByteSize(s string) (ByteSize, error) {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return ByteSize{}, fmt.Errorf("invalid size %q (expected e.g. \"10MiB\", \"1GiB\")", s)
	}
	unit, ok := sizeUnits[strings.ToLower(m[2])]
	if !ok {
		return ByteSize{}, fmt.Errorf("invalid size unit %q in %q (use B, KiB, MiB, GiB, TiB or KB, MB, GB, TB)", m[2], s)
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return ByteSize{}, fmt.Errorf("invalid size %q: %v", s, err)
	}
	return ByteSize{bytes: int64(n * unit), raw: strings.TrimSpace(s)}, nil
}

func (b ByteSize) Bytes() int64   { return b.bytes }
func (b ByteSize) IsZero() bool   { return b.raw == "" }
func (b ByteSize) String() string { return b.raw }

func (b *ByteSize) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: size must be a scalar such as \"10MiB\"", node.Line)
	}
	v, err := ParseByteSize(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: %v", node.Line, err)
	}
	*b = v
	return nil
}

func (b ByteSize) MarshalJSON() ([]byte, error) { return json.Marshal(b.raw) }

func (ByteSize) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "string",
		Description: "A size such as \"512KiB\", \"10MiB\", or \"1GiB\".",
		Pattern:     `^\d+(\.\d+)?\s*(B|KB|KiB|MB|MiB|GB|GiB|TB|TiB)?$`,
	}
}

// Duration is a Go-style duration such as "30s" or "5m".
type Duration struct {
	d   time.Duration
	raw string
}

func (d Duration) Value() time.Duration { return d.d }
func (d Duration) IsZero() bool         { return d.raw == "" }
func (d Duration) String() string       { return d.raw }

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: duration must be a scalar such as \"30s\"", node.Line)
	}
	v, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q (expected e.g. \"30s\", \"5m\")", node.Line, node.Value)
	}
	if v < 0 {
		return fmt.Errorf("line %d: duration %q must not be negative", node.Line, node.Value)
	}
	*d = Duration{d: v, raw: node.Value}
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.raw) }

func (Duration) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "string",
		Description: "A duration such as \"30s\" or \"5m\".",
		Pattern:     `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`,
	}
}

// EnvValue is a container environment value: either a literal string or a
// reference to a declared secret ({secretRef: name}).
type EnvValue struct {
	Literal   string
	IsLiteral bool
	SecretRef string
}

func (e *EnvValue) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		// Numbers and booleans arrive as their textual form, which is what an
		// environment variable would carry anyway.
		e.Literal = node.Value
		e.IsLiteral = true
		return nil
	case yaml.MappingNode:
		if len(node.Content) != 2 {
			return fmt.Errorf("line %d: environment value must be a string or {secretRef: name}", node.Line)
		}
		key, val := node.Content[0], node.Content[1]
		if key.Value != "secretRef" {
			return fmt.Errorf("line %d: unknown key %q in environment value (only secretRef is allowed)", key.Line, key.Value)
		}
		if val.Kind != yaml.ScalarNode || val.Value == "" {
			return fmt.Errorf("line %d: secretRef must be a secret name", val.Line)
		}
		e.SecretRef = val.Value
		return nil
	default:
		return fmt.Errorf("line %d: environment value must be a string or {secretRef: name}", node.Line)
	}
}

func (e EnvValue) MarshalJSON() ([]byte, error) {
	if e.IsLiteral {
		return json.Marshal(e.Literal)
	}
	return json.Marshal(map[string]string{"secretRef": e.SecretRef})
}

func (EnvValue) JSONSchema() *jsonschema.Schema {
	props := jsonschema.NewProperties()
	props.Set("secretRef", &jsonschema.Schema{Type: "string"})
	return &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			{Type: "string"},
			{Type: "object", Properties: props, Required: []string{"secretRef"}, AdditionalProperties: jsonschema.FalseSchema},
		},
	}
}
