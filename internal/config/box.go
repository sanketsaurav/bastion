// Package config defines, parses, and validates the bastion/v1alpha1
// configuration surface: Box definitions and the ClientConfig. Parsing is
// strict — unknown fields are errors — and values reserved for later
// milestones are rejected with an error naming the milestone (SPEC.md Δ8).
package config

const (
	APIVersion       = "bastion/v1alpha1"
	KindBox          = "Box"
	KindClientConfig = "ClientConfig"
)

// BuiltinFeatures are the host features Bastion knows how to install
// (applied from milestone B; names are validated today).
var BuiltinFeatures = []string{
	"build-essential",
	"bun",
	"claude-code",
	"codex",
	"docker",
	"github-cli",
	"mise",
	"tmux",
	"uv",
}

// Box is the desired state of one development box (kind: Box).
type Box struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion" jsonschema:"required"`
	Kind       string   `yaml:"kind" json:"kind" jsonschema:"required,enum=Box"`
	Metadata   Metadata `yaml:"metadata" json:"metadata" jsonschema:"required"`
	Provider   Provider `yaml:"provider" json:"provider" jsonschema:"required"`

	Connection *Connection `yaml:"connection,omitempty" json:"connection,omitempty"`
	Runtime    *Runtime    `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Workspace  *Workspace  `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	Host       *Host       `yaml:"host,omitempty" json:"host,omitempty"`
	Ingress    *Ingress    `yaml:"ingress,omitempty" json:"ingress,omitempty"`

	Volumes  map[string]Volume  `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Secrets  map[string]Secret  `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	Services map[string]Service `yaml:"services,omitempty" json:"services,omitempty"`
}

// Ingress enables public HTTPS endpoints through a bastion-managed reverse
// proxy (SPEC.md §9.8). Bastion never manages DNS, IPs, or firewall rules —
// `doctor` verifies the required records and reachability instead.
type Ingress struct {
	// BaseDomain gives every public endpoint its default hostname,
	// <service>.<baseDomain>; one wildcard DNS record covers all of them.
	BaseDomain string `yaml:"baseDomain" json:"baseDomain" jsonschema:"required"`
	// ACMEEmail is optionally registered with the certificate authority
	// for expiry notices.
	ACMEEmail string `yaml:"acmeEmail,omitempty" json:"acmeEmail,omitempty"`
}

