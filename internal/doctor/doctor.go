// Package doctor diagnoses the local environment and a box's connectivity.
// Every failing check carries remediation text (SPEC.md §13).
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/execx"
	"github.com/sanketsaurav/bastion/internal/provider"
	"github.com/sanketsaurav/bastion/internal/provider/gcp"
	"github.com/sanketsaurav/bastion/internal/shellquote"
)

type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
)

// Result is one completed check.
type Result struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

// Deps are the injectable collaborators, so checks run against fakes in tests.
type Deps struct {
	Runner   execx.Runner
	LookPath func(string) (string, error)
	Client   *gcp.Client
	Box      *config.Box

	// LookupHost and DialTimeout back the ingress checks; nil selects the
	// real resolver and dialer.
	LookupHost  func(ctx context.Context, host string) ([]string, error)
	DialTimeout func(network, addr string, timeout time.Duration) (net.Conn, error)
}

// Failed reports whether any check failed outright.
func Failed(results []Result) bool {
	for _, r := range results {
		if r.Status == Fail {
			return true
		}
	}
	return false
}

// Run executes milestone-A checks in dependency order. Guest-level checks
// (OS support, sudo, Docker health) arrive with milestone B.
func Run(ctx context.Context, d Deps) []Result {
	var results []Result
	add := func(r Result) { results = append(results, r) }

	direct := d.Box.Connection.Type == "direct"
	if direct {
		if _, err := d.LookPath("ssh"); err != nil {
			add(Result{Name: "ssh binary", Status: Fail, Detail: "ssh not found on PATH",
				Hint: "install an OpenSSH client"})
		} else {
			add(Result{Name: "ssh binary", Status: OK, Detail: "found on PATH"})
		}
	}

	if _, err := d.LookPath("gcloud"); err != nil {
		add(Result{Name: "gcloud CLI", Status: Fail, Detail: "gcloud not found on PATH",
			Hint: "install the Google Cloud SDK: https://cloud.google.com/sdk/docs/install"})
		return results
	}
	add(gcloudVersion(ctx, d))

	account, res := activeAccount(ctx, d)
	add(res)
	if account == "" {
		return results
	}

	add(projectAccess(ctx, d))

	inst, instRes := instanceCheck(ctx, d)
	add(instRes)

	if !direct && d.Box.Connection.UseOSLogin() {
		add(osLogin(ctx, d, inst))
	}

	switch {
	case inst == nil:
		add(Result{Name: "SSH reachability", Status: Skip, Detail: "instance state unknown"})
	case !inst.Running():
		add(Result{Name: "SSH reachability", Status: Skip,
			Detail: fmt.Sprintf("instance is %s; start it with `bastion up`", inst.Status)})
	default:
		// A box that just booted needs a few seconds for sshd and OS Login
		// key propagation; retry briefly before declaring failure.
		var sshErr error
		for attempt := 1; attempt <= sshProbeAttempts; attempt++ {
			if sshErr = d.Client.CheckSSH(ctx); sshErr == nil {
				break
			}
			if attempt < sshProbeAttempts {
				select {
				case <-ctx.Done():
					sshErr = ctx.Err()
					attempt = sshProbeAttempts
				case <-time.After(sshProbeDelay):
				}
			}
		}
		if sshErr != nil {
			add(Result{Name: "SSH reachability", Status: Fail, Detail: sshErr.Error(),
				Hint: "check IAP firewall access and OS Login for this account; a box that just started may need another ~30s"})
		} else {
			add(Result{Name: "SSH reachability", Status: OK, Detail: "remote command executed"})
			results = append(results, guestChecks(ctx, d)...)
		}
	}
	if d.Box.Ingress != nil {
		results = append(results, ingressChecks(ctx, d, inst)...)
	}
	return results
}

// Retry knobs are package variables so tests can collapse the delay.
var (
	sshProbeAttempts = 3
	sshProbeDelay    = 5 * time.Second
)

