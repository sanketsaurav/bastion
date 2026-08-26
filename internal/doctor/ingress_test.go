package doctor

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/execx"
	"github.com/sanketsaurav/bastion/internal/provider"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func ingressBox(t *testing.T) *config.Box {
	t.Helper()
	box, err := config.Parse([]byte(`
apiVersion: bastion/v1alpha1
kind: Box
metadata:
  name: dev
provider:
  name: gcp
  project: proj
  zone: us-west1-a
  instance: vm
ingress:
  baseDomain: apps.example.com
services:
  blog:
    image: x
    endpoints:
      web:
        containerPort: 8000
        visibility: public
        auth: none
  news:
    image: y
    endpoints:
      web:
        containerPort: 8001
        visibility: public
        auth: none
        hostname: news.example.org
`))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func findResult(t *testing.T, results []Result, name string) Result {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no result named %q in %v", name, results)
	return Result{}
}

func TestIngressChecks(t *testing.T) {
	const ip = "203.0.113.7"
	inst := &provider.Instance{Status: "RUNNING", ExternalIP: ip}
	runner := &execx.Fake{Rules: []execx.Rule{{
		Match:  execx.Prefix("gcloud", "compute", "addresses", "list"),
		Result: execx.Result{Stdout: []byte(`[]`)},
	}}}
	d := Deps{
		Runner: runner,
		Box:    ingressBox(t),
		LookupHost: func(_ context.Context, host string) ([]string, error) {
			switch {
			case strings.HasSuffix(host, ".apps.example.com"):
				return []string{ip}, nil
			case host == "news.example.org":
				return []string{"104.16.0.1"}, nil // proxied elsewhere
			}
			return nil, errors.New("no such host")
		},
		DialTimeout: func(_, addr string, _ time.Duration) (net.Conn, error) {
			if strings.HasSuffix(addr, ":80") {
				return nil, timeoutErr{}
			}
			return nil, errors.New("connection refused")
		},
	}
	results := ingressChecks(context.Background(), d, inst)

	static := findResult(t, results, "ingress: static IP")
	if static.Status != Warn || !strings.Contains(static.Hint, "addresses create") ||
		!strings.Contains(static.Hint, "--region us-west1") {
		t.Errorf("ephemeral IP must warn with a promote command: %+v", static)
	}
	wildcard := findResult(t, results, "ingress: wildcard DNS *.apps.example.com")
	if wildcard.Status != OK {
		t.Errorf("wildcard resolving to the instance IP must pass: %+v", wildcard)
	}
	custom := findResult(t, results, "ingress: hostname news.example.org")
	if custom.Status != Fail || !strings.Contains(custom.Hint, "DNS only") {
		t.Errorf("a proxied custom hostname must fail with the grey-cloud hint: %+v", custom)
	}
	p80 := findResult(t, results, "ingress: port 80")
	if p80.Status != Fail || !strings.Contains(p80.Hint, "firewall-rules create") {
		t.Errorf("a timeout must read as firewall-blocked: %+v", p80)
	}
	p443 := findResult(t, results, "ingress: port 443")
	if p443.Status != Warn || !strings.Contains(p443.Detail, "nothing is listening") {
		t.Errorf("a refusal must read as open-but-idle: %+v", p443)
	}
}

// Per-host records instead of a wildcard are legitimate: the wildcard row
// must downgrade to a warning when every declared hostname resolves.
func TestIngressPerHostRecordsWarnNotFail(t *testing.T) {
	const ip = "203.0.113.7"
	d := Deps{
		Runner: &execx.Fake{Rules: []execx.Rule{{
			Match:  execx.Prefix("gcloud", "compute", "addresses", "list"),
			Result: execx.Result{Stdout: []byte(`[{"name":"static"}]`)},
		}}},
		Box: ingressBox(t),
		LookupHost: func(_ context.Context, host string) ([]string, error) {
			if strings.HasPrefix(host, "bastion-doctor-probe.") {
				return nil, errors.New("no such host") // no wildcard
			}
			return []string{ip}, nil // every declared hostname resolves
		},
		DialTimeout: func(_, addr string, _ time.Duration) (net.Conn, error) {
			return nil, errors.New("connection refused")
		},
	}
	results := ingressChecks(context.Background(), d, &provider.Instance{Status: "RUNNING", ExternalIP: ip})
	wildcard := findResult(t, results, "ingress: wildcard DNS *.apps.example.com")
	if wildcard.Status != Warn || !strings.Contains(wildcard.Hint, "zero-touch") {
		t.Errorf("per-host strategy must warn, not fail: %+v", wildcard)
	}
	blog := findResult(t, results, "ingress: hostname blog.apps.example.com")
	if blog.Status != OK {
		t.Errorf("resolving per-host record must pass: %+v", blog)
	}
}

func TestIngressChecksNeedExternalIP(t *testing.T) {
	d := Deps{Box: ingressBox(t)}
	results := ingressChecks(context.Background(), d, &provider.Instance{Status: "RUNNING"})
	if len(results) != 1 || results[0].Status != Fail {
		t.Fatalf("missing external IP must fail immediately: %v", results)
	}
}
