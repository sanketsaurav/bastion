# Bastion product and technical specification

- Status: Draft for review
- Specification version: 0.1
- Configuration API: `bastion.dev/v1alpha1`
Last updated: 2026-08-11

## 1. Purpose

Bastion is a local, config-driven CLI for defining and operating a personal
development box in the cloud.

The primary use case is a durable Linux development machine that a user can:

- start when needed and stop when idle;
- reach securely over SSH;
- use for interactive development and cloud coding agents;
- reproduce from a version-controlled definition;
- equip with a small host toolchain;
- use to run long-lived services from OCI container images; and
- optionally expose selected HTTP services publicly over HTTPS.

Bastion is intentionally smaller than a cloud development environment platform.
It has no hosted control plane, team administration layer, or required resident
daemon. The CLI on the user's computer is the control plane.

### 1.1 Product contract

Given a box definition, this command:

```text
bastion up agents
```

MUST ensure that the selected VM exists when Bastion manages its infrastructure,
is running, is reachable, is configured as declared, and has its declared
container services running.

This command:

```text
bastion down agents
```

MUST stop compute without deleting persistent workspace or service data.

This command:

```text
bastion apply agents
```

MUST converge a running box toward the declared host and service configuration.
It MUST be safe to run repeatedly.

## 2. Normative language

The terms MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY describe requirements for
the eventual stable product. Requirements assigned to a later milestone are not
required in earlier releases.

## 3. Scope

### 3.1 Goals

Bastion MUST:

1. Manage multiple named development boxes from one local installation.
2. Support Google Compute Engine as its first provider.
3. Attach to an existing VM without requiring Bastion to own its infrastructure.
4. Eventually create and replace a narrowly defined set of GCP resources.
5. Provide lifecycle, SSH, command execution, diagnostics, and port forwarding.
6. Apply idempotent host features and managed files.
7. Run user-defined services as OCI-image-based containers.
8. Keep container services private unless the user explicitly exposes them.
9. Preserve declared durable data when a VM is stopped, replaced, or destroyed.
10. Show proposed changes before performing consequential mutations.
11. Work without a hosted service or always-running Bastion agent.
12. Offer machine-readable output for automation.

### 3.2 Non-goals for version 1

Bastion will not initially provide:

- team accounts, role-based access control, or organization policy;
- a web dashboard or hosted control plane;
- Kubernetes orchestration;
- feature parity across multiple cloud providers;
- coding-agent task scheduling or agent-loop orchestration;
- a replacement for project-level devcontainers, Nix, `mise`, or repository setup;
- a general-purpose configuration-management language;
- zero-downtime deployment or service autoscaling;
- a general-purpose PaaS;
- arbitrary public TCP/UDP exposure;
- a third-party remote feature marketplace; or
- backup guarantees beyond explicitly configured cloud snapshots.

### 3.3 Host versus project responsibility

Bastion manages the shared development host. Appropriate host configuration
includes Docker, Git, tmux, tool managers, coding-agent clients, shell integration,
and common diagnostics.

Individual repositories SHOULD continue to own their language versions,
dependencies, databases used only during development, and test setup. Bastion MAY
install a tool manager, but it SHOULD NOT become the authoritative dependency
manifest for every repository on the box.

Interactive coding agents launched from SSH or tmux are not Bastion services.
Long-running agent gateways, dashboards, webhook handlers, and similar workloads
MAY be declared as container services.

## 4. Design principles

### 4.1 Desired state over imperative setup

The box definition is the desired state. Bastion SHOULD expose imperative
commands for lifecycle and diagnostics, but configuration changes MUST originate
from the box definition rather than hidden CLI mutations.

For example, public exposure is added by editing an endpoint in configuration and
running `bastion plan` and `bastion apply`; there is no persistent imperative
`bastion expose` operation that creates configuration drift.

### 4.2 Private by default

The default managed GCP box MUST have no public SSH port and SHOULD have no
external IP. The default connection method is Identity-Aware Proxy (IAP) with OS
Login.

Container endpoints MUST be internal unless their configuration declares private
forwarding or public ingress. Bastion MUST NOT publish arbitrary container ports
on `0.0.0.0`.

### 4.3 Replaceable compute, durable data

The VM and its boot disk are replaceable. Workspace data and service data declared
as durable live on a separately managed persistent disk. Bastion MUST distinguish
between stopping compute, destroying compute, and deleting data.

### 4.4 GCP first

The first implementation supports GCP only. Internal boundaries SHOULD avoid
embedding GCP logic in configuration parsing and reconciliation, but the project
MUST NOT delay a useful release to build unused provider abstractions.

### 4.5 Transparent operations

Bastion MUST have a read-only planning mode. Plans SHOULD name the cloud resource,
remote file, feature, container, volume, endpoint, or firewall rule that would
change. Destructive operations MUST be visually distinct and require explicit
confirmation unless `--yes` is supplied.

### 4.6 Safe escape hatches

Users MAY provide local features or scripts because host customization cannot be
fully predicted. Such code is trusted code and MUST be clearly identified in a
plan. Bastion MUST NOT silently download and execute unpinned remote features.

## 5. Terminology

**Box:** A named desired development environment and its associated cloud
resource.

**Box definition:** A versioned `bastion.yaml` plus its local files, features,
and scripts.

**Provider:** The cloud-specific implementation used to inspect and operate the
VM.

