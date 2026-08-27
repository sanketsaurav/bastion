package gcp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sanketsaurav/bastion/internal/execx"
)

const testSock = "/tmp/bastion-test-mux.sock"

func muxClient(t *testing.T, masterAlive bool) *Client {
	t.Helper()
	code := 0
	if !masterAlive {
		code = 255
	}
	run := &execx.Fake{Rules: []execx.Rule{{
		Match:  execx.Prefix("ssh", "-O", "check"),
		Result: execx.Result{ExitCode: code},
	}}}
	return New(Options{
		Project: "proj", Zone: "us-west1-a", Instance: "vm",
		Transport: TransportIAP, MuxSocket: testSock,
	}, run)
}

// Without a live master, invocations stay on gcloud but carry the
// ControlMaster options so the connection they open becomes the master.
func TestMuxSlowPathAddsControlOptions(t *testing.T) {
	got := muxClient(t, false).SSHArgv(nil)
	wantTail := []string{"--",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + testSock,
		"-o", "ControlPersist=600",
	}
	if !reflect.DeepEqual(got[len(got)-len(wantTail):], wantTail) {
		t.Errorf("slow-path SSHArgv tail = %v", got)
	}
	if got[0] != "gcloud" {
		t.Errorf("slow path must still go through gcloud, got %v", got[0])
	}

	exec := muxClient(t, false).ExecArgv([]string{"true"}, false)
	if !reflect.DeepEqual(exec[len(exec)-len(wantTail):], wantTail) {
		t.Errorf("slow-path ExecArgv must carry mux options after --, got %v", exec)
	}
}

// With a live master, every invocation collapses to plain ssh on the
// control socket — no gcloud, no tunnel setup.
func TestMuxFastPathSkipsGcloud(t *testing.T) {
	c := muxClient(t, true)
	if got, want := c.SSHArgv(nil), []string{"ssh", "-o", "ControlPath=" + testSock, muxHost}; !reflect.DeepEqual(got, want) {
		t.Errorf("fast SSHArgv = %v, want %v", got, want)
	}
	if got, want := c.ExecArgv([]string{"bash", "-s"}, false), []string{"ssh", "-o", "ControlPath=" + testSock, muxHost, "bash -s"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fast ExecArgv = %v, want %v", got, want)
	}
	if got, want := c.ExecArgvTTY([]string{"top"}, false), []string{"ssh", "-o", "ControlPath=" + testSock, "-t", muxHost, "top"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fast ExecArgvTTY = %v, want %v", got, want)
	}
	tun := c.TunnelArgv(8080, 3000)
	want := []string{"ssh", "-o", "ControlPath=" + testSock,
		"-N", "-o", "ExitOnForwardFailure=yes", "-L", "127.0.0.1:8080:127.0.0.1:3000", muxHost}
	if !reflect.DeepEqual(tun, want) {
		t.Errorf("fast TunnelArgv = %v, want %v", tun, want)
	}
}

// The aliveness probe runs once per client, not once per argv build.
func TestMuxProbesOnce(t *testing.T) {
	c := muxClient(t, true)
	c.SSHArgv(nil)
	c.ExecArgv([]string{"true"}, false)
	c.TunnelArgv(1, 2)
	fake := c.run.(*execx.Fake)
	if len(fake.Calls) != 1 {
		t.Errorf("expected exactly one -O check probe, got %d calls: %v", len(fake.Calls), fake.Calls)
	}
}

// Agent forwarding grants are per-session and must never ride a shared
// master: multiplexing turns itself off entirely.
func TestMuxDisabledWithAgentForwarding(t *testing.T) {
	// The zero-value Fake rejects every command, so any -O check probe
	// would fail this test loudly.
	c := New(Options{
		Project: "p", Zone: "z", Instance: "i", Transport: TransportIAP,
		ForwardAgent: true, MuxSocket: testSock,
	}, &execx.Fake{})
	got := c.SSHArgv(nil)
	for _, arg := range got {
		if arg == "ControlMaster=auto" {
			t.Errorf("agent forwarding must disable multiplexing, got %v", got)
		}
	}
}

// A leftover socket with no master behind it is removed so the next
// connection can bind it as the new master.
func TestMuxRemovesStaleSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "stale.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	run := &execx.Fake{Rules: []execx.Rule{{
		Match:  execx.Prefix("ssh", "-O", "check"),
		Result: execx.Result{ExitCode: 255},
	}}}
	c := New(Options{Project: "p", Zone: "z", Instance: "i", Transport: TransportIAP, MuxSocket: sock}, run)
	c.SSHArgv(nil)
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("stale socket must be removed, stat err = %v", err)
	}
}
