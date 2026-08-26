# Bastion — specification

- Status: Active (supersedes `docs/original-spec.md`, draft 0.1)
- Specification version: 0.3
- Configuration API: `bastion/v1alpha1`
- Last updated: 2026-08-26

Bastion is a local, config-driven CLI for operating a personal Linux development
box on Google Compute Engine. The CLI on your computer is the control plane:
there is no hosted service, no team layer, and no resident daemon on the VM.

This document is decision-complete for version 1. Draft 0.1 remains in
`docs/original-spec.md` as the long-form rationale and as the design reference
for deferred capabilities (managed infrastructure, public ingress). Where the
two documents disagree, this one wins.

## 1. Product contract

Given a box definition:

```text
bastion up agents      # box exists → running → reachable → configured → services healthy
bastion down agents    # stop compute; never deletes workspace or service data
bastion apply agents   # converge a running box toward declared state; always re-runnable
bastion plan agents    # read-only: show what apply would do
```

Everything else in this spec serves that contract.

## 2. Decisions

Resolutions to draft 0.1's open questions, plus deliberate deltas from its text.

| # | Question | Decision |
|---|----------|----------|
| D1 | Public name | **`bastion`, final** (decided 2026-08-26). The project needs no domain: the apiVersion group is a bare `bastion/` (the config is read only by this CLI, so a DNS-style namespace buys nothing — skaffold precedent). Docs live under sanketsaurav.com/bastion, which also serves the published JSON Schema (`$id` points there). |
| D2 | Initial scope | **Attached mode only.** Version 1 = lifecycle + host convergence + container services against an existing VM. Managed infrastructure (VM/disk/firewall creation) is deferred entirely; existing Terraform keeps owning infra. |
| D3 | Guest support | Ubuntu 24.04 LTS (`amd64`/`arm64`) only. Detected and enforced before any host mutation. |
| D4 | Runtime | Docker Engine + Compose v2 only. The schema speaks OCI; runtime parity with Podman is not assumed or attempted. |
| D5 | Local features | Built-in features ship first (milestone B); the local feature contract ships in the same milestone, after built-ins work. The contract is designed now so built-ins are implemented against it. |
| D6 | Public auth | **Revised 2026-08-26 (milestone D, §9.8):** public HTTP endpoints ship in attached mode via a managed Caddy proxy; the firewall/IP/DNS work stays the user's, verified by `doctor`. The required `auth` field is the internet-facing acknowledgement — `auth: none` declares the app owns authentication; `basic` is reserved for a follow-up. Public TCP passthrough stays out of scope. |
| D7 | Secrets | Environment-variable and local-file sources only. GCP Secret Manager and keychains deferred. |
| D8 | Compose stacks | Deferred. One service = one container. |
| D9 | Idle shutdown | Deferred with managed mode. When it lands it will be an opt-in systemd timer on the VM — a deliberate, documented exception to "no resident daemon". |
| D10 | Config filename | `bastion.yaml`. |

Deltas from draft 0.1:

