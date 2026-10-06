# SOX presentation â€” BUG-057

The main inventory and equipment painter omitted the existing animated slot
overlays. Other item controls already called that owner. The shared painter
now supplies the same wash, sparkle, flash and animation invalidation to the
bag, equipment, quickslots, avatar slots, exchange, stalls, stall search and
item-mall inventory. NPC offers and buyback rows now pass their underlying
item instances to the effect painter instead of losing rarity in an offer
wrapper. Existing `nativeItem` callers retain the same behavior.

Item headings, seal names and magic attributes now select the bold font.
Ordinary equipment details and requirements keep normal weight. The seal
appears below the heading, before the equipment details.

The expanded audit also restores the capacity row for non-avatar equipment,
the older `MATTR_STR_AVATAR` and `MATTR_INT_AVATAR` text branches (including
zero-value fallback), signed degree lookup for class-zero items, and omission
of positive attributes when their matching degree definition is absent.
Ordinary equipment descriptions follow identity/capacity; job and avatar
descriptions precede their details. Job suits do not gain an equipment-degree
row. Existing server combat already handles the older avatar `strv`/`intv`
parameter tags; their omission was in tooltip presentation.

## Native evidence and explicit presentation exception

Evidence comes from the open v1.150 `SRO_Client.exe.bndb`, checked against x86
instructions through the Binary Ninja MCP. Function names alone were not
used as proof.

| Native path | Observed behavior |
| --- | --- |
| `564230`, `558FF0` | Heading and seal use text style 3. |
| `55B540`, calls at `55C3FB`, `55C650` | Magic attributes use style 3, including avatar text formatted by `553980` (`55C4D4`). |
| `782E10`, `7825B0`, `7818D0` | Style flag `0x2` selects the bold font for rendering and measurement. |
| `5570E0`, six equipment formatter callers | Maximum magic-option count is not limited to avatars. |
| `55C150`, `55C1FB` | Older avatar STR/INT use the instance amount, falling back to definition degree when zero, without percentages. |
| `55B608`, `55B632` | Signed division maps class zero to degree one; a missing matching positive-option definition suppresses its row. |
| `56ADA0`, call at `56B099` | Native weapon tooltip appends the seal after requirements. Armor/accessory paths follow the same late placement. |
| `54FA60`, `555110`, `565850` | Rare icon effect uses 32 frames, 40 ms per frame, with per-control starting phase. The port already owned these rules in `item-slot-effects.ts`. |
| `8EA960`, `9155B0`, `A9C4C0`, `AE3AC0`, `AEC710` | Rare equipment selects its authored effect descriptor and attaches it to the specified weapon bone. Enhancement effects are a separate selection. |

The user explicitly requested the seal placement in `D:/Downloads/referencesox.png`.
That reference places the seal beneath the name, unlike v1.150. This is an
intentional presentation exception, not a claim of native ordering parity.
Later-version features visible in that reference are not introduced here.
The user explicitly confirmed keeping v1.150 scope after the Advanced Elixir
question. No Advanced Elixir tooltip branch or entry was found in the native
client or the published English item/system tables. `MATTR_REINFORCE_ITEM`
is temporary reinforcement with a duration, not Advanced Elixir.

The investigation labeled 50 relevant formatters, helpers and exception
handlers. Saved client snapshot 390 was read back: all 50 labels were present.
The previously identified repair-magic predicate was also verified in snapshot
389. The magic-option wrapper distinguishes instance options from a specialized
preview list; ordinary NPC package templates already preserve their authored
magic options through the commerce projection.

## Equipped bow effect findings

The granted level-8 Bronz Bows are references 4161, 4162 and 4163, all +0.
Their authored descriptors select `system/system_rarebow_a.efp`, `_b.efp`
and `_c.efp`, respectively. They are distinct programs and texture layers,
with one, two and three populated moving BAN paths. Each path has 30 position
and 30 rotation samples. Empty controller tables are not missing motion:
the populated tables are in the render programs.