**Attached box:** An existing VM registered with Bastion. Bastion can operate its
lifecycle and configure the guest, but does not create or delete its cloud
infrastructure.

**Managed box:** A VM and associated resources created and reconciled by Bastion.

**Host feature:** An idempotent capability installed directly on the Linux host.

**Service:** A long-running user workload defined by one OCI container image.

**Stack:** A future escape hatch for a multi-container application supplied as a
Compose file. Stacks are not part of the first service milestone.

**Endpoint:** A named container port and the policy through which it is accessed.

**Durable volume:** A service data directory stored on the persistent Bastion
data disk.

**Remote runner:** An ephemeral invocation of Bastion code on the VM during
inspection or apply. It does not listen on a network port and is not a daemon.

## 6. User experience

### 6.1 Primary workflow

```text
# Register an existing VM.
bastion box adopt agents --config ~/boxes/agents

# Review and start it.
bastion status agents
bastion up agents

# Connect or run a command.
bastion ssh agents
bastion exec agents -- tmux list-sessions

# Review configuration edits before applying them.
bastion plan agents
bastion apply agents

# Access a private container endpoint.
bastion port agents dashboard:web

# Stop compute while retaining data.
bastion down agents
```

### 6.2 Configuration resolution

Bastion resolves a box definition in the following order:

1. The path passed through `--config`.
2. The path in `BASTION_CONFIG`.
3. A `bastion.yaml` in the current directory.
4. The named box in the local box registry.
5. The current box selected in global configuration.

If resolution is ambiguous, Bastion MUST fail and show the candidates. It MUST
NOT silently choose between two definitions with the same name.

### 6.3 Local directory layout

The default Unix layout is:

```text
~/.config/bastion/
├── config.yaml
└── boxes/
    └── agents/
        ├── bastion.yaml
        ├── files/
        ├── features/
        └── scripts/
```

On macOS and Windows, Bastion SHOULD use the platform user-config directory. The
CLI MUST report the resolved paths through `bastion config paths`.

A box directory MAY live in any Git repository and be registered by absolute
path. Local operational state MUST NOT be written into a version-controlled box
directory.

### 6.4 Global configuration

Global configuration contains client preferences and box registrations, not box
desired state:

```yaml
apiVersion: bastion.dev/v1alpha1
kind: ClientConfig

currentBox: agents

boxes:
  agents: /Users/alice/boxes/agents

output:
  color: auto
```

## 7. Box configuration

### 7.1 Format and compatibility

The canonical format is YAML. Every configuration MUST include `apiVersion` and
`kind`. Bastion MUST publish a JSON Schema for supported configuration versions.

Unknown fields MUST be errors during `v1alpha1`. Silently ignoring misspelled
fields is unsafe. A future stable release MAY introduce an explicit extensions
namespace.

Bastion MUST provide automatic configuration migration between supported stable
versions or print an actionable manual migration. It MUST NOT rewrite a box
definition without confirmation.

### 7.2 Complete illustrative definition

This example is illustrative; a field is not necessarily required in the first
implementation milestone.

```yaml
apiVersion: bastion.dev/v1alpha1
kind: Box

metadata:
  name: agents
  labels:
    purpose: personal-development

provider:
  name: gcp
  mode: attached
  project: example-project
  zone: us-west1-a
  instance: agents-devbox

connection:
  type: iap
  osLogin: true
  forwardSshAgent: false

runtime:
  engine: docker
  logRotation:
    maxSize: 10MiB
    maxFiles: 3

workspace:
  mount: /workspace
  dataRoot: /mnt/bastion

host:
  packages:
    - git
    - jq
    - tmux

  features:
    - uses: docker
    - uses: github-cli
    - uses: mise
    - uses: codex
    - uses: ./features/personal-tools
      with:
        channel: stable

  files:
    - source: files/tmux.conf
      target: ~/.tmux.conf
      mode: replace
      permissions: "0600"

    - source: files/bastion-shell.sh
      target: ~/.config/bastion/shell.sh
      mode: replace
      permissions: "0644"

volumes:
  dashboard-data:
    persistence: durable

secrets:
  dashboard-api-token:
    source:
      environment: DASHBOARD_API_TOKEN

services:
  dashboard:
    image: ghcr.io/example/dashboard:1.4.2
    pullPolicy: if-not-present
    restartPolicy: unless-stopped

    environment:
      LOG_LEVEL: info
      API_TOKEN:
        secretRef: dashboard-api-token

    mounts:
      - volume: dashboard-data
        target: /app/data

    resources:
      cpus: 1
      memory: 1GiB

    healthcheck:
      command: ["/app/dashboard", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3

    endpoints:
      web:
        containerPort: 3000
        protocol: http
        visibility: private
```

## 8. Provider and compute semantics

### 8.1 Attached mode

Attached mode is the first supported mode. The `project`, `zone`, and `instance`
fields identify an existing GCE VM.

In attached mode, Bastion MAY:

- inspect, start, and stop the instance;
- connect through configured GCP connectivity;
- inspect and modify the guest according to the box definition; and
- operate Bastion-owned containers and files.

In attached mode, Bastion MUST NOT:

- delete or recreate the instance;
- resize or replace disks;
- modify its network interfaces;
- modify its service account; or
- assume ownership of cloud resources merely because their names match.

`bastion destroy` is unavailable for attached boxes.

### 8.2 Managed mode

