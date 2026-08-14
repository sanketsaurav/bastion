package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/execx"
	"github.com/sanketsaurav/bastion/internal/provider/gcp"
)

func testBox(t *testing.T) *config.Box {
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
`))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

const runningInstanceJSON = `{
  "name": "vm", "status": "RUNNING",
  "zone": ".../zones/us-west1-a", "machineType": ".../machineTypes/e2-standard-4",
  "metadata": {"items": [{"key": "enable-oslogin", "value": "TRUE"}]}
}`

const healthyProbeOutput = `@d os ubuntu 24.04 x86_64
@d sudo ok
@d egress ok
@d disk 26214400
@d docker ok sudo
@d end
`

// probeMatch matches only the guest-probe invocation (bash -s over ssh),
// leaving plain CheckSSH probes to the generic ssh rule.
func probeMatch(argv []string) bool {
	if len(argv) < 3 || argv[0] != "gcloud" || argv[2] != "ssh" {
		return false
	}
	for _, a := range argv {
		if a == "bash -s" {
			return true
		}
	}
	return false
}

func healthyRules() []execx.Rule {
	return []execx.Rule{
		{Match: execx.Prefix("gcloud", "version"), Result: execx.Result{Stdout: []byte(`{"Google Cloud SDK": "502.0.0"}`)}},
		{Match: execx.Prefix("gcloud", "auth", "list"), Result: execx.Result{Stdout: []byte(`[{"account": "dev@example.com", "status": "ACTIVE"}]`)}},
		{Match: execx.Prefix("gcloud", "projects", "describe"), Result: execx.Result{Stdout: []byte(`{"projectId": "proj"}`)}},
		{Match: execx.Prefix("gcloud", "compute", "instances", "describe"), Result: execx.Result{Stdout: []byte(runningInstanceJSON)}},
		{Match: probeMatch, Result: execx.Result{Stdout: []byte(healthyProbeOutput)}},
		{Match: execx.Prefix("gcloud", "compute", "ssh"), Result: execx.Result{}},
	}
}

func deps(t *testing.T, fake *execx.Fake) Deps {
	t.Helper()
	box := testBox(t)
	return Deps{
		Runner:   fake,
		LookPath: func(string) (string, error) { return "/usr/bin/gcloud", nil },
		Client:   gcp.FromBox(box, fake),
		Box:      box,
	}
}

func byName(results []Result, name string) *Result {
	for i := range results {
		if results[i].Name == name {
			return &results[i]
		}
	}
	return nil
}

func TestRunAllHealthy(t *testing.T) {
	fake := &execx.Fake{Rules: healthyRules()}
	results := Run(context.Background(), deps(t, fake))
	if Failed(results) {
		t.Fatalf("expected no failures: %+v", results)
	}
	for _, name := range []string{
		"gcloud CLI", "gcloud account", "project access", "instance", "OS Login", "SSH reachability",
		"guest OS", "sudo", "internet egress", "data root", "Docker",
	} {
		r := byName(results, name)
		if r == nil {
			t.Errorf("missing check %q", name)
			continue
		}
		if r.Status != OK {
			t.Errorf("%s = %s (%s)", name, r.Status, r.Detail)
		}
	}
	if r := byName(results, "data root"); r != nil && r.Detail != "/mnt/bastion (25.0 GiB free)" {
		t.Errorf("data root detail = %q", r.Detail)
	}
}

func TestRunGuestProblems(t *testing.T) {
	rules := healthyRules()
	rules[4] = execx.Rule{Match: probeMatch, Result: execx.Result{Stdout: []byte(`@d os ubuntu 24.04 aarch64
@d sudo missing
@d egress blocked
@d disk missing
@d docker absent
@d end
`)}}
	fake := &execx.Fake{Rules: rules}
	results := Run(context.Background(), deps(t, fake))

	if r := byName(results, "internet egress"); r == nil || r.Status != Fail || r.Hint == "" {
		t.Errorf("egress check = %+v (a VM without NAT or an external IP must fail loudly)", r)
	}
	if r := byName(results, "sudo"); r == nil || r.Status != Warn {
		t.Errorf("sudo check = %+v", r)
	}
	if r := byName(results, "data root"); r == nil || r.Status != Warn {
		t.Errorf("data root check = %+v", r)
	}
	if r := byName(results, "Docker"); r == nil || r.Status != Skip {
		t.Errorf("docker check with no declared services = %+v", r)
	}
	if !Failed(results) {
		t.Error("blocked egress must fail the doctor run")
	}
}

func TestSSHProbeRetriesThroughBootRace(t *testing.T) {
	old := sshProbeDelay
	sshProbeDelay = 0
	defer func() { sshProbeDelay = old }()

	probeCmd := func(argv []string) bool {
		for _, a := range argv {
			if a == "true" {
				return true
			}
		}
		return false
	}
	failures := 0
	rules := healthyRules()
	// Fail the first two SSH probes (key propagation), succeed on the third.
	rules = append([]execx.Rule{{
		Match: func(argv []string) bool {
			if !probeCmd(argv) || failures >= 2 {
				return false
			}
			failures++
			return true
		},
		Result: execx.Result{ExitCode: 255, Stderr: []byte("Permission denied (publickey).")},
	}}, rules...)
	fake := &execx.Fake{Rules: rules}
	results := Run(context.Background(), deps(t, fake))
	r := byName(results, "SSH reachability")
	if r == nil || r.Status != OK {
		t.Errorf("ssh probe must survive a boot race via retries, got %+v", r)
	}
	if failures != 2 {
		t.Errorf("expected 2 failed attempts before success, got %d", failures)
	}
}

func TestRunUnsupportedGuestOS(t *testing.T) {
	rules := healthyRules()
	rules[4] = execx.Rule{Match: probeMatch, Result: execx.Result{Stdout: []byte("@d os debian 12 x86_64\n@d end\n")}}
	fake := &execx.Fake{Rules: rules}
	results := Run(context.Background(), deps(t, fake))
	if r := byName(results, "guest OS"); r == nil || r.Status != Fail {
		t.Errorf("guest OS check = %+v", r)
	}
}

func TestRunNoActiveAccount(t *testing.T) {
	rules := healthyRules()
	rules[1] = execx.Rule{Match: execx.Prefix("gcloud", "auth", "list"), Result: execx.Result{Stdout: []byte(`[]`)}}
	fake := &execx.Fake{Rules: rules}
	results := Run(context.Background(), deps(t, fake))
	if !Failed(results) {
		t.Fatal("expected a failure")
	}
	r := byName(results, "gcloud account")
	if r == nil || r.Status != Fail || r.Hint == "" {
		t.Errorf("account check = %+v", r)
	}
	// Later checks must not run without an account.
	if byName(results, "instance") != nil {
		t.Error("instance check should not run without an active account")
	}
}

func TestRunStoppedInstanceSkipsSSH(t *testing.T) {
	rules := healthyRules()
	stopped := `{"name": "vm", "status": "TERMINATED", "zone": "z", "machineType": "m",
	  "metadata": {"items": [{"key": "enable-oslogin", "value": "TRUE"}]}}`
	rules[3] = execx.Rule{Match: execx.Prefix("gcloud", "compute", "instances", "describe"), Result: execx.Result{Stdout: []byte(stopped)}}
	fake := &execx.Fake{Rules: rules}
	results := Run(context.Background(), deps(t, fake))
	r := byName(results, "SSH reachability")
	if r == nil || r.Status != Skip {
		t.Errorf("ssh check = %+v", r)
	}
	if Failed(results) {
		t.Errorf("a stopped instance is not a failure: %+v", results)
	}
}

func TestRunMissingGcloud(t *testing.T) {
	d := deps(t, &execx.Fake{})
	d.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	results := Run(context.Background(), d)
	if !Failed(results) {
		t.Fatal("expected a failure")
	}
	if len(results) != 1 || results[0].Name != "gcloud CLI" || results[0].Hint == "" {
		t.Errorf("results = %+v", results)
	}
}
