# Getting started

From a fresh checkout to playing locally: build the game data, run the game,
log in. One command runs the whole stack on your machine - no deployment
tooling, nothing to install beyond the prerequisites below.

Local development targets one Windows 10/11 workstation; macOS (Apple Silicon)
also works, see [Developing on macOS](#developing-on-macos). Production
deployment is described in [DEPLOYMENT.md](../../apps/server/ops/docs/DEPLOYMENT.md).

## 1. Prerequisites

| Tool | Version |
| --- | --- |
| Go | 1.27.1, the release `apps/server/go.mod` names; an older Go downloads it automatically |
| Node.js | 24 LTS (CI runs it); 22.15 is the oldest supported |
| pnpm | 12.6.0, the version `package.json` `packageManager` names; through Corepack (`npm install --global corepack` if missing) |
| Python | 3.12 (`py -3`) with `py -3 -m pip install -r scripts/requirements-build.txt`: client extraction, image and font conversion, cursor extraction |
| ffmpeg | Any current build on `PATH`; `assets prepare` converts the client's OGG music to MP3 with it |
| DirectX End-User Runtime | Install [Microsoft's runtime](https://www.microsoft.com/en-us/download/details.aspx?id=35); lens mip generation uses its 32-bit `d3dx9_39.dll`. Windows' built-in DirectX alone does not supply this library. |
| Browser | Current Chrome or Edge with WebGPU |

```powershell
go version; node --version; corepack pnpm --version
```

## 2. Game data

Retail game media is not in this repository. You need a Silkroad Online
v1.150 (Legend III) client that you have the right to use. The tools below extract and
convert it on your machine; nothing is uploaded. On Windows the asset build
reproduces the original lens mips (32-bit D3DX) and font atlas (GDI) byte for
byte; on macOS portable generators produce equivalent output.

The **game root** is the client folder. By default it is the folder that
contains this checkout; to keep the client elsewhere, set `SRO_GAME_ROOT` to
it (the folder holding `SRO_Client.exe`, not `extracted`):

```text
<game root>\
  SRO_Client.exe
  Media.pk2  Data.pk2  Map.pk2  Particles.pk2  Music.pk2
  extracted\          <- written by `assets prepare`
  <this checkout>\    <- any name, when it lives here
```

Then, from the checkout:

```powershell
$env:SRO_GAME_ROOT = 'D:\Games\Silkroad-v1.150'   # only if the client is elsewhere
corepack pnpm install --frozen-lockfile
py -3 -m pip install -r scripts/requirements-build.txt
corepack pnpm assets prepare            # extract the archives, convert the music (a few minutes)
corepack pnpm assets doctor             # every input and tool, with the fix for each problem
corepack pnpm assets build full         # outdoor world, everything else, every family; about 6 minutes on 16 cores
corepack pnpm assets ledger             # must report nothing unclaimed
corepack pnpm task build server-game-data
corepack pnpm assets check integrity
```

`assets prepare` writes `extracted\Media_extracted`, `Data_extracted`,
`Map_extracted`, `Particles_extracted` and `Music_mp3`, byte for byte from
your archives. Running it again repairs missing or damaged files and leaves
correct ones alone. `assets doctor` only reads; run it whenever a build
refuses to start. The first build must be `assets build full`: plain
`assets build` reuses the outdoor world of an earlier full build and has
none on a fresh machine.

The second `assets build` and `assets ledger` check the tree against the
publication ledger: every packed file must come from a current build step,
so nothing that only exists on your machine reaches a release.
[ASSET_DELIVERY.md](../architecture/ASSET_DELIVERY.md) explains the ledger, and how to
size the build with `SRO_BUILD_JOBS` (default: every core but one).

The results land in the ignored `.generated/` folder: browser assets in
`.generated/client-public/assets/`, the server projection in
`.generated/game-data/1.150/`. See [ASSET_PIPELINE.md](../architecture/ASSET_PIPELINE.md).

This prepares data for the browser client and the Go server. The original
`SRO_Client.exe` does not connect to this server: the server speaks to
browsers over WebTransport/WebSocket.

## 3. Run the game

One command, from the checkout:

```powershell
corepack pnpm local
```

That is the whole recipe. The command builds the Agent and GameWorld
binaries (the Go build cache makes repeats take seconds), provisions the
development world on first run - cluster credentials, the shared
development account, an empty authority for the enabled shard - and then
runs three processes as its own children, each prefixed in the output:

- the **Agent** on <http://127.0.0.1:8787> (logins, shard directory),
- one **GameWorld** for the shard `LOCAL_SHARD_ID` names (default
  `global-official` in `apps/server/config/shards.json`), transport on
  <http://127.0.0.1:8788>, control on <http://127.0.0.1:8791>,
- the **Vite** dev server on <http://127.0.0.1:5180>, which proxies
  `/api` to the Agent and `/shards/<id>` to the GameWorld so the browser
  only talks to its own origin.

When everything answers its readiness check the runner prints the line

```text
Local game is ready: open http://127.0.0.1:5180/ and log in as tester / 123123 (...)
```

and waits. Open the URL and log in with the development account from
`apps/server/config/dev-account.env`. A new world has no characters;
create one on `global-official`. Press **Ctrl+C** in the runner's
terminal to stop: on Windows the children share the runner's console and
each closes its own store; elsewhere the runner forwards SIGTERM. A
process that outlives a 30-second grace period is killed, and if one of
the three exits early the runner stops the rest - nothing is left
orphaned either way.

The first boot of a fresh world fills the world population and can take
a few minutes on a slower machine (seconds on a warm one); the runner
keeps waiting and says what it is waiting for. If a prerequisite is
missing - Go, unbuilt game data, a port already in use - the runner
names it and its fix before starting anything. `pnpm local --doctor`
runs exactly those checks without starting anything.

Two flags change the run:

- `pnpm local --doctor` - the prerequisite checks only, each with its fix.
- `pnpm local --reset` - asks, then moves the local world (accounts,
  characters, cluster state under `apps/server/.state`) to
  `apps/server/.state/backups/local-reset-<timestamp>` and starts fresh.
  It never deletes; delete the backup yourself once the new world is good.

Settings for the run live in the checked-in
`apps/server/config/local.env`: `LOCAL_SHARD_ID` picks the shard,
`LOCAL_AGENT_ADDR` the Agent's listen address, and any other `KEY=VALUE`
line is passed to both server processes as environment (server tuning
flags such as `SRO_TICK_RATE` go there - see `apps/server/AGENTS.md`).

`pnpm dev:https` serves the same client over TLS for other devices on
your network, once the stack is up; see
[HOSTING.md](../../apps/client-next/docs/HOSTING.md).

## 4. Daily workflow

```powershell
corepack pnpm local
```

Edit, stop with Ctrl+C, run it again. Re-running reuses the world under
`apps/server/.state`; only `--reset` starts over. Server and client code
changes need nothing else: the binaries are rebuilt on every run, and
Vite serves client edits as you save.

Contributors changing the local stack itself can verify it end to end
without a browser:

```powershell
node scripts/local/smoke.mjs
```

The smoke starts the real stack, logs the development account in over
the wire, checks GameWorld readiness, stops everything, and asserts no
port is left bound.

## Developing on macOS

The same steps work on an Apple Silicon Mac, with these differences.

**Tools.** Install Go, Node.js (Corepack), ffmpeg and Python 3.12, for example
with Homebrew and [uv](https://docs.astral.sh/uv/):

```sh
brew install go node ffmpeg uv unar
corepack enable --install-directory ~/.local/bin pnpm
uv venv --python 3.12 .venv && uv pip install --python .venv/bin/python -r scripts/requirements-build.txt
export SRO_PYTHON="$PWD/.venv/bin/python"   # used by every asset build step
```

There is no `py` launcher outside Windows: build steps run `SRO_PYTHON`, then
`python3`, then `python`. A client shipped as a `.rar` extracts with
`unar`.

**Assets.** No DirectX runtime is needed. `scripts/build/native_texture_mips.py`
generates the lens and character mip resources (authored levels are copied,
missing levels are box-filtered; not byte-identical to D3DX), and the font
atlas is rasterized with FreeType from the system Arial (`SRO_FONT_ARIAL` and
`SRO_FONT_ARIAL_BOLD` override the faces). `pnpm assets doctor` reports the
platform as a warning, not a failure. Character mip caches are keyed by
generator, so Windows and macOS builds never share entries.

**Known gaps.** Native verification tools that execute the retail PE
(`apps/client-next/tools/verify-*`, `audit-*`) still need Windows, and
`ui-glyphs` parity tests compare against GDI glyph metrics.

## 5. Troubleshooting

| Symptom | Fix |
| --- | --- |
| A `MISS` line from `pnpm local` | It names the prerequisite and its fix; `pnpm local --doctor` reruns the same checks without starting anything. |
| Port 5180, 8787, 8788 or 8791 already in use | Another `pnpm local` (or a leftover from a crashed one) is running. Stop it; on Windows `Get-NetTCPConnection -State Listen -LocalPort 5180` names the owning process. |
| Missing game-data or asset file | Run `pnpm assets doctor`, then rerun `pnpm assets build full` and `pnpm task build server-game-data`, and clear any `SRO_SERVER_GAME_DATA_*` environment overrides. |
| `INVALID_CREDENTIALS` at login | Use the account in `apps/server/config/dev-account.env` (default `tester` / `123123`). If you changed that file after provisioning, reset the local world (`pnpm local --reset`). |
| A process exits early during a run | The runner prints its last output lines after the failure and stops the rest. Antivirus scanning a freshly built server binary is the usual cause on Windows; rerun and give it a few minutes. |