Managed mode is a later milestone. Bastion manages a deliberately narrow resource
set:

- one GCE VM and boot disk;
- one optional durable data disk;
- one instance-specific network tag;
- IAP ingress firewall policy scoped to that tag;
- one optional snapshot schedule;
- one optional static external IP when public endpoints require it;
- HTTP/HTTPS ingress scoped to that tag when public endpoints exist; and
- DNS records in a named existing Cloud DNS zone when configured.

Bastion MUST NOT create a GCP project or take ownership of an existing root DNS
zone. Initial managed mode SHOULD require an existing network and subnet. Support
for creating a dedicated VPC can be considered separately.

All managed cloud resources MUST carry a stable Bastion box ID and ownership
labels. Resource discovery MUST be possible from those labels if local operational
state is lost.

Bastion MAY format a data disk only when the provider confirms that it was newly
created for the current box and live inspection confirms that it has no filesystem.
An existing, attached, adopted, or unexpectedly formatted disk MUST never be
reformatted automatically. Mounting and formatting decisions MUST appear in the
plan.

Managed configuration will extend `provider` with a `compute` section:

```yaml
provider:
  name: gcp
  mode: managed
  project: example-project
  zone: us-west1-a
  instance: agents-devbox

  compute:
    machineType: e2-standard-4
    image: ubuntu-2404-lts-amd64
    network: projects/example-project/global/networks/default
    subnet: projects/example-project/regions/us-west1/subnetworks/default
    externalIp: automatic-for-public-services
    idleStopAfter: 30m

    dataDisk:
      name: agents-data
      size: 200GiB
      type: pd-balanced
      protect: true
      snapshots:
        schedule: daily
        retention: 14d
```

### 8.3 Supported guest

The initial supported guest is Ubuntu 24.04 LTS on `amd64` or `arm64`. Bastion MUST
detect and reject unsupported operating systems before applying host changes.

Additional distributions require a defined package, service, filesystem, and
privilege abstraction and are not implied by OCI runtime compatibility.

### 8.4 Connectivity

The default is:

```yaml
connection:
  type: iap
  osLogin: true
```

The first implementation MAY invoke the installed `gcloud` CLI for authentication,
instance lifecycle, OS Login, SSH, SCP, and IAP forwarding. `bastion doctor` MUST
validate the executable, active account, project access, and required IAM
permissions as far as practical.

Direct SSH MAY be supported for attached machines:

```yaml
connection:
  type: direct
  host: dev.example.com
  user: alice
  identityFile: ~/.ssh/id_ed25519
```

SSH agent forwarding MUST default to false and require explicit configuration or
a command flag.

Host convergence requires non-interactive `sudo` for privileged operations. The
remote runner MUST use `sudo -n` and fail with remediation instructions rather
than prompting through an unreliable hidden terminal. Features that do not require
root remain usable without sudo. `bastion doctor` reports which declared changes
require privilege and whether the selected identity has it.

## 9. Host configuration

### 9.1 Packages

`host.packages` is a convenience for packages from the supported guest's system
package manager. Package installation MUST be idempotent.

Package removal is not automatic. Removing a name from configuration means
Bastion no longer requires it; it does not mean uninstalling a potentially shared
host dependency. A future explicit removal policy MAY be added.

### 9.2 Built-in features

A built-in feature installs and verifies a coherent host capability. Initial
candidates are:

- `docker`;
- `github-cli`;
- `tmux`;
- `mise`;
- `uv`;
- `bun`;
- `codex`; and
- common build tools.

Built-in features MUST:

- declare supported guest platforms;
- validate their options;
- expose a read-only check operation;
- apply idempotently;
- report whether they changed the host;
- avoid unpinned `latest` installations where a stable version can be declared;
- distinguish user-level and root-level changes; and
- record their applied version and configuration digest.

### 9.3 Local features

A local feature is referenced relative to the box directory:

```yaml
host:
  features:
    - uses: ./features/personal-tools
      with:
        channel: stable
```

The proposed feature directory contract is:

```text
features/personal-tools/
├── feature.yaml
├── check
└── apply
```

`feature.yaml` defines the feature name, version, supported operating systems,
privilege requirement, and input schema. `check` MUST be read-only. `apply` MUST
be safe to rerun after a partial failure.

Bastion passes validated feature inputs through a JSON file rather than
interpolating them into a shell command. Feature output MUST use the remote-runner
event protocol so it can be rendered or emitted as JSON.

Local feature code executes with the user's trust and, when declared, with `sudo`.
Plans MUST identify such code as locally supplied executable content.

### 9.4 Managed files

Managed files are copied atomically. A plan MUST report creation, content changes,
permission changes, ownership changes, and deletion when deletion is explicitly
configured.

Supported modes are:

- `replace`: the source file is authoritative for the complete target; and
- `template`: the source is rendered from non-secret box metadata and feature
  outputs before replacing the target.

Secret values MUST NOT be accepted in a template during the initial release
because they would be persisted in generated plans and remote state.

Bastion MUST NOT edit arbitrary suffixes of shell startup files. Shell integration
SHOULD use one small, clearly delimited source line that loads a Bastion-owned file
such as `~/.config/bastion/shell.sh`.

Before replacing an existing unmanaged file for the first time, Bastion MUST show
the action in a plan and SHOULD retain one recoverable backup unless the user opts
out.

## 10. Container runtime

### 10.1 Runtime support