| # | Delta | Rationale |
|---|-------|-----------|
| Δ1 | Default `restartPolicy` is **`unless-stopped`**, not `no`. | On a personal box, a VM reboot outside Bastion should bring services back. `no` is Docker's default, but wrong ergonomics here. |
| Δ2 | Remote runner uses **exact-version match**, not an independently versioned protocol. | The CLI uploads its own version's runner (checksum-verified) and refuses to drive any other. A compatibility matrix buys nothing pre-1.0. The structured stdin/stdout event protocol stays. |
| Δ3 | Private endpoint forwarding is **`ssh -L` over IAP-tunneled SSH**, never `start-iap-tunnel` to the application port. | IAP TCP forwarding connects to the VM's NIC and cannot reach loopback-bound ports. Container ports publish on `127.0.0.1` on the VM; only `ssh -L` reaches them. |
| Δ4 | Local paths follow **XDG on all platforms, including macOS** (`~/.config/bastion`, `~/.local/state/bastion`, `~/.cache/bastion`). | Dev CLI convention; `~/Library/Application Support` adds friction for a tool whose config users edit by hand. `bastion config paths` reports the resolved locations. `XDG_*` env vars are honored. |
| Δ5 | GCP operations go through the installed **`gcloud` CLI exclusively** in v1. | Reuses user auth, OS Login key handling, IAP, and battle-tested errors. Cost: ~1–2 s latency per call. A native compute API for describe/start/stop is a performance follow-up, not an architecture change. |
| Δ6 | `claude-code` joins the built-in feature candidates. | The primary use case is an agents box. |
| Δ7 | Managed-file `template` mode renders **locally at plan time** with a non-secret context (`.Box`, `.Provider`, `.Workspace`) and strict missing-key errors; the rendered content then flows through the identical replace pipeline. | Rendering locally means template failures surface in `plan`, digests cover rendered output, and the remote side never learns templating exists. Secrets are structurally absent from the context. (`replace` shipped first; `template` followed once proven.) |
| Δ8 | Schema fields for deferred capabilities (`mode: managed`, `visibility: public`, `mode: template`, `stacks`) are **defined in the schema and rejected by validation** with an error naming the milestone. | Misspellings must be errors (strict parsing), but planned fields shouldn't look like typos. Reserved-and-rejected keeps both properties. |
| Δ9 | The remote runner is a **bash program generated per request** by this exact CLI version and piped to `bash -s` over SSH stdin — no runner binary is shipped, uploaded, or persisted. | Cross-compiling and embedding linux binaries in a macOS CLI is distribution pain with no v1 payoff. A generated program is version-matched by construction, leaves nothing on the VM, keeps every decision on the testable CLI side, and needs ~2–3 SSH round trips per plan/apply. Payloads (file contents, compose files, secret env files, feature tarballs) travel base64 inside the script — never argv, never `ps`-visible. |
| Δ10 | Remote state is a **directory of per-resource marker files** (`/var/lib/bastion/state/<box>/{files,features,lfeatures,services}/…`), each written atomically by the step that completes it — not one JSON manifest. | Per-action atomic state updates from within a single script run, with no JSON merging in shell. A mid-apply failure leaves exactly the markers of what completed. |
| Δ11 | Private endpoints publish on an explicit **`vmPort`** (default: `containerPort`); effective VM ports must be unique across the box. | Deterministic from configuration alone — no port-allocation state to persist, recover, or drift. |
| Δ12 | The box ID is **`metadata.name`** in attached mode. | Human-legible container labels, paths, and network names; uniqueness is already enforced by the registry. Managed mode can revisit with a generated suffix if needed. |
| Δ13 | `dependsOn` ordering (and health gating) is enforced by the **apply engine**, not Compose. | Each service is an independent Compose project; cross-project `depends_on` does not exist. The engine sequences deploys and inserts health waits between dependencies. |

## 3. Scope

### 3.1 Version 1 goals

1. Manage multiple named boxes from one local install.
2. Attach to an existing GCE VM; operate lifecycle (start/stop/status) without owning its infrastructure.
3. SSH, remote exec, port forwarding, diagnostics — all via IAP + OS Login by default.
4. Idempotent host convergence: system packages, built-in and local features, managed files.
5. Long-running services from OCI images via generated per-service Compose projects.
6. Durable service data on a user-provided data disk path that survives VM stop/replace.
7. Private-by-default networking: endpoints are container-internal unless declared `private` or routed through the managed ingress proxy (§9.8); user services never bind `0.0.0.0`.
8. Read-only planning before every consequential mutation; destructive actions separately confirmed.
9. Machine-readable (`--json`) output alongside human output.

### 3.2 Version 1 non-goals

Everything in draft 0.1 §3.2, plus: managed GCP resource creation, public
HTTPS ingress, Cloud DNS, static IPs, snapshot schedules, and idle shutdown —
all deferred with their milestones (see §14).

### 3.3 Host versus project responsibility

Bastion manages the shared host: Docker, Git, tmux, tool managers, coding-agent
CLIs, shell integration. Repositories keep owning their language versions,
dependencies, and dev databases. Interactive agents launched from SSH/tmux are
not Bastion services; long-running gateways and dashboards may be.

## 4. Concepts

- **Box** — a named desired environment plus its VM. **Box definition** — a
  versioned `bastion.yaml` with its `files/`, `features/`, `scripts/`.
- **Attached box** — an existing VM registered with Bastion. Bastion operates
  lifecycle and configures the guest; it never creates, deletes, or reshapes
  cloud resources for it.
- **Host feature** — an idempotent, versioned capability installed on the host.
- **Service** — one long-running container from one OCI image.
- **Endpoint** — a named container port plus its access policy.
- **Durable volume** — a service data directory under the box's data root.
- **Remote runner** — an ephemeral, version-matched Bastion binary invoked over
  SSH for inspect/apply. Exits after each request; never listens on anything.

## 5. Configuration

### 5.1 Files and locations

