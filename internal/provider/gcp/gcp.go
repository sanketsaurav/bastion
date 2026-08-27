// Package gcp operates an attached Google Compute Engine VM by wrapping the
// installed gcloud CLI (SPEC.md Δ5). Every invocation passes explicit
// --project and --zone; Bastion never reads or mutates gcloud's own
// configuration. Attached mode never creates, deletes, or reshapes cloud
// resources (SPEC.md §7.1).
package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sanketsaurav/bastion/internal/config"
	"github.com/sanketsaurav/bastion/internal/execx"
	"github.com/sanketsaurav/bastion/internal/provider"
)

// Transport selects how SSH traffic reaches the box.
type Transport string

const (
	TransportIAP    Transport = "iap"
	TransportDirect Transport = "direct"
)

type Options struct {
	Project  string
	Zone     string
	Instance string

	Transport    Transport
	ForwardAgent bool

	// Direct transport only.
	Host         string
	User         string
	IdentityFile string

	// MuxSocket enables SSH connection multiplexing (SPEC.md Δ14): every
	// connection carries ControlMaster options so the first one leaves a
	// persistent background master behind, and an invocation that finds
	// that master alive skips gcloud entirely and opens a channel on the
	// live tunnel. Empty disables multiplexing. Agent forwarding also
	// disables it: a per-session agent grant must not ride a shared master.
	MuxSocket string
}

// Client drives one attached VM.
type Client struct {
	opt Options
	run execx.Runner

	muxOnce sync.Once
	muxFast bool
}

func New(opt Options, run execx.Runner) *Client { return &Client{opt: opt, run: run} }

// SetMuxSocket enables connection multiplexing on this client. The CLI
// computes the per-box socket path (the state dir plus the box ID);
// providers never invent paths.
func (c *Client) SetMuxSocket(path string) { c.opt.MuxSocket = path }

// FromBox builds a client from a validated, normalized box definition.
func FromBox(b *config.Box, run execx.Runner) *Client {
	return New(Options{
		Project:      b.Provider.Project,
		Zone:         b.Provider.Zone,
		Instance:     b.Provider.Instance,
		Transport:    Transport(b.Connection.Type),
		ForwardAgent: b.Connection.ForwardSSHAgent,
		Host:         b.Connection.Host,
		User:         b.Connection.User,
		IdentityFile: b.Connection.IdentityFile,
	}, run)
}

func (c *Client) instances(verb string, extra ...string) []string {
	argv := []string{
		"gcloud", "compute", "instances", verb, c.opt.Instance,
		"--project", c.opt.Project, "--zone", c.opt.Zone,
	}
	return append(argv, extra...)
}

// Describe fetches the instance's observed state.
func (c *Client) Describe(ctx context.Context) (*provider.Instance, error) {
	res, err := c.run.Run(ctx, c.instances("describe", "--format", "json"))
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("gcloud could not describe instance %q in %s/%s: %s",
			c.opt.Instance, c.opt.Project, c.opt.Zone, stderrSummary(res))
	}
	return parseInstance(res.Stdout)
}

func (c *Client) lifecycle(ctx context.Context, verb string) error {
	res, err := c.run.Run(ctx, c.instances(verb, "--quiet"))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("gcloud failed to %s instance %q: %s", verb, c.opt.Instance, stderrSummary(res))
	}
	if verb == "stop" {
		c.retireMux(ctx)
	}
	return nil
}

// retireMux asks a lingering connection master to exit after the instance
// stopped — the tunnel is dead anyway; this just cleans up promptly.
// Best-effort: a missing master is the common case.
func (c *Client) retireMux(ctx context.Context) {
	if !c.muxEnabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c.run.Run(ctx, []string{"ssh", "-O", "exit", "-o", "ControlPath=" + c.opt.MuxSocket, muxHost})
	os.Remove(c.opt.MuxSocket)
}

func (c *Client) Start(ctx context.Context) error  { return c.lifecycle(ctx, "start") }
func (c *Client) Stop(ctx context.Context) error   { return c.lifecycle(ctx, "stop") }
func (c *Client) Resume(ctx context.Context) error { return c.lifecycle(ctx, "resume") }

// muxPersistSeconds is how long a connection master outlives its last
// session; muxHost is the placeholder ssh target for multiplexed
// invocations — with a live control socket, ssh never resolves it.
const (
	muxPersistSeconds = "600"
	muxHost           = "bastion-mux"
)

