# 🏯 OpenSRO

[![Discord](https://img.shields.io/discord/1521398829260210337?label=Discord&logo=discord&logoColor=white&color=5865F2)](https://discord.gg/RUua9HY657)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
[![Support on Ko-fi](https://img.shields.io/badge/Ko--fi-support-FF5E5B?logo=kofi&logoColor=white)](https://ko-fi.com/skillman1337)

**Silkroad Online v1.150 (Legend III), rebuilt end to end: play it in your browser.**

OpenSRO is a complete game stack, not just an emulator:

- 🎮 **A full browser client:** TypeScript and WebGPU, no install and no plugin. The original interface, world, characters, effects and sound, drawn natively in the browser.
- ⚔️ **A full game server:** Go, with an Agent (login, accounts, characters) and GameWorld shards (combat, skills, monsters, quests, guilds, fortresses, PK, trade).
- 🧰 **An asset pipeline:** turns your own v1.150 client installation into compact browser- and server-ready game data.
- 🛰️ **Operations tooling:** Nomad jobs, a release pipeline, and an observatory dashboard for running a live server.

Behaviour is ported from the original game, not guessed: the v1.150 client defines what exists, and server rules follow the original server's instructions. Reverse-engineering evidence (function addresses) sits right next to the code that ports it.

> 🎯 **Join the beta** on [opensro.online](https://opensro.online) and our [Discord](https://discord.gg/RUua9HY657).

---

## 🚀 Quick start

The full walkthrough, with the exact tool versions, is [docs/GETTING_STARTED.md](docs/GETTING_STARTED.md). In short:

1. **Install the tools:** Go, Node.js 24, pnpm (through Corepack), Python 3.12, and a WebGPU browser (current Chrome or Edge). Nomad is a pinned download.
2. **Build the game data** from your own v1.150 client:
   ```sh
   corepack pnpm install --frozen-lockfile
   corepack pnpm assets prepare
   corepack pnpm assets build full     # about 40 minutes, once
   corepack pnpm task build server-game-data
   ```
3. **Start the server cluster** (two terminals):
   ```sh
   go -C apps/server run ./cmd/operations/sro-nomad dev-agent
   go -C apps/server run ./cmd/operations/sro-nomad deploy -build
   ```
4. **Start the client** with `corepack pnpm dev` and open <http://127.0.0.1:5180>. 🎉

Only changing code, not game data? `corepack pnpm check source` runs the CI gates with no game data at all.

---

## 🗺️ Where things live

| Folder | What's inside |
| --- | --- |
| 🎮 `apps/client-next/` | The browser client: TypeScript, WebGPU, Vite |
| ⚔️ `apps/server/` | The Go Agent and GameWorld, Nomad jobs, operator docs |
| 🛰️ `apps/server-observatory/` | Operations dashboard, with optional authenticated player recovery |
| 🧰 `scripts/` | Asset pipeline, repository checks and the `pnpm task` runner |
| 📚 `docs/` | Setup, architecture, asset pipeline and design notes |
| 🩹 `patches/` | Dependency patches applied by pnpm |

Local only and ignored: `.generated/` (all generated game data), `.state/` (caches and locks), `.tools/` (pinned binaries such as Nomad), `temp/` (scratch output) and `node_modules/`.

Retail game media is never committed. The asset build reads an extraction of your own client next to the checkout; see [docs/ASSET_PIPELINE.md](docs/ASSET_PIPELINE.md).

---

## ⌨️ Everyday commands

| Command | What it does |
| --- | --- |
| `pnpm dev` | Client dev server on <http://127.0.0.1:5180> |
| `pnpm assets prepare` | Extract your client into `extracted/` |
| `pnpm assets doctor` | Check the game root, extraction and build tools, with a fix for each problem |
| `pnpm assets build full` | Full asset build, outdoor world included (`assets build` reuses the last outdoor world) |
| `pnpm check source` | The CI gate; needs no game data |
| `pnpm --filter @sro/client-next check` | Every client gate |
| `pnpm check` | Every gate, including the asset-dependent suites |
| `pnpm task list` | Every task with its prerequisites |

---

## 🤝 Contributing

New here? Start with **[CONTRIBUTING.md](CONTRIBUTING.md)**: setup, the native-first rule, how to put new ideas behind a flag, and what a good pull request looks like. Then pick an issue labelled [good first issue](https://github.com/opensro-dev/opensro/labels/good%20first%20issue) or say hi in #🛠️contributing on [Discord](https://discord.gg/RUua9HY657).

- 🎨 **Code style:** id Software (Quake III Arena): file and function banners, plain data and functions, explicit lifecycles. dprint formats JavaScript/TypeScript and gofmt formats Go.
- 🤖 **AI coding agents are welcome** and follow the same rules as humans: [AGENTS.md](AGENTS.md).

---

## 📚 More

- 📖 [docs/README.md](docs/README.md): the documentation index (architecture, asset pipeline, profiling, design notes)
- 🚢 [RELEASE.md](RELEASE.md): the release checklist
- 🔒 [SECURITY.md](SECURITY.md): how to report a vulnerability
- ⚖️ [AGPL-3.0-or-later](LICENSE); origin, trademark and asset scope are in [NOTICE.md](NOTICE.md)
