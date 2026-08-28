// Package audit runs bastion's hardening checks: a deliberately short list
// of high-value findings, each mapping to a real compromise path, each with
// its exact remediation. Read-only by contract — audit never mutates the
// guest or the cloud.
package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/execx"
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

// CloudAuditor contributes provider-specific checks — attached identities,
// firewall exposure, platform boot security. Each provider implements it
// next to its client; GCP is the only implementation today.
type CloudAuditor interface {
	AuditCloud(ctx context.Context, ingressDeclared bool) []Result
}

// Session reaches the guest (satisfied by the provider client).
type Session interface {
	RunScript(ctx context.Context, script []byte, onLine func(string)) (execx.Result, error)
}

// Deps are the injectable collaborators.
type Deps struct {
	Cloud   CloudAuditor
	Session Session
	Box     *config.Box
	// GuestReachable gates the guest checks; when false they report Skip
	// instead of failing on transport errors.
	GuestReachable bool
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

// Run executes the audit: provider checks first, then the guest.
func Run(ctx context.Context, d Deps) []Result {
	results := d.Cloud.AuditCloud(ctx, d.Box.Ingress != nil)
	if !d.GuestReachable {
		return append(results, Result{Name: "guest checks", Status: Skip,
			Detail: "box is not running or not reachable"})
	}
	return append(results, guestAudit(ctx, d)...)
}

// guestScript gathers everything the guest checks need in one read-only
// SSH round trip, as an "@a key value…" line protocol.
const guestScript = `set -u
echo "@a unatt $(dpkg-query -W -f='${db:Status-Status}' unattended-upgrades 2>/dev/null || echo absent) $(apt-config dump APT::Periodic::Unattended-Upgrade 2>/dev/null | grep -om1 '"[0-9]*"' | tr -d '"')"
if [ -f /var/run/reboot-required ]; then echo "@a reboot $(( ($(date +%s) - $(stat -c %Y /var/run/reboot-required)) / 86400 ))"; else echo "@a reboot no"; fi
echo "@a passauth $(sudo -n sshd -T 2>/dev/null | awk '/^passwordauthentication /{print $2}')"
sudo -n ss -Htln 2>/dev/null | awk '$4 ~ /^0\.0\.0\.0:|^\[::\]:/' | sed -E 's/.*[]:]([0-9]+)( .*)?$/\1/' | sort -un | while read -r p; do echo "@a listen $p"; done
echo "@a end"
exit 0
`

func guestAudit(ctx context.Context, d Deps) []Result {
	facts := map[string][]string{}
	var listeners []string
	_, err := d.Session.RunScript(ctx, []byte(guestScript), func(line string) {
		if !strings.HasPrefix(line, "@a ") {
			return
		}
		parts := strings.Fields(line[3:])
		if len(parts) == 0 {
			return
		}
		if parts[0] == "listen" && len(parts) >= 2 {
			listeners = append(listeners, parts[1])
			return
		}
		facts[parts[0]] = parts[1:]
	})
	if _, done := facts["end"]; err != nil || !done {
		return []Result{{Name: "guest checks", Status: Warn, Detail: "guest probe did not complete",
			Hint: "rerun with --verbose; the box may be mid-boot"}}
	}

	var results []Result
	results = append(results, checkUnattended(facts["unatt"]))
	results = append(results, checkReboot(facts["reboot"]))
	results = append(results, checkPasswordAuth(facts["passauth"]))
	results = append(results, checkListeners(d.Box, listeners))
	return results
}

// Unpatched software is the boring way boxes get owned: security updates
// must apply themselves.
func checkUnattended(v []string) Result {
	name := "security updates"
	installed := len(v) >= 1 && v[0] == "installed"
	enabled := len(v) >= 2 && v[1] != "0" && v[1] != ""
	if installed && enabled {
		return Result{Name: name, Status: OK, Detail: "unattended-upgrades installed and enabled"}
	}
	detail := "unattended-upgrades is not installed"
	if installed {
		detail = "unattended-upgrades is installed but not enabled"
	}
	return Result{Name: name, Status: Fail, Detail: detail,
		Hint: "add `unattended-upgrades` to host.packages and apply; enable with APT::Periodic::Unattended-Upgrade \"1\""}
}

// A pending reboot means an already-downloaded kernel or libc fix is not
// actually protecting anything yet.
func checkReboot(v []string) Result {
	name := "pending reboot"
	if len(v) == 0 || v[0] == "no" {
		return Result{Name: name, Status: OK, Detail: "no reboot pending"}
	}
	days := v[0]
	return Result{Name: name, Status: Warn,
		Detail: fmt.Sprintf("updates have waited %s day(s) for a reboot", days),
		Hint:   "reboot to activate them (`bastion down && bastion up`), or set host.hardening.autoReboot so it happens in a nightly window"}
}

// With any world-open SSH path, password authentication is a brute-force
// target; keys and OS Login never are.
func checkPasswordAuth(v []string) Result {
	name := "SSH password auth"
	if len(v) >= 1 && v[0] == "no" {
		return Result{Name: name, Status: OK, Detail: "disabled"}
	}
	if len(v) == 0 || v[0] == "" {
		return Result{Name: name, Status: Warn, Detail: "could not read sshd configuration"}
	}
	return Result{Name: name, Status: Fail, Detail: "sshd accepts password logins",
		Hint: "set `PasswordAuthentication no` in /etc/ssh/sshd_config.d/ and restart sshd"}
}

// Every port listening on 0.0.0.0 is attack surface; the declared surface
// is sshd plus the ingress proxy and nothing else.
func checkListeners(box *config.Box, ports []string) Result {
	name := "exposed listeners"
	expected := map[string]bool{"22": true}
	if box.Ingress != nil {
		expected["80"], expected["443"] = true, true
	}
	var unexpected []string
	for _, p := range ports {
		if !expected[p] {
			unexpected = append(unexpected, p)
		}
	}
	if len(unexpected) == 0 {
		return Result{Name: name, Status: OK, Detail: "only the declared surface listens publicly"}
	}
	sort.Strings(unexpected)
	return Result{Name: name, Status: Fail,
		Detail: "ports listening on 0.0.0.0 outside the declared surface: " + strings.Join(unexpected, ", "),
		Hint:   "identify the process with " + shellquote.Join([]string{"bastion", "exec", box.Metadata.Name, "--", "sudo", "ss", "-tlnp"}) + " and stop or loopback-bind it"}
}