The configuration describes OCI images. The first supported execution engine is
Docker Engine with Docker Compose v2:

```yaml
runtime:
  engine: docker
```

The user-facing service model SHOULD remain independent of generated Compose
syntax. Podman or another OCI runtime MAY be added later, but runtime parity is not
assumed.

### 10.2 Generated Compose projects

Bastion SHOULD implement each service as an independently generated Compose
project stored under:

```text
/var/lib/bastion/services/<box-id>/<service-name>/compose.yaml
```

The project name is `bastion-<box-id>-<service-name>`. Generated containers MUST
carry labels identifying:

- the Bastion box ID;
- service name;
- configuration digest;
- image reference; and
- Bastion version that generated the definition.

All services join one Bastion-managed bridge network for the box. Service names
are network DNS names. Generated configuration MUST NOT mount the Docker socket
inside a user service.

### 10.3 Runtime installation

The Docker host feature owns installation and daemon configuration. It SHOULD
configure bounded JSON log rotation unless the user explicitly chooses another
logging driver.

Membership in the Docker group is effectively privileged access and MUST be
reported by `bastion doctor` and during first installation.

## 11. Service model

### 11.1 One service, one container

In version 1, a service is one container. This keeps the primary schema smaller
than Compose and makes lifecycle operations unambiguous.

Every service MUST declare an `image`. A service MAY declare:

- platform;
- entrypoint/command and arguments;
- working directory and user;
- environment values and secret references;
- mounts;
- resource limits;
- health checks;
- restart policy;
- dependencies; and
- endpoints.

Service names MUST be valid DNS labels and unique within a box.

### 11.2 Image references and pull policy

Example:

```yaml
services:
  dashboard:
    image: ghcr.io/example/dashboard:1.4.2
    pullPolicy: if-not-present
```

Supported pull policies are:

- `if-not-present`, the default;
- `always`; and
- `never`.

Bastion SHOULD warn when a service uses `latest` or an untagged image. Digest-pinned
images provide the strongest reproducibility:

```yaml
image: ghcr.io/example/dashboard@sha256:0123456789abcdef...
```

`bastion plan` reports the configured image reference and currently installed
digest. Registry refresh MAY require `--refresh-images` to avoid surprising network
requests. Bastion MUST NOT silently enable automatic updates.

### 11.3 Command semantics

Entrypoint and argument overrides MUST be arrays to avoid implicit shell parsing:

```yaml
entrypoint: ["/app/dashboard"]
args: ["serve", "--port", "3000"]
```

If shell behavior is required, the user must request it explicitly:

```yaml
entrypoint: ["/bin/sh", "-lc"]
args: ["./start-dashboard"]
```

### 11.4 Environment

Literal non-secret environment values are strings:

```yaml
environment:
  LOG_LEVEL: info
```

Secret environment values use a reference:

```yaml
environment:
  API_TOKEN:
    secretRef: dashboard-api-token
```

Bastion MUST redact secret values from terminal output, logs, plans, generated
Compose files where possible, and local operational state. Because container
environment variables can be observed through the runtime, secret-file mounts
SHOULD be preferred when supported by an application.

### 11.5 Restart policy

Supported values are:

- `no`, the default;
- `on-failure`;
- `unless-stopped`; and
- `always`.

`bastion up` starts all declared services unless a service is explicitly disabled.
`bastion down` stops the VM rather than individually stopping every container.
Docker restart behavior restores services when the VM starts.

### 11.6 Resources

Services MAY set CPU and memory limits. A plan SHOULD warn when the sum of declared
memory limits exceeds available VM memory, but overcommit is allowed.

Resource limits do not imply quotas or scheduling. Bastion operates one box and
does not reschedule a service elsewhere.

### 11.7 Health checks

Health checks use an exec-array form compatible with the selected runtime:

```yaml
healthcheck:
  command: ["/app/dashboard", "healthcheck"]
  interval: 30s
  timeout: 5s
  retries: 3
  startPeriod: 10s
```

`bastion up` SHOULD wait for declared health checks up to a configurable deadline.
A service that is running but unhealthy makes `bastion up` fail after the deadline
without automatically rolling back unrelated successful changes.

### 11.8 Dependencies

Services MAY declare startup ordering:

```yaml
dependsOn:
  - database
```

Ordering does not guarantee application readiness unless the dependency has a
health check. Cycles are configuration errors.

### 11.9 Reconciliation and removal

On apply, Bastion creates or replaces containers whose generated definition has
changed. Single-box services do not require zero-downtime replacement.

If a Bastion-owned service is removed from configuration, the plan MUST show that
its container will be stopped and removed. Durable volumes MUST be retained and
reported as orphaned. Ephemeral volumes MAY be deleted after confirmation.

Bastion MUST NOT modify or remove containers lacking Bastion ownership labels.

### 11.10 Compose stack escape hatch

Multi-container Compose stacks are deferred. A future design MAY support:

```yaml
stacks:
  analytics:
    composeFile: services/analytics.compose.yaml
```

Stack support must define ownership, secret handling, endpoints, volume durability,
and generated override behavior before it is implemented. Bastion MUST NOT attempt
to recreate the entire Compose specification in its primary service schema.

## 12. Volumes and persistence

### 12.1 Declared volumes

Volumes are declared at box scope and mounted into services by name:

```yaml
volumes:
  dashboard-data:
    persistence: durable

services:
  dashboard:
    mounts:
      - volume: dashboard-data
        target: /app/data
```