| Path | Contents |
|------|----------|
| `<config-dir>/config.yaml` | Client config: registrations and preferences. Never box desired state. |
| `<config-dir>/boxes/<name>/` | Default home for box definitions (any registered absolute path works, including inside a Git repo). |
| `<state-dir>/` | Local operational state: cached observations, locks. A cache and index — always recoverable from the VM and remote state. |
| `<cache-dir>/` | Downloaded artifacts, runner binaries. |

`<config-dir>` = `$XDG_CONFIG_HOME/bastion` or `~/.config/bastion`;
`<state-dir>` = `$XDG_STATE_HOME/bastion` or `~/.local/state/bastion`;
`<cache-dir>` = `$XDG_CACHE_HOME/bastion` or `~/.cache/bastion` (Δ4).
Local operational state MUST NOT be written into a box directory.

Client config:

```yaml
apiVersion: bastion/v1alpha1
kind: ClientConfig
currentBox: agents
boxes:
  agents: /Users/alice/boxes/agents
output:
  color: auto
```

### 5.2 Box resolution

Given an optional box name argument, the definition is resolved as:

1. `--config <path>` (file or directory). If a name was also given, it must
   match `metadata.name`.
2. `$BASTION_CONFIG`, same rules.
3. Name given: candidates are `./bastion.yaml` (if its `metadata.name` matches)
   and the registry entry for that name. Two distinct candidates → error
   listing both. One → use it. None → error listing registered boxes.
4. No name: `./bastion.yaml` if present.
5. No name: `currentBox` from client config.
6. Otherwise: error with remediation (`bastion init`, `bastion box adopt`).

Ambiguity is always an error. Bastion never silently chooses between two
definitions with the same name.

### 5.3 Parsing rules

- Every document declares `apiVersion: bastion/v1alpha1` and its `kind`.
- Unknown fields are errors, with line numbers. No silent tolerance of typos.
- Reserved-but-deferred values fail validation with an error naming the
  deferring milestone (Δ8) — distinguishable from typos.
- `bastion config schema` prints the JSON Schema for `kind: Box`.
- v1alpha1 makes no compatibility promises; stable versions will migrate
  explicitly and never rewrite a definition without confirmation.

### 5.4 Box schema

The complete v1 surface. Fields marked *(B)* / *(C)* are validated today and
acted on from that milestone (§14).

```yaml
apiVersion: bastion/v1alpha1
kind: Box

metadata:
  name: agents                      # required; DNS label
  labels: { purpose: personal-development }

provider:
  name: gcp                         # gcp only
  mode: attached                    # attached only; `managed` reserved
  project: example-project          # required
  zone: us-west1-a                  # required
  instance: agents-devbox           # required

connection:
  type: iap                         # iap (default) | direct
  osLogin: true                     # default true
  forwardSshAgent: false            # default false; also per-invocation flag
  # direct only:
  # host: dev.example.com
  # user: alice
  # identityFile: ~/.ssh/id_ed25519

runtime:                            # (C)
  engine: docker                    # docker only
  logRotation: { maxSize: 10MiB, maxFiles: 3 }

workspace:                          # (B)
  mount: /workspace                 # default
  dataRoot: /mnt/bastion            # default; must already be a mounted path in attached mode

host:                               # (B)
  packages: [git, jq, tmux]         # apt, idempotent; removal is never automatic
  features:
    - uses: docker                  # built-in
    - uses: ./features/personal-tools   # local, relative to box dir
      with: { channel: stable }
  files:
    - source: files/tmux.conf       # relative to box dir; must exist
      target: ~/.tmux.conf
      mode: replace                 # replace only; `template` reserved (Δ7)
      permissions: "0600"
  shell:                            # (B)
    prompt: alice                   # PS1 shows alice@<host> in place of the
                                    # OS Login username; cosmetic only

ingress:                            # (D) enables public endpoints (§9.8)
  baseDomain: apps.example.com      # default hostname: <service>.<baseDomain>
  acmeEmail: alice@example.com      # optional; CA expiry notices

volumes:                            # (C)
  dashboard-data:
    persistence: durable            # durable | ephemeral

secrets:                            # (C)
  dashboard-api-token:
    source:
      environment: DASHBOARD_API_TOKEN   # exactly one of environment | file
      # file: ~/.secrets/dashboard-token

services:                           # (C)
  dashboard:
    image: ghcr.io/example/dashboard:1.4.2   # required; mutable tags warn
    pullPolicy: if-not-present      # if-not-present (default) | always | never
    restartPolicy: unless-stopped   # default (Δ1); no | on-failure | unless-stopped | always
    enabled: true                   # default
    platform: linux/amd64           # optional
    entrypoint: ["/app/dashboard"]  # arrays only; no implicit shell
    args: ["serve"]
    workingDir: /app
    user: "1000:1000"
    environment:
      LOG_LEVEL: info               # literal string
      API_TOKEN: { secretRef: dashboard-api-token }
    mounts:
      - volume: dashboard-data      # exactly one of volume | source
        target: /app/data
      - source: /workspace/dashboard/config   # absolute bind mount; shown in plans
        target: /app/config
        readOnly: true
    resources: { cpus: 1, memory: 1GiB }      # overcommit allowed; plan warns on sum > VM
    healthcheck:
      command: ["/app/dashboard", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
      startPeriod: 10s
    dependsOn: [database]           # ordering only; cycles are errors
    endpoints:
      web:
        containerPort: 3000
        protocol: http              # http (default) | tcp
        visibility: private         # internal (default) | private | public (§9.8)
        # vmPort: 43000             # private only; VM loopback port (default: containerPort, unique per box) (Δ11)
        # public only:
        # auth: none                # required; the internet-facing acknowledgement. `basic` reserved
        # hostname: app.example.com # overrides <service>.<baseDomain>; required with >1 public endpoint per service
```

