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
	"strings"

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
}

// Client drives one attached VM.
type Client struct {
	opt Options
	run execx.Runner
}

func New(opt Options, run execx.Runner) *Client { return &Client{opt: opt, run: run} }

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
	return nil
}

func (c *Client) Start(ctx context.Context) error  { return c.lifecycle(ctx, "start") }
func (c *Client) Stop(ctx context.Context) error   { return c.lifecycle(ctx, "stop") }
func (c *Client) Resume(ctx context.Context) error { return c.lifecycle(ctx, "resume") }

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
// "--" for gcloud, before the target for plain ssh.
func (c *Client) withSSHArgs(sshArgs []string) []string {
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
	if c.opt.Transport == TransportDirect {
		return append(c.sshBase(), cmd)
	}
	return append(c.sshBase(), "--command", cmd)
}

// ExecArgvTTY is ExecArgv with a forced pseudo-terminal on the remote side —
// needed for interactive container sessions (`service exec --tty`).
func (c *Client) ExecArgvTTY(remote []string, shell bool) []string {
	cmd := QuoteJoin(remote)
	if shell {
		cmd = strings.Join(remote, " ")
	}
	if c.opt.Transport == TransportDirect {
		base := c.sshBase()
		target := base[len(base)-1]
		out := append(base[:len(base)-1:len(base)-1], "-t", target)
		return append(out, cmd)
	}
	return append(c.sshBase(), "--command", cmd, "--", "-t")
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