Supported persistence classes are:

- `durable`: a bind-mounted directory on the Bastion data disk; and
- `ephemeral`: a runtime-managed volume that may be lost with the VM.

### 12.2 Durable layout

Durable service data lives under the configured data root:

```text
<data-root>/volumes/<volume-name>/
```

The default managed data root is `/mnt/bastion`. Workspace repositories SHOULD
live in a sibling path such as `/mnt/bastion/workspace`, exposed as `/workspace`.

Docker's own image, layer, and build cache SHOULD remain ephemeral and SHOULD NOT
share the durable volume by default.

### 12.3 Bind mounts

Read-only or read-write host bind mounts MAY be declared explicitly:

```yaml
mounts:
  - source: /workspace/dashboard/config
    target: /app/config
    readOnly: true
```

Relative remote paths are not allowed. Bastion MUST show bind mounts in a plan and
MUST validate that their source is inside an allowed path unless an unsafe override
is configured.

### 12.4 Deletion safety

Stopping or destroying compute MUST NOT delete durable volume data. Removing a
volume declaration MUST leave the directory orphaned until the user runs an
explicit prune/delete operation.

Deleting durable data requires a separate confirmation naming the volume. A broad
`--force` flag alone is insufficient for deleting all durable data.

## 13. Endpoints and ingress

### 13.1 Visibility classes

Every endpoint has one of three visibility values:

**internal:** Reachable only by other containers on the Bastion network. This is
the default.

**private:** Reachable from the user's computer through a temporary IAP/SSH tunnel
created by `bastion port`.

**public:** Reachable over HTTPS through Bastion-managed ingress.

### 13.2 Private endpoints

Example:

```yaml
endpoints:
  web:
    containerPort: 3000
    protocol: http
    visibility: private
```

`bastion port agents dashboard:web` chooses an available local port unless
`--local-port` is provided. It MUST bind only to local loopback by default and
print the resulting URL or address.

The remote side MAY use a loopback-only published port or an ephemeral forwarding
helper. It MUST NOT open a GCP firewall port for a private endpoint.

### 13.3 Public endpoints

Example:

```yaml
endpoints:
  web:
    containerPort: 3000
    protocol: http
    visibility: public
    hostname: dashboard.example.com
```

Public endpoints in version 1 support HTTP applications exposed as HTTPS. Bastion
manages a Caddy ingress container that:

- binds host ports 80 and 443;
- joins the Bastion container network;
- routes by configured hostname to `<service>:<container-port>`;
- stores certificate state on a durable system volume; and
- does not mount the Docker socket.

User containers MUST NOT publish their application ports directly on the public
interface. Public TCP/UDP passthrough is out of scope.

### 13.4 DNS

If Cloud DNS integration is configured, the user MUST name an existing managed
zone. Bastion creates only the necessary record sets; it MUST NOT create or
delegate a root zone implicitly.

Without DNS integration, `bastion plan` prints the required record and public IP.
Apply MAY leave the endpoint in a `waiting-for-dns` state until it resolves.

### 13.5 Authentication

Public authentication is an open design item. Until a supported authentication
policy is defined, Bastion MUST require an explicit acknowledgement for a public
endpoint and state that the application is responsible for authentication.

Basic authentication, identity-aware ingress, and external tunnel providers MAY be
considered separately. They are not implied by `visibility: public`.

## 14. Secrets

### 14.1 Secret declarations

Secrets are named at box scope and consumed by reference. Initial sources are:

- a local environment variable; and
- a local file with restrictive permissions.

Example:

```yaml
secrets:
  dashboard-api-token:
    source:
      environment: DASHBOARD_API_TOKEN
```

GCP Secret Manager and OS keychain sources MAY be added later.

### 14.2 Secret handling invariants

Bastion MUST NOT:

- serialize resolved secret values into box configuration;
- store secret values in local operational state;
- show secret values in plans or normal logs;
- pass secrets as remote process command-line arguments; or
- include secrets in configuration digests in a recoverable form.

When secrets must exist on the remote box, Bastion stores them under a
Bastion-owned directory with restrictive permissions. Secret files SHOULD be
mounted read-only into containers. Generated Compose configuration references
remote secret-file paths rather than containing their values.

## 15. State and ownership

### 15.1 Desired state

The box directory is the only user-authored desired state. It SHOULD be safe to
commit when it contains no literal secrets.

### 15.2 Local operational state

Local state contains box registrations, stable IDs, cached observations, and
connection preferences. It lives in the platform user-state directory, not the box
directory.

Local state is a cache and index, not the sole source of truth. A user MUST be able
to restore operation through GCP resource labels and `bastion box adopt`.

### 15.3 Remote applied state

The remote state manifest lives under:

```text
/var/lib/bastion/state/<box-id>.json
```

It records non-secret feature versions, file digests, service configuration
digests, volume ownership, runner version, and completed operations. It MUST use
atomic replacement and a versioned format.

Remote state is evidence of what Bastion applied, not proof that the host has not
drifted. Planning SHOULD combine state with live checks.

### 15.4 Ownership

Bastion only reconciles resources carrying its stable box ID or explicitly
adopted by the user. Names alone do not establish ownership.

For remote files, first ownership is established by a successful apply after plan
approval. For containers, ownership is established through labels. For cloud
resources, ownership is established through labels plus local/adoption metadata.

## 16. Planning and apply

