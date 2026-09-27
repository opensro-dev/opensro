# Nomad operations

Use [Getting started](../../../../docs/GETTING_STARTED.md) first for tool installation, the pinned
development binary, initial asset/account/shard provisioning, and the
three-terminal startup sequence. This guide is the Nomad behavior and
production-operations reference.

Nomad is the only bare-metal supervisor for Agent and GameWorld. The Go
processes expose standard liveness/readiness endpoints and respond to native
termination signals; they do not expose an orchestration state machine.

## Why these primitives

This integration uses Nomad's own mechanisms:

| Concern | Nomad primitive |
| --- | --- |
| Process ownership | Allocation + raw-exec driver |
| Environment isolation | Task `env` and template environment |
| Secret delivery | Nomad Variables + workload identity |
| Concurrent writes | Variable CAS modify index |
| Fleet operation ownership | Leased Nomad Variable Lock |
| Safe job update | Plan + enforced job modify index |
| Deployment completion | Version-matched deployment blocking query |
| Shard rollout | Bounded parallel reconciliation after Agent health |
| Crash recovery | `restart` then `reschedule` |
| Health | Nomad service liveness/readiness checks |
| Port ownership | Static named-host-network reservation |
| Drain | `shutdown_delay`, native signal, `kill_timeout` |
| Disabled shard | Job deregistration and allocation drain |
| Logs | Native allocation log rotation |

The repository does not poll OS process tables, maintain PID files, scan
ports, or implement a second crash-loop state machine.

## Development

