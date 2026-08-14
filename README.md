# bastion

A local, config-driven CLI for operating a personal Linux development box on
Google Compute Engine. Your terminal is the control plane: no hosted service,
no team layer, no daemon on the VM.

> **Status: pre-alpha, milestones A–C implemented.** Attached-VM lifecycle
> (adopt, status, up/down, ssh, exec, port, doctor), host convergence
> (plan/apply: packages, features, managed files), and container services
> (Compose generation, volumes, secrets, private endpoints) are built and
> covered by fake-transport tests — **not yet validated against a real VM**.
> The name `bastion` is a working title. See [SPEC.md](SPEC.md) for the full
> specification and [docs/original-spec.md](docs/original-spec.md) for the
> long-form design rationale.

## What it does

Given a versioned box definition (`bastion.yaml`) describing an existing GCE
VM, Bastion operates it privately by default — IAP + OS Login, no public SSH
port, loopback-only tunnels:

```console
$ bastion up agents           # start the VM, wait for SSH, converge, wait for health
$ bastion plan agents         # read-only diff: what apply would change
$ bastion apply agents        # packages, features, dotfiles, services
$ bastion ssh agents          # interactive session over IAP
$ bastion exec agents -- tmux list-sessions
$ bastion service logs agents dashboard -f
$ bastion port agents dashboard:web   # private endpoint → local loopback
$ bastion down agents         # stop compute; disks and data are retained
```

Host state (apt packages, built-in features like docker/github-cli/mise/
claude-code, local feature scripts, managed dotfiles) and one-container
services (with durable volumes, secrets, and loopback-only private endpoints)
all converge from the definition — idempotently, with a read-only plan first.

## Getting started

Requires Go 1.26+ to build and the [gcloud CLI](https://cloud.google.com/sdk)
authenticated against your project.

```console
$ go build -o bastion ./cmd/bastion

$ ./bastion init ~/boxes/mybox        # scaffold a definition
$ $EDITOR ~/boxes/mybox/bastion.yaml  # point it at your VM
$ ./bastion box adopt mybox --config ~/boxes/mybox
$ ./bastion doctor                    # verify gcloud, auth, access, SSH
$ ./bastion up
```

A complete annotated definition lives in
[examples/agents/bastion.yaml](examples/agents/bastion.yaml). Every command
accepts `--json` for machine-readable output.

## Layout

```
cmd/bastion/        entry point
internal/cli/       command tree
internal/config/    v1alpha1 schema, strict parsing, validation
internal/registry/  box registrations and resolution
internal/provider/  provider-neutral instance model
internal/provider/gcp/  gcloud-wrapping provider (attached mode)
internal/engine/    convergence: inspect/plan/apply, features, Compose generation
internal/doctor/    environment diagnosis
internal/lockfile/  local per-box operation lock
examples/agents/    reference box definition
```

## Development

```console
$ make check        # gofmt + go vet + go test
```

Provider behavior is tested against a fake process runner — no GCP access is
needed to develop or run the test suite.
