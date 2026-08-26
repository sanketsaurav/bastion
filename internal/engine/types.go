// Package engine implements host convergence and container services: it
// inspects a box over SSH, diffs observed facts against the declared
// definition, renders an ordered plan, and generates the apply program.
//
// The remote runner is a bash program generated per request by this exact CLI
// version and piped to `bash -s` over SSH (SPEC.md Δ9). It emits a line
// protocol on stdout: "@f ..." facts during inspect, "@e <step> <status>" and
// "@l <step> <b64 line>" events during apply. All intelligence stays on the
// CLI side; the remote side executes explicit steps and reports.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/sanketsaurav/bastion/internal/config"
)

// Remote layout roots (SPEC.md §8.6, Δ10).
const (
	remoteStateRoot    = "/var/lib/bastion/state"
	remoteServicesRoot = "/var/lib/bastion/services"
	remoteSecretsRoot  = "/var/lib/bastion/secrets"
)

// Input carries everything a plan or apply needs from the local side.
type Input struct {
	Box     *config.Box
	Dir     string // box directory, anchors files/ and features/
	BoxID   string // metadata.name in attached mode (SPEC.md Δ12)
	Version string // CLI version, stamped into generated artifacts
}

// BoxID derives the stable box identifier from a definition.
func BoxID(b *config.Box) string { return b.Metadata.Name }

func (in *Input) stateDir() string    { return remoteStateRoot + "/" + in.BoxID }
func (in *Input) servicesDir() string { return remoteServicesRoot + "/" + in.BoxID }
func (in *Input) secretsDir() string  { return remoteSecretsRoot + "/" + in.BoxID }
func (in *Input) networkName() string { return "bastion-" + in.BoxID }

func (in *Input) containerName(svc string) string { return "bastion-" + in.BoxID + "-" + svc }
func (in *Input) projectName(svc string) string   { return "bastion-" + in.BoxID + "-" + svc }
func (in *Input) ephemeralVolume(vol string) string {
	return "bastion-" + in.BoxID + "-" + vol
}
func (in *Input) durableVolumeDir(vol string) string {
	return in.Box.Workspace.DataRoot + "/volumes/" + vol
}
func (in *Input) composePath(svc string) string {
	return in.servicesDir() + "/" + svc + "/compose.yaml"
}
func (in *Input) secretEnvPath(svc string) string {
	return in.secretsDir() + "/" + svc + ".env"
}

// Action kinds, in the order apply executes them.
const (
	KindBootstrap     = "bootstrap"
	KindPackages      = "packages"
	KindFeature       = "feature"
	KindLocalFeature  = "local-feature"
	KindFile          = "file"
	KindShellLine     = "shell-line"
	KindNetwork       = "network"
	KindVolume        = "volume"
	KindSecret        = "secret"
	KindService       = "service"
	KindServiceHealth = "service-health"
	KindServiceStop   = "service-stop"
	KindServiceRemove = "service-remove"
	KindInstanceStart = "instance-start"
)

// Action is one ordered plan entry. Payload fields drive script generation.
type Action struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Summary      string   `json:"summary"`
	Detail       []string `json:"detail,omitempty"`
	Destructive  bool     `json:"destructive,omitempty"`
	RequiresRoot bool     `json:"requiresRoot,omitempty"`
	// LocalCode marks locally supplied executable content (SPEC.md §8.4):
	// plans must identify it distinctly.
	LocalCode bool `json:"localCode,omitempty"`

	pkgs    []string
	file    *fileAction
	feature *featureAction
	lfeat   *localFeatureAction
	volume  *volumeAction
	secret  *secretAction
	service *serviceAction
	target  string // service name for health/stop/remove
}

type fileAction struct {
	Target     string // as written in config (~/… or absolute)
	Content    []byte
	SHA256     string
	Mode       string // "0644" style; empty = leave default
	Root       bool
	FirstTouch bool // existing unmanaged file: keep one backup
}

type featureAction struct {
	Name    string
	Def     *Builtin
	With    map[string]any
	Digest  string
	Version string
}

type localFeatureAction struct {
	Name         string
	Meta         *LocalFeatureMeta
	TarGz        []byte
	SourceDigest string
	InputsJSON   []byte
	InputsDigest string
}

type volumeAction struct {
	Name      string
	Ephemeral bool
}

type secretAction struct {
	Service string
	// Env content is resolved at apply time, never at plan time.
	Refs []string // secret names, for the plan display
}

type serviceAction struct {
	Name         string
	Compose      []byte
	ConfigDigest string
	Image        string
	PullPolicy   string
	HasHealth    bool
}

// Plan is the ordered result of diffing desired state against observed facts.
type Plan struct {
	BoxID          string   `json:"boxId"`
	InstanceStatus string   `json:"instanceStatus,omitempty"`
	GuestUnknown   bool     `json:"guestUnknown,omitempty"`
	Actions        []Action `json:"actions"`
	Warnings       []string `json:"warnings,omitempty"`
	Notes          []string `json:"notes,omitempty"`
}

// Changes reports whether the plan proposes any work.
func (p *Plan) Changes() bool { return len(p.Actions) > 0 }

// HasDestructive reports whether any action is destructive.
func (p *Plan) HasDestructive() bool {
	for _, a := range p.Actions {
		if a.Destructive {
			return true
		}
	}
	return false
}

// RootNeeded reports whether any action requires sudo.
func (p *Plan) RootNeeded() bool {
	for _, a := range p.Actions {
		if a.RequiresRoot {
			return true
		}
	}
	return false
}

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// shortHash keys marker filenames for targets (filesystem-safe, stable).
func shortHash(s string) string { return sha256hex([]byte(s))[:16] }

// optionsDigest canonicalizes a feature's `with` map (json.Marshal sorts map
// keys) so option changes force reapply.
func optionsDigest(with map[string]any) string {
	if len(with) == 0 {
		return sha256hex(nil)
	}
	data, err := json.Marshal(with)
	if err != nil {
		return sha256hex([]byte(fmt.Sprintf("%v", with)))
	}
	return sha256hex(data)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
