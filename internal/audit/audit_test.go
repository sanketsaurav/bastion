package audit

import (
	"context"
	"strings"
	"testing"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/execx"
)

type fakeSession struct{ lines []string }

func (f *fakeSession) RunScript(_ context.Context, _ []byte, onLine func(string)) (execx.Result, error) {
	for _, l := range f.lines {
		onLine(l)
	}
	return execx.Result{}, nil
}

type fakeCloud struct{ results []Result }

func (f *fakeCloud) AuditCloud(context.Context, bool) []Result { return f.results }

func auditBox(t *testing.T, ingress bool) *config.Box {
	t.Helper()
	doc := `
apiVersion: bastion/v1alpha1
kind: Box
metadata:
  name: dev
provider:
  name: gcp
  project: p
  zone: us-west1-a
  instance: vm
`
	if ingress {
		doc += `
ingress:
  baseDomain: apps.example.com
services:
  web:
    image: x
    endpoints:
      http: {containerPort: 8000, visibility: public, auth: none}
`
	}
	box, err := config.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func find(t *testing.T, results []Result, name string) Result {
	t.Helper()
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no result %q in %v", name, results)
	return Result{}
}

func TestGuestChecksHealthy(t *testing.T) {
	sess := &fakeSession{lines: []string{
		"@a unatt installed 1",
		"@a reboot no",
		"@a passauth no",
		"@a listen 22", "@a listen 80", "@a listen 443",
		"@a end",
	}}
	results := Run(context.Background(), Deps{
		Cloud: &fakeCloud{}, Session: sess, Box: auditBox(t, true), GuestReachable: true,
	})
	for _, name := range []string{"security updates", "pending reboot", "SSH password auth", "exposed listeners"} {
		if r := find(t, results, name); r.Status != OK {
			t.Errorf("%s = %+v, want ok", name, r)
		}
	}
}

func TestGuestChecksFindings(t *testing.T) {
	sess := &fakeSession{lines: []string{
		"@a unatt installed 0", // installed but disabled
		"@a reboot 12",
		"@a passauth yes",
		"@a listen 22", "@a listen 80", "@a listen 5432", // no ingress declared
		"@a end",
	}}
	results := Run(context.Background(), Deps{
		Cloud: &fakeCloud{}, Session: sess, Box: auditBox(t, false), GuestReachable: true,
	})
	if r := find(t, results, "security updates"); r.Status != Fail {
		t.Errorf("disabled unattended-upgrades must fail: %+v", r)
	}
	if r := find(t, results, "pending reboot"); r.Status != Warn || !strings.Contains(r.Detail, "12 day") {
		t.Errorf("stale reboot must warn with age: %+v", r)
	}
	if r := find(t, results, "SSH password auth"); r.Status != Fail {
		t.Errorf("password auth must fail: %+v", r)
	}
	r := find(t, results, "exposed listeners")
	// Without declared ingress, 80 is as unexpected as 5432; 22 never is.
	if r.Status != Fail || !strings.Contains(r.Detail, "5432") || !strings.Contains(r.Detail, "80") ||
		strings.Contains(r.Detail, "22") {
		t.Errorf("listener check = %+v", r)
	}
}

func TestGuestUnreachableSkips(t *testing.T) {
	results := Run(context.Background(), Deps{
		Cloud: &fakeCloud{results: []Result{{Name: "cloud", Status: OK}}},
		Box:   auditBox(t, false),
	})
	if r := find(t, results, "guest checks"); r.Status != Skip {
		t.Errorf("unreachable guest must skip, got %+v", r)
	}
	find(t, results, "cloud") // provider checks still run
}