Semantic validation (enforced by `bastion validate` and before every plan):
names are DNS labels and unique; images are present; mounts reference declared
volumes or absolute sources, never both; secret references resolve; dependency
graphs are acyclic; durations/sizes/permissions parse; managed-file sources
exist inside the box directory; feature `uses` is a known built-in or a `./`
path inside the box directory; reserved values produce milestone errors.

## 6. CLI

### 6.1 Global behavior

```text
--config <path>   Select a box definition directly
--box <name>      Select a registered box (equivalent to the positional name)
--json            Structured output on stdout; human diagnostics on stderr
--no-color        Disable color (NO_COLOR honored)
--yes             Approve non-data-destructive prompts
--verbose         Diagnostic detail
```

Exit codes: `0` success, `1` failure, unless documented otherwise
(`plan --detailed-exitcode` returns `2` for "valid, changes proposed").
`exec` and `ssh` propagate the remote exit code.

### 6.2 Commands

Milestone A — lifecycle:

```text
bastion init [path]                    Scaffold a box definition; never overwrites
bastion validate [box|path]            Schema + semantic validation; no GCP contact
bastion config paths                   Resolved config/state/cache locations
bastion config schema                  JSON Schema for kind: Box
bastion box adopt <name> --config <p>  Register a definition by absolute path
bastion box list | use <name> | forget <name>
bastion status [box]                   Provider + instance state
bastion up [box]                       Start if stopped; wait for SSH readiness
bastion down [box]                     Stop compute; data is retained
bastion ssh [box] [-- ssh-args]        Interactive SSH (IAP by default)
bastion exec [box] -- <cmd> [args...]  Argv forwarded verbatim; --shell opts into shell
bastion port [box] <remote-port> [--local-port N]   Loopback ssh -L forward
bastion doctor [box]                   Environment and connectivity diagnosis
```

Milestone B — convergence: `plan`, `apply`, and `up` gains its apply step.
Milestone C — services: `service list|status|logs|start|stop|restart|exec|update`,
`endpoint list`, `volume delete`, and `port` learns `<service>:<endpoint>` targets.
Post-C: `feature remove [box] <feature>` — explicit removal of a user-level
built-in's installed payload (§8.3; declared and apt-based features are
refused; configuration and credentials are kept).

Notes:

- `box forget` removes only the local registration; the VM is untouched.
- `exec` builds a remote command by strictly quoting each argument — remote
  shells never interpret user argv unless `--shell` is passed.
- `port` binds `127.0.0.1` locally, picks a free local port unless told,
  prints the address, and stays attached until interrupted.
- An imperative `service stop` is operational state; the next `up`/`apply`
  restores declared state. Configuration changes only ever come from the box
  definition — there is no imperative command that creates hidden drift.

## 7. GCP provider (attached)

### 7.1 Authority boundary

Bastion MAY: inspect, start, stop, and resume the instance; connect over
configured connectivity; converge the guest; operate Bastion-owned containers,
files, and directories.

Bastion MUST NOT: delete or recreate the instance; resize, attach, or replace
disks; modify network interfaces, tags, or firewall rules; modify the service
account or instance IAM; claim ownership of anything by name. `bastion destroy`
does not exist for attached boxes. Instance metadata is read-only in v1 — if
OS Login isn't enabled, `doctor` says so and tells the user how to fix it;
Bastion doesn't flip it.

