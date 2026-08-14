package gcp

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/sanketsaurav/bastion/internal/execx"
)

func iapClient(run execx.Runner) *Client {
	return New(Options{Project: "proj", Zone: "us-west1-a", Instance: "vm", Transport: TransportIAP}, run)
}

func TestSSHArgvIAP(t *testing.T) {
	got := iapClient(nil).SSHArgv(nil)
	want := []string{"gcloud", "compute", "ssh", "vm", "--project", "proj", "--zone", "us-west1-a", "--verbosity=error", "--tunnel-through-iap"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SSHArgv = %v, want %v", got, want)
	}
}

func TestSSHArgvExtraArgsAfterDash(t *testing.T) {
	got := iapClient(nil).SSHArgv([]string{"-o", "ServerAliveInterval=30"})
	tail := got[len(got)-3:]
	if !reflect.DeepEqual(tail, []string{"--", "-o", "ServerAliveInterval=30"}) {
		t.Errorf("tail = %v", tail)
	}
}

func TestSSHArgvForwardAgent(t *testing.T) {
	c := New(Options{Project: "p", Zone: "z", Instance: "i", Transport: TransportIAP, ForwardAgent: true}, nil)
	got := c.SSHArgv(nil)
	tail := got[len(got)-2:]
	if !reflect.DeepEqual(tail, []string{"--", "-A"}) {
		t.Errorf("agent forwarding must be explicit ssh -A after --, got tail %v", tail)
	}
}

func TestSSHArgvDirect(t *testing.T) {
	c := New(Options{Transport: TransportDirect, Host: "dev.example.com", User: "alice", IdentityFile: "~/.ssh/id"}, nil)
	got := c.SSHArgv([]string{"-o", "LogLevel=quiet"})
	want := []string{"ssh", "-i", "~/.ssh/id", "-o", "LogLevel=quiet", "alice@dev.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SSHArgv = %v, want %v", got, want)
	}
}

func TestExecArgvQuotesEachArgument(t *testing.T) {
	got := iapClient(nil).ExecArgv([]string{"echo", "hi there", "a;b", "$(reboot)"}, false)
	if got[len(got)-2] != "--command" {
		t.Fatalf("argv = %v", got)
	}
	cmd := got[len(got)-1]
	want := `echo 'hi there' 'a;b' '$(reboot)'`
	if cmd != want {
		t.Errorf("command = %q, want %q", cmd, want)
	}
}

func TestExecArgvShellOptIn(t *testing.T) {
	got := iapClient(nil).ExecArgv([]string{"ls", "|", "wc", "-l"}, true)
	if cmd := got[len(got)-1]; cmd != "ls | wc -l" {
		t.Errorf("command = %q", cmd)
	}
}

func TestExecArgvDirect(t *testing.T) {
	c := New(Options{Transport: TransportDirect, Host: "h", User: "u"}, nil)
	got := c.ExecArgv([]string{"true"}, false)
	want := []string{"ssh", "u@h", "true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExecArgv = %v, want %v", got, want)
	}
}

func TestTunnelArgv(t *testing.T) {
	got := iapClient(nil).TunnelArgv(8080, 3000)
	want := []string{
		"gcloud", "compute", "ssh", "vm", "--project", "proj", "--zone", "us-west1-a", "--verbosity=error", "--tunnel-through-iap",
		"--", "-N", "-o", "ExitOnForwardFailure=yes", "-L", "127.0.0.1:8080:127.0.0.1:3000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TunnelArgv = %v, want %v", got, want)
	}
}

func TestExecArgvTTY(t *testing.T) {
	got := iapClient(nil).ExecArgvTTY([]string{"bash"}, false)
	tail := got[len(got)-4:]
	if !reflect.DeepEqual(tail, []string{"--command", "bash", "--", "-t"}) {
		t.Errorf("TTY exec must force ssh -t after --, got tail %v", tail)
	}

	direct := New(Options{Transport: TransportDirect, Host: "h", User: "u"}, nil)
	gotDirect := direct.ExecArgvTTY([]string{"bash"}, false)
	want := []string{"ssh", "-t", "u@h", "bash"}
	if !reflect.DeepEqual(gotDirect, want) {
		t.Errorf("direct TTY exec = %v, want %v", gotDirect, want)
	}
}

