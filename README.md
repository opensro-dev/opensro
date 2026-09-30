# OpenSRO

[![Discord](https://img.shields.io/discord/1521398829260210337?label=Discord&logo=discord&logoColor=white&color=5865F2)](https://discord.gg/RUua9HY657)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
[![Support on Ko-fi](https://img.shields.io/badge/Ko--fi-support-FF5E5B?logo=kofi&logoColor=white)](https://ko-fi.com/skillman1337)

A Silkroad Online v1.150 (Legend III) server written in Go and a WebGPU browser
client, plus the asset pipeline that turns a licensed client installation into
browser- and server-ready data.

**To run it locally, follow [docs/GETTING_STARTED.md](docs/GETTING_STARTED.md).**

## Layout

| Folder | Contents |
| --- | --- |
| `apps/server/` | Go Agent and GameWorld processes, Nomad jobs, operator docs |
| `apps/client-next/` | Browser client (TypeScript, WebGPU, Vite) |
| `apps/server-observatory/` | Local read-only operations dashboard |
| `scripts/` | Asset pipeline, repository checks and the `pnpm task` runner |
| `docs/` | Setup, architecture and asset-pipeline documentation |
| `patches/` | Dependency patches applied by pnpm |

Ignored and local only: `.generated/` (all generated game data), `.state/`
(caches and locks), `.tools/` (pinned binaries such as Nomad), `temp/` (scratch
output), `node_modules/`.

Retail game media is never committed. The asset build reads a licensed
extraction next to the checkout; see
[docs/ASSET_PIPELINE.md](docs/ASSET_PIPELINE.md).

## Common commands

| Command | Does |
| --- | --- |
| `pnpm dev` | Client development server on <http://127.0.0.1:5180> |
| `pnpm assets prepare` | Extract your licensed client into `extracted/` |
| `pnpm assets doctor` | Check the game root, extraction and build tools |
| `pnpm assets build full` | Full asset build, outdoor world included; `assets build` reuses the last outdoor world |
| `pnpm check source` | CI gate; needs no game data |
| `pnpm check` | Every gate, including asset-dependent suites |
| `pnpm task list` | Every task with its prerequisites |

## Code style

All code follows the id Software (Quake III Arena) style: header and function
banners, plain data and functions, explicit lifecycles. dprint enforces the
JavaScript/TypeScript layout and gofmt the Go layout. The rules for humans and
coding agents are in [AGENTS.md](AGENTS.md).

## Documentation

[docs/README.md](docs/README.md) indexes everything. Releases follow
[RELEASE.md](RELEASE.md). Security reports: [SECURITY.md](SECURITY.md).
Licensed under [AGPL-3.0-or-later](LICENSE); origin, trademark and asset
scope are in [NOTICE.md](NOTICE.md).