A subsequent Binary Ninja audit established an equipped-effect defect:
`SetBANRot` mode 2 replaced the element matrix instead of composing it with
its parent. Ordinary `SetRotation` mode 2 had the same omission. The worker
also dropped their flags and schedules from its executable command list,
then the graph applied BAN position/rotation tables after all other commands.

The fix emits these commands in source order with their flags, schedules and
individual tables. Runtime orientation modes retain parent/sibling bases;
BAN position selects its authored element/parent/sibling basis and origin.
Constant rotation and BAN accumulation retain their distinct operand orders.
No effect strength, color or texture was tuned by eye.

Native evidence checked in this follow-up:

- `AFE210` emits BAN rotation opcodes `0x0F + flags`; `AFE360` emits position
  opcodes `0x2B + flags` (assembly checked).
- `AF4581..AF45BC` copies a BAN frame then multiplies by the parent matrix;
  `AF44B1..AF44E1` does the same for constant rotation mode 2.
- `AF4559` accumulates BAN rotation in the opposite operand order from the
  constant rotation branch at `AF448A`. `878A40` confirms the in-place product.
- `AF521B` uses the element matrix for BAN position mode 4; dispatcher case
  `0x32` uses the parent matrix and position for mode 7.
- Previously unnamed frame accessors `AF1F90` and `735290` were investigated
  and labeled `EffectMatrixFrames_CheckedAt` and `EffectVectorFrames_CheckedAt`.
  Both labels were verified in saved client BNDB snapshot 392.

The published rare-effect census contains 49 BAN rotation commands, all mode
2, and 49 BAN position commands, all mode 7. The entire effect catalog contains
188 BAN rotations in mode 2, 187 BAN positions in mode 7 and one in mode 4.
The regression suite decodes every published `system/system_rare*` effect and
checks that its executable rotation/BAN command order and flags survive.
Rotated-parent, noncommuting-matrix, command-order and delayed-command tests
exercise the runtime rather than only checking that animation tables exist.

## Verification

- `pnpm --filter @sro/client-next check`: all 11 gates passed, including the
  source test runner and test type checking.
- `pnpm check source`: all 13 gates passed, including server checks.
- `sox-presentation.test.mjs`: seal order, heading/seal/magic weights, ordinary
  row weights, non-rare items, capacity/description/degree behavior across 12
  equipment families and all three compiled authored bow effects.
- `item-tooltip-magic.test.mjs`: exact text/color/weight cases for 37 option
  families; a census of all 255 active licensed definitions and their degrees;
  old-avatar zero values, temporary reinforcement at 59,999/60,000 ms, repair
  counts, accessory penalty exclusion, missing-degree suppression, and the
  inventory-to-tooltip publication of class-zero degree definitions.
- `beta-ui-regressions.test.mjs`: real inventory/equipment painter publishes
  both sparkle overlays; 40 ms advances their UVs without gameplay changes;
  closing the inventory removes them. NPC offer and buyback overlays also
  advance and disappear when replaced by an ordinary item.
- Connected Chrome probe on the isolated worktree: normal authentication as
  CodexProbe, live inventory hover screenshot, four distinct pixel captures
  of one stationary rare bag icon, ordinary equip actions for all three bows,
  correct `equipment:6:EQ0:Bone01` attachment, advancing actor time, one active
  rare-bow effect after each change, and zero page errors. The previously
  equipped Sun bow was restored. No items were granted by this probe.

Local diagnostic artifacts are under `.state/sox-visuals/`, including
`sox-browser-report.json`, `sox-tooltip-live.png`, `sparkle-0.png` through
`sparkle-3.png`, and `equipped-a.png` through `equipped-c.png`.

This establishes the specified UI fixes, authored-effect selection and
lifecycle, and the corrected orientation/position command semantics. It does not establish pixel-for-pixel agreement with a running
original client. User visual confirmation remains pending. The final live
probe ran at `http://localhost:5180/`, served from `codex/sox-visual-parity`
in the isolated worktree, without restarting the game services.