// guestChecks probes the guest itself in one SSH round trip: OS support,
// sudo, internet egress, data-root state, and Docker health. A VM can pass
// every cloud-level check yet be unable to download anything at apply time
// (no external IP, no Cloud NAT) — the egress probe catches exactly that.
func guestChecks(ctx context.Context, d Deps) []Result {
	dataRoot := d.Box.Workspace.DataRoot
	// The egress probe targets a non-Google host deliberately: Private
	// Google Access would make *.google.com reachable while the registries
	// and repos apply actually needs stay blocked.
	script := `set -u
. /etc/os-release 2>/dev/null || true
echo "@d os ${ID:-unknown} ${VERSION_ID:-0} $(uname -m)"
if sudo -n true 2>/dev/null; then echo "@d sudo ok"; else echo "@d sudo missing"; fi
if timeout 8 bash -c 'exec 3<>/dev/tcp/download.docker.com/443' 2>/dev/null; then echo "@d egress ok"; else echo "@d egress blocked"; fi
if [ -d ` + shellquote.Quote(dataRoot) + ` ]; then echo "@d disk $(df -Pk ` + shellquote.Quote(dataRoot) + ` | awk 'NR==2{print $4}')"; else echo "@d disk missing"; fi
if ! command -v docker >/dev/null 2>&1; then echo "@d docker absent"
elif docker info >/dev/null 2>&1; then echo "@d docker ok group"
elif sudo -n docker info >/dev/null 2>&1; then echo "@d docker ok sudo"
else echo "@d docker unhealthy"; fi
echo "@d end"
exit 0
`
	var lines []string
	res, err := d.Client.RunScript(ctx, []byte(script), func(l string) { lines = append(lines, l) })
	if err != nil || res.ExitCode != 0 {
		return []Result{{Name: "guest checks", Status: Warn, Detail: "guest probe did not complete",
			Hint: "rerun with --verbose; the box may be mid-boot"}}
	}
	probe := map[string][]string{}
	for _, line := range lines {
		if !strings.HasPrefix(line, "@d ") {
			continue
		}
		parts := strings.Fields(line[3:])
		if len(parts) > 0 {
			probe[parts[0]] = parts[1:]
		}
	}
	var out []Result

	if os := probe["os"]; len(os) >= 3 {
		if os[0] == "ubuntu" && strings.HasPrefix(os[1], "24.04") {
			out = append(out, Result{Name: "guest OS", Status: OK, Detail: fmt.Sprintf("ubuntu %s (%s)", os[1], os[2])})
		} else {
			out = append(out, Result{Name: "guest OS", Status: Fail,
				Detail: fmt.Sprintf("%s %s is not supported", os[0], os[1]),
				Hint:   "Bastion converges Ubuntu 24.04 LTS guests only"})
		}
	}
	if s := probe["sudo"]; len(s) >= 1 {
		if s[0] == "ok" {
			out = append(out, Result{Name: "sudo", Status: OK, Detail: "non-interactive sudo available"})
		} else {
			out = append(out, Result{Name: "sudo", Status: Warn,
				Detail: "non-interactive sudo unavailable",
				Hint:   "root-requiring apply actions will fail; grant NOPASSWD sudo to this account"})
		}
	}
	if e := probe["egress"]; len(e) >= 1 {
		if e[0] == "ok" {
			out = append(out, Result{Name: "internet egress", Status: OK, Detail: "outbound 443 reachable"})
		} else {
			out = append(out, Result{Name: "internet egress", Status: Fail,
				Detail: "the box cannot reach the internet",
				Hint:   "apply cannot download packages or images; add an external IP or Cloud NAT to the VPC"})
		}
	}
	if disk := probe["disk"]; len(disk) >= 1 {
		switch {
		case disk[0] == "missing":
			out = append(out, Result{Name: "data root", Status: Warn,
				Detail: dataRoot + " does not exist yet",
				Hint:   "apply will create it on the boot disk; mount a dedicated disk there for durable data"})
		default:
			if kb, err := strconv.ParseInt(disk[0], 10, 64); err == nil {
				gib := float64(kb) / (1 << 20)
				status, hint := OK, ""
				if gib < 2 {
					status, hint = Warn, "free up space; image pulls and builds will fail soon"
				}
				out = append(out, Result{Name: "data root", Status: status,
					Detail: fmt.Sprintf("%s (%.1f GiB free)", dataRoot, gib), Hint: hint})
			}
		}
	}
	if dk := probe["docker"]; len(dk) >= 1 {
		switch dk[0] {
		case "absent":
			if len(d.Box.Services) > 0 {
				out = append(out, Result{Name: "Docker", Status: Warn,
					Detail: "not installed but services are declared",
					Hint:   "apply installs it via the docker feature"})
			} else {
				out = append(out, Result{Name: "Docker", Status: Skip, Detail: "not installed (no services declared)"})
			}
		case "ok":
			detail := "daemon healthy"
			if len(dk) >= 2 && dk[1] == "group" {
				detail += " (via docker group — effectively root access)"
			}
			out = append(out, Result{Name: "Docker", Status: OK, Detail: detail})
		default:
			out = append(out, Result{Name: "Docker", Status: Fail,
				Detail: "docker is installed but the daemon is not responding",
				Hint:   "check `systemctl status docker` on the box"})
		}
	}
	return out
}

