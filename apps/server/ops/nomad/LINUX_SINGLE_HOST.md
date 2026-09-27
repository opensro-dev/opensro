# Linux single-host public test deployment

Use Nomad 2.0.4 under systemd with
`config/single-linux-production.hcl.example`. This is a durable single-node
server/client with ACLs, not `-dev`. It has no node redundancy. HTTP, RPC,
gossip and game/control listeners stay on loopback. Use the distributed TLS
examples when control traffic crosses machines.

Install the configuration as root in `/etc/nomad.d/nomad.hcl`, validate with
`nomad config validate /etc/nomad.d`, then enable `nomad.service`. Explicit
loopback `advertise` addresses are required outside development mode. The
Linux Nomad client runs as root so it can launch tasks as the dedicated
`sro` system user. Give `sro` a non-login shell and no interactive credentials.
Both the client and jobs retain the same `denied_envvars` list.

Bootstrap ACLs once and retain the management credential in a root-only file.
Apply `access/sro-namespace.hcl`, `sro-deployer-policy.hcl`, and the workload
binding documented in [Production](README.md#production):

```sh
nomad acl policy apply -namespace sro -job sro-agent -group agent -task agent \
  sro-agent-accounts ops/nomad/access/sro-agent-accounts-policy.hcl
```

Issue a one-hour, region-local client token with only the direct `sro-deployer`
policy for each deployment. Supply its SecretID through `NOMAD_TOKEN`, never
stdout or command arguments, and revoke it afterward. The administrative
token must not enter the deployer's environment.

## Runtime tree and permissions

Place static Linux `agent`, `gameworld`, and operations binaries in a module
tree such as `/opt/opensro/apps/server`, alongside `go.mod`, `config/shards.json`
and `ops/nomad/jobs`. Build with `GOOS=linux CGO_ENABLED=0 go build` on the build
machine. The deployer selects executable names and the node kernel from its
own OS; run the Linux deployer on the target host.

Provision identity with `sro-provision-identity`, a real bcrypt account catalog,
and `sro-init -shard global-official`. Keep the catalog and signing-key input
files root-only. An empty `.state/cluster/gm-characters.txt` grants no GM
characters. Set permissions after provisioning:

- `.state/cluster`: `root:sro`, mode `0710`, allowing task traversal without
  exposing the credential inputs. Identity provisioning secures this directory
  as `0700`, so restore traversal after running that provisioner.
- Immutable releases and catalog: task read/execute access, no task write
  access. Staged release directories and executables use `0755`.
- `.state/cluster/agent`, `.state/cluster/dev-certs`, and
  `.state/shards/global-official/authority`: `sro:sro`, mode `0700`.
- Game-data archive and its verified materialized cache: task read access.
  Privileged validation can materialize `server.srogz` first; publish cache
  directories as `0755` and files as `0644`, owned by the deploy identity.

`-task-user sro` resolves its numeric UID/GID and assigns rendered secrets to
that account with mode `0600`. If this layout is expanded, keep those account
IDs identical on every eligible node. Keep all credential, authority, and
server game-data paths outside the public web root.

## Deploy and measure

From the module directory, with only the short-lived token in `NOMAD_TOKEN`:

```sh
export NOMAD_ADDR=http://127.0.0.1:4646
export NOMAD_NAMESPACE=sro
export SRO_SERVER_GAME_DATA_ROOT=/opt/opensro/.generated/game-data/1.150/server.srogz

./sro-nomad validate -namespace sro -task-user sro \
  -allowed-origins https://opensro.online \
  -agent-cpu 100 -gameworld-cpu 500 \
  -agent-memory-mb 128 -gameworld-memory-mb 384
./sro-nomad deploy -namespace sro -task-user sro \
  -allowed-origins https://opensro.online \
  -agent-cpu 100 -gameworld-cpu 500 \
  -agent-memory-mb 128 -gameworld-memory-mb 384
./sro-nomad status -namespace sro
unset NOMAD_TOKEN
```

CPU and memory values are scheduling reservations, not a capacity guarantee.
Measure host memory, swap, task RSS, and CPU after startup and with players
connected. Include Nomad's executor and log helpers. The default GameWorld
reservation does not fit a one-GiB VPS. Benchmark fixture controls are enabled
only in the loopback `default` development namespace, never in `sro`.

Keep catalog control/transport URLs on loopback and advertise
`/shards/global-official` publicly. Caddy strips `/api` before proxying to
`127.0.0.1:8787`, and strips `/shards/global-official` before proxying transport
requests to `127.0.0.1:8788`. Its normal reverse proxy handles WebSocket upgrades
and forwarded HTTPS metadata. This edge-route deployment uses WebSocket.
Public WebTransport requires the separate GameWorld TLS deployment described
in the main operations guide.