const describeJSON = `{
  "id": "12345",
  "name": "vm",
  "status": "RUNNING",
  "zone": "https://www.googleapis.com/compute/v1/projects/proj/zones/us-west1-a",
  "machineType": "https://www.googleapis.com/compute/v1/projects/proj/zones/us-west1-a/machineTypes/e2-standard-4",
  "lastStartTimestamp": "2026-08-12T08:00:00.000-07:00",
  "labels": {"purpose": "dev"},
  "metadata": {"items": [{"key": "enable-oslogin", "value": "TRUE"}]},
  "networkInterfaces": [
    {"networkIP": "10.138.0.2", "accessConfigs": [{"natIP": ""}]}
  ]
}`

func TestDescribeParsesInstance(t *testing.T) {
	fake := &execx.Fake{Rules: []execx.Rule{{
		Match:  execx.Prefix("gcloud", "compute", "instances", "describe"),
		Result: execx.Result{Stdout: []byte(describeJSON)},
	}}}
	inst, err := iapClient(fake).Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != "RUNNING" || inst.MachineType != "e2-standard-4" || inst.Zone != "us-west1-a" {
		t.Errorf("instance = %+v", inst)
	}
	if inst.InternalIP != "10.138.0.2" || inst.ExternalIP != "" {
		t.Errorf("IPs = %q / %q", inst.InternalIP, inst.ExternalIP)
	}
	if inst.Metadata["enable-oslogin"] != "TRUE" {
		t.Errorf("metadata = %v", inst.Metadata)
	}
	call := fake.Calls[0]
	assertContainsPair(t, call, "--project", "proj")
	assertContainsPair(t, call, "--zone", "us-west1-a")
}

func TestDescribeSurfacesGcloudError(t *testing.T) {
	fake := &execx.Fake{Rules: []execx.Rule{{
		Match:  execx.Prefix("gcloud"),
		Result: execx.Result{ExitCode: 1, Stderr: []byte("ERROR: (gcloud.compute.instances.describe) Could not fetch resource:\n - The resource 'vm' was not found")},
	}}}
	_, err := iapClient(fake).Describe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("expected the gcloud stderr summary, got: %v", err)
	}
}

func TestLifecycleCommands(t *testing.T) {
	fake := &execx.Fake{Rules: []execx.Rule{{Match: execx.Prefix("gcloud"), Result: execx.Result{}}}}
	c := iapClient(fake)
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	for i, verb := range []string{"start", "stop"} {
		call := fake.Calls[i]
		if call[3] != verb {
			t.Errorf("call %d verb = %q, want %q", i, call[3], verb)
		}
		if call[len(call)-1] != "--quiet" {
			t.Errorf("lifecycle calls must pass --quiet, got %v", call)
		}
	}
}

func TestCheckSSH(t *testing.T) {
	ok := &execx.Fake{Rules: []execx.Rule{{Match: execx.Prefix("gcloud", "compute", "ssh"), Result: execx.Result{}}}}
	if err := iapClient(ok).CheckSSH(context.Background()); err != nil {
		t.Fatal(err)
	}
	bad := &execx.Fake{Rules: []execx.Rule{{
		Match:  execx.Prefix("gcloud", "compute", "ssh"),
		Result: execx.Result{ExitCode: 255, Stderr: []byte("Connection refused")},
	}}}
	if err := iapClient(bad).CheckSSH(context.Background()); err == nil || !strings.Contains(err.Error(), "Connection refused") {
		t.Fatalf("expected the probe failure detail, got: %v", err)
	}
}

func assertContainsPair(t *testing.T, argv []string, flag, value string) {
	t.Helper()
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == flag && argv[i+1] == value {
			return
		}
	}
	t.Errorf("argv %v missing %s %s", argv, flag, value)
}
