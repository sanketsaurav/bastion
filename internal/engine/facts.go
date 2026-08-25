package engine

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Facts is the observed state of the guest, parsed from the inspect script's
// "@f" line protocol. Zero values mean "not observed".
type Facts struct {
	Complete  bool // the end sentinel arrived; output was not truncated
	OSID      string
	OSVersion string
	Arch      string
	SudoOK    bool

	Packages          map[string]bool     // wanted package → installed
	Files             map[string]FileFact // keyed by target as written in config
	Backups           map[string]bool     // target → backup file exists
	Features          map[string]string   // builtin name → detected version ("" = absent)
	Secrets           map[string]bool     // service → env file present
	LocalFeatureCheck map[string]string   // name → ok|needs

	// Every feature marker present on the box, declared or not — the plan
	// subtracts declared names to report orphans.
	FeatureMarkerNames      []string
	LocalFeatureMarkerNames []string
	// Apt prerequisites bastion recorded installing, by owning feature.
	PrereqMarkers map[string][]string

	Docker           DockerFacts
	Services         map[string]ServiceFact // declared and discovered, by service name
	Orphans          []string               // bastion-labeled services no longer declared
	EphemeralVolumes map[string]bool
	DurableVolumes   map[string]bool
	OrphanDurable    []string

	Markers Markers
}

type FileFact struct {
	Exists bool
	SHA256 string // "unreadable" when present but not readable as this user
	Mode   string
}

type DockerFacts struct {
	Installed      bool
	Version        string
	ComposeVersion string
	NetworkExists  bool
}

type ServiceFact struct {
	Exists       bool
	State        string // running, exited, …
	Health       string // healthy, unhealthy, starting, none
	ConfigDigest string
	Image        string
}

// Markers are the per-resource state records Bastion wrote on previous
// applies (SPEC.md Δ10) — evidence, combined with live checks.
type Markers struct {
	Files         map[string]FileMarker // by target
	Features      map[string]FeatureMarker
	LocalFeatures map[string]LocalFeatureMarker
	Services      map[string]ServiceMarker
}

type FileMarker struct {
	Target string `json:"target"`
	SHA256 string `json:"sha256"`
	Mode   string `json:"mode,omitempty"`
	Backup bool   `json:"backup,omitempty"`
}

type FeatureMarker struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	OptionsDigest string `json:"optionsDigest"`
}

type LocalFeatureMarker struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	SourceDigest string `json:"sourceDigest"`
	InputsDigest string `json:"inputsDigest"`
}

type ServiceMarker struct {
	Name         string `json:"name"`
	ConfigDigest string `json:"configDigest"`
	Image        string `json:"image"`
}

// PrereqMarker records one apt package a feature's apply installed because
// it was missing — evidence of ownership, consumed by feature removal.
type PrereqMarker struct {
	Package string `json:"package"`
	Feature string `json:"feature"`
}

func newFacts() *Facts {
	return &Facts{
		Packages:          map[string]bool{},
		Files:             map[string]FileFact{},
		Backups:           map[string]bool{},
		Features:          map[string]string{},
		Secrets:           map[string]bool{},
		LocalFeatureCheck: map[string]string{},
		Services:          map[string]ServiceFact{},
		EphemeralVolumes:  map[string]bool{},
		DurableVolumes:    map[string]bool{},
		PrereqMarkers:     map[string][]string{},
		Markers: Markers{
			Files:         map[string]FileMarker{},
			Features:      map[string]FeatureMarker{},
			LocalFeatures: map[string]LocalFeatureMarker{},
			Services:      map[string]ServiceMarker{},
		},
	}
}

// ParseFacts consumes inspect-script output lines. Unknown or non-protocol
// lines are ignored so stray remote output cannot corrupt the model.
func ParseFacts(lines []string) (*Facts, error) {
	facts := newFacts()
	for _, line := range lines {
		if !strings.HasPrefix(line, "@f ") {
			continue
		}
		parts := strings.Fields(line[3:])
		if len(parts) == 0 {
			continue
		}
		if err := facts.absorb(parts); err != nil {
			return nil, fmt.Errorf("parsing inspect output %q: %w", line, err)
		}
	}
	return facts, nil
}