func (c *Client) muxEnabled() bool {
	return c.opt.MuxSocket != "" && !c.opt.ForwardAgent
}

// muxOptions create or join the shared master on the ordinary (gcloud or
// direct) path.
func (c *Client) muxOptions() []string {
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + c.opt.MuxSocket,
		"-o", "ControlPersist=" + muxPersistSeconds,
	}
}

// useFast reports whether a live master exists, probing at most once per
// Client (one CLI invocation). A leftover socket with no master behind it is
// removed so the next connection can become the master.
func (c *Client) useFast() bool {
	if !c.muxEnabled() || c.run == nil {
		return false
	}
	c.muxOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		res, err := c.run.Run(ctx, []string{"ssh", "-O", "check", "-o", "ControlPath=" + c.opt.MuxSocket, muxHost})
		if err == nil && res.ExitCode == 0 {
			c.muxFast = true
			return
		}
		if _, statErr := os.Stat(c.opt.MuxSocket); statErr == nil {
			os.Remove(c.opt.MuxSocket)
		}
	})
	return c.muxFast
}

// fastArgv rides the live master with plain ssh: options, placeholder
// target, then the remote command.
func (c *Client) fastArgv(opts []string, command ...string) []string {
	argv := []string{"ssh", "-o", "ControlPath=" + c.opt.MuxSocket}
	argv = append(argv, opts...)
	argv = append(argv, muxHost)
	return append(argv, command...)
}

// sshBase returns the transport-appropriate argv prefix for reaching the box.
// For direct transport the final element is the ssh target.
func (c *Client) sshBase() []string {
	if c.opt.Transport == TransportDirect {
		argv := []string{"ssh"}
		if c.opt.IdentityFile != "" {
			argv = append(argv, "-i", c.opt.IdentityFile)
		}
		if c.opt.ForwardAgent {
			argv = append(argv, "-A")
		}
		target := c.opt.Host
		if c.opt.User != "" {
			target = c.opt.User + "@" + c.opt.Host
		}
		return append(argv, target)
	}
	return []string{
		"gcloud", "compute", "ssh", c.opt.Instance,
		"--project", c.opt.Project, "--zone", c.opt.Zone,
		// error verbosity silences gcloud's per-invocation IAP performance
		// warning (NumPy) without touching the ssh session's own stderr.
		"--verbosity=error",
		"--tunnel-through-iap",
	}
}

// withSSHArgs appends options destined for the underlying ssh binary: after
// "--" for gcloud, before the target for plain ssh. With a live mux master
// the whole invocation collapses to plain ssh on the control socket.
func (c *Client) withSSHArgs(sshArgs []string) []string {
	if c.useFast() {
		return c.fastArgv(sshArgs)
	}
	if c.muxEnabled() {
		sshArgs = append(c.muxOptions(), sshArgs...)
	}
	base := c.sshBase()
	if len(sshArgs) == 0 {
		return base
	}
	if c.opt.Transport == TransportDirect {
		target := base[len(base)-1]
		out := append(base[:len(base)-1:len(base)-1], sshArgs...)
		return append(out, target)
	}
	out := append(base, "--")
	return append(out, sshArgs...)
}

// SSHArgv builds an interactive SSH invocation; extra args are forwarded to
// the underlying ssh binary.
func (c *Client) SSHArgv(extra []string) []string {
	var sshArgs []string
	if c.opt.Transport == TransportIAP && c.opt.ForwardAgent {
		sshArgs = append(sshArgs, "-A")
	}
	sshArgs = append(sshArgs, extra...)
	return c.withSSHArgs(sshArgs)
}

// ExecArgv forwards remote argv verbatim: each argument is strictly quoted so
// the remote shell cannot reinterpret it (SPEC.md §11). shell=true joins the
// words unquoted — the caller's explicit opt-in to shell evaluation.
func (c *Client) ExecArgv(remote []string, shell bool) []string {
	cmd := QuoteJoin(remote)
	if shell {
		cmd = strings.Join(remote, " ")
	}
	if c.useFast() {
		return c.fastArgv(nil, cmd)
	}
	if c.opt.Transport == TransportDirect {
		return append(c.withSSHArgs(nil), cmd)
	}
	argv := append(c.sshBase(), "--command", cmd)
	if c.muxEnabled() {
		argv = append(argv, "--")
		argv = append(argv, c.muxOptions()...)
	}
	return argv
}

