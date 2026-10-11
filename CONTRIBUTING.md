# 🤝 Contributing to OpenSRO

Thanks for helping rebuild Silkroad Online! OpenSRO is a full client and server, so there is room for every kind of work: rendering, UI, gameplay rules, networking, tools and docs. This page tells you how to land a change smoothly.

**The rules for humans and AI coding agents are the same:** [AGENTS.md](AGENTS.md). Read it once before your first pull request.

---

## 🚀 1. Get set up

1. Follow [docs/GETTING_STARTED.md](docs/GETTING_STARTED.md). The asset build needs your own v1.150 client.
2. Working only on code? `corepack pnpm check source` runs the CI gates with no game data at all.
3. Find something to do: issues labelled [good first issue](https://github.com/opensro-dev/opensro/labels/good%20first%20issue), the bug reports on [Discord](https://discord.gg/RUua9HY657), or ask in #🛠️contributing.

---

## 🏯 2. Native first: the original game is the spec

OpenSRO ports the original game's behaviour; it does not reinvent it.

- **The v1.150 client defines what exists:** its data, its network messages and its interface.
- **Server-only rules** follow the original server's instructions (the v1.188 server binary), trimmed to the v1.150 feature set.
- **When neither binary shows a rule,** infer how the original would have done it, write that inference in a code comment, and ship it. Don't leave an open "no evidence" question behind.
- **Cite your evidence briefly:** a function address in a comment, the value in a named constant. Long disassembly notes belong in the research tree, not in the source.

A change that makes the game behave like the original needs no special approval; it needs evidence and tests.

You do not need Binary Ninja to contribute native evidence. Start with the
[Python reverse-engineering tutorial](docs/REVERSE_ENGINEERING.md), then follow
[saving labels and research progress](docs/RESEARCH_PROGRESS.md) so people and
AI agents can continue your investigation later.

---

## 🚩 3. Anything non-native goes behind a flag

Ideas that the original game does not have are welcome: quality-of-life features, modern graphics, browser conveniences, beta tuning. **They must never change the native default.** Every non-native behaviour ships switched off behind a flag, so the original game stays one setting away:

| Where | How |
| --- | --- |
| 🎮 **Client** | Add it to the **Experimental** window (Escape → Experimental), not to the native Options window. Add a key to `apps/client-next/src/engine/foundation/ui/experimental-options.ts` with a row on the right tab (Video, Chat or Developer). It defaults to **off**. |
| ⚔️ **Server** | Read an environment flag named `SRO_<FEATURE>` where off is native (see `SRO_BETA_GROWTH` in `apps/server/internal/game/progression/growth.go`). Mark the code "port-only, not native" in its banner. |

Also:

- 🙋 **Non-native behaviour needs the project owner's approval** before it merges, and each visual or gameplay deviation is approved on its own. Say clearly in the pull request what differs from the original and why.
- 🧪 **Test both settings:** flag off must behave exactly like the original; flag on must behave as described.
- 🚫 **No dev-only shortcuts.** Never use flags to fake a result: no response rewriting, no special localhost paths, no "skip this check in dev". localhost, LAN and production run one code path.

---

## ✍️ 4. Write code that reads like the rest

- 🎨 **id Software (Quake III) style:** a banner at the top of every file saying what it owns, a banner on every function, plain data and functions, explicit `Init` / `Shutdown` / `Frame` lifecycles, named constants for every magic number. Details and examples are in [AGENTS.md](AGENTS.md).
- 🧹 **Formatting:** `pnpm exec dprint fmt <path>` for JavaScript/TypeScript, `gofmt` for Go. Files are UTF-8 without a BOM, with LF line endings.
- 🔍 **Search before you edit** (`rg`): find every caller and test of what you change, and reuse an existing helper instead of adding a second one.
- 📏 **Files over 1000 lines** are split only at a real responsibility boundary.
- 🗃️ **Never commit** retail game media, generated data (`.generated/`), caches (`.state/`) or anything from `temp/`.

---

## 🧪 5. Tests

- Tests must break when **behaviour** changes, not when something is renamed. Don't assert on source text.
- A bug fix comes with a test that fails without the fix.
- Run the checks for what you touched, and say in the pull request which ones ran:

| You changed | Run |
| --- | --- |
| Anything | `corepack pnpm check source` |
| The client | `corepack pnpm --filter @sro/client-next check` |
| The Go server | see [apps/server/AGENTS.md](apps/server/AGENTS.md) |
| Assets or the pipeline | `corepack pnpm check` (includes the asset build) |

---

## 📬 6. Pull requests

- **One concern per pull request.** Small and focused gets reviewed fast.
- **Describe what a player or operator will notice,** the native evidence you used, and the checks you ran. Paste failing output rather than "all green" when something failed.
- **List every native claim the change relies on** in a table, one row per
  fact about the original: a function's behaviour, a field offset, a constant,
  a message layout, a data rule. Write "none" when the change makes no native
  claim. Each row names how it was checked, and an unchecked row is marked
  open, never left out:

  | Claim | Native evidence | Checked by | Status |
  | --- | --- | --- | --- |
  | The overhead pass draws the job icon right of the name | client 86B350 | disassembly read | verified |
  | The bag size is learned only at world entry | client 8675F0, +0x1848 | disassembly read | verified |

  "Checked by" is one of: disassembly read, emulator or difference test, game
  data, native capture, or inference (stated in a code comment). A claim
  that contradicts an existing comment or research note says so, and the PR
  corrects the old text.
- **Link the issue or bug report** (for example `BUG-064`) it fixes.
- **Mark non-native behaviour** and name its flag (section 3).
- Keep secrets out of commits and pull requests: tokens, passwords, webhook URLs.

---

## 🐞 Reporting bugs

In the game, type `/bug` to send a report with a short replay. Otherwise post the steps and a screenshot or video in the bug reports channel on [Discord](https://discord.gg/RUua9HY657). Security problems go privately through [SECURITY.md](SECURITY.md).

Questions? Ask in #🛠️contributing on [Discord](https://discord.gg/RUua9HY657). Welcome aboard! 🏯