func b64dec(s string) (string, error) {
	if s == "-" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (f *Facts) absorb(p []string) error {
	switch p[0] {
	case "end":
		f.Complete = true
	case "os":
		if len(p) >= 4 {
			f.OSID, f.OSVersion, f.Arch = p[1], p[2], p[3]
		}
	case "sudo":
		f.SudoOK = len(p) >= 2 && p[1] == "ok"
	case "pkg":
		if len(p) >= 3 {
			f.Packages[p[1]] = p[2] == "installed"
		}
	case "file":
		if len(p) >= 3 {
			target, err := b64dec(p[1])
			if err != nil {
				return err
			}
			ff := FileFact{Exists: p[2] == "present"}
			if ff.Exists && len(p) >= 5 {
				ff.SHA256, ff.Mode = p[3], p[4]
			}
			f.Files[target] = ff
		}
	case "bak":
		if len(p) >= 3 {
			target, err := b64dec(p[1])
			if err != nil {
				return err
			}
			f.Backups[target] = p[2] == "present"
		}
	case "feat":
		if len(p) >= 3 {
			if p[2] == "absent" {
				f.Features[p[1]] = ""
			} else {
				// Tolerate plain tokens (e.g. "present") alongside b64.
				ver, err := b64dec(p[2])
				if err != nil || ver == "" {
					ver = p[2]
				}
				f.Features[p[1]] = ver
			}
		}
	case "sec":
		if len(p) >= 3 {
			f.Secrets[p[1]] = p[2] == "present"
		}
	case "lcheck":
		if len(p) >= 3 {
			f.LocalFeatureCheck[p[1]] = p[2]
		}
	case "docker":
		if len(p) >= 2 && p[1] == "present" {
			f.Docker.Installed = true
			if len(p) >= 4 {
				var err error
				if f.Docker.Version, err = b64dec(p[2]); err != nil {
					return err
				}
				if f.Docker.ComposeVersion, err = b64dec(p[3]); err != nil {
					return err
				}
			}
		}
	case "network":
		f.Docker.NetworkExists = len(p) >= 2 && p[1] == "present"
	case "svc":
		if len(p) >= 3 {
			sf := ServiceFact{Exists: p[2] == "present"}
			if sf.Exists && len(p) >= 6 {
				sf.State, sf.Health, sf.ConfigDigest = p[3], p[4], p[5]
				if sf.ConfigDigest == "none" {
					sf.ConfigDigest = ""
				}
				if len(p) >= 7 {
					img, err := b64dec(p[6])
					if err != nil {
						return err
					}
					sf.Image = img
				}
			}
			f.Services[p[1]] = sf
		}
	case "osvc":
		if len(p) >= 2 {
			f.Orphans = append(f.Orphans, p[1])
		}
	case "evol":
		if len(p) >= 3 {
			f.EphemeralVolumes[p[1]] = p[2] == "present"
		}
	case "dvol":
		if len(p) >= 3 {
			f.DurableVolumes[p[1]] = p[2] == "present"
		}
	case "odvol":
		if len(p) >= 2 {
			f.OrphanDurable = append(f.OrphanDurable, p[1])
		}
	case "fmark":
		if len(p) >= 2 {
			f.FeatureMarkerNames = append(f.FeatureMarkerNames, p[1])
		}
	case "lmark":
		if len(p) >= 2 {
			f.LocalFeatureMarkerNames = append(f.LocalFeatureMarkerNames, p[1])
		}
	case "pmark":
		if len(p) >= 3 {
			f.PrereqMarkers[p[1]] = append(f.PrereqMarkers[p[1]], p[2])
		}
	case "marker":
		if len(p) >= 4 {
			raw, err := b64dec(p[3])
			if err != nil {
				return err
			}
			key := p[2]
			switch p[1] {
			case "file":
				var m FileMarker
				if json.Unmarshal([]byte(raw), &m) == nil && m.Target != "" {
					f.Markers.Files[m.Target] = m
				}
			case "feature":
				var m FeatureMarker
				if json.Unmarshal([]byte(raw), &m) == nil {
					f.Markers.Features[key] = m
				}
			case "lfeature":
				var m LocalFeatureMarker
				if json.Unmarshal([]byte(raw), &m) == nil {
					f.Markers.LocalFeatures[key] = m
				}
			case "service":
				var m ServiceMarker
				if json.Unmarshal([]byte(raw), &m) == nil {
					f.Markers.Services[key] = m
				}
			}
		}
	}
	return nil
}

// GuestSupported enforces the supported-guest contract before any mutation.
func (f *Facts) GuestSupported() error {
	if f.OSID != "ubuntu" || !strings.HasPrefix(f.OSVersion, "24.04") {
		return fmt.Errorf("unsupported guest OS %s %s: Bastion supports Ubuntu 24.04 LTS only", f.OSID, f.OSVersion)
	}
	switch f.Arch {
	case "x86_64", "aarch64":
		return nil
	default:
		return fmt.Errorf("unsupported guest architecture %q (supported: x86_64, aarch64)", f.Arch)
	}
}
