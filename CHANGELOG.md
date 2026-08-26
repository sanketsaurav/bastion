# Changelog

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