// ExecArgvTTY is ExecArgv with a forced pseudo-terminal on the remote side —
// needed for interactive container sessions (`service exec --tty`).
func (c *Client) ExecArgvTTY(remote []string, shell bool) []string {
	cmd := QuoteJoin(remote)
	if shell {
		cmd = strings.Join(remote, " ")
	}
	if c.useFast() {
		return c.fastArgv([]string{"-t"}, cmd)
	}
	if c.opt.Transport == TransportDirect {
		return append(c.withSSHArgs([]string{"-t"}), cmd)
	}
	argv := append(c.sshBase(), "--command", cmd, "--", "-t")
	if c.muxEnabled() {
		argv = append(argv, c.muxOptions()...)
	}
	return argv
}

// TunnelArgv builds a loopback-to-loopback ssh -L forward. Private endpoints
// bind the VM's loopback, which IAP TCP forwarding cannot reach — only ssh -L
// over the SSH session can (SPEC.md Δ3).
func (c *Client) TunnelArgv(localPort, remotePort int) []string {
	forward := fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", localPort, remotePort)
	return c.withSSHArgs([]string{"-N", "-o", "ExitOnForwardFailure=yes", "-L", forward})
}

// CheckSSH probes remote reachability with a trivial command.
func (c *Client) CheckSSH(ctx context.Context) error {
	res, err := c.run.Run(ctx, c.ExecArgv([]string{"true"}, false))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("ssh probe failed (exit %d): %s", res.ExitCode, stderrSummary(res))
	}
	return nil
}

// Runner exposes the underlying executor for callers composing custom flows
// (doctor, wait loops).
func (c *Client) Runner() execx.Runner { return c.run }

// RunScript pipes a generated bash program to the box over SSH (`bash -s`
// reads it from stdin, so script content never appears in argv or ps) and
// streams stdout lines to onLine. This is the remote-runner transport
// (SPEC.md Δ9).
func (c *Client) RunScript(ctx context.Context, script []byte, onLine func(string)) (execx.Result, error) {
	return c.run.RunStream(ctx, c.ExecArgv([]string{"bash", "-s"}, false), script, onLine)
}

func stderrSummary(res execx.Result) string {
	lines := strings.Split(strings.TrimSpace(string(res.Stderr)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			if len(line) > 300 {
				line = line[:300] + "…"
			}
			return line
		}
	}
	return "no error output"
}

func parseInstance(data []byte) (*provider.Instance, error) {
	var raw struct {
		ID                 string            `json:"id"`
		Name               string            `json:"name"`
		Status             string            `json:"status"`
		Zone               string            `json:"zone"`
		MachineType        string            `json:"machineType"`
		LastStartTimestamp string            `json:"lastStartTimestamp"`
		LastStopTimestamp  string            `json:"lastStopTimestamp"`
		Labels             map[string]string `json:"labels"`
		Metadata           struct {
			Items []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"items"`
		} `json:"metadata"`
		NetworkInterfaces []struct {
			NetworkIP     string `json:"networkIP"`
			AccessConfigs []struct {
				NatIP string `json:"natIP"`
			} `json:"accessConfigs"`
		} `json:"networkInterfaces"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing gcloud instance description: %w", err)
	}
	inst := &provider.Instance{
		Name:        raw.Name,
		ID:          raw.ID,
		Zone:        pathBase(raw.Zone),
		MachineType: pathBase(raw.MachineType),
		Status:      raw.Status,
		Labels:      raw.Labels,
		Metadata:    map[string]string{},
		LastStart:   raw.LastStartTimestamp,
		LastStop:    raw.LastStopTimestamp,
	}
	for _, item := range raw.Metadata.Items {
		inst.Metadata[item.Key] = item.Value
	}
	for _, nic := range raw.NetworkInterfaces {
		if inst.InternalIP == "" {
			inst.InternalIP = nic.NetworkIP
		}
		for _, ac := range nic.AccessConfigs {
			if inst.ExternalIP == "" && ac.NatIP != "" {
				inst.ExternalIP = ac.NatIP
			}
		}
	}
	return inst, nil
}

func pathBase(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
