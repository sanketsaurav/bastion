# Changelog

## v0.2.0 - 2026-08-27

### Features

- **Connection multiplexing.** bastion reuses one SSH connection across
  commands via an OpenSSH control master with a 10-minute idle window: the
  first connection pays the IAP tunnel setup once, and everything after —
  `ssh`, `exec`, `port`, the round trips inside every `plan` and `apply` —
  rides it near-instantly (measured against a real box: 4.7 s cold,
  0.15 s multiplexed). On by default; `connection.multiplex: false` opts
  out, and agent forwarding disables it automatically.
- **`bastion ssh-config`** generates (and with `--install` manages) a
  marker-delimited `~/.ssh/config` Host block, so any standard SSH tool —
  IDE remote workspaces, scp, rsync — reaches the box by name over the
  same IAP transport and shares the multiplexed connection with bastion.
  `--remove` deletes the block; nothing outside the markers is touched.
- **A login worth looking at.** `bastion ssh` opens with a nameplate: the
  box name in block art with a stable per-box accent color, the project
  and zone, and the box's public app URLs. Disable with
  `host.shell.banner: off` or `--no-banner`; it stays out of the way for
  scripted and argument-passing invocations.
- **`host.shell.motd: quiet`** silences the distribution's login output —
  system information, news, update notices, and sshd's last-login line —
  with a managed `~/.hushlogin`.
- **`host.shell.userAlias: true`** makes `whoami`, PS1, and file listings
  show your prompt name instead of the OS Login-derived `ext_…` username:
  a same-uid passwd alias with its own sudo grant. Login, authentication,
  and your home directory are unchanged.

### Upgrade notes

- Multiplexing is on by default and keeps per-box control sockets under
  `~/.local/state/bastion/mux/`; `bastion down` retires the box's master.
  Set `connection.multiplex: false` for the old one-connection-per-command
  behavior.

### Internal

- Release notes are now sourced from CHANGELOG.md (v0.1.0 shipped with an
  empty notes body), and the README was rewritten for users of bastion.

## v0.1.0 - 2026-08-26

First release. bastion is a local, config-driven CLI for operating a
personal Linux dev box on Google Compute Engine: your terminal is the
control plane — no hosted service, no daemon on the VM.

### Highlights

- **Attached-VM lifecycle** over IAP + OS Login, private by default:
  `adopt`, `status`, `up`/`down`, `ssh`, `exec`, `port`, and a `doctor`
  that diagnoses gcloud, IAM, connectivity, and the guest itself — every
  failure with concrete remediation.
- **Declarative host convergence**: apt packages, built-in features
  (docker, github-cli, tmux, build-essential, mise, uv, bun, claude-code,
  codex), your own local feature scripts, managed dotfiles with template
  support, and a readable shell prompt over OS Login's derived usernames.
  Read-only `plan` before every `apply`; applies are idempotent and
  resumable, and never uninstall on undeclare — orphans are reported, and
  `feature remove` cleans up user-level features (configuration and
  credentials are always kept).
- **Services**: one-container services from OCI images as generated
  Compose projects — durable volumes that survive `down`/`up`, secrets
  from local files or environment variables (rotated explicitly with
  `apply --rotate-secrets`, never present in plans, logs, or digests),
  health gating, and loopback-only private endpoints reached through
  `bastion port`.
- **Public HTTPS ingress**: declare `ingress.baseDomain`, point one
  wildcard DNS record at the VM, and any endpoint marked
  `visibility: public` (with an explicit `auth` policy) is served at
  `https://<service>.<baseDomain>` through a managed Caddy proxy with
  automatic per-host certificates. bastion never touches DNS, IPs, or
  firewall rules — `doctor` verifies them and prints exactly what to
  create.
- **Signed releases**: archives ship with a cosign-signed checksum file
  (verification instructions in SECURITY.md), installable via
  `brew install sanketsaurav/tap/bastion`.

Requires the gcloud CLI and an existing Ubuntu 24.04 GCE VM. Full
specification in SPEC.md.
