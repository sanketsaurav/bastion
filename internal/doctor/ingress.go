package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/sanketsaurav/bastion/internal/provider"
)

// ingressChecks verifies everything public endpoints need that bastion
// deliberately does not manage (SPEC.md §9.8): a stable external IP, DNS
// records pointing at it, and an open 80/443 path. Every failure names the
// exact record or command to fix it.
func ingressChecks(ctx context.Context, d Deps, inst *provider.Instance) []Result {
	ing := d.Box.Ingress
	if inst == nil {
		return []Result{{Name: "ingress: external IP", Status: Skip, Detail: "instance state unknown"}}
	}
	if inst.ExternalIP == "" {
		return []Result{{Name: "ingress: external IP", Status: Fail,
			Detail: "the instance has no external IP",
			Hint:   "public endpoints need one; attach an external IP (ideally static) to the VM"}}
	}
	ip := inst.ExternalIP

	results := []Result{staticIPCheck(ctx, d, ip)}

	wildcard := dnsCheck(ctx, d, "ingress: wildcard DNS *."+ing.BaseDomain,
		"bastion-doctor-probe."+ing.BaseDomain, ip,
		fmt.Sprintf("create a wildcard A record `*.%s → %s`; on Cloudflare set it to DNS only (grey cloud) — a proxied record breaks certificate issuance", ing.BaseDomain, ip))

	// Per-host rows for hostnames the wildcard does not cover — every
	// hostname when the wildcard is absent, custom domains always.
	var hosts []Result
	hostsOK := true
	for _, pe := range d.Box.PublicEndpoints() {
		if wildcard.Status == OK && strings.HasSuffix(pe.Hostname, "."+ing.BaseDomain) {
			continue // covered by the wildcard
		}
		r := dnsCheck(ctx, d, "ingress: hostname "+pe.Hostname, pe.Hostname, ip,
			fmt.Sprintf("create an A record `%s → %s` (DNS only if the zone is proxied)", pe.Hostname, ip))
		if r.Status != OK {
			hostsOK = false
		}
		hosts = append(hosts, r)
	}

	// Per-host records are a legitimate strategy: when every declared
	// hostname resolves, a missing wildcard is advice, not a failure.
	if wildcard.Status == Fail && hostsOK && len(hosts) > 0 {
		wildcard.Status = Warn
		wildcard.Detail = "no wildcard record, but every declared hostname resolves to " + ip
		wildcard.Hint = fmt.Sprintf("works as-is; a wildcard `*.%s → %s` would make new apps zero-touch", ing.BaseDomain, ip)
	}
	results = append(results, wildcard)
	results = append(results, hosts...)

	results = append(results, portCheck(d, ip, "80"), portCheck(d, ip, "443"))
	return results
}

// staticIPCheck warns when the external IP is ephemeral: it changes across
// stop/start, which silently breaks every DNS record pointing at it.
func staticIPCheck(ctx context.Context, d Deps, ip string) Result {
	name := "ingress: static IP"
	region := d.Box.Provider.Zone
	if i := strings.LastIndex(region, "-"); i > 0 {
		region = region[:i]
	}
	res, err := d.Runner.Run(ctx, []string{"gcloud", "compute", "addresses", "list",
		"--project", d.Box.Provider.Project, "--filter", "address=" + ip, "--format", "json"})
	if err != nil || res.ExitCode != 0 {
		return Result{Name: name, Status: Warn, Detail: "could not list reserved addresses",
			Hint: "verify manually that " + ip + " is a reserved static address"}
	}
	var addrs []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(res.Stdout, &addrs) == nil && len(addrs) > 0 {
		return Result{Name: name, Status: OK, Detail: fmt.Sprintf("%s is reserved (%s)", ip, addrs[0].Name)}
	}
	return Result{Name: name, Status: Warn,
		Detail: ip + " appears to be ephemeral; it changes when the instance stops and breaks DNS",
		Hint: fmt.Sprintf("promote it: gcloud compute addresses create %s-ingress --project %s --region %s --addresses %s",
			d.Box.Metadata.Name, d.Box.Provider.Project, region, ip)}
}

func dnsCheck(ctx context.Context, d Deps, name, host, wantIP, createHint string) Result {
	lookup := d.LookupHost
	if lookup == nil {
		lookup = func(ctx context.Context, h string) ([]string, error) {
			return net.DefaultResolver.LookupHost(ctx, h)
		}
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return Result{Name: name, Status: Fail, Detail: host + " does not resolve", Hint: createHint}
	}
	if slices.Contains(addrs, wantIP) {
		return Result{Name: name, Status: OK, Detail: "resolves to " + wantIP}
	}
	return Result{Name: name, Status: Fail,
		Detail: fmt.Sprintf("resolves to %s instead of %s", strings.Join(addrs, ", "), wantIP),
		Hint:   "point the record at the instance IP; if it is proxied (Cloudflare orange cloud), switch it to DNS only"}
}

// portCheck distinguishes a dropped connection (GCP firewalls drop, so a
// timeout means "blocked") from a refused one (the path is open; nothing is
// listening yet, which is expected before the first ingress apply).
func portCheck(d Deps, ip, port string) Result {
	name := "ingress: port " + port
	dial := d.DialTimeout
	if dial == nil {
		dial = net.DialTimeout
	}
	conn, err := dial("tcp", net.JoinHostPort(ip, port), 4*time.Second)
	if err == nil {
		conn.Close()
		return Result{Name: name, Status: OK, Detail: "reachable"}
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return Result{Name: name, Status: Fail, Detail: "connection timed out — likely blocked by firewall",
			Hint: fmt.Sprintf("allow ingress: gcloud compute firewall-rules create allow-bastion-ingress --project %s --allow tcp:80,tcp:443 --direction INGRESS (add --network for a non-default VPC)", d.Box.Provider.Project)}
	}
	return Result{Name: name, Status: Warn,
		Detail: "connection refused — the firewall path is open but nothing is listening yet",
		Hint:   "expected before the first `bastion apply` deploys the ingress proxy"}
}
