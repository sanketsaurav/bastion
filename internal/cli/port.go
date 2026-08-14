package cli

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func (a *App) portCmd() *cobra.Command {
	var localPort int
	cmd := &cobra.Command{
		Use:   "port [box] <service>:<endpoint> | <remote-port>",
		Short: "Forward a private endpoint (or raw VM port) to local loopback over SSH",
		Long: "Forward a private endpoint or a raw port on the box's loopback interface\n" +
			"to a local loopback port through the SSH connection (IAP by default).\n" +
			"No firewall is ever touched for private access.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			boxArgs, target := args[:len(args)-1], args[len(args)-1]
			res, err := a.resolveBox(boxArgs)
			if err != nil {
				return err
			}
			var remotePort int
			var protocol string
			if svcName, epName, isEndpoint := strings.Cut(target, ":"); isEndpoint {
				svc, ok := res.Loaded.Box.Services[svcName]
				if !ok {
					return fmt.Errorf("service %q is not declared in this box", svcName)
				}
				ep, ok := svc.Endpoints[epName]
				if !ok {
					return fmt.Errorf("service %q has no endpoint %q", svcName, epName)
				}
				if ep.Visibility != "private" {
					return fmt.Errorf("endpoint %s:%s is %q; only private endpoints publish on the VM loopback (set visibility: private)",
						svcName, epName, ep.Visibility)
				}
				remotePort = ep.EffectiveVMPort()
				protocol = ep.Protocol
			} else {
				remotePort, err = strconv.Atoi(target)
				if err != nil || remotePort < 1 || remotePort > 65535 {
					return fmt.Errorf("%q is neither a valid port nor a service:endpoint target", target)
				}
			}
			client := a.clientFor(res)
			ctx := cmd.Context()

			inst, err := client.Describe(ctx)
			if err != nil {
				return err
			}
			if !inst.Running() {
				return fmt.Errorf("box %q is not running (status %s); run `bastion up %s` first",
					res.Loaded.Box.Metadata.Name, inst.Status, res.Loaded.Box.Metadata.Name)
			}

			local := localPort
			if local == 0 {
				if local, err = freeLoopbackPort(); err != nil {
					return err
				}
			}

			u := a.ui()
			address := fmt.Sprintf("127.0.0.1:%d", local)
			if protocol == "http" {
				address = "http://" + address
			}
			if u.json {
				if err := u.emit(map[string]any{"localPort": local, "remotePort": remotePort, "address": address}); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(u.out, address)
			}
			u.progressf("Forwarding %s → %s:%d — Ctrl-C to stop.", address, res.Loaded.Box.Metadata.Name, remotePort)

			argv := client.TunnelArgv(local, remotePort)
			u.debugf("+ %s", strings.Join(argv, " "))
			started := time.Now()
			code, err := a.runner.Interactive(ctx, argv)
			if err != nil {
				return err
			}
			// The tunnel blocks until interrupted; an immediate exit means it
			// never established (busy local port, auth failure, …).
			if code != 0 && time.Since(started) < 3*time.Second {
				return fmt.Errorf("tunnel exited immediately (code %d); is local port %d free?", code, local)
			}
			u.progressf("Tunnel closed.")
			return nil
		},
	}
	cmd.Flags().IntVar(&localPort, "local-port", 0, "local port to bind (default: an available port)")
	return cmd
}

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("finding a free local port: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
