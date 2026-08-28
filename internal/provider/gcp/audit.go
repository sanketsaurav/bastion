package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sanketsaurav/bastion/internal/audit"
	"github.com/sanketsaurav/bastion/internal/provider"
)

// AuditCloud implements audit.CloudAuditor: the GCP-specific slice of the
// hardening checks. Attached mode never fixes these itself (SPEC.md §7.1);
// every finding carries the exact command.
func (c *Client) AuditCloud(ctx context.Context, ingressDeclared bool) []audit.Result {
	inst, err := c.Describe(ctx)
	if err != nil {
		return []audit.Result{{Name: "cloud checks", Status: audit.Warn,
			Detail: "could not describe the instance", Hint: err.Error()}}
	}
	results := []audit.Result{
		c.checkServiceAccount(inst.ServiceAccounts),
		c.checkSecureBoot(inst.SecureBoot),
	}
	results = append(results, c.checkFirewall(ctx, inst.Network, ingressDeclared)...)
	return results
}

// Anything running on the box — a compromised container, an SSRF in a
// public app — can mint tokens for an attached service account via the
// metadata server. A personal dev box rarely needs one at all.
func (c *Client) checkServiceAccount(sas []provider.ServiceAccount) audit.Result {
	name := "service account"
	if len(sas) == 0 {
		return audit.Result{Name: name, Status: audit.OK, Detail: "no service account attached"}
	}
	sa := sas[0]
	hint := fmt.Sprintf("if the box does not need cloud API access, detach it: bastion down, then "+
		"`gcloud compute instances set-service-account %s --project %s --zone %s --no-service-account --no-scopes`, then bastion up",
		c.opt.Instance, c.opt.Project, c.opt.Zone)
	if strings.HasSuffix(sa.Email, "-compute@developer.gserviceaccount.com") {
		return audit.Result{Name: name, Status: audit.Fail,
			Detail: fmt.Sprintf("the default compute service account is attached (%s) — every process on the box can use its project-wide permissions", sa.Email),
			Hint:   hint}
	}
	return audit.Result{Name: name, Status: audit.Warn,
		Detail: fmt.Sprintf("%s is attached with %d scope(s); everything on the box can act as it", sa.Email, len(sa.Scopes)),
		Hint:   hint}
}

// Secure Boot blocks persistence via tampered boot components.
func (c *Client) checkSecureBoot(sb *bool) audit.Result {
	name := "secure boot"
	switch {
	case sb == nil:
		return audit.Result{Name: name, Status: audit.Warn, Detail: "the platform did not report Shielded VM configuration"}
	case *sb:
		return audit.Result{Name: name, Status: audit.OK, Detail: "enabled"}
	}
	return audit.Result{Name: name, Status: audit.Warn, Detail: "disabled",
		Hint: fmt.Sprintf("enable while stopped: bastion down, then `gcloud compute instances update %s --project %s --zone %s --shielded-secure-boot`, then bastion up",
			c.opt.Instance, c.opt.Project, c.opt.Zone)}
}

// checkFirewall flags world-open ingress on the box's network beyond the
// declared surface. Port 22 gets the IAP-range fix rather than deletion —
// bastion's own transport still needs it.
func (c *Client) checkFirewall(ctx context.Context, network string, ingressDeclared bool) []audit.Result {
	name := "firewall exposure"
	res, err := c.run.Run(ctx, []string{"gcloud", "compute", "firewall-rules", "list",
		"--project", c.opt.Project, "--format", "json"})
	if err != nil || res.ExitCode != 0 {
		return []audit.Result{{Name: name, Status: audit.Warn, Detail: "could not list firewall rules",
			Hint: "grant compute.firewalls.list or review rules manually"}}
	}
	var rules []struct {
		Name         string   `json:"name"`
		Network      string   `json:"network"`
		Direction    string   `json:"direction"`
		Disabled     bool     `json:"disabled"`
		SourceRanges []string `json:"sourceRanges"`
		Allowed      []struct {
			IPProtocol string   `json:"IPProtocol"`
			Ports      []string `json:"ports"`
		} `json:"allowed"`
	}
	if err := json.Unmarshal(res.Stdout, &rules); err != nil {
		return []audit.Result{{Name: name, Status: audit.Warn, Detail: "could not parse firewall rules"}}
	}

	expected := map[string]bool{}
	if ingressDeclared {
		expected["80"], expected["443"] = true, true
	}
	var findings []audit.Result
	for _, rule := range rules {
		if rule.Disabled || rule.Direction != "INGRESS" || pathBase(rule.Network) != network {
			continue
		}
		world := false
		for _, sr := range rule.SourceRanges {
			if sr == "0.0.0.0/0" || sr == "::/0" {
				world = true
			}
		}
		if !world {
			continue
		}
		var exposed []string
		sshOnly := true
		for _, allow := range rule.Allowed {
			proto := strings.ToLower(allow.IPProtocol)
			if proto == "icmp" {
				continue
			}
			if len(allow.Ports) == 0 {
				exposed = append(exposed, proto+":all")
				sshOnly = false
				continue
			}
			for _, port := range allow.Ports {
				if expected[port] {
					continue
				}
				exposed = append(exposed, proto+":"+port)
				if port != "22" {
					sshOnly = false
				}
			}
		}
		if len(exposed) == 0 {
			continue
		}
		hint := fmt.Sprintf("delete it: `gcloud compute firewall-rules delete %s --project %s`", rule.Name, c.opt.Project)
		if sshOnly {
			hint = fmt.Sprintf("bastion connects over IAP; scope it: `gcloud compute firewall-rules update %s --project %s --source-ranges=35.235.240.0/20`",
				rule.Name, c.opt.Project)
		}
		findings = append(findings, audit.Result{
			Name:   name,
			Status: audit.Fail,
			Detail: fmt.Sprintf("rule %q opens %s to the world on network %q", rule.Name, strings.Join(exposed, ", "), network),
			Hint:   hint,
		})
	}
	if len(findings) == 0 {
		return []audit.Result{{Name: name, Status: audit.OK,
			Detail: "nothing world-open on this network beyond the declared surface"}}
	}
	return findings
}