### 7.2 gcloud invocation rules (Δ5)

- Every invocation passes explicit `--project` and `--zone`; Bastion never
  reads or mutates gcloud global configuration.
- Machine-readable calls use `--format=json`.
- Lifecycle: `compute instances describe|start|stop|resume`.
- SSH/exec: `compute ssh` with `--tunnel-through-iap` when `type: iap`.
- Forwarding: `compute ssh -- -N -L 127.0.0.1:<local>:127.0.0.1:<remote>` (Δ3).
- `type: direct` uses the system `ssh` with the configured host, user, and
  identity file; same argv-quoting rules.

### 7.3 Readiness and privilege

`up` waits for `RUNNING`, then polls a trivial remote command until SSH answers
or a deadline passes (default 180 s). Convergence requires non-interactive
`sudo` (`sudo -n`) for privileged steps; when it's missing, the failure names
the step and the remediation instead of hanging on a hidden prompt. Features
that don't need root run without it. SSH agent forwarding defaults off and
requires explicit config or a flag.

## 8. Host convergence (milestone B)

### 8.1 Remote runner (Δ2, Δ9)

The runner is a bash program generated per request by the CLI and piped to
`bash -s` over the SSH connection:

- the **inspect** program is strictly read-only and emits observed facts as a
  `@f …` line protocol (base64 for opaque values);
- the **apply** program executes the approved plan's steps in order, emitting
  `@e <step> start|ok|fail` events and `@l` log lines; steps run under strict
  shell semantics (`set -e -o pipefail`), so a marker is written only after
  every command in its step succeeded, and the first failure stops the run
  after its event, leaving earlier successes and markers in place;
- nothing is persisted on the VM and nothing ever listens on a socket;
- `sudo -n` is used only by steps that declare root; a missing sudo fails with
  remediation, never an interactive prompt;
- all writes are atomic (temp + rename); all payloads (file contents, compose
  projects, secret env files, feature tarballs) travel base64 via stdin —
  never argv, never visible in `ps`.

Version matching is inherent: the program *is* this CLI version. There is no
protocol negotiation and nothing to upgrade on the box. A remote mkdir-based
apply lock (stale after an hour, recoverable by `rmdir` alone) guards
concurrent applies from different machines; the runner releases it on any
signal death the guest can observe, not only on clean exit.

Interruption is advisory, not transactional: cancelling the CLI signals the
whole transport process group, but over IAP the relay can hold the backend
leg open, so the runner may finish outstanding work or die late. Safety
comes from marker-last writes, the self-releasing lock, and lock refusals
that report the holder's age and their own recovery.

### 8.2 Packages

`host.packages` installs from apt, idempotently and non-interactively;
every apt invocation waits out dpkg locks held by cloud-init or
unattended-upgrades instead of failing a freshly booted box. Removing a name
stops requiring the package; it never uninstalls. Unsupported guests are rejected
before any mutation (D3).

### 8.3 Built-in features

Candidates: `docker`, `github-cli`, `tmux`, `mise`, `uv`, `bun`, `claude-code`,
`codex`, `build-essential`. Every built-in: declares supported platforms;
validates its options; has a read-only check; applies idempotently; reports
changed/unchanged; pins versions where upstream allows; separates user-level
from root-level steps; records its applied version and options digest in remote
state. Installer downloads use HTTPS and verify checksums/signatures when
upstream publishes them. The Docker feature owns daemon config, bounded JSON
log rotation, and reports that docker-group membership is effectively root.

Undeclaring a feature never uninstalls it (packages likewise, §8.2). Instead
every plan reports leftover feature markers as orphan notes — installed by
Bastion, no longer declared. Cleanup is explicit: `bastion feature remove
<box> <feature>` deletes exactly what the installer wrote for user-level
built-ins (binaries, versioned installs, caches, and the state marker) and
always keeps user configuration and credentials (`~/.claude`, `~/.codex`,
`~/.config/mise`, …). Apt-based built-ins are refused with the manual apt
command — their packages may be shared, so removal is not Bastion's call —
and a still-declared feature is refused outright, since the next apply would
reinstall it. The guaranteed-clean path remains rebuilding the VM: the box
is disposable, the data root is not.

Built-ins declare the apt prerequisites their installers need (`bun` →
`unzip`). Apply installs a missing prerequisite and records a per-package
marker under the owning feature — one already on the box is never claimed.
`feature remove` uninstalls exactly the recorded ones, keeping any that
other packages now depend on (reported, never cascade-removed), and plans
report prerequisites whose owning feature is gone.

