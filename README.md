# bastion

A config-driven CLI for running a personal dev box on Google Compute Engine —
and for hosting your side projects on it. Your terminal is the control plane:
no hosted service, no daemon on the VM, nothing between you and your machine
but `gcloud` and SSH.

You describe the box in one versioned file — system packages, dev tools,
dotfiles, long-running services, public domains — and bastion converges the
VM to it. Idempotently, with a read-only plan first, over a private-by-default
connection.

```console
$ bastion up dev              # start the VM, wait for SSH, converge, wait for health
$ bastion ssh dev             # interactive shell over IAP — no public SSH port
$ bastion plan dev            # what would apply change? (read-only)
$ bastion apply dev           # packages, tools, dotfiles, services
$ bastion port dev metrics:web        # private endpoint → localhost tunnel
$ bastion service logs dev blog -f    # tail a service
$ bastion down dev            # stop compute; disks and data stay
```

## Install

```console
$ brew install sanketsaurav/tap/bastion
```

Or `go install github.com/sanketsaurav/bastion/cmd/bastion@latest`, or grab a
signed archive from the
[releases page](https://github.com/sanketsaurav/bastion/releases)
(verification instructions in [SECURITY.md](SECURITY.md)).

## What you need

- The [gcloud CLI](https://cloud.google.com/sdk), authenticated against your
  project.
- A GCE VM running **Ubuntu 24.04 LTS** with IAP + OS Login access. bastion
  *attaches* to a VM you own — it never creates, resizes, or deletes cloud
  resources, so your VM can come from the console, `gcloud`, or Terraform.

Not sure your setup qualifies? `bastion doctor` checks everything — gcloud,
IAM, connectivity, the guest OS, sudo, egress, Docker — and every failure
comes with the exact fix.

## Quickstart

```console
$ bastion init ~/boxes/dev           # scaffold a definition
$ $EDITOR ~/boxes/dev/bastion.yaml   # point it at your VM
$ bastion box adopt dev --config ~/boxes/dev
$ bastion doctor dev                 # verify everything before touching anything
$ bastion up dev                     # start → reachable → converged → healthy
$ bastion ssh dev
```

## The box definition

Everything bastion manages comes from `bastion.yaml` — there is no imperative
command that creates hidden state. A working example:

```yaml
apiVersion: bastion/v1alpha1
kind: Box

metadata:
  name: dev

provider:
  name: gcp
  project: my-project
  zone: us-central1-a
  instance: my-devbox

host:
  packages: [git, jq, tmux]
  features:
    - uses: docker
    - uses: github-cli
    - uses: claude-code
    - uses: mise
  files:
    - source: files/tmux.conf
      target: ~/.tmux.conf
      mode: replace
  shell:
    prompt: alice          # PS1 shows alice@dev, not ext_alice_gmail_com@dev
    motd: quiet            # no Ubuntu login wall of text (or ads)

ingress:
  baseDomain: apps.example.com   # one wildcard DNS record serves every app

volumes:
  blog-data: { persistence: durable }

secrets:
  blog-admin-token:
    source: { file: ~/.secrets/dev/blog-admin-token }

services:
  blog:
    image: ghcr.io/example/blog:1.4.2
    environment:
      ADMIN_TOKEN: { secretRef: blog-admin-token }
    mounts:
      - { volume: blog-data, target: /data }
    endpoints:
      web:
        containerPort: 8000
        visibility: public   # → https://blog.apps.example.com, cert included
        auth: none           # explicit: the app owns authentication

  metrics:
    image: ghcr.io/example/metrics:2.1.0
    endpoints:
      web:
        containerPort: 3000
        visibility: private  # loopback-only; reached via `bastion port`
```

`bastion plan` shows exactly what `apply` would do before it does it. Applies
are idempotent (a second run is a no-op), resumable after interruption, and
**additive**: removing a package or tool from the definition stops managing
it but never uninstalls it — plans report such orphans, and
`bastion feature remove` cleans up user-level tools when you ask (your
configuration and credentials are always kept).

Built-in features: `docker`, `github-cli`, `tmux`, `build-essential`,
`mise`, `uv`, `bun`, `claude-code`, `codex` — plus your own local feature
scripts. `bastion config schema` prints the full JSON Schema.

## Hosting apps publicly

Any HTTP service can get a real domain with automatic HTTPS:

1. Declare `ingress.baseDomain` and mark an endpoint `visibility: public`
   with an explicit `auth` policy — the policy is your acknowledgement that
   the endpoint faces the internet.
2. One-time setup, guided and verified by `bastion doctor`: a static IP for
   the VM, a wildcard DNS record (`*.apps.example.com → <IP>`), and an open
   80/443 firewall path. bastion never touches DNS, IPs, or firewall rules
   itself.
3. `bastion apply`. The service is live at `https://<service>.<baseDomain>`
   with a real certificate — bastion runs a managed Caddy proxy that routes
   by hostname and issues per-host certificates automatically.

After the one-time setup, hosting the next app is purely a yaml change. Use
`hostname:` on an endpoint for custom domains, and `bastion endpoint list`
to see every URL. Your services never bind a public port themselves — only
the hardened proxy faces the internet.

## Secrets

Secrets come from local files or environment variables on *your* machine,
resolve only at apply time, and land in root-owned env files on the box.
They never appear in the definition, plans, logs, generated configs, or
digests — which also means changing a value is invisible to a normal plan:

```console
$ bastion apply dev --rotate-secrets   # re-resolve values, replace the containers using them
```

## Data

Durable volumes live on the box's data root and survive `down`/`up` and
container replacement. Deleting data is never a side effect: removing a
service orphans its volume (reported in plans), and `bastion volume delete`
requires a confirmation naming the volume.

## How it works

bastion talks to GCP exclusively through your installed `gcloud` (your auth,
your IAP tunnels) and to the guest over SSH. For each plan or apply it
generates a bash program, pipes it to the VM, and reads back a structured
event stream — nothing is installed or left running on the box, and every
mutation is recorded in per-resource marker files so partial failures resume
cleanly. Remote commands are strictly quoted; nothing user-supplied is ever
interpreted by a shell unless you pass `--shell`.

What bastion will never do: create or destroy cloud resources, modify
firewall rules or IAM, uninstall things because you undeclared them, bind
your services to `0.0.0.0`, or put a secret in a log. The full contract is
in [SPEC.md](SPEC.md).

Every command accepts `--json` for machine-readable output.

## Status

Early software (v0.x): the configuration API is `bastion/v1alpha1` and may
change between minor versions until 1.0. Current scope: attached mode (your
VM), Ubuntu 24.04 guests, Docker runtime, GCP. The
[specification](SPEC.md) is decision-complete and documents what's deferred.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Development needs no GCP access —
the whole suite runs against fakes.

## License

[MIT](LICENSE)
