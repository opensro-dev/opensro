# Getting started

From a fresh checkout to playing locally: build the game data, run the server
cluster, run the browser client, log in.

Local development targets one Windows 10/11 workstation. Production deployment
is described in [DEPLOYMENT.md](../apps/server/ops/docs/DEPLOYMENT.md).

## 1. Prerequisites

| Tool | Version |
| --- | --- |
| Go | 1.27.1, the release `apps/server/go.mod` names; an older Go downloads it automatically |
| Node.js | 24 LTS (CI runs it); 22.15 is the oldest supported |
| pnpm | 12.6.0, the version `package.json` `packageManager` names; through Corepack (`npm install --global corepack` if missing) |
| Python | 3.12 (`py -3`) with `py -3 -m pip install -r requirements-build.txt`: client extraction, image and font conversion, cursor extraction |
| ffmpeg | Any current build on `PATH`; `assets prepare` converts the client's OGG music to MP3 with it |
| DirectX End-User Runtime | Install [Microsoft's runtime](https://www.microsoft.com/en-us/download/details.aspx?id=35); lens mip generation uses its 32-bit `d3dx9_39.dll`. Windows' built-in DirectX alone does not supply this library. |
| Nomad | 2.0.7 at `.tools/nomad/2.0.7/nomad.exe` (see below) |
| Browser | Current Chrome or Edge with WebGPU |

```powershell
go version; node --version; corepack pnpm --version
```

### Nomad

The server processes run under HashiCorp Nomad, even locally. Install the
pinned binary into the repository's ignored `.tools/` folder, verifying
HashiCorp's published checksum:

```powershell
$Version = '2.0.7'
$File = "nomad_${Version}_windows_amd64.zip"
$Dir = ".tools\nomad\$Version"
$Zip = Join-Path $env:TEMP $File
New-Item -ItemType Directory -Force $Dir | Out-Null
Invoke-WebRequest "https://releases.hashicorp.com/nomad/$Version/$File" -OutFile $Zip
$Sums = Invoke-WebRequest "https://releases.hashicorp.com/nomad/$Version/nomad_${Version}_SHA256SUMS"
$Expected = ($Sums.Content -split "`n" | Where-Object { $_ -match [regex]::Escape($File) }) -split '\s+' | Select-Object -First 1
if ((Get-FileHash -Algorithm SHA256 $Zip).Hash.ToLower() -ne $Expected) { throw 'Nomad checksum mismatch' }
Expand-Archive $Zip -DestinationPath $Dir -Force
& "$Dir\nomad.exe" version
```

## 2. Game data

Retail game media is not in this repository. You need your own licensed
Silkroad Online v1.150 (Legend III) client. The tools below extract and
convert it on your machine; nothing is uploaded. The asset build runs on
Windows only (32-bit D3DX lens mips, the GDI font atlas).

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
py -3 -m pip install -r requirements-build.txt
corepack pnpm assets prepare            # extract the archives, convert the music (a few minutes)
corepack pnpm assets doctor             # every input and tool, with the fix for each problem
corepack pnpm assets build full         # outdoor world, then everything else; roughly 40 minutes
corepack pnpm assets publish            # families the full build does not produce yet
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

The results land in the ignored `.generated/` folder: browser assets in
`.generated/client-public/assets/`, the server projection in
`.generated/game-data/1.150/`. See [ASSET_PIPELINE.md](ASSET_PIPELINE.md).

This prepares data for the browser client and the Go server. The original
`SRO_Client.exe` does not connect to this server: the server speaks to
browsers over WebTransport/WebSocket.

## 3. Server cluster

Use three terminals, all opened in `rebuild`.

**Terminal A: local Nomad.** Leave it running.

```powershell
go -C apps/server run ./cmd/operations/sro-nomad dev-agent
```

It starts one loopback Nomad server and client with the pinned binary, reuses a
compatible agent that is already running, and refuses a mismatched one. Do not
start `nomad agent -dev` by hand.

**Terminal B: provision once, then deploy.**

```powershell
go -C apps/server run ./cmd/operations/sro-bootstrap-development
go -C apps/server run ./cmd/operations/sro-nomad deploy -build
go -C apps/server run ./cmd/operations/sro-nomad status
```

`sro-bootstrap-development` creates the local credentials and an empty world
for each enabled shard under `apps/server/.state/`, using the development
account in `apps/server/config/dev-account.env`. It never overwrites a valid
existing world. `deploy -build` builds the Agent and GameWorld binaries and
rolls them out; re-running it is the normal update path.

`status` should report the Agent and one GameWorld per enabled shard in
`apps/server/config/shards.json` (by default only `global-official`):

```text
sro-agent                        configured=true  status=running
sro-gameworld-global-official    configured=true  status=running
```

Both processes also answer readiness checks:

```powershell
Invoke-WebRequest http://127.0.0.1:8787/readyz            # Agent
Invoke-WebRequest http://127.0.0.1:8788/transport/readyz  # GameWorld global-official
```

## 4. Browser client

**Terminal C:**

```powershell
corepack pnpm dev
```

Open <http://127.0.0.1:5180/> and log in as `tester` / `123123`. A new world
has no characters; create one on `global-official`.

The development server proxies `/api` to the Agent and `/shards/<id>` to each
GameWorld, so the browser only talks to its own origin. `pnpm dev:https`
serves the same client over TLS for other devices on your network; see
[HOSTING.md](../apps/client-next/docs/HOSTING.md).

## 5. Daily workflow

1. Terminal A: `go -C apps/server run ./cmd/operations/sro-nomad dev-agent`
2. Terminal B: `go -C apps/server run ./cmd/operations/sro-nomad deploy -build`
3. Terminal C: `corepack pnpm dev`

To stop, run `go -C apps/server run ./cmd/operations/sro-nomad stop`, wait for
it to finish, then press Ctrl+C in terminal A. Stopping Nomad first can cut off
a GameWorld's graceful database shutdown.

## 6. Resetting the local world

With the fleet stopped and Nomad closed, move the old state aside instead of
deleting it, then repeat step 3:

```powershell
$Backup = "apps\server\.state\backups\dev-reset-$(Get-Date -Format yyyyMMdd-HHmmss)"
New-Item -ItemType Directory -Force $Backup | Out-Null
foreach ($Path in 'cluster', 'shards', 'nomad\dev-agent') {
  $Full = "apps\server\.state\$Path"
  if (Test-Path $Full) { Move-Item $Full $Backup }
}
```

## 7. Troubleshooting

| Symptom | Fix |
| --- | --- |
| `nomad.exe` missing | Install it as in step 1, or pass `-nomad-binary <path>` to `dev-agent` (only 2.0.7 is accepted). |
| Port 4647 already in use | Another Nomad is running. Stop it from the terminal or service that owns it, then rerun `dev-agent`. |
| Jobs slow to become healthy after a rebuild | Antivirus may be scanning the new `agent.exe`/`gameworld.exe`. Wait; the jobs allow several minutes. Check with `sro-nomad status`. |
| Missing game-data or asset file | Run `pnpm assets doctor`, then rerun `pnpm assets build full`, `pnpm assets publish` and `pnpm task build server-game-data`, and clear any `SRO_SERVER_GAME_DATA_*` environment overrides. |
| `INVALID_CREDENTIALS` at login | Use `tester` / `123123`; rerun `sro-bootstrap-development`. If you changed `dev-account.env`, reset the local world (step 6). |
| A job keeps restarting | `.tools\nomad\2.0.7\nomad.exe alloc logs <alloc-id> gameworld` (with `NOMAD_ADDR=http://127.0.0.1:4646`), fix the reported prerequisite, then `deploy` again. |