### 8.4 Local features

```text
features/personal-tools/
├── feature.yaml    # name, version, supported OS, privilege needs, input schema
├── check           # read-only
└── apply           # safe to re-run after partial failure
```

Inputs arrive as a JSON file path, never interpolated into a shell line.
Output uses the runner event protocol. Local features are trusted code and
plans label them as locally supplied executables.

### 8.5 Managed files

`replace` mode copies atomically with declared permissions. Plans report
create/content/permission changes and deletions (deletion only when explicitly
configured). First replacement of an unmanaged file keeps one recoverable
backup unless opted out. Shell integration is exactly one delimited source line
loading `~/.config/bastion/shell.sh` — Bastion never edits arbitrary regions of
shell startup files. `host.shell.prompt` is the first use: it puts a chosen
name into PS1 in place of the login username (OS Login derives unreadable
`ext_…` names for external accounts, and the identity itself is directory
data Bastion cannot rename). The integration file is a generated managed
file flowing through the standard replace pipeline — digests, plans, and
markers included — and the source line is appended once, delimited, at the
end of `~/.bashrc` so it wins over the distribution's own PS1. Cosmetic
only: authentication, `whoami`, and home directory are unchanged. `template` mode (Δ7) renders the source locally at plan
time via Go text/template with the non-secret context `.Box.{Name,Labels}`,
`.Provider.{Project,Zone,Instance}`, `.Workspace.{Mount,DataRoot}`; unknown
keys are plan-time errors and secret values are never reachable.

### 8.6 Remote state (Δ10)

`/var/lib/bastion/state/<box-id>/` holds one small JSON marker per managed
resource — `files/<hash>.json` (target, digest, mode, backup), `features/` and
`lfeatures/` (version + options/source digests), `services/` (config digest,
image). Each marker is written atomically as the final act of the step that
completed it. Markers are evidence of what Bastion applied, not proof of no
drift — planning always combines them with live checks (package state, file
digests, container labels). Recovery never requires deleting broad state
directories.

## 9. Services (milestone C)

### 9.1 Model

One service, one container (D8). Each service becomes an independently
generated Compose project at
`/var/lib/bastion/services/<box-id>/<service>/compose.yaml`, project name
`bastion-<box-id>-<service>`. Containers carry labels: box ID, service name,
config digest, image reference, generating Bastion version. All services join
one Bastion-managed bridge network; service names are DNS names on it. The
Docker socket is never mounted into a user service. The user-facing model stays
independent of Compose syntax.

### 9.2 Images

Pull policies `if-not-present` (default) / `always` / `never`. Mutable or
missing tags warn; digest pins are the reproducibility path. Registry refresh
requires `--refresh-images`; nothing auto-updates. `service update` resolves a
newer digest for a mutable tag, shows it, requires confirmation, and offers to
write the pin back into configuration instead of leaving drift.

### 9.3 Volumes and data

`durable` volumes are bind mounts at `<data-root>/volumes/<name>`; `ephemeral`
volumes are runtime-managed and may die with the VM. Workspace lives at
`<data-root>/workspace`, exposed as `/workspace`. Docker's own image/build
cache stays off the durable disk by default. Stopping or replacing compute
never deletes durable data; removing a volume declaration orphans the directory
until an explicit `volume delete <box> <volume>`, which requires a confirmation
naming the volume — `--yes` or `--force` alone is never sufficient.

### 9.4 Secrets

Sources: local environment variable or local file (D7). Invariants — secret
values never appear in: box configuration, local state, plans, logs, generated
Compose content, command-line arguments, or recoverable digests. A direct
consequence: value changes are invisible to plans, so rotation is explicit —
`apply --rotate-secrets` rewrites every secret env file from freshly resolved
values and force-recreates the referencing containers (`plan
--rotate-secrets` previews it). Nothing rotates implicitly. On the box
they live under a Bastion-owned directory with restrictive permissions;
generated Compose references those paths (`env_file`, file mounts). File mounts
are preferred over env vars where the app allows; terminal output is redacted.

### 9.5 Endpoints

`internal` (default): reachable only on the box network. `private`: the
container port publishes on the VM's loopback only, at `vmPort` (default
`containerPort`; effective ports unique per box, Δ11); `bastion port <box>
<service>:<endpoint>` opens an `ssh -L` forward to it (Δ3) and prints the local
URL. No GCP firewall is ever touched for a private endpoint; nothing publishes
on `0.0.0.0` except the managed ingress proxy (§9.8). `public` routes through
that proxy — user services never publish public ports themselves.