func gcloudVersion(ctx context.Context, d Deps) Result {
	res, err := d.Runner.Run(ctx, []string{"gcloud", "version", "--format", "json"})
	if err != nil || res.ExitCode != 0 {
		return Result{Name: "gcloud CLI", Status: Warn, Detail: "found, but `gcloud version` failed"}
	}
	var versions map[string]any
	if err := json.Unmarshal(res.Stdout, &versions); err == nil {
		if sdk, ok := versions["Google Cloud SDK"].(string); ok {
			return Result{Name: "gcloud CLI", Status: OK, Detail: "Google Cloud SDK " + sdk}
		}
	}
	return Result{Name: "gcloud CLI", Status: OK, Detail: "found on PATH"}
}

func activeAccount(ctx context.Context, d Deps) (string, Result) {
	res, err := d.Runner.Run(ctx, []string{"gcloud", "auth", "list", "--filter", "status:ACTIVE", "--format", "json"})
	if err != nil || res.ExitCode != 0 {
		return "", Result{Name: "gcloud account", Status: Fail, Detail: "could not list gcloud accounts",
			Hint: "run `gcloud auth login`"}
	}
	var accounts []struct {
		Account string `json:"account"`
	}
	if err := json.Unmarshal(res.Stdout, &accounts); err != nil || len(accounts) == 0 {
		return "", Result{Name: "gcloud account", Status: Fail, Detail: "no active gcloud account",
			Hint: "run `gcloud auth login`"}
	}
	return accounts[0].Account, Result{Name: "gcloud account", Status: OK, Detail: accounts[0].Account}
}

func projectAccess(ctx context.Context, d Deps) Result {
	project := d.Box.Provider.Project
	res, err := d.Runner.Run(ctx, []string{"gcloud", "projects", "describe", project, "--format", "json"})
	if err != nil || res.ExitCode != 0 {
		return Result{Name: "project access", Status: Fail,
			Detail: fmt.Sprintf("cannot access project %q", project),
			Hint:   "verify the project ID and that your account has access"}
	}
	return Result{Name: "project access", Status: OK, Detail: project}
}

func instanceCheck(ctx context.Context, d Deps) (*provider.Instance, Result) {
	inst, err := d.Client.Describe(ctx)
	if err != nil {
		return nil, Result{Name: "instance", Status: Fail, Detail: err.Error(),
			Hint: "verify provider.project, provider.zone, and provider.instance in the box definition"}
	}
	return inst, Result{Name: "instance", Status: OK,
		Detail: fmt.Sprintf("%s is %s (%s)", inst.Name, inst.Status, inst.MachineType)}
}

func osLogin(ctx context.Context, d Deps, inst *provider.Instance) Result {
	name := "OS Login"
	if inst != nil && truthy(inst.Metadata["enable-oslogin"]) {
		return Result{Name: name, Status: OK, Detail: "enabled via instance metadata"}
	}
	res, err := d.Runner.Run(ctx, []string{"gcloud", "compute", "project-info", "describe",
		"--project", d.Box.Provider.Project, "--format", "json"})
	if err == nil && res.ExitCode == 0 {
		var raw struct {
			CommonInstanceMetadata struct {
				Items []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"items"`
			} `json:"commonInstanceMetadata"`
		}
		if json.Unmarshal(res.Stdout, &raw) == nil {
			for _, item := range raw.CommonInstanceMetadata.Items {
				if item.Key == "enable-oslogin" && truthy(item.Value) {
					return Result{Name: name, Status: OK, Detail: "enabled via project metadata"}
				}
			}
		}
	}
	// Attached mode treats instance configuration as read-only (SPEC.md §7.1),
	// so this is a warning with remediation, not something Bastion fixes.
	return Result{Name: name, Status: Warn,
		Detail: "connection.osLogin is true but enable-oslogin metadata was not found",
		Hint:   "set instance metadata enable-oslogin=TRUE (or disable osLogin in the box definition)"}
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
	case "true", "1", "yes", "y":
		return true
	}
	return false
}