The complete first-time setup sequence is in
[Getting started](../../../../docs/GETTING_STARTED.md#3-server-cluster). The commands
below are the Nomad-specific summary, not an alternative setup path.

Nomad 2.0.4 is pinned for the current checkout. Start one loopback development
agent:

```powershell
go run ./cmd/operations/sro-nomad dev-agent
```

The command resolves Nomad's required absolute paths and starts the pinned
agent in the foreground. It is idempotent across terminals:

- no listener starts the pinned Nomad binary;
- the exact healthy agent for this checkout is reused;
- a compatible agent that is still starting gets a bounded readiness wait;
- another Nomad configuration or a non-Nomad listener fails closed.

The check uses Nomad's agent health, effective workload-identity issuer,
registered-node, driver, host-network, and client-metadata APIs. Merely
pointing at the same config path is not enough: an Agent started before an
issuer change is refused until it is restarted. The check does not use PID
files or inspect the operating-system process table.

The `-dev` agent combines server and client roles and is not durable
production infrastructure.

Deploy from another terminal:

```powershell
go run ./cmd/operations/sro-bootstrap-development
go run ./cmd/operations/sro-nomad deploy -build
go run ./cmd/operations/sro-nomad status
```

The API defaults to `http://127.0.0.1:4646`. Credential-free commands in the
`default` namespace are accepted only when that listener proves it is the
pinned development agent for this exact checkout: development mode, ACLs
disabled, loopback bind, version, configuration path, data directory, and
workload-identity issuer must all match. Merely setting `NOMAD_ADDR` to a
different or forwarded cluster never grants the development exemption. Use
the production namespace and credential contract below for every other
cluster.

## Production

GM grants can be stored in `<state-dir>/gm-characters.txt` (by default
`.state/cluster/gm-characters.txt`) as a comma-separated `shard:character`
allowlist. `-gm-characters` or `SRO_GM_CHARACTERS` overrides this file. An
existing empty file explicitly grants nobody; an absent file retains the
loopback development default. The file is host-owned operator configuration,
never writable by a gameplay request. This prevents an ordinary redeploy from
silently dropping locally configured GM grants. GM prefixes remain actual
character names; the prefix alone grants no permissions.

For an operator rename, stop the authority through `sro-nomad stop`, then run
`go run ./cmd/operations/sro-rename-character -shard SHARD -character OLD -name NEW`
to inspect the identity, followed by `-commit` to apply. The authority lock must
be available. Update the GM allowlist if necessary and deploy again. The rename
keeps the character/account IDs and inventory and updates stored social name
references in one transaction.

For a distributed fleet, use three or five Nomad servers and separate Windows
clients. A single Linux public-test host behind a local TLS edge uses the
[single-host layout](#single-linux-host) below. Start distributed deployments from:

```text
config/server-production.hcl.example
config/client-windows-agent-production.hcl.example
config/client-windows-production.hcl.example
```

Replace every example address and certificate path. Bootstrap Nomad ACLs,
create a dedicated `sro` namespace, issue a least-privilege deployment token,
configure mTLS, and run Windows clients as a dedicated service identity with
only:

- read/execute access to the immutable release tree;
- read access to the immutable, manifest-verified server game-data projection;
- write access to Nomad data and the authority directories assigned to that
  node.

Create the production namespace, apply the deployer policy, and issue its
token through your normal secret delivery system:

```powershell
nomad namespace apply .\ops\nomad\access\sro-namespace.hcl
nomad acl policy apply sro-deployer `
  .\ops\nomad\access\sro-deployer-policy.hcl
nomad acl policy apply -namespace sro -job sro-agent -group agent -task agent `
  sro-agent-accounts .\ops\nomad\access\sro-agent-accounts-policy.hcl

# Run this in a separate administrative shell or secret broker. Do not leave
# the management token in the environment used by sro-nomad.
$IssuedDeployToken = nomad acl token create `
  -name "sro-deploy-$([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())" `
  -type client `
  -global=false `
  -policy sro-deployer `
  -ttl 1h `
  -json | ConvertFrom-Json

# Hand $IssuedDeployToken.SecretID directly to the deployment runner's secret
# input. Never Write-Output it or place it in a command-line argument.
```

Nomad job capabilities are namespace-scoped, not job-name-scoped. The policy
can submit any job inside `sro`; it cannot express an `sro-*` job prefix.
Keep this namespace dedicated to this fleet, issue short-lived deploy tokens,
and do not place unrelated workloads in it. The Variable rules remain
path-scoped to managed workload Variables and the `sro/operations/fleet`
lease. A deploy or stop holds that lease for its full mutation window. Nomad
renews it while work continues and cancels the operation if renewal fails.
For every production mutation or validation, `sro-nomad` asks Nomad to
identify the caller and refuses management tokens, global tokens, ACL roles,
extra policies, permanent tokens, tokens issued for more than two hours, or
tokens with less than twenty minutes remaining. The intended credential is a
region-local client token with exactly the direct `sro-deployer` policy and a
one-hour TTL. `status` remains compatible with a separate read-only token.
Deliver the short-lived SecretID through the deployment runner, set it only
for the command, and remove `NOMAD_TOKEN` afterward. Vault's Nomad secrets
engine may broker these tokens instead of an administrator minting them by
hand.

Variable `read` and `list` are required for read-only validation and CAS
planning; an HTTP 403 is reported as a permission error and is never treated
as a missing Variable. “Read-only” describes `validate`'s effect, not a
weaker caller identity: production validation deliberately requires the same
short-lived deployer credential because it plans jobs, reads the protected
desired-state plane, and applies the same drift guards as deployment.
`status` is the command intended for a separate read-only token. Nomad
Enterprise installations that require job-name-level policy must add a
Sentinel submission policy.

The GameWorld Variable rule names the complete automatic task path
`nomad/jobs/sro-gameworld-*/gameworld/gameworld`; its wildcard covers only
the validated shard-bearing job ID in this policy shape. GameWorld tasks do
not inherit the deploy token. Nomad's implicit workload policy independently
grants each task read/list access to its own exact job/group/task path.
Account chunks are descendants of that path and require the additional
`sro-agent-accounts` policy bound to the exact Agent workload. Without it,
ACL-enabled allocations cannot render `accounts.json` (HTTP 403). Do not
attach that policy to deployment tokens or GameWorld tasks.

### Single Linux host

The [Linux single-host runbook](LINUX_SINGLE_HOST.md) uses the durable
`config/single-linux-production.hcl.example` configuration under systemd,
ACLs, the `sro` namespace, and unprivileged raw-exec tasks. Nomad binds and
advertises only loopback; Caddy terminates public HTTPS and forwards browser
WebSockets to the loopback game listener. This topology has no node redundancy.
It uses the same deployer and jobs as development and the distributed fleet.

### Distributed private network

Deploy on a private named host network:

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

`sro-nomad` refuses a non-loopback host network in Nomad's shared `default`
namespace. `-private-network` is an explicit safety acknowledgement. It does
not make a public network private and it does not configure TLS. The named network must
be routable between the Agent and every GameWorld node and isolated behind
the intended TLS ingress and host firewall. `-allowed-origins` accepts a
comma-separated set of exact HTTP(S) browser origins. It rejects wildcards,
paths, credentials, queries, fragments, and empty entries. The deployer owns
this required jobspec value; only the loopback development topology receives
the local Vite development and preview defaults.

Non-loopback WebTransport is terminated by each GameWorld, not by the
plaintext TCP ingress. The certificate and key flags are therefore mandatory
outside loopback. Their absolute paths must exist with read-only access for
the dedicated Nomad service identity on every node eligible for a GameWorld.
The certificate must be currently valid and cover every enabled shard's
advertised `transportUrl` host; validation fails before Nomad mutation
otherwise. The leaf certificate fingerprint is part of the GameWorld jobspec,
so replacing the pair at the same paths and running `deploy` performs a
rolling restart instead of silently leaving the old in-memory certificate.
Use `-identity-jwks-url` only when the JWKS endpoint is not
`<identity-issuer>/.well-known/jwks.json`.

`validate` performs a read-only snapshot preflight: jobspec validation and
plan, generated Nomad Variable path/item limits, Variable reads, and the same
live credential-drift refusal used by deploy. It does not take the mutation
lock or reserve a future CAS result. `deploy` repeats preflight under the
fleet lock and enforces current modify indexes before writing.

Each process binds to the address and static port allocated by Nomad through
`NOMAD_IP_<label>` and `NOMAD_PORT_<label>`. GameWorld resolves the singleton
Agent through a watched Nomad native service-discovery template. It never
assumes Agent is on its own loopback. A transiently empty discovery snapshot
renders no route; GameWorld's required-route gate refuses startup until Nomad
publishes one, and the template then restarts the task with that route.

### Stateful placement

Client metadata is an authority grant:

```hcl
client {
  meta {
    sro_agent  = "false"
    sro_shards = "global-official"
  }
}
```

Agent requires `sro_agent=true`. Each GameWorld requires its shard ID in the
comma-separated `sro_shards` value. Advertise a shard only on the node that
owns its current SQLite authority and provision the catalog, assets, and
immutable release path there before deployment.

With local SQLite, Nomad intentionally cannot relocate a shard after complete
node loss. The allocation remains unplaced instead of opening a stale or
missing world. Restore a verified backup on a replacement node, remove the
old node's metadata claim, apply the claim to the replacement with
`nomad node meta apply`, wait for propagation, and redeploy. Do not label two
independent database copies as the same shard.

The default immutable release root is `.state/cluster/releases`. In a
multi-client cluster, pass `-release-dir` with a cluster-specific location
visible at the same absolute path to the deployer and every eligible client,
or distribute that exact content-addressed tree before job registration.
Nomad `raw_exec` requires an absolute host path. Clients get read/execute
permission; only the deploy identity gets write permission.

Windows does not support Nomad's isolated `exec` task driver, so these native
processes use `raw_exec`. That driver has no filesystem isolation and tasks
inherit the Nomad client service identity. This is a host trust boundary, not
a container boundary: do not co-locate unrelated workloads on these clients.
The production client examples configure the driver's `denied_envvars`, and
both jobs repeat the same denylist, so host Nomad, Consul, Vault, cloud, and
source-control credentials are scrubbed even if one layer drifts. Do not
remove either layer or replace Nomad's built-in `env.denylist` without
carrying forward its defaults.
Use a dedicated non-interactive Windows account, deny interactive logon,
grant only the paths listed above, restrict outbound and inbound traffic with
the host firewall, and use AppLocker or an equivalent application allow-list.
Never run the Nomad client as an interactive or domain administrator.
Keep Nomad at `INFO` or a stricter level. DEBUG output from the embedded
template renderer may include rendered environment values and must be treated
as a credential disclosure.

Install Nomad through its native Windows service command:

```powershell
nomad windows service install `
  -config-dir D:\nomad\config `
  -data-dir D:\nomad\data `
  -install-dir D:\nomad\bin
```

## Failure behavior

### GameWorld crash

Nomad restarts the task in its allocation. GameWorld uses a 12-second restart
delay plus Nomad jitter, exceeding the ten-second Agent lease left by a
hard-killed writer. If restart attempts are exhausted, Nomad applies the
bounded reschedule policy.

### Agent crash

Nomad restarts Agent independently. GameWorld tolerates transient heartbeat
failure. If Agent remains unavailable for three heartbeat attempts,
GameWorld exits fail-closed and Nomad recovers it after Agent is available.

### Dependency degradation

Readiness fails without triggering a restart. This removes a degraded
allocation from deployment health and prevents a shared dependency incident
from becoming an immediate restart storm. Repeated liveness failure is a
restart signal.

The jobspecs make readiness a native Nomad service check with
`on_update = "require_healthy"`. The deployer waits for the exact submitted
job version and does not duplicate those checks with an HTTP polling loop.
The separate browser live-stack probes validate the advertised ingress after
deployment.

### Partial rollout failure

Agent reaches a healthy deployment first, then GameWorld jobs reconcile with
bounded parallelism. Each job has one deadline for its complete reconciliation
(plan, optimistic registration, deployment health, and allocation
convergence); phase changes do not reset it. The deployer derives that budget
from the parsed jobspec's `progress_deadline` plus one minute for observation,
so Nomad's native deadline remains authoritative. The command itself is
cancelled by the operator signal rather than a fixed timeout that scales
incorrectly with shard count. One failed shard does not stop other shard
reconciliations. Nomad auto-reverts the failed job, retains its monotonically
versioned deployment history, and keeps referenced immutable releases.

Correct the failed binary, jobspec, placement, or authority input and rerun
`sro-nomad deploy`. The declarative job state is the recovery artifact. Do
not add a second fleet rollback journal or try to transact independent shard
authorities as one unit.

### Nomad client loss

Production placement and replacement behavior must be selected with Nomad's
`disconnect` policy for the actual node/network topology. Do not add an
application-side node-loss timer.

### Static port conflict

Nomad reserves each static port on the named host network. `ignore_collision`
is absent. The scheduler leaves a conflicting allocation unplaced and reports
the failed placement through plan/evaluation metrics.

### Policy or binary update

The release identity includes the executable and jobspec hashes. The deployer
atomically publishes each binary at
`releases/<role>/<release-id>/<binary>`, verifies the staged digest, and puts
that absolute immutable path in the job. A changed binary or lifecycle policy
therefore forces a real allocation replacement. Old releases are pruned only
after the complete enabled fleet reaches its intended versions and no
retained Nomad job version references them. This preserves native
`nomad job revert` rollback.

Runtime job variables are intentionally not part of this artifact cache key.
Nomad job versions record catalog-derived ports and paths; workload Variables
record secrets with CAS. Including either would duplicate identical binaries
and could expose secret rotation through release path names.

### Credential change

Agent owns an Ed25519 private signing ring. GameWorld Variables receive only
its public projection. Rotate it online with the guarded command:

```powershell
go run ./cmd/operations/sro-nomad rotate-session-key `
  -namespace sro `
  -host-network game-private `
  -agent-url http://agent.service.consul:8787 `
  -private-network
```

The command takes the fleet lock, adds one pending key, publishes its public
ring to every running GameWorld, and requires exact digest acknowledgement
from each `/readyz` endpoint before Agent begins signing with it. The old
public key overlaps for the 13-hour maximum token horizon. Local pending state
makes every phase resumable, including the narrow case where Agent activated
before the deployer persisted completion.

Before changing either local or Nomad key state, rotation requires the Agent
and the exact enabled GameWorld job set to be registered and running. It then
performs a bounded parallel current-key preflight against Agent `/readyz` and
every enabled shard's private `controlUrl` `/readyz`. The endpoints must be
ready, report a key-ring digest compatible with the durable resumable state,
and Agent must report the expected active key. A
disabled-but-still-registered, missing, stopped, dead, degraded, or
unreachable managed process fails closed before any file or Variable is
changed. Run `sro-nomad deploy` to repair allocation state.

The machine running rotation must therefore route to Agent `-agent-url` and
every private catalog `controlUrl`. Run it on the private deployment runner
or a bastion with those routes; never make control listeners public to solve
reachability. Rotation never silently reconciles topology—deployment remains
the sole owner of that operation.

GameWorld control calls use renewable Nomad workload-identity JWTs rather than
a shared secret. Nomad refreshes the identity file around half its TTL and the
reporter reads the current file for each call. EnterWorld tickets use a random
key that exists only inside one GameWorld process. Neither mechanism needs an
operator rotation window.

The account catalog only seeds the Agent's live account database (see
LINUX_SINGLE_HOST.md, "Accounts and the provisioning API"); accounts created
through the provisioning API never pass through it.

The account catalog may be as large as the server's one-MiB input limit.
Nomad Variables limit one variable to 64 KiB, so the deployer divides the
validated UTF-8 document into ordered 48-KiB workload variables:

```text
nomad/jobs/sro-agent/agent/agent/accounts/000000
nomad/jobs/sro-agent/agent/agent/accounts/000001
...
```

The Agent allocation concatenates those chunks into `accounts.json` before
startup. Chunk creation, replacement, and removal use Nomad CAS indexes.
Unknown paths are never deleted. Changing the account catalog while Agent is
registered is refused; stop Agent/fleet, update the catalog, then deploy.
The one-MiB application limit yields at most 22 short HCL references. Account
payloads are never injected into the jobspec itself.

## Live integration test

The auto-revert and graceful-stop acceptance test deliberately registers a
unique raw-exec job, submits one broken version, waits for Nomad's automatic
revert, and purges the recovered job. It is therefore opt-in and Windows-only:
ordinary `go test ./...` must not mutate whichever scheduler a developer's
`NOMAD_ADDR` happens to name.

Run it only against the repository's isolated development agent:

```powershell
$env:SRO_NOMAD_INTEGRATION = '1'
try {
  go test -count=1 `
    -run '^TestNomadAutoRevertAndGracefulStopIntegration$' `
    ./cmd/operations/sro-nomad
} finally {
  Remove-Item Env:SRO_NOMAD_INTEGRATION -ErrorAction SilentlyContinue
}
```

Never set this switch in a production namespace or a general-purpose shared
Nomad cluster.

## Official references

- [Nomad architecture](https://developer.hashicorp.com/nomad/docs/architecture)
- [Production requirements](https://developer.hashicorp.com/nomad/docs/deploy/production/requirements)
- [Windows service installation](https://developer.hashicorp.com/nomad/docs/deploy/production/windows-service)
- [Raw-exec driver](https://developer.hashicorp.com/nomad/docs/job-declare/task-driver/raw_exec)
- [Exec driver platform support](https://developer.hashicorp.com/nomad/docs/deploy/task-driver/exec)
- [Runtime network variables](https://developer.hashicorp.com/nomad/docs/reference/runtime-variable-interpolation)
- [Nomad native service discovery](https://developer.hashicorp.com/nomad/docs/networking/service-discovery)
- [Job plan and modify-index enforcement](https://developer.hashicorp.com/nomad/commands/job/plan)
- [Jobs HTTP API](https://developer.hashicorp.com/nomad/api-docs/jobs)
- [Deployment HTTP API](https://developer.hashicorp.com/nomad/api-docs/deployments)
- [Job history and version API](https://developer.hashicorp.com/nomad/api-docs/jobs)
- [Blocking queries](https://developer.hashicorp.com/nomad/api-docs)
- [Nomad Variables CAS](https://developer.hashicorp.com/nomad/docs/manage/variables)
- [Nomad Variable Locks](https://developer.hashicorp.com/nomad/api-docs/variables/locks)
- [Variables and workload paths](https://developer.hashicorp.com/nomad/docs/concepts/variables)
- [Variables API limits](https://developer.hashicorp.com/nomad/api-docs/variables/variables)
- [Workload identity](https://developer.hashicorp.com/nomad/docs/concepts/workload-identity)
- [Workload identity specification](https://developer.hashicorp.com/nomad/docs/reference/workload-identity-specification)
- [Nomad JSON Web Key Set](https://developer.hashicorp.com/nomad/api-docs/operator/identity)
- [ACL policy specification](https://developer.hashicorp.com/nomad/docs/other-specifications/acl-policy)
- [ACL token creation](https://developer.hashicorp.com/nomad/commands/acl/token/create)
- [Vault-issued Nomad tokens](https://developer.hashicorp.com/nomad/docs/secure/acl/tokens/vault)
- [Namespaces](https://developer.hashicorp.com/nomad/docs/govern/namespaces)
- [Client environment denylist](https://developer.hashicorp.com/nomad/docs/configuration/client)
- [Template behavior](https://developer.hashicorp.com/nomad/docs/job-specification/template)
- [Placement constraints](https://developer.hashicorp.com/nomad/docs/job-specification/constraint)
- [Dynamic node metadata](https://developer.hashicorp.com/nomad/docs/commands/node/meta/apply)
- [Restart policy](https://developer.hashicorp.com/nomad/docs/job-specification/restart)
- [Reschedule policy](https://developer.hashicorp.com/nomad/docs/job-specification/reschedule)
- [Rolling updates and auto-revert](https://developer.hashicorp.com/nomad/docs/job-specification/update)
- [Network and static ports](https://developer.hashicorp.com/nomad/docs/job-specification/network)
- [Allocation stop/reschedule API](https://developer.hashicorp.com/nomad/api-docs/allocations)
- [Nomad snapshots](https://developer.hashicorp.com/nomad/api-docs/operator/snapshot)