### 16.1 Plan

`bastion plan` MUST be read-only. It:

1. parses and validates configuration;
2. inspects provider and VM state;
3. inspects the remote guest when it is running and reachable;
4. checks features and managed files;
5. inspects Bastion-owned containers and volumes;
6. computes ingress and cloud-resource changes; and
7. prints an ordered action plan.

If a box is stopped, plan MUST NOT start it. It reports cloud-level changes and
marks guest-level results as unknown. The user can run `bastion up --plan-only`
if they explicitly want Bastion to start the box, inspect it, and leave it running
without applying guest changes.

With `--detailed-exitcode`, plan returns:

- `0` when valid and no changes are required;
- `2` when valid and changes are proposed; and
- `1` on validation or operational error.

### 16.2 Apply

`bastion apply` requires a running, reachable VM. It executes the approved plan in
dependency order. It MUST revalidate relevant preconditions immediately before a
consequential action.

Apply is resumable but not globally transactional. Each successful action updates
remote state atomically. A later failure leaves earlier successful changes in
place and prints the exact resume command.

Container replacement, managed-file replacement, and generated-configuration
updates SHOULD be atomic at their individual resource boundary.

### 16.3 Up

`bastion up` is the convenient composition:

1. create managed cloud resources when absent;
2. start the VM when stopped;
3. wait for provider and SSH readiness;
4. apply desired configuration; and
5. wait for declared service health checks.

`bastion up --no-apply` starts without configuration convergence.

### 16.4 Down

`bastion down` stops the VM cleanly. It reports continuing charges for persistent
disks, snapshots, static IPs, and other retained resources when known.

### 16.5 Drift and pruning

Bastion distinguishes:

- **managed drift**, where a declared resource differs and can be reconciled;
- **orphaned Bastion resources**, previously owned but no longer declared; and
- **unmanaged resources**, which Bastion reports only when relevant and never
  changes.

Removed container services are stopped and removed after appearing in an approved
plan. Durable data is retained. Removed host packages and features are not
automatically uninstalled.

## 17. CLI specification

### 17.1 Global behavior

Global flags:

```text
--config <path>    Select a box definition directly
--box <name>       Select a registered box
--json             Emit structured JSON/JSON Lines
--no-color         Disable color
--yes              Approve non-data-destructive prompts
--verbose          Include diagnostic detail
```

Commands return `0` on success and `1` on failure unless a command documents
another behavior. Human-readable diagnostics go to stderr when stdout is reserved
for structured or streamed command output.

### 17.2 Configuration commands

```text
bastion init [path]
bastion validate [box]
bastion config paths
bastion config schema
```

`init` creates a minimal example without overwriting existing files. `validate`
performs schema and semantic validation without contacting GCP unless
`--with-provider` is supplied.

### 17.3 Box commands

```text
bastion box adopt <name> --config <path>
bastion box list
bastion box use <name>
bastion box forget <name>
```

`forget` removes only the local registration. It does not mutate the VM.

### 17.4 Lifecycle and connection commands

```text
bastion status [box]
bastion plan [box]
bastion apply [box]
bastion up [box]
bastion down [box]
bastion ssh [box]
bastion exec [box] -- <command> [args...]
bastion doctor [box]
```

`exec` preserves the remote exit code when possible. It does not invoke an
implicit shell; the argument vector is forwarded as a command. A `--shell` option
MAY explicitly request shell evaluation.

### 17.5 Service commands

```text
bastion service list [box]
bastion service status [box] <service>
bastion service logs [box] <service>
bastion service start [box] <service>
bastion service stop [box] <service>
bastion service restart [box] <service>
bastion service exec [box] <service> -- <command> [args...]
bastion service update [box] <service>
```

Service commands operate only on declared or still-owned Bastion services. An
imperative stop is operational state; a later `bastion up` or `apply` MAY restore
the declared running state.

`service update` resolves and pulls a newer digest for a mutable tag, shows the
change, and requires confirmation. It SHOULD offer to write an updated digest or
tag into configuration rather than creating permanent drift.

### 17.6 Endpoint commands

```text
bastion port [box] <service>:<endpoint> [--local-port <port>]
bastion endpoint list [box]
```

`port` remains attached and forwards signals until interrupted. `--background`
MAY be added later if lifecycle and cleanup semantics are defined.

### 17.7 Destructive commands

```text
bastion destroy <box>
bastion volume delete <box> <volume>
```

`destroy` is available only for managed boxes and retains protected/durable data
by default. `volume delete` requires a separate confirmation containing the volume
name. Non-interactive data deletion requires a narrowly named flag, not only
`--yes`.

## 18. Implementation architecture

### 18.1 Language and distribution

The recommended implementation language is Go. The project SHOULD distribute a
single CLI executable for macOS and Linux on `amd64` and `arm64`. Windows client
support MAY follow once process, SSH, path, and terminal behavior is tested.

### 18.2 Components

```text
CLI
├── configuration loader and schema validator
├── box registry and state store
├── planner and reconciler
├── GCP provider
├── SSH/IAP transport
├── remote-runner client
├── host feature and file engine
├── Docker/Compose service engine
├── volume manager
├── ingress manager
└── human and machine-readable renderers
```

### 18.3 Remote runner

The CLI MAY copy a version-matched runner to a user cache directory on the VM and
invoke it over SSH. The runner:

