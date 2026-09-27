# Deployment

Use [Getting started](../../../../docs/GETTING_STARTED.md) for prerequisite
installation, first local startup, the three-terminal development workflow,
readiness checks and safe reset. This document defines deployment and state
contracts; it is not a second first-time tutorial.

The server has two process roles:

- one Agent for global accounts, title login, shard discovery, and routing;
- one GameWorld per enabled shard.

A shard is an authority boundary: one database, one process, one transport,
one session set, and one world tick. Never mount one shard database into two
GameWorld processes.

Nomad is the only bare-metal process manager. The retired PowerShell
supervisor is not a supported launch path.

## 1. State layout

Local provisioning state:

```text
.state/cluster/accounts.json
.state/cluster/agent-session-keys.json
.state/cluster/agent/shard-leases.json
.state/cluster/releases/agent/<release-id>/agent.exe
.state/cluster/releases/gameworld/<release-id>/gameworld.exe
.state/shards/global-official/authority/state.db
.state/shards/test/authority/state.db
```

Nomad Variables deliver credentials to allocations. Local `.state/cluster`
files are deployment inputs, not files that tasks read directly.

The development account is `tester` / `123123` on shard
`global-official`. It is intentionally weak and acceptable only for the
loopback development stack.

`sro-bootstrap-development` provisions it from
`apps/server/config/dev-account.env`. client-next signs in with it through the
normal title login; there is no anonymous bypass and no auto-login. A fresh
authority has no characters; create one on `global-official` before running
live acceptance probes.

The deterministic performance-fixture reset endpoint is a separate
development authority. Nomad registers it only for the loopback topology by
setting `SRO_BENCHMARK_FIXTURE_CONTROL=1`; non-loopback allocations do not
register the route. Requests require an authenticated Agent session, remain
account-scoped, refuse a character with an active GameWorld lease, and replace
only the benchmark-owned spawn and movement state. Performance benchmarks use
this exact reset before boot instead of attempting to reproduce fixture state
through ordinary gameplay movement.

## 2. Nomad topology

The combined `-dev` server/client configuration is local development only.
Production uses:

- three or five dedicated Nomad server nodes for Raft quorum;
- separate Windows Nomad client nodes for Agent and GameWorld allocations;
- ACL enforcement on every Nomad server and client;
- mTLS for all Nomad HTTP and RPC traffic;
- a dedicated low-privilege Windows service account on raw-exec clients;
- a dedicated `sro` Nomad namespace containing no unrelated jobs.

Examples:

```text
ops/nomad/config/server-production.hcl.example
ops/nomad/config/client-windows-agent-production.hcl.example
ops/nomad/config/client-windows-production.hcl.example
```

Install the Nomad client as a Windows service using HashiCorp's native
`nomad windows service install` command. Do not wrap Nomad in another process
manager.

The deploy command uses the official Nomad environment:

```text
NOMAD_ADDR
NOMAD_REGION
NOMAD_NAMESPACE
NOMAD_TOKEN
NOMAD_CACERT
NOMAD_CLIENT_CERT
NOMAD_CLIENT_KEY
NOMAD_TLS_SERVER_NAME
```

Do not set `NOMAD_SKIP_VERIFY` in production.

Production `validate`, `deploy`, `stop`, and `rotate-session-key` commands
accept only a region-local Nomad client token with exactly the direct
`sro-deployer` policy. It must expire, have been issued for no more than two
hours, and retain at least twenty minutes at command start. A one-hour token
brokered by Vault or created with `nomad acl token create -ttl 1h` is the
normal posture. Management, global, permanent, role-based, and
additional-policy tokens fail before the fleet lock or any deployment
mutation. Use a separate read-only token for `status`.

The shared `default` namespace is not a credential bypass. Credential-free
development commands are accepted only from the pinned loopback Nomad agent
whose dev mode, disabled ACLs, version, config path, data directory, and OIDC
issuer identify this exact checkout. A remote production cluster, including
one forwarded onto a loopback port, must use the dedicated `sro` namespace
and production token contract.

See [ops/nomad/README.md](../nomad/README.md) for exact operations and
official HashiCorp references.

## 3. Provisioning

Development:

```powershell
go run ./cmd/operations/sro-bootstrap-development
```