### 9.6 Isolation defaults

User services never get: privileged mode, host network/PID/IPC, the Docker
socket, device mounts, or public port bindings. `no-new-privileges` is set when
compatible; running as root without an explicit `user` warns. Future unsafe
overrides will be per-service, prominent in plans, and never a global default.

### 9.7 Reconciliation

Apply replaces containers whose generated definition changed (no zero-downtime
requirement). Removing a service from config: the plan shows stop + removal;
durable volumes are retained and reported orphaned; ephemeral volumes are
deleted only after confirmation. Containers without Bastion labels are never
touched. `up` starts declared services and waits for health checks up to a
deadline; an unhealthy service fails `up` without rolling back unrelated
successes.

### 9.8 Public endpoints and ingress (milestone D)

A top-level `ingress` block with a `baseDomain` enables `visibility: public`
on HTTP endpoints. One wildcard DNS record (`*.<baseDomain>` → the VM's
static IP) covers every derived hostname, so hosting another app is a pure
configuration change: a service with exactly one public endpoint is served
at `<service>.<baseDomain>`; `hostname` overrides it (and is required when a
service declares several public endpoints). Effective hostnames are unique
box-wide.

Bastion manages one Caddy container (pinned image, generated Caddyfile and
Compose project, digest-labeled like any service): it binds 80/443 (and
443/udp for HTTP/3), joins the box network, routes by hostname to
`<service>:<containerPort>`, redirects HTTP to HTTPS, and issues per-host
certificates automatically via ACME — which is why no DNS-provider API is
needed. Certificate state lives at `<dataRoot>/ingress` and survives proxy
removal, so re-enabling ingress never re-issues certificates. The proxy gets
no Docker socket and `admin off`.

`auth` is required on every public endpoint and is the internet-facing
acknowledgement: `auth: none` states, in versioned configuration, that the
application owns authentication. `basic` is reserved for a follow-up. Public
TCP passthrough is out of scope (D6): `protocol: http` only.

Bastion still never touches DNS, IPs, or firewall rules. `doctor` checks the
external IP is reserved (static), the wildcard and any custom hostnames
resolve to it (calling out proxied/orange-cloud records, which break
certificate issuance), and that 80/443 are reachable — distinguishing
firewall drops from nothing-listening-yet. Per-host records instead of a
wildcard are legitimate: when every declared hostname resolves, the missing
wildcard downgrades to advice. Removing the last public endpoint
plans a destructive proxy removal; private services never become public as a
side effect of anything.

## 10. Plan and apply (milestone B)

`plan` is strictly read-only: parse and validate → inspect provider state → if
running and reachable, inspect the guest (features, files, containers,
volumes) → print an ordered action plan. A stopped box is never started by
`plan`; guest-level results are reported as unknown (`up --plan-only` exists
for "start, inspect, don't apply"). `--detailed-exitcode`: `0` no changes, `2`
changes proposed, `1` error.

`apply` requires a running, reachable box, executes in dependency order,
revalidates preconditions immediately before each consequential action,
emits an elapsed-time heartbeat while a long action runs, and is
resumable but not transactional: each success updates remote state atomically;
a failure leaves earlier successes in place and prints the exact resume
command. Individual resources (a file, a container, a generated config) change
atomically. Destructive actions are visually distinct and confirmed unless
`--yes` — except durable-data deletion, which always requires its named
confirmation.

Drift taxonomy: **managed drift** (declared, differs, reconcilable),
**orphaned** (Bastion-owned, no longer declared — reported, removed only via
plan), **unmanaged** (reported when relevant, never changed).

Concurrency: a local per-box lock plus a remote apply lock. Reads run
concurrently; conflicting mutations fail fast with owner and age. Stale locks
are recoverable without deleting state directories.

## 11. Security invariants

- IAP + OS Login by default; no listening daemon on the VM; agent forwarding
  off by default.
- The box definition and the VM are trusted; Bastion limits accidental
  exposure but a compromised VM owns whatever you forwarded to it.
- Remote argv is strictly quoted; no shell interpretation without `--shell`.
- No recursive deletion of broad or unresolved paths, ever.
- Secrets: see §9.4 invariants.
- Containers: see §9.6 defaults.
- Attached cloud resources are never destroyed (§7.1).
- Release artifacts ship with checksums and keyless signatures (verification in SECURITY.md).

