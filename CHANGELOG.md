# Changelog

## v0.3.1 - 2026-08-31

### Fixes

- **Deploys are stability-probed.** `compose up` accepts a container that
  starts and immediately dies, so a service crash-looping on bad
  configuration used to deploy with a green check and surface later as a
  502. Every service and ingress deploy now watches the container for its
  first seconds; one that exits or restarts fails the apply step with the
  container's recent logs right in the error, and the step replans on the
  next apply. Declared healthchecks still gate full readiness afterwards.
- **Doctor's ingress DNS checks query public resolvers** (1.1.1.1, then
  8.8.8.8; falling back to the system resolver when public DNS is
  unreachable). The system resolver negative-caches lookups made before a
  record existed, which kept a hostname check red long after the record
  was fixed — and public DNS is the vantage that certificate authorities
  and visitors actually see.

## v0.3.0 - 2026-08-27

### Features

- **`bastion audit`** — hardening checks with none of the compliance
  theater: a short list of high-value findings, each mapping to a real
  compromise path, each with its exact, copy-pasteable fix. Cloud-side it
  flags attached service accounts (anything on the box can mint their
  tokens through the metadata server), firewall rules open to the world
  beyond your declared surface (world-open SSH gets the scope-to-IAP fix
  rather than deletion), and disabled Secure Boot; guest-side it verifies
  security updates apply themselves, no update is stuck waiting on a
  reboot, password authentication is off, and nothing listens on
  `0.0.0.0` that you didn't declare. Read-only, nonzero exit on findings,
  and provider checks run even against a stopped box. Provider-specific
  checks sit behind an auditor interface, ready for hosts beyond GCP.
- **`host.hardening.autoReboot: "HH:MM"`** — lets unattended-upgrades
  reboot in a nightly window when a security update requires it, closing
  the "kernel patch downloaded but inactive" gap audit most often finds. A
  bastion box is uniquely safe to reboot: services restart themselves,
  data is durable, and the IP should be static.
- **Animated progress** — in-progress steps (starting, waiting for SSH,
  inspecting, every apply action, doctor and audit runs) now spin in
  place on a terminal, show elapsed time when they drag, and resolve to a
  check or cross. Pipes, `--json`, and `--verbose` keep the plain
  append-only output.

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