That command is one step of the canonical
[local setup sequence](../../../../docs/GETTING_STARTED.md#3-server-cluster). It is
not a production account provisioner.

Production creates credentials and accounts separately:

```powershell
go run ./cmd/operations/sro-provision-identity
go run ./cmd/operations/hash-agent-password
```

`sro-provision-identity` creates Agent's missing Ed25519 signing-key ring
without printing or replacing it. It deliberately does not create accounts. Provision
`.state/cluster/accounts.json` from the real account authority.

Agent accepts a strict JSON array:

```json
[
  {
    "id": "alice",
    "passwordHash": "$2a$10$..."
  }
]
```

Rules:

- bcrypt cost 10 through 12;
- no plaintext password field;
- regular non-symlink file, maximum 1 MiB;
- every live character owner must be a configured account;
- reserved internal owner `__open_dev__` is forbidden.

GameWorld fetches only the Agent identity set during startup and validates
every character owner before opening listeners. Password hashes remain inside
Agent.

## 4. Deploy, status, and stop

From the module root:

```powershell
go run ./cmd/operations/sro-nomad validate
go run ./cmd/operations/sro-nomad deploy -build
go run ./cmd/operations/sro-nomad status
go run ./cmd/operations/sro-nomad stop
```

Those defaults are the loopback development topology. Production selects the
private named network configured on every eligible Nomad client and explicitly
acknowledges that trust boundary:

```powershell
$TransportCert = 'C:\ProgramData\SRO\tls\fullchain.pem'
$TransportKey = 'C:\ProgramData\SRO\tls\private-key.pem'

go run ./cmd/operations/sro-nomad validate `
  -namespace sro `
  -host-network game-private `
  -allowed-origins https://play.example.com `
  -identity-issuer https://nomad.internal.example:4646 `
  -transport-cert-file $TransportCert `
  -transport-key-file $TransportKey `
  -private-network

go run ./cmd/operations/sro-nomad deploy -build `
  -namespace sro `
  -host-network game-private `
  -allowed-origins https://play.example.com `
  -identity-issuer https://nomad.internal.example:4646 `
  -transport-cert-file $TransportCert `
  -transport-key-file $TransportKey `
  -private-network

go run ./cmd/operations/sro-nomad status -namespace sro
go run ./cmd/operations/sro-nomad stop -namespace sro
```

The deployer refuses non-loopback deployment into the shared `default`
namespace. Nomad ACL job capabilities apply to an entire namespace rather
than a job-name prefix, so the `sro` namespace is the production containment
boundary. The acknowledgement is a guardrail, not network security. The selected
network must be private, firewalled, reachable between Agent and GameWorld
nodes, and fronted by the intended TLS edge. `-allowed-origins` is a
comma-separated list of exact browser origins, including scheme and optional
port. Wildcards and URL paths are refused. Loopback development defaults to
the local Vite development and preview origins; a non-loopback network has no
implicit browser origin.

Each GameWorld terminates its own WebTransport/HTTP3 TLS connection. A
non-loopback deployment therefore requires `-transport-cert-file` and
`-transport-key-file`; a TCP reverse proxy cannot terminate the UDP QUIC
leg. Provision the same absolute read-only paths on every eligible GameWorld
node. The deployer loads the pair, rejects expired/not-yet-valid material,
requires HTTPS shard transport URLs, and verifies that the certificate covers
every enabled `transportUrl` host before mutating Nomad. It records the leaf
SHA-256 in the jobspec, so renewal at the same path rolls the allocations on
the next `deploy`. `-identity-jwks-url` is optional when Nomad serves JWKS at
`<identity-issuer>/.well-known/jwks.json`.

Agent advertises a shard's `publicTransportUrl` instead of `transportUrl` when
the catalog sets one. An edge route such as `/shards/<id>` sends browser
WebSockets through the TLS edge that serves the client; see
`apps/client-next/docs/HOSTING.md`. The checks above still cover each
GameWorld's own `transportUrl`, which also serves WebTransport.

### Authenticated transport admission

Every HELLO carries a short-lived, one-use Agent ticket before the GameWorld
allocates a transport session. The browser always mints the ticket through the
Agent endpoint, and `Server.Start` fails closed unless the GameWorld has a
HELLO admission verifier. There is no unauthenticated protocol mode or rollout
flag.

The Agent ticket issuer and every GameWorld verifier must share the active
transport-admission signing configuration. A deployment is healthy when
`/transport/metrics` reports increasing `hello_admission_accepted` and any
`hello_admission_refused` count is attributable to expired, replayed, or
otherwise invalid tickets. Validate a fresh connection and a resumed
connection after rotating admission credentials.

Deployment uses Nomad plan output and enforces the returned job modify index,
so a concurrent operator change causes a conflict instead of being
overwritten. Nomad Variables are created and updated with their CAS modify
indexes. Deploy and stop also hold the leased Nomad Variable Lock at
`sro/operations/fleet`; a second mutation fails before it can build, stage,
register, deregister, or rotate anything.

`validate` is deliberately read-only. It validates and plans every jobspec,
constructs every workload Variable, enforces Nomad's path and item-size
limits, reads the current Variables, and applies the same live-credential
drift refusal as deploy. It does not acquire the fleet mutation lock or claim
that future CAS writes are reserved. Read-only describes its effect, not its
ACL identity: production validation intentionally uses the same short-lived
`sro-deployer` token, while `status` supports a narrower read-only token.
`deploy` repeats the complete preflight while holding the fleet lock, then
relies on modify indexes for the actual writes.

The job release identity hashes both the executable and the HCL jobspec. A
binary is atomically staged under its content-addressed identity and the
absolute immutable path is submitted to `raw_exec`. A binary or policy change
therefore creates a replacement allocation. Cleanup preserves every release
still referenced by Nomad job history, so retained versions remain revertible.
This is deliberately an artifact identity, not a hash of runtime state:
catalog rows, ports, paths, and other HCL variable values live in the
versioned Nomad job specification, while secrets live in CAS-protected Nomad
Variables. Putting either into the executable cache key would duplicate the
same binary and would make secret rotation part of a public path name.

Agent converges first. Once its deployment is healthy, enabled GameWorld jobs
converge independently with bounded parallelism. Every job is planned before
the first registration. Each registration has one whole-operation deadline
covering its plan, optimistic registration, native deployment health, and
final allocation convergence; it is not reset between phases. That deadline
comes from the parsed jobspec's `progress_deadline` plus a one-minute observer
margin, so the deployer cannot abandon a rollout that Nomad still considers
valid. The CLI parent is cancelled by `Ctrl+C`/termination rather than an
unrelated fixed fleet timer. A failed shard does not prevent the remaining
shards from being attempted. Nomad auto-reverts that job to its last stable
specification and retains both deployment history and immutable releases. Fix
the failed input and rerun `deploy`; reconciliation resumes from Nomad's
recorded state. There is intentionally no second rollback journal or
cross-shard transaction.

The default release root is `.state/cluster/releases`. For several Nomad
clients, use `-release-dir` with a cluster-specific path that is present at
the same absolute location on every eligible node. Clients need read/execute
access only; the deploy identity owns publication and pruning.

`stop` deregisters without immediate purge, waits for the original
allocations to become terminal, and only then purges the stopped job. Nomad's
shutdown delay and task kill timeout remain active.

### Credential rotation

Agent session signing keys rotate online:

```powershell
go run ./cmd/operations/sro-nomad rotate-session-key `
  -namespace sro `
  -host-network game-private `
  -agent-url http://agent.service.consul:8787 `
  -private-network
```

The command holds the fleet lock and uses Variable CAS. It durably creates a
pending Ed25519 key without changing the signing key, publishes the public
ring to every GameWorld, and waits for each process to acknowledge the exact
digest. Only then does Agent activate it. The former public key remains
accepted for the 13-hour maximum token horizon and is removed by a later
rotation after that deadline.

Before the first write, rotation proves that Agent and the exact catalog
GameWorld job set are running, then checks Agent and every GameWorld private
`/readyz` endpoint in parallel. Each process must be ready and report a key
ring compatible with the durable rotation state; this also proves that the
deployment runner can reach every catalog `controlUrl`. A degraded allocation
or unreachable control network aborts before local state or Nomad Variables
change. Run rotation from the private deployment runner or a routed bastion;
do not publish control listeners.

An interrupted command resumes its pending key. If Agent activated before the
local completion write, the next run adopts that remote state instead of
reverting it. Nomad renews GameWorld workload-identity JWTs automatically;
GameWorld EnterWorld keys are process-local and need no operator rotation.
Changing the account catalog still requires stopping Agent because it changes
login authority rather than an overlap-capable signing key.

## 5. Shard catalog and leases

`config/shards.json` is the process catalog. Each row contains:

- stable string shard ID;
- native title server/farm IDs;
- capacity;
- private control URL;
- public transport URL;
- enabled/default/test flags.

Deploy starts exactly one GameWorld job for each enabled row. A previously
registered job whose shard is now disabled is gracefully stopped and purged.
Its workload Variable is then deleted with its CAS modify index.

Nomad placement is also fail-closed:

- Agent requires client metadata `sro_agent=true`;
- GameWorld requires its shard ID in client metadata `sro_shards`;
- `sro_shards` is a comma-separated set such as
  `global-official,test`.

This metadata declares authority ownership; it is not a capacity hint. With
the current SQLite authority, exactly one storage node claims a shard.
Nomad restarts on that node but will leave the shard unplaced after node loss.
Restore and validate the authority on a replacement before transferring the
metadata claim. Never make two independent database copies eligible for the
same shard.

Each GameWorld generates a random boot instance ID and renews a lease every
three seconds. Agent permits one fresh owner per shard. Three consecutive
heartbeat failures terminate the worker; a short Agent restart is tolerated.
After lease expiry Agent reports the shard non-operating with population zero.

The GameWorld restart delay is 12 seconds plus Nomad's documented jitter. It
exceeds the ten-second ownership lease, so a crashed writer consumes one
restart attempt instead of repeatedly failing the lease fence.

## 6. Shard creation

Create a fresh shard explicitly:

```powershell
go run ./cmd/operations/sro-init `
  -shard global-official `
  -authority-dir .state/shards/global-official/authority
```

The shard must exist in the catalog. There is no unscoped or combined-store
path. This pre-release build accepts only the current authority schema; replace
incompatible development state with an explicitly initialized shard.

The v1.150 character dock supports four characters per account per shard.
Delete-pending characters retain their slot until deletion matures. Resolve
pre-release overflow explicitly:

```powershell
go run ./cmd/operations/sro-archive-character `
  -shard global-official `
  -character ExactName

go run ./cmd/operations/sro-archive-character `
  -shard global-official `
  -character ExactName `
  -commit
```

## 7. Authentication boundaries

| Credential | Owner and purpose |
| --- | --- |
| Agent Ed25519 private ring | Agent alone signs 12-hour account + shard sessions |
| Agent public ring | Every GameWorld verifies sessions but cannot mint one |
| Nomad workload identity | Nomad gives each GameWorld a renewable, short-lived JWT for Agent control calls |
| EnterWorld process key | One GameWorld mints and verifies short-lived, character-bound, one-use tickets in memory |

Agent derives the worker route from the signed session shard. Request
`divisionId` fields are untrusted echoes. GameWorld verifies that Agent
sessions and EnterWorld tickets target its sole owned shard. Agent validates
the Nomad issuer, audience, namespace, exact job, task, and requested shard
before accepting a control call. Each GameWorld Variable contains only the
non-secret Agent public ring. Compromise of it cannot mint a session, call
Agent as another workload, or recover a sibling's process-local EnterWorld
key.

Nomad stores task values at its workload-scoped automatic paths:

```text
nomad/jobs/sro-agent/agent/agent
nomad/jobs/sro-agent/agent/agent/accounts/000000
nomad/jobs/sro-agent/agent/agent/accounts/000001
nomad/jobs/sro-gameworld-<shard>/gameworld/gameworld
```

The task workload identity grants read access only to its own automatic path.
Nomad caps one Variable at 64 KiB while the validated account catalog permits
one MiB. The deployer therefore splits that UTF-8 catalog at rune boundaries
into ordered 48-KiB account Variables and renders their exact concatenation.
Variable creation, replacement, and stale canonical-chunk removal use CAS.
Unrecognized paths are left untouched.
The application limit produces at most 22 references in the jobspec; chunk
payloads remain in Nomad Variables and are never embedded into HCL.

## 8. Network, readiness, and TLS

Defaults are loopback:

| Endpoint | Purpose |
| --- | --- |
| Agent `127.0.0.1:8787` | Title/login and character-select proxy |
| GameWorld TCP | WebSocket alternate, health, readiness, metrics |
| GameWorld UDP | WebTransport/HTTP3 |
| GameWorld control | Private character/control API |

Nomad reserves the catalog's static ports on a named host network. Collision
bypass is not enabled. A conflicting allocation remains unplaced rather than
starting a second listener.

Tasks bind to Nomad's allocated `NOMAD_IP_<label>` and
`NOMAD_PORT_<label>` values. GameWorld obtains the singleton Agent address
from a watched Nomad native service-discovery template; it never assumes
Agent is on the same node. If no Agent registration exists, the template
provides no route and the GameWorld's required-route gate refuses startup
without a loopback fallback. When the registration appears or changes, Nomad
re-renders the template and restarts the task with the current route.

Liveness and readiness are different:

- `/healthz` means the process is alive; repeated liveness failure may restart
  the task;
- `/readyz` means the role can accept new work; readiness failure removes it
  from deployment health without causing restart storms.

GameWorld paths are `/transport/healthz` and `/transport/readyz`.
Both roles declare these as native Nomad service checks. Readiness uses
`on_update = "require_healthy"`, so Nomad—not the deployment CLI—decides when
an allocation and its deployment are healthy. Browser/ingress acceptance is
verified separately by the live-stack probes.

The plaintext Agent and control listeners must remain private behind a TLS
edge. Use exact browser origins. Public WebTransport requires a production
certificate supplied through the required deploy flags; a missing pair is
rejected before jobs are registered and again by GameWorld startup.

Nomad's isolated `exec` driver is unavailable on Windows. The required
`raw_exec` driver provides process ownership but no filesystem isolation, and
the task inherits the Nomad client service identity. Treat each client as a
dedicated game-server host; do not co-locate unrelated workloads. Run it under
a dedicated non-interactive, non-administrator account with minimal file
ACLs, host-firewall rules, and application allow-listing.
The production client configurations and both jobspecs independently scrub
host Nomad, Consul, Vault, cloud, and source-control credentials through
`denied_envvars`. This closes environment inheritance but does not create
filesystem isolation; the dedicated host identity and file ACL boundary
remain mandatory.
Keep Nomad at `INFO` or stricter: DEBUG output from its template renderer may
contain the Agent private key or account hashes. Treat such a log as a
credential disclosure, remove it after stopping its writer, rotate the Agent
session key online, and replace exposed account credentials.

Containers are not a supported runtime in this repository. The obsolete image
and shell entrypoint were removed when Nomad became the sole process owner;
maintaining a second credential-delivery and lifecycle path would reintroduce
configuration drift.

## 9. Backup and recovery

Back up each shard independently while its GameWorld is stopped, or use a
SQLite-aware snapshot:

```text
state.db
state.db.bak
manifest.json
```

`authority.lock` is liveness metadata, not world data. WAL/SHM files belong to
a running database and must not be copied outside a consistent snapshot.

Nomad server state, including Variables, requires a separate Nomad operator
snapshot and the key-management material required by the installed Nomad
version. Test restore into a fresh control plane; never rehearse restore over
the live Raft cluster.

## 10. Engineering gates

```powershell
go version # must report the release go.mod names (go1.27.1) or a newer patched release
go mod verify
go mod tidy -diff
go fmt ./...
go test -count=1 -timeout 300s ./...
go test -race -count=1 ./internal/agent/api ./internal/agent/server ./internal/security/auth ./internal/cluster/shard ./internal/data/store ./internal/transport
go vet ./...
go build ./...
govulncheck ./...
```

The earlier `sro-nomad validate` commands are deployment integration checks.
They require a reachable Nomad agent and the credentials appropriate to the
selected namespace; they are intentionally not part of the standalone source
gate.

No maintained executable source or test may exceed 1,000 lines. `testdata`,
vendored/generated code, data fixtures, documentation, and captured wire
evidence are excluded.