- accepts a versioned request over stdin;
- emits structured events over stdout;
- uses stderr for diagnostics;
- exits after each inspect or apply request;
- never listens on a socket or network port;
- invokes `sudo` only for operations that declare it; and
- writes remote state and generated files atomically.

The runner protocol MUST be versioned independently from box configuration.
Client/runner incompatibility MUST produce an actionable upgrade or replacement,
not undefined behavior.

### 18.4 GCP implementation

The first implementation SHOULD wrap `gcloud` to reuse application-default/user
authentication, OS Login, SSH key handling, IAP, and established error messages.
Commands MUST pass explicit project and zone values rather than mutating global
`gcloud` configuration.

A later native GCP API implementation MAY replace lifecycle calls, but SSH/IAP
behavior does not need to be rewritten merely to remove the dependency.

### 18.5 Concurrency

Bastion MUST use a local per-box operation lock and a remote apply lock. Concurrent
read-only status operations are allowed. Concurrent applies, lifecycle mutations,
or service updates for the same box MUST fail or wait with clear ownership and
timeout information.

Stale locks MUST be recoverable without deleting broad state directories.

## 19. Security and safety requirements

### 19.1 Cloud access

- IAP and OS Login are the managed default.
- Managed boxes have no public SSH ingress.
- Bastion uses an explicit, minimally privileged service account for managed VMs.
- The VM service account receives no cloud permissions unless a feature or service
  explicitly requires them.
- `cloud-platform` OAuth scope MAY be used only when IAM roles remain least
  privilege.

### 19.2 Container isolation

By default, user services MUST NOT use:

- privileged mode;
- host networking;
- host PID or IPC namespaces;
- the Docker socket;
- arbitrary device mounts; or
- public host-port bindings.

Future unsafe overrides must be explicit per service, appear prominently in plans,
and never be enabled by a global convenience default.

Bastion SHOULD enable `no-new-privileges` when compatible. It SHOULD warn when an
image runs as root and no user override is supplied.

### 19.3 Supply chain

- Image references using mutable tags produce a warning.
- Remote features are not supported initially.
- Downloaded installers in built-in features MUST use HTTPS and SHOULD verify
  signatures or checksums when upstream supports them.
- Release artifacts SHOULD be signed and accompanied by checksums.

### 19.4 Destructive operations

- Broad or unresolved paths MUST never be recursive-deletion targets.
- Durable data deletion is separately confirmed.
- Attached cloud resources are never destroyed.
- A failed apply prints what changed and what did not.
- Managed-file replacement is atomic and first adoption is recoverable.

### 19.5 Threat boundary

The remote box and local box definition are trusted. A compromised VM can access
data and credentials intentionally forwarded or deployed to it. Bastion reduces
accidental exposure; it does not make an untrusted development VM safe for SSH
agent forwarding or unrestricted cloud credentials.

## 20. Diagnostics and observability

`bastion doctor` SHOULD check:

- local executable dependencies;
- CLI and configuration versions;
- GCP authentication and selected account;
- required project permissions;
- instance existence and state;
- IAP/SSH reachability;
- guest OS compatibility;
- sudo availability;
- disk mount and free space;
- Docker and Compose health;
- remote-runner compatibility;
- service health;
- public DNS and TLS state when configured; and
- known unsafe configuration such as agent forwarding or mutable images.

Every apply operation receives an operation ID. Human logs include concise phases;
`--json` emits versioned events containing operation ID, resource, action, status,
duration, and a redacted message.

## 21. Testing strategy

### 21.1 Unit tests

Unit tests MUST cover:

- configuration parsing and unknown-field rejection;
- semantic validation;
- box resolution precedence;
- planning and drift classification;
- ownership decisions;
- command argument construction and shell-injection resistance;
- redaction;
- durable-data deletion guards; and
- state-format migrations.

### 21.2 Golden tests

Golden tests SHOULD cover:

- generated Compose files;
- Caddy configuration;
- managed-file templates;
- human-readable plans; and
- machine-readable events.

### 21.3 Integration tests

Local integration tests SHOULD use a fake provider and SSH executor. Docker tests
SHOULD exercise service create, replace, logs, health, endpoint, and removal
behavior.

GCP integration tests SHOULD use a dedicated project and ephemeral fixtures. They
MUST label all resources and have an independent cleanup mechanism. Destructive
data tests MUST use uniquely created disposable disks.

### 21.4 Compatibility tests

The project SHOULD test supported macOS client architectures and Ubuntu guest
architectures. Configuration and runner protocol compatibility MUST be exercised
across at least one previous supported version before stable release.

## 22. Delivery plan

### 22.1 Phase 0: specification and skeleton

Deliverables:

- reviewed product and technical specification;
- final public project and binary name;
- `v1alpha1` JSON Schema;
- example attached box configuration;
- Go CLI skeleton; and
- automated formatting, linting, and unit-test workflow.

Exit criteria:

- the example validates;
- box resolution behavior is tested; and
- open design questions blocking phase 1 are decided.

### 22.2 Phase 1: attached GCP box

Deliverables:

- `init`, `validate`, and global paths;
- box registry, `adopt`, `list`, and `use`;
- `status`, `up`, and `down` for an attached VM;
- `ssh`, `exec`, and IAP forwarding;
- initial `doctor`;
- human and JSON output; and
- fake-provider and manual GCP integration tests.

Exit criteria:

