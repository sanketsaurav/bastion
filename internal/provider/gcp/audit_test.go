package gcp

import (
	"context"
	"strings"
	"testing"

	"github.com/sanketsaurav/bastion/internal/audit"
	"github.com/sanketsaurav/bastion/internal/execx"
)

const auditInstanceJSON = `{
  "name": "vm", "status": "RUNNING",
  "zone": ".../zones/us-west1-a", "machineType": ".../machineTypes/e2-standard-2",
  "networkInterfaces": [{"network": ".../networks/default", "networkIP": "10.0.0.2"}],
  "serviceAccounts": [{"email": "12345-compute@developer.gserviceaccount.com", "scopes": ["a", "b"]}],
  "shieldedInstanceConfig": {"enableSecureBoot": false}
}`

const auditFirewallJSON = `[
  {"name": "default-allow-ssh", "network": ".../networks/default", "direction": "INGRESS",
   "sourceRanges": ["0.0.0.0/0"], "allowed": [{"IPProtocol": "tcp", "ports": ["22"]}]},
  {"name": "default-allow-rdp", "network": ".../networks/default", "direction": "INGRESS",
   "sourceRanges": ["0.0.0.0/0"], "allowed": [{"IPProtocol": "tcp", "ports": ["3389"]}]},
  {"name": "allow-web", "network": ".../networks/default", "direction": "INGRESS",
   "sourceRanges": ["0.0.0.0/0"], "allowed": [{"IPProtocol": "tcp", "ports": ["80", "443"]}]},
  {"name": "other-net", "network": ".../networks/prod", "direction": "INGRESS",
   "sourceRanges": ["0.0.0.0/0"], "allowed": [{"IPProtocol": "tcp", "ports": ["9999"]}]},
  {"name": "scoped-ssh", "network": ".../networks/default", "direction": "INGRESS",
   "sourceRanges": ["35.235.240.0/20"], "allowed": [{"IPProtocol": "tcp", "ports": ["22"]}]}
]`

func auditClient() *Client {
	run := &execx.Fake{Rules: []execx.Rule{
		{Match: execx.Prefix("gcloud", "compute", "instances", "describe"),
			Result: execx.Result{Stdout: []byte(auditInstanceJSON)}},
		{Match: execx.Prefix("gcloud", "compute", "firewall-rules", "list"),
			Result: execx.Result{Stdout: []byte(auditFirewallJSON)}},
	}}
	return New(Options{Project: "proj", Zone: "us-west1-a", Instance: "vm", Transport: TransportIAP}, run)
}

func findResult(t *testing.T, results []audit.Result, name, detail string) audit.Result {
	t.Helper()
	for _, r := range results {
		if r.Name == name && strings.Contains(r.Detail, detail) {
			return r
		}
	}
	t.Fatalf("no %q result containing %q in %+v", name, detail, results)
	return audit.Result{}
}

func TestAuditCloud(t *testing.T) {
	results := auditClient().AuditCloud(context.Background(), true)

	sa := findResult(t, results, "service account", "default compute service account")
	if sa.Status != audit.Fail || !strings.Contains(sa.Hint, "--no-service-account") {
		t.Errorf("default SA must fail with the detach command: %+v", sa)
	}
	sb := findResult(t, results, "secure boot", "disabled")
	if sb.Status != audit.Warn || !strings.Contains(sb.Hint, "--shielded-secure-boot") {
		t.Errorf("secure boot off must warn with the enable command: %+v", sb)
	}
	// World-open 22 gets the IAP scoping fix, not deletion.
	ssh := findResult(t, results, "firewall exposure", `"default-allow-ssh"`)
	if ssh.Status != audit.Fail || !strings.Contains(ssh.Hint, "35.235.240.0/20") {
		t.Errorf("world-open ssh must suggest IAP scoping: %+v", ssh)
	}
	rdp := findResult(t, results, "firewall exposure", `"default-allow-rdp"`)
	if rdp.Status != audit.Fail || !strings.Contains(rdp.Hint, "delete") {
		t.Errorf("world-open rdp must suggest deletion: %+v", rdp)
	}
	// With ingress declared, 80/443 is the declared surface; other
	// networks and already-scoped rules are silent.
	for _, r := range results {
		if strings.Contains(r.Detail, `"allow-web"`) || strings.Contains(r.Detail, `"other-net"`) ||
			strings.Contains(r.Detail, `"scoped-ssh"`) {
			t.Errorf("must not flag %+v", r)
		}
	}
}

func TestAuditCloudNoIngressFlagsWeb(t *testing.T) {
	results := auditClient().AuditCloud(context.Background(), false)
	web := findResult(t, results, "firewall exposure", `"allow-web"`)
	if web.Status != audit.Fail {
		t.Errorf("without declared ingress, world-open 80/443 must be flagged: %+v", web)
	}
}