type Metadata struct {
	Name   string            `yaml:"name" json:"name" jsonschema:"required"`
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

type Provider struct {
	Name     string `yaml:"name" json:"name" jsonschema:"required,enum=gcp"`
	Mode     string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=attached,enum=managed"`
	Project  string `yaml:"project" json:"project" jsonschema:"required"`
	Zone     string `yaml:"zone" json:"zone" jsonschema:"required"`
	Instance string `yaml:"instance" json:"instance" jsonschema:"required"`
}

type Connection struct {
	Type            string `yaml:"type,omitempty" json:"type,omitempty" jsonschema:"enum=iap,enum=direct"`
	OSLogin         *bool  `yaml:"osLogin,omitempty" json:"osLogin,omitempty"`
	ForwardSSHAgent bool   `yaml:"forwardSshAgent,omitempty" json:"forwardSshAgent,omitempty"`
	// Multiplex reuses one SSH connection across invocations via an
	// OpenSSH control master (SPEC.md Δ14). Default true; agent
	// forwarding disables it regardless.
	Multiplex *bool `yaml:"multiplex,omitempty" json:"multiplex,omitempty"`

	// Direct connections only.
	Host         string `yaml:"host,omitempty" json:"host,omitempty"`
	User         string `yaml:"user,omitempty" json:"user,omitempty"`
	IdentityFile string `yaml:"identityFile,omitempty" json:"identityFile,omitempty"`
}

// UseOSLogin reports the effective osLogin setting (default true).
func (c *Connection) UseOSLogin() bool { return c.OSLogin == nil || *c.OSLogin }

// UseMultiplex reports the effective multiplex setting (default true).
func (c *Connection) UseMultiplex() bool { return c.Multiplex == nil || *c.Multiplex }

type Runtime struct {
	Engine      string       `yaml:"engine,omitempty" json:"engine,omitempty" jsonschema:"enum=docker"`
	LogRotation *LogRotation `yaml:"logRotation,omitempty" json:"logRotation,omitempty"`
}

type LogRotation struct {
	MaxSize  ByteSize `yaml:"maxSize,omitempty" json:"maxSize,omitempty"`
	MaxFiles int      `yaml:"maxFiles,omitempty" json:"maxFiles,omitempty"`
}

type Workspace struct {
	Mount    string `yaml:"mount,omitempty" json:"mount,omitempty"`
	DataRoot string `yaml:"dataRoot,omitempty" json:"dataRoot,omitempty"`
}

type Host struct {
	Packages  []string      `yaml:"packages,omitempty" json:"packages,omitempty"`
	Features  []Feature     `yaml:"features,omitempty" json:"features,omitempty"`
	Files     []ManagedFile `yaml:"files,omitempty" json:"files,omitempty"`
	Shell     *Shell        `yaml:"shell,omitempty" json:"shell,omitempty"`
	Hardening *Hardening    `yaml:"hardening,omitempty" json:"hardening,omitempty"`
}

// Hardening converges guest security posture (SPEC.md §8.5). `bastion
// audit` reports; this section fixes.
type Hardening struct {
	// AutoReboot ("HH:MM") lets unattended-upgrades reboot in a nightly
	// window when a security update needs it. A bastion box is uniquely
	// safe to reboot: services restart, data is durable, the IP is
	// static.
	AutoReboot string `yaml:"autoReboot,omitempty" json:"autoReboot,omitempty"`
}

// Shell configures bastion's login experience on the box (SPEC.md §8.5):
// PS1 via a managed ~/.config/bastion/shell.sh plus exactly one delimited
// source line in ~/.bashrc, MOTD suppression via ~/.hushlogin, and the
// client-side `bastion ssh` nameplate. At least one field must be set.
type Shell struct {
	// Prompt is the name PS1 shows in place of the login username
	// (e.g. `alice@devbox` instead of `ext_..._gmail_com@devbox`).
	// Cosmetic only: authentication and `whoami` are unchanged.
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	// MOTD "quiet" writes an empty ~/.hushlogin, silencing the
	// distribution's login messages and sshd's last-login line for every
	// SSH path. "default" (or empty) leaves login output alone — though an
	// existing ~/.hushlogin is never deleted, only unmanaged.
	MOTD string `yaml:"motd,omitempty" json:"motd,omitempty" jsonschema:"enum=default,enum=quiet"`
	// Banner controls the nameplate `bastion ssh` prints before the
	// session: "art" (the default) or "off".
	Banner string `yaml:"banner,omitempty" json:"banner,omitempty" jsonschema:"enum=art,enum=off"`
	// UserAlias additionally creates a local passwd entry named after
	// prompt with the login user's uid, gid, and home — so `whoami`,
	// PS1's \u, and file listings show the prompt name. The login name,
	// authentication, and home directory are unchanged. Requires prompt.
	UserAlias bool `yaml:"userAlias,omitempty" json:"userAlias,omitempty"`
}

type Feature struct {
	Uses string         `yaml:"uses" json:"uses" jsonschema:"required"`
	With map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
}

// Local reports whether the feature is locally supplied executable content
// (referenced by path) rather than a built-in.
func (f *Feature) Local() bool {
	return len(f.Uses) >= 2 && (f.Uses[:2] == "./" || f.Uses[:2] == "..")
}

type ManagedFile struct {
	Source      string `yaml:"source" json:"source" jsonschema:"required"`
	Target      string `yaml:"target" json:"target" jsonschema:"required"`
	Mode        string `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"enum=replace,enum=template"`
	Permissions string `yaml:"permissions,omitempty" json:"permissions,omitempty"`
}

type Volume struct {
	Persistence string `yaml:"persistence" json:"persistence" jsonschema:"required,enum=durable,enum=ephemeral"`
}

type Secret struct {
	Source SecretSource `yaml:"source" json:"source" jsonschema:"required"`
}

// SecretSource names where a secret value comes from. Exactly one field must
// be set. Values are resolved at use time and never serialized (SPEC.md §9.4).
type SecretSource struct {
	Environment string `yaml:"environment,omitempty" json:"environment,omitempty"`
	File        string `yaml:"file,omitempty" json:"file,omitempty"`
}

type Service struct {
	Image         string   `yaml:"image" json:"image" jsonschema:"required"`
	PullPolicy    string   `yaml:"pullPolicy,omitempty" json:"pullPolicy,omitempty" jsonschema:"enum=if-not-present,enum=always,enum=never"`
	RestartPolicy string   `yaml:"restartPolicy,omitempty" json:"restartPolicy,omitempty" jsonschema:"enum=no,enum=on-failure,enum=unless-stopped,enum=always"`
	Enabled       *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Platform      string   `yaml:"platform,omitempty" json:"platform,omitempty"`
	Entrypoint    []string `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Args          []string `yaml:"args,omitempty" json:"args,omitempty"`
	WorkingDir    string   `yaml:"workingDir,omitempty" json:"workingDir,omitempty"`
	User          string   `yaml:"user,omitempty" json:"user,omitempty"`

	Environment map[string]EnvValue `yaml:"environment,omitempty" json:"environment,omitempty"`
	Mounts      []Mount             `yaml:"mounts,omitempty" json:"mounts,omitempty"`
	Resources   *Resources          `yaml:"resources,omitempty" json:"resources,omitempty"`
	Healthcheck *Healthcheck        `yaml:"healthcheck,omitempty" json:"healthcheck,omitempty"`
	DependsOn   []string            `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	Endpoints   map[string]Endpoint `yaml:"endpoints,omitempty" json:"endpoints,omitempty"`
}

// IsEnabled reports the effective enabled setting (default true).
func (s *Service) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// Mount attaches either a declared volume or an explicit host path (bind
// mount) into a service. Exactly one of Volume and Source must be set.
type Mount struct {
	Volume   string `yaml:"volume,omitempty" json:"volume,omitempty"`
	Source   string `yaml:"source,omitempty" json:"source,omitempty"`
	Target   string `yaml:"target" json:"target" jsonschema:"required"`
	ReadOnly bool   `yaml:"readOnly,omitempty" json:"readOnly,omitempty"`
}

type Resources struct {
	CPUs   float64  `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Memory ByteSize `yaml:"memory,omitempty" json:"memory,omitempty"`
}

type Healthcheck struct {
	Command     []string `yaml:"command" json:"command" jsonschema:"required"`
	Interval    Duration `yaml:"interval,omitempty" json:"interval,omitempty"`
	Timeout     Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retries     int      `yaml:"retries,omitempty" json:"retries,omitempty"`
	StartPeriod Duration `yaml:"startPeriod,omitempty" json:"startPeriod,omitempty"`
}

type Endpoint struct {
	ContainerPort int    `yaml:"containerPort" json:"containerPort" jsonschema:"required,minimum=1,maximum=65535"`
	Protocol      string `yaml:"protocol,omitempty" json:"protocol,omitempty" jsonschema:"enum=http,enum=tcp"`
	Visibility    string `yaml:"visibility,omitempty" json:"visibility,omitempty" jsonschema:"enum=internal,enum=private,enum=public"`

	// VMPort is the VM loopback port a private endpoint publishes on
	// (default: containerPort). Must be unique across the box (SPEC.md Δ11).
	VMPort int `yaml:"vmPort,omitempty" json:"vmPort,omitempty" jsonschema:"minimum=1,maximum=65535"`

	// Hostname overrides a public endpoint's derived name
	// (<service>.<baseDomain>). Required when a service declares more than
	// one public endpoint.
	Hostname string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	// Auth is the declared access policy of a public endpoint and doubles
	// as the internet-facing acknowledgement: "none" states the application
	// owns authentication. Required when visibility is public. "basic" is
	// reserved.
	Auth string `yaml:"auth,omitempty" json:"auth,omitempty" jsonschema:"enum=none,enum=basic"`
}

// EffectiveVMPort returns the loopback port a private endpoint binds on the VM.
func (e Endpoint) EffectiveVMPort() int {
	if e.VMPort != 0 {
		return e.VMPort
	}
	return e.ContainerPort
}

// PublicEndpoint is one internet-facing endpoint with its effective hostname.
type PublicEndpoint struct {
	Service       string
	Endpoint      string
	Hostname      string // empty ⇒ an explicit hostname is required (validation rejects)
	ContainerPort int
	Auth          string
}

// PublicEndpoints resolves every public endpoint and its effective hostname
// in deterministic order. A service with exactly one public endpoint gets
// <service>.<baseDomain> unless the endpoint sets hostname; with more than
// one, each endpoint must set its own (validated, reported here as "").
func (b *Box) PublicEndpoints() []PublicEndpoint {
	base := ""
	if b.Ingress != nil {
		base = b.Ingress.BaseDomain
	}
	var out []PublicEndpoint
	for _, svc := range sortedKeys(b.Services) {
		s := b.Services[svc]
		public := 0
		for _, ep := range s.Endpoints {
			if ep.Visibility == "public" {
				public++
			}
		}
		for _, en := range sortedKeys(s.Endpoints) {
			ep := s.Endpoints[en]
			if ep.Visibility != "public" {
				continue
			}
			hostname := ep.Hostname
			if hostname == "" && public == 1 && base != "" {
				hostname = svc + "." + base
			}
			out = append(out, PublicEndpoint{
				Service: svc, Endpoint: en, Hostname: hostname,
				ContainerPort: ep.ContainerPort, Auth: ep.Auth,
			})
		}
	}
	return out
}

// Normalize fills defaulted fields in place. Parse calls it before validation,
// so downstream code can rely on defaults being present.
func (b *Box) Normalize() {
	if b.Provider.Mode == "" {
		b.Provider.Mode = "attached"
	}
	if b.Connection == nil {
		b.Connection = &Connection{}
	}
	if b.Connection.Type == "" {
		b.Connection.Type = "iap"
	}
	if b.Connection.OSLogin == nil {
		t := true
		b.Connection.OSLogin = &t
	}
	if b.Runtime == nil {
		b.Runtime = &Runtime{}
	}
	if b.Runtime.Engine == "" {
		b.Runtime.Engine = "docker"
	}
	if b.Workspace == nil {
		b.Workspace = &Workspace{}
	}
	if b.Workspace.Mount == "" {
		b.Workspace.Mount = "/workspace"
	}
	if b.Workspace.DataRoot == "" {
		b.Workspace.DataRoot = "/mnt/bastion"
	}
	if b.Host != nil {
		for i := range b.Host.Files {
			if b.Host.Files[i].Mode == "" {
				b.Host.Files[i].Mode = "replace"
			}
		}
	}
	for name, svc := range b.Services {
		if svc.PullPolicy == "" {
			svc.PullPolicy = "if-not-present"
		}
		if svc.RestartPolicy == "" {
			// unless-stopped, not Docker's `no`: a personal box should bring
			// services back after a reboot outside Bastion (SPEC.md Δ1).
			svc.RestartPolicy = "unless-stopped"
		}
		if svc.Enabled == nil {
			t := true
			svc.Enabled = &t
		}
		for en, ep := range svc.Endpoints {
			if ep.Protocol == "" {
				ep.Protocol = "http"
			}
			if ep.Visibility == "" {
				ep.Visibility = "internal"
			}
			svc.Endpoints[en] = ep
		}
		b.Services[name] = svc
	}
}