- the new CLI can safely replace the current Bash lifecycle controller for a new
  VM without reading `terraform.tfvars`.

### 22.3 Phase 2: host convergence

Deliverables:

- remote runner and protocol;
- plan/apply engine;
- system packages;
- built-in Docker, GitHub CLI, tmux, and tool-manager features;
- local feature contract;
- managed files;
- remote applied-state manifest; and
- partial-failure recovery.

Exit criteria:

- a clean supported Ubuntu VM converges to the example host configuration;
- a second apply is a no-op; and
- interrupted apply can resume safely.

### 22.4 Phase 3: OCI container services

Deliverables:

- Docker/Compose engine;
- service schema and generated projects;
- service lifecycle, logs, exec, health, and resource limits;
- durable and ephemeral volumes;
- private endpoints and `bastion port`; and
- service ownership and pruning.

Exit criteria:

- two services can run, communicate privately, persist declared data, and survive
  a VM stop/start cycle;
- removed service containers are pruned without deleting durable data; and
- undeclared containers are untouched.

### 22.5 Phase 4: managed GCP infrastructure

Deliverables:

- managed VM creation;
- IAP-only network path;
- dedicated service account;
- persistent data disk and safe replacement;
- snapshot schedule;
- idle shutdown;
- resource discovery/adoption; and
- guarded compute destroy.

Exit criteria:

- `bastion up` can create a box from configuration;
- replacing compute retains workspace and service data; and
- deleting compute does not delete protected data.

### 22.6 Phase 5: public HTTPS services

Deliverables:

- Caddy ingress container;
- public endpoint planning;
- static IP and HTTP/HTTPS firewall management;
- existing-zone Cloud DNS integration;
- manual-DNS workflow;
- certificate and endpoint status; and
- explicit unauthenticated-public-service acknowledgement.

Exit criteria:

- a configured HTTP service is reachable at its HTTPS hostname;
- removing public exposure closes ingress no longer needed; and
- private services never become public as a side effect.

### 22.7 Phase 6: open-source version 1

Deliverables:

- stable schema and migration tooling;
- installable macOS/Linux releases;
- checksums and signed release artifacts;
- Homebrew distribution;
- complete user and feature-author documentation;
- contribution and security policies;
- recovery and troubleshooting guides; and
- automated GCP integration coverage.

## 23. Version 1 acceptance criteria

Version 1 is complete when a new user can:

1. Install one local executable.
2. Authenticate with GCP using documented prerequisites.
3. Adopt or create a named box from a checked-in definition.
4. Review a plan before changes.
5. Start, stop, SSH into, and execute commands on the box.
6. Apply host features twice with the second run producing no changes.
7. Run and operate an OCI-image service.
8. Access that service privately without opening a public firewall port.
9. Explicitly expose an HTTP service over HTTPS.
10. Replace managed compute while preserving durable data.
11. Recover local registration from provider labels and remote state.
12. Understand and recover from a partial apply using CLI diagnostics.

## 24. Migration from the existing repository

The existing repository is a behavior reference, not the target architecture.

Concepts to retain:

- start, stop, status, SSH, logs, and private-port workflows;
- a separately mounted persistent data disk;
- tmux-oriented remote development;
- agent-friendly host tooling;
- Shielded VM settings; and
- explicit cost awareness when compute is stopped.

Concepts to replace:

- parsing adjacent `terraform.tfvars` from the CLI;
- deriving one VM from an owner name;
- a monolithic startup script that runs on every boot;
- destructive whole-file dotfile synchronization;
- public development ports by default;
- manually mutating a firewall rule and Terraform input together;
- hard-coded personal paths and identities; and
- using Terraform state as local CLI configuration.

Terraform MAY remain as a migration aid or example, but Bastion configuration and
CLI operation MUST not depend on a Terraform working directory.

## 25. Open design questions

These questions should be decided during review or phase 0:

1. **Public name:** another active coding-environment project already ships a
   `bastion` CLI. Should this project choose a distinct public name while retaining
   Bastion as a working name?
2. **Initial scope:** should the first public release be attached-VM-only, or is
   managed GCP creation required for the first useful release?
3. **Host support:** is Ubuntu 24.04-only acceptable through version 1?
4. **Runtime:** is Docker-only acceptable through version 1 while the schema uses
   OCI image terminology?
5. **Local features:** should arbitrary local features be in the first host
   convergence milestone, or should v0 initially ship built-ins only?
6. **Public authentication:** should version 1 offer a built-in authentication
   mechanism, or clearly delegate authentication to the application?
7. **Secrets:** are environment and local-file secret sources sufficient for
   version 1, or is GCP Secret Manager required?
8. **Compose stacks:** should advanced users receive a Compose-file escape hatch
   before the stable release?
9. **Idle detection:** should managed idle shutdown use a fixed schedule, lack of
   SSH sessions, service/agent activity, or an explicit user action?
10. **Configuration naming:** should the canonical file remain `bastion.yaml` if
    the public binary name changes?

## 26. Deferred ideas

The following ideas are intentionally deferred but compatible with the model:

- Podman runtime support;
- AWS or other cloud providers;
- declarative Compose stacks;
- devcontainer-aware project helpers;
- scheduled service jobs;
- service backup hooks;
- authenticated public ingress;
- GCP Secret Manager and keychain secret sources;
- IDE connection helpers;
- coding-agent session management;
- reusable signed feature packages; and
- a read-only local UI backed by CLI state.