## 12. Testing

- **Unit**: config parsing and unknown-field rejection; semantic validation;
  resolution precedence and ambiguity; ownership decisions; gcloud/ssh argv
  construction and quoting (injection resistance); redaction; deletion guards;
  state migrations.
- **Golden**: generated Compose projects; plans (human and `--json`); doctor
  reports; event streams.
- **Integration**: fake provider + fake SSH executor for CLI flows; real
  Docker tests for the service engine (create/replace/logs/health/remove).
  Real-GCP tests are manual in v1 (developer-run against a personal
  project); automated GCP fixtures are deferred.
- **Compatibility**: macOS/Linux clients on amd64/arm64; config and runner
  version-match behavior across one prior version before any stable release.

## 13. Diagnostics

`bastion doctor` checks: gcloud presence and version; active account; project
access; instance existence and state; SSH/IAP reachability; OS Login
configuration; guest OS support; guest internet egress (probed against a non-Google host, so Private Google Access cannot mask a missing NAT); non-interactive sudo; data-root mount and free
space; Docker/Compose health; runner version match; service health; and known
unsafe configuration (agent forwarding, mutable tags). Failures come with
remediation text. Every apply gets an operation ID; `--json` emits versioned
events (operation, resource, action, status, duration, redacted message).

## 14. Milestones

**A — attached lifecycle** *(implemented)*: schema + strict loader + JSON
Schema; registry, resolution, adopt/list/use/forget; init/validate/config
paths; status/up/down; ssh/exec/port; doctor; `--json`; fake-provider tests;
CI (fmt, vet, test, cross-compile).
*Exit:* daily lifecycle driving of a real attached VM needs no other tool;
a second `validate`/`status` run is stable; all argv construction is tested.

**B — host convergence** *(implemented; validated end-to-end on real GCE VMs)*:
generated-runner inspect/apply + event protocol (Δ9); plan/apply engine with
local flock + remote mkdir locks; packages; built-ins (docker, github-cli,
tmux, build-essential via apt; mise, uv, bun, claude-code via official HTTPS
installers; codex via GitHub releases); local feature contract
(feature.yaml + check/apply, JSON inputs, deterministic source digests);
managed files (replace, first-touch backups); marker state (Δ10);
resume-after-partial-failure.
*Exit:* clean Ubuntu 24.04 VM converges to the example config; second apply is
a no-op; an interrupted apply resumes safely. *(All proven live.)*

**C — services** *(implemented; validated end-to-end on real GCE VMs)*: deterministic
Compose generation with ownership labels and isolation defaults; engine-side
dependency ordering + health gating (Δ13); service
list/status/logs/start/stop/restart/exec/update; durable + ephemeral volumes;
secrets (resolve-at-apply, env files, never in digests/logs/argv);
internal/private endpoints with `vmPort` (Δ11); `port service:endpoint`;
orphan pruning that retains durable data; `volume delete` with typed
confirmation.
*Exit:* two services run, talk over the box network, persist declared data
across a VM stop/start; a removed service is pruned without touching its
durable volume; unlabeled containers are never touched. *(All proven live.)*

**D — public ingress** *(implemented; validated live on a real domain; §9.8)*: `ingress.baseDomain`
+ `visibility: public` + required `auth` acknowledgement; generated
Caddyfile/Compose with digest-labeled proxy container; per-host ACME (no
DNS-provider API); hostname derivation and box-wide uniqueness; destructive
proxy removal retaining certificate state; doctor checks for static IP,
wildcard/custom DNS (flagging proxied records), and 80/443 reachability;
`endpoint list` URLs.
*Exit:* a real application serves HTTPS on a subdomain of a user-configured
base domain with a wildcard record created once; a second app needs only
configuration; removing the last public endpoint removes the proxy and keeps
certificates.

**Deferred** (design intact in `docs/original-spec.md`): managed GCP
infrastructure; ingress basic auth; Cloudflare Tunnel as an alternate
ingress provider; Cloud DNS record management; public TCP passthrough;
snapshot schedules; idle shutdown; Compose stacks; Podman; other clouds; GCP
Secret Manager; Windows client; IDE helpers; feature marketplace.

## 15. Version 1 acceptance

A new user can: install one binary; authenticate with documented gcloud
prerequisites; adopt a box from a checked-in definition; see a plan before any
change; start, stop, SSH, exec; apply twice with the second run a no-op; run an
OCI service and reach it privately with no public port; survive a partial apply
with clear recovery; re-register a machine from its remote state after losing
local state.
