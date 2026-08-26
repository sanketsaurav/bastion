package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var (
	dnsLabelRe    = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	envKeyRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	permissionsRe = regexp.MustCompile(`^0[0-7]{3}$`)
	debianPkgRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)
	// Prompt aliases end up inside a single-quoted PS1 assignment; the
	// charset forbids quotes, backslashes, and whitespace by construction.
	promptRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$`)
)

// ValidateBox returns every semantic issue in a normalized Box. dir is the box
// directory used to resolve relative references; when empty, filesystem checks
// are skipped (useful for pure document validation in tests).
func ValidateBox(b *Box, dir string) []string {
	v := &validator{dir: dir}

	v.document(b)
	v.provider(&b.Provider)
	v.connection(b.Connection)
	v.runtime(b.Runtime)
	v.workspace(b.Workspace)
	v.host(b.Host)
	v.ingress(b.Ingress)
	v.volumes(b.Volumes)
	v.secrets(b.Secrets)
	v.services(b)

	return v.issues
}

type validator struct {
	dir    string
	issues []string
}

func (v *validator) addf(format string, args ...any) {
	v.issues = append(v.issues, fmt.Sprintf(format, args...))
}

func reservedf(field, value, capability string) string {
	return fmt.Sprintf("%s: %q is reserved for a later milestone (%s; see SPEC.md §14)", field, value, capability)
}

func (v *validator) document(b *Box) {
	if b.APIVersion != APIVersion {
		v.addf("apiVersion: expected %q, got %q", APIVersion, b.APIVersion)
	}
	if b.Kind != KindBox {
		v.addf("kind: expected %q, got %q", KindBox, b.Kind)
	}
	if b.Metadata.Name == "" {
		v.addf("metadata.name is required")
	} else if !dnsLabelRe.MatchString(b.Metadata.Name) {
		v.addf("metadata.name: %q must be a DNS label (lowercase letters, digits, hyphens)", b.Metadata.Name)
	}
}

func (v *validator) provider(p *Provider) {
	if p.Name != "gcp" {
		v.addf("provider.name: only \"gcp\" is supported, got %q", p.Name)
	}
	switch p.Mode {
	case "attached":
	case "managed":
		v.issues = append(v.issues, reservedf("provider.mode", p.Mode, "managed infrastructure"))
	default:
		v.addf("provider.mode: must be \"attached\", got %q", p.Mode)
	}
	if p.Project == "" {
		v.addf("provider.project is required")
	}
	if p.Zone == "" {
		v.addf("provider.zone is required")
	}
	if p.Instance == "" {
		v.addf("provider.instance is required")
	}
}

func (v *validator) connection(c *Connection) {
	switch c.Type {
	case "iap":
		if c.Host != "" || c.User != "" || c.IdentityFile != "" {
			v.addf("connection: host, user, and identityFile apply only to type \"direct\"")
		}
	case "direct":
		if c.Host == "" {
			v.addf("connection.host is required for type \"direct\"")
		}
		if c.User == "" {
			v.addf("connection.user is required for type \"direct\"")
		}
	default:
		v.addf("connection.type: must be \"iap\" or \"direct\", got %q", c.Type)
	}
}

func (v *validator) runtime(r *Runtime) {
	if r.Engine != "docker" {
		v.addf("runtime.engine: only \"docker\" is supported, got %q", r.Engine)
	}
	if r.LogRotation != nil && r.LogRotation.MaxFiles < 0 {
		v.addf("runtime.logRotation.maxFiles must not be negative")
	}
}

func (v *validator) workspace(w *Workspace) {
	if !strings.HasPrefix(w.Mount, "/") {
		v.addf("workspace.mount: %q must be an absolute path", w.Mount)
	}
	if !strings.HasPrefix(w.DataRoot, "/") {
		v.addf("workspace.dataRoot: %q must be an absolute path", w.DataRoot)
	}
}

func (v *validator) host(h *Host) {
	if h == nil {
		return
	}
	for i, pkg := range h.Packages {
		if !debianPkgRe.MatchString(pkg) {
			v.addf("host.packages[%d]: %q is not a valid Debian package name", i, pkg)
		}
	}
	for i, f := range h.Features {
		v.feature(i, &f)
	}
	for i, f := range h.Files {
		v.managedFile(i, &f)
	}
	if h.Shell != nil {
		if h.Shell.Prompt == "" {
			v.addf("host.shell: prompt is required when shell is set")
		} else if !promptRe.MatchString(h.Shell.Prompt) {
			v.addf("host.shell.prompt: %q must be 1-32 characters of letters, digits, or . _ - (no spaces or quotes)", h.Shell.Prompt)
		}
	}
}

func (v *validator) feature(i int, f *Feature) {
	field := fmt.Sprintf("host.features[%d]", i)
	if f.Uses == "" {
		v.addf("%s.uses is required", field)
		return
	}
	if strings.HasPrefix(f.Uses, "../") || f.Uses == ".." {
		v.addf("%s.uses: %q must stay inside the box directory", field, f.Uses)
		return
	}
	if strings.HasPrefix(f.Uses, "./") {
		rel := filepath.Clean(strings.TrimPrefix(f.Uses, "./"))
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			v.addf("%s.uses: %q must stay inside the box directory", field, f.Uses)
			return
		}
		if v.dir != "" {
			info, err := os.Stat(filepath.Join(v.dir, rel))
			if err != nil || !info.IsDir() {
				v.addf("%s.uses: local feature directory %q not found in the box directory", field, f.Uses)
			}
		}
		return
	}
	if !slices.Contains(BuiltinFeatures, f.Uses) {
		v.addf("%s.uses: unknown built-in feature %q (known: %s; local features start with ./)",
			field, f.Uses, strings.Join(BuiltinFeatures, ", "))
	}
}

func (v *validator) managedFile(i int, f *ManagedFile) {
	field := fmt.Sprintf("host.files[%d]", i)
	switch {
	case f.Source == "":
		v.addf("%s.source is required", field)
	case filepath.IsAbs(f.Source):
		v.addf("%s.source: %q must be relative to the box directory", field, f.Source)
	default:
		rel := filepath.Clean(f.Source)
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			v.addf("%s.source: %q must stay inside the box directory", field, f.Source)
		} else if v.dir != "" {
			if _, err := os.Stat(filepath.Join(v.dir, rel)); err != nil {
				v.addf("%s.source: %q not found in the box directory", field, f.Source)
			}
		}
	}
	if f.Target == "" {
		v.addf("%s.target is required", field)
	} else if !strings.HasPrefix(f.Target, "/") && f.Target != "~" && !strings.HasPrefix(f.Target, "~/") {
		v.addf("%s.target: %q must be absolute or start with ~/", field, f.Target)
	}
	switch f.Mode {
	case "replace", "template":
	default:
		v.addf("%s.mode: must be \"replace\" or \"template\", got %q", field, f.Mode)
	}
	if f.Permissions != "" && !permissionsRe.MatchString(f.Permissions) {
		v.addf("%s.permissions: %q must be a four-digit octal string such as \"0600\"", field, f.Permissions)
	}
}

func (v *validator) volumes(volumes map[string]Volume) {
	for _, name := range sortedKeys(volumes) {
		vol := volumes[name]
		if !dnsLabelRe.MatchString(name) {
			v.addf("volumes: name %q must be a DNS label", name)
		}
		switch vol.Persistence {
		case "durable", "ephemeral":
		case "":
			v.addf("volumes.%s.persistence is required (\"durable\" or \"ephemeral\")", name)
		default:
			v.addf("volumes.%s.persistence: must be \"durable\" or \"ephemeral\", got %q", name, vol.Persistence)
		}
	}
}

func (v *validator) secrets(secrets map[string]Secret) {
	for _, name := range sortedKeys(secrets) {
		s := secrets[name]
		if !dnsLabelRe.MatchString(name) {
			v.addf("secrets: name %q must be a DNS label", name)
		}
		env, file := s.Source.Environment != "", s.Source.File != ""
		switch {
		case env && file:
			v.addf("secrets.%s.source: set exactly one of environment or file, not both", name)
		case !env && !file:
			v.addf("secrets.%s.source: set exactly one of environment or file", name)
		case env && !envKeyRe.MatchString(s.Source.Environment):
			v.addf("secrets.%s.source.environment: %q is not a valid environment variable name", name, s.Source.Environment)
		case file && !strings.HasPrefix(s.Source.File, "/") && !strings.HasPrefix(s.Source.File, "~/"):
			// A bare relative path would resolve against whatever directory
			// bastion happens to run from — silently the wrong file.
			v.addf("secrets.%s.source.file: %q must be an absolute path or start with ~/", name, s.Source.File)
		}
	}
}

func (v *validator) services(b *Box) {
	for _, name := range sortedKeys(b.Services) {
		svc := b.Services[name]
		v.service(b, name, &svc)
	}
	v.dependencyCycles(b.Services)

	// Public hostnames must be resolvable from configuration alone: every
	// public endpoint gets exactly one effective hostname, unique box-wide.
	hostnames := map[string]string{}
	for _, pe := range b.PublicEndpoints() {
		ref := pe.Service + ":" + pe.Endpoint
		if pe.Hostname == "" {
			v.addf("services.%s.endpoints.%s: hostname is required when a service declares more than one public endpoint", pe.Service, pe.Endpoint)
			continue
		}
		if prev, taken := hostnames[pe.Hostname]; taken {
			v.addf("services.%s.endpoints.%s: hostname %q is already used by %s", pe.Service, pe.Endpoint, pe.Hostname, prev)
		} else {
			hostnames[pe.Hostname] = ref
		}
	}

	// Private endpoints publish on the VM loopback; their effective ports
	// must be unique across the whole box (SPEC.md Δ11) — and stay off
	// 80/443 when ingress is enabled, which the proxy binds on the host.
	used := map[int]string{}
	for _, name := range sortedKeys(b.Services) {
		for _, en := range sortedKeys(b.Services[name].Endpoints) {
			ep := b.Services[name].Endpoints[en]
			if ep.Visibility != "private" {
				continue
			}
			port := ep.EffectiveVMPort()
			if b.Ingress != nil && (port == 80 || port == 443) {
				v.addf("services.%s.endpoints.%s: VM port %d is reserved for the ingress proxy; set a different vmPort", name, en, port)
				continue
			}
			if prev, taken := used[port]; taken {
				v.addf("services.%s.endpoints.%s: VM port %d is already used by %s; set a distinct vmPort", name, en, port, prev)
			} else {
				used[port] = name + ":" + en
			}
		}
	}
}

func (v *validator) service(b *Box, name string, s *Service) {
	field := "services." + name
	if !dnsLabelRe.MatchString(name) {
		v.addf("services: name %q must be a DNS label", name)
	}
	if s.Image == "" {
		v.addf("%s.image is required", field)
	}
	switch s.PullPolicy {
	case "if-not-present", "always", "never":
	default:
		v.addf("%s.pullPolicy: must be if-not-present, always, or never; got %q", field, s.PullPolicy)
	}
	switch s.RestartPolicy {
	case "no", "on-failure", "unless-stopped", "always":
	default:
		v.addf("%s.restartPolicy: must be no, on-failure, unless-stopped, or always; got %q", field, s.RestartPolicy)
	}
	for _, key := range sortedKeys(s.Environment) {
		val := s.Environment[key]
		if !envKeyRe.MatchString(key) {
			v.addf("%s.environment: %q is not a valid environment variable name", field, key)
		}
		if !val.IsLiteral {
			if _, ok := b.Secrets[val.SecretRef]; !ok {
				v.addf("%s.environment.%s: secretRef %q is not a declared secret", field, key, val.SecretRef)
			}
		}
	}
	for i, m := range s.Mounts {
		mf := fmt.Sprintf("%s.mounts[%d]", field, i)
		vol, src := m.Volume != "", m.Source != ""
		switch {
		case vol && src:
			v.addf("%s: set exactly one of volume or source, not both", mf)
		case !vol && !src:
			v.addf("%s: set exactly one of volume or source", mf)
		case vol:
			if _, ok := b.Volumes[m.Volume]; !ok {
				v.addf("%s.volume: %q is not a declared volume", mf, m.Volume)
			}
		case src:
			if !strings.HasPrefix(m.Source, "/") {
				v.addf("%s.source: bind mount source %q must be an absolute remote path", mf, m.Source)
			}
		}
		if m.Target == "" {
			v.addf("%s.target is required", mf)
		} else if !strings.HasPrefix(m.Target, "/") {
			v.addf("%s.target: %q must be an absolute container path", mf, m.Target)
		}
	}
	if s.Resources != nil && s.Resources.CPUs < 0 {
		v.addf("%s.resources.cpus must not be negative", field)
	}
	if s.Healthcheck != nil {
		if len(s.Healthcheck.Command) == 0 {
			v.addf("%s.healthcheck.command is required", field)
		}
		if s.Healthcheck.Retries < 0 {
			v.addf("%s.healthcheck.retries must not be negative", field)
		}
	}
	for _, dep := range s.DependsOn {
		if dep == name {
			v.addf("%s.dependsOn: a service cannot depend on itself", field)
		} else if _, ok := b.Services[dep]; !ok {
			v.addf("%s.dependsOn: %q is not a declared service", field, dep)
		}
	}
	for _, en := range sortedKeys(s.Endpoints) {
		ep := s.Endpoints[en]
		ef := fmt.Sprintf("%s.endpoints.%s", field, en)
		if !dnsLabelRe.MatchString(en) {
			v.addf("%s: endpoint name %q must be a DNS label", field, en)
		}
		if ep.ContainerPort < 1 || ep.ContainerPort > 65535 {
			v.addf("%s.containerPort: %d is not a valid port", ef, ep.ContainerPort)
		}
		if ep.VMPort != 0 && (ep.VMPort < 1 || ep.VMPort > 65535) {
			v.addf("%s.vmPort: %d is not a valid port", ef, ep.VMPort)
		}
		if ep.VMPort != 0 && ep.Visibility != "private" {
			v.addf("%s.vmPort applies only to private endpoints", ef)
		}
		switch ep.Protocol {
		case "http", "tcp":
		default:
			v.addf("%s.protocol: must be \"http\" or \"tcp\", got %q", ef, ep.Protocol)
		}
		switch ep.Visibility {
		case "internal", "private":
			if ep.Hostname != "" {
				v.addf("%s.hostname applies only to public endpoints", ef)
			}
			if ep.Auth != "" {
				v.addf("%s.auth applies only to public endpoints", ef)
			}
		case "public":
			if b.Ingress == nil {
				v.addf("%s: public endpoints need a top-level ingress block with baseDomain", ef)
			}
			if ep.Protocol != "http" {
				v.addf("%s: public endpoints support protocol \"http\" only (exposed as HTTPS)", ef)
			}
			switch ep.Auth {
			case "none":
			case "":
				v.addf("%s.auth is required for a public endpoint: `auth: none` acknowledges the endpoint is internet-facing and the application owns authentication", ef)
			case "basic":
				v.issues = append(v.issues, reservedf(ef+".auth", ep.Auth, "ingress basic auth"))
			default:
				v.addf("%s.auth: must be \"none\", got %q", ef, ep.Auth)
			}
			if ep.Hostname != "" && !fqdn(ep.Hostname) {
				v.addf("%s.hostname: %q is not a valid DNS name", ef, ep.Hostname)
			}
		default:
			v.addf("%s.visibility: must be \"internal\", \"private\", or \"public\", got %q", ef, ep.Visibility)
		}
	}
}

// fqdn reports whether s is a lowercase DNS name of at least two labels.
func fqdn(s string) bool {
	labels := strings.Split(s, ".")
	if len(labels) < 2 || len(s) > 253 {
		return false
	}
	for _, l := range labels {
		if !dnsLabelRe.MatchString(l) {
			return false
		}
	}
	return true
}

func (v *validator) ingress(ing *Ingress) {
	if ing == nil {
		return
	}
	if ing.BaseDomain == "" {
		v.addf("ingress.baseDomain is required")
	} else if !fqdn(ing.BaseDomain) {
		v.addf("ingress.baseDomain: %q is not a valid DNS name", ing.BaseDomain)
	}
	if ing.ACMEEmail != "" && (!strings.Contains(ing.ACMEEmail, "@") || strings.ContainsAny(ing.ACMEEmail, " \t\"'")) {
		v.addf("ingress.acmeEmail: %q is not a plausible email address", ing.ACMEEmail)
	}
}

// dependencyCycles reports one representative cycle per strongly connected
// dependency loop using an iterative-enough DFS over the declared graph.
func (v *validator) dependencyCycles(services map[string]Service) {
	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := map[string]int{}
	var stack []string
	var reported bool

	var visit func(name string)
	visit = func(name string) {
		if reported {
			return
		}
		state[name] = inStack
		stack = append(stack, name)
		for _, dep := range services[name].DependsOn {
			if _, ok := services[dep]; !ok || dep == name {
				continue // reported elsewhere
			}
			switch state[dep] {
			case unvisited:
				visit(dep)
			case inStack:
				start := slices.Index(stack, dep)
				cycle := append(slices.Clone(stack[start:]), dep)
				v.addf("services: dependency cycle: %s", strings.Join(cycle, " -> "))
				reported = true
			}
			if reported {
				return
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = done
	}

	for _, name := range sortedKeys(services) {
		if state[name] == unvisited {
			visit(name)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
