# Optional AI caravans and thieves

Port-only, not native. The switch below is off by default, and off leaves the shard native. When on, AI caravan
and thief agents travel the Jangan - Donwhang - Hotan trade highway, remember the players they meet and decide
what to do next.

The nine agents are transient server-owned characters with shipped job suits, weapons and trader
transport COS. They share normal player targeting, skill damage, region protection and transport
cargo scattering. They never become account-owned or persisted player characters.

Agents use the configured level cap (native 90, or 110 with the matching expansion data), full
level-appropriate armour/accessories, and alternating ordinary and Seal of Sun sets. Three supported
weapon skills are trained at their highest eligible ranks; casts honor native reuse/cost rules and
fall back to a base attack while those skills cool down. Low mana triggers the paced AI potion lane.
Traders mount through normal ride admission and publish the mount after its spawn for late viewers.
They dismount to fight; hunters acquire and attack opposing native job NPCs as well as player thieves.
Loaded AI transports register with the normal caravan ambush timer. Native thieves spawn only in
battlefield regions, after the ordinary randomized timer; they can kill a transport and scatter cargo.
AI characters enter the population and monster target census without becoming socket viewers.

Routes load specialty traders, gate guards and ferry links from shipped data. Each walking leg uses
the authoritative navigation geometry; bounded incremental road searches find detours around blocked
terrain. Ferries require source proximity, admission and the native fare. Convoy hunters follow their
trader, including admitted ferry crossings. Route heat selects alternate crossings.

Escort commands require the correct worn job suit, a living nearby player and an available recruiter
in town. `!hire <hunter>` charges 15,000 gold once. `!accept <trader>` requires a Hunter to remain within
320 units for at least 90% of a surviving run (minimum 30 seconds; observation gaps invalidate it).
Rewards are limited to three per character per UTC day and persist with the gold credit. Schema 48
adds these optional counters; the local launcher retains a backup and runs the offline upgrade.
Commands are limited to one per character per second. Chat cannot deal damage: use ordinary combat.
Dialogue and model notes are filtered as plain, bounded text.

## Local testing

Start the launcher from this worktree so it builds this implementation. The feature is off by default;
the standard `apps/server/config/local.env` does not enable it. In PowerShell, set the switch in the same
terminal that launches the game:

```powershell
$env:SRO_AI_CARAVANS = "on"
$env:SRO_AGENT_BRAIN = "hardcoded"
node scripts/local/run.mjs
```

The GameWorld log must contain `AI caravan & thief agents ON`. In Jangan, Bao and Kwan stand beside
Adventurer Flora: region **25000**, approximately **X=1681/1691, Y=11, Z=261**. The stable is a different
landmark. GM testing can use `/warp 25000 1675 11 260` while dismounted. Other agents travel or stage
at Donwhang and Hotan. Only the default outdoor world receives them. Use `!caravans` for status;
admin world-map markers show current positions. Use full agent names, e.g. `!hire [AI]Hunter_Kwan`.

## Verification checkpoint (2026-11-17)

Automated checks exercise all six directed highway routes and six alternate routes against shipped
navmesh/ferry data, real actor skill damage, cargo drops, transient actor persistence, command/job
validation, contract survival/proximity and persisted daily reward limits. Feature-off boundaries
remain covered. Hands-off browser checks use a separate copy of the local world; test mutations never
provision or move the owner's original character.

The hardcoded brain is the tested mode. Remote model integration remains loopback-only and disabled
for this run. The Windows race gate requires a cgo-capable C compiler; the host currently has no
configured compatible compiler, so ordinary tests/static checks run but race verification is incomplete.

## Switches

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_AI_CARAVANS` | GameWorld, job variable `ai_caravans` | The agent roster and its tick; memories persist in store layout 14 (`caravan_agent_memories`, `caravan_route_heat`) |

## The decision brain

The agents use the hardcoded brain: deterministic utility and state-machine rules.

The code also has an LLM brain (`SRO_AGENT_BRAIN=llm` or `hybrid`, with `SRO_AGENT_LLM_URL`). Its prompt carries
nearby players' character names and stats, so by owner decision it stays off, and these settings are not job
variables: no deployment sets them. On a developer machine the GameWorld still refuses any `SRO_AGENT_LLM_URL`
that is not a loopback address (`localhost`, `127.0.0.0/8`, `::1`), logs the refusal and uses the hardcoded brain.

## Follow-up checkpoint: mounted caravans and native NPCs

Latest main was merged at `945340ea` (main `cb43b671`); no PR was opened. The native mount,
ambush registration and damage owners are reused rather than duplicating their protocol/formulas.
Regression checks cover AI-only monster acquisition without viewers, actual ambush NPC creation,
mount/dismount admission, all highway legs, maximum-level builds and advanced hunter skill damage
against a shipped level-90 thief reference. Hands-off browser testing confirms visible equipped armour and a seated rider on the shipped
armored transport. Test-world logs record native NPC transport kills and real cargo drops. Defeat now marks the actor for a fresh loaded transport and resets observer
appearance before respawning; this is covered by a transport-recovery regression.

Public implementation references consulted:

- [JadeRunner](https://github.com/B1Q/JadeRunner): compatible learned skill rotations, reuse-aware
  retries and server-owned approach/cast outcomes; client automation, not a server AI replacement.
- [phBot xCaravan](https://github.com/Vette1123/phbot-plugins#xcaravan): explicit role routes, job
  equipment, ferry travel and settlement states; its later job system is not copied into v1.150.
- [iSRO AI-player developer report](https://www.reddit.com/r/silkroadonline/comments/1xrm1x2/i_spent_months_rebuilding_a_silkroad_online/):
  full characters, trader/hunter/thief roles and mounted travel; implementation is not public, so its
  claims are treated as a comparison rather than code or performance evidence.

Fresh native investigation attempted the released revtool seed. Its checksum is valid, but this
host's client executable hashes to `f6580bf65213c0c074efe312514c55c73040667e7a9ad1aae9b8595d48ccd3a2`,
not the seed's `375e8682...`; the mismatched labels were not applied. Existing verified native owners
and regression fixtures provide the mount/ambush contracts used here. No new native-default rule changes.


## Active implementation checkpoint (2026-11-17)

Status: **implemented and checked for this batch; longer live validation remains; do not open a PR or push**. This section supersedes earlier verification claims. The original implementation-plan file is not
present in this checkout; this tracked document is the implementation and handoff record.

### Completed

- Merged latest fetched main (`cb43b671`) into `feat/ai-caravan-agents`, merge `945340ea`.
  Preserved main's seasonal NPC, weather, Roc, Nasrun and position-stream startup wiring.
- Local commits: `6e302aa4` initial checkpoint and `8e95d8bd` mounted/native-combat implementation.
- Nine transient characters use the configured cap (90 here), normal/Sun equipment, trained
  advanced weapon skills and native cost/cooldown admission. Paced recovery restores mana too.
- Native ride admission, ordered character/COS/ride publication, mounted movement, native ambush
  registration, AI-only monster targeting, hunter job-NPC acquisition, and defeat recovery implemented.
- Corrected transient identities into native player/COS namespaces. Persistent character creation
  skips occupied transient IDs without persisting AI characters or advancing its durable ID counter
  when AI actors register. Regression coverage added.
- Actual v1.150 extraction contains `t_dhorse1.bsr`, but **no** `t_dhorse2.bsr` or `t_dhorse3.bsr`.
  Selected real `ITEM_COS_T_DHORSE1` with its native HP/capacity/model instead of invisible DHORSE3.
  This is a transport asset limitation; all nine AI characters remain max level.
- Online comparison sources and native-research hash mismatch are recorded above.

### Verified so far

- Current full package tests pass: GameWorld composition (52 s), store (16 s), action (70 s),
  caravan AI, and the correct `internal/game/world/simulation` package (13 s). Domain/store tests
  also pass after moving job record types out of the over-limit character source file.
- Earlier correct simulation, feature-off, route/ferry, combat, ambush, recovery, and mount-admission
  regressions passed. Earlier vet, lint and source gates passed before the final identity changes.
- Scratch-world live logs show native thieves killing transports and scattering actual cargo.
- Browser diagnosis saw Bao's `mountedOn` link arrive but no displayed vehicle for DHORSE3.
  Server-side tests prove mounted snapshots survive synchronization. Fresh-client live testing
  now confirms Bao visibly seated on DHORSE1; client diagnostics resolved both the vehicle and
  its rider seat. Temporary rendering diagnostics have been removed.

### Latest validation and remaining work

- Confirmed native riding in a fresh browser session; `.state/caravan-mounted-proof.jpg` now
  contains an actual seated trader beside the town hunter. The earlier stale image was replaced.
- All 18 source-portable pipeline tasks pass (45 s), including TypeScript, 686 Python preparation
  tests, pipeline contracts, formatting, source size, doc links and shared fixtures. Use installed
  Python 3.11 via `SRO_PYTHON` and the existing audit-python dependencies: default `py -3` selected
  incompatible Python 3.9. This required no product dependency changes.
- Changed-scope Go vet and golangci-lint pass (zero issues). No diagnostic source remains.
- Native ambush/mount/recovery and advanced hunter skill damage against a real level-90 NPC thief
  pass automated shipped-data tests. Six directed and six alternate highway/ferry routes are covered.
- Live scratch-world cargo destruction was observed again on the final transport build. Hunter
  escort duty is visible in `!caravans`; moving rider/guard combat was not captured as a video.
- Final fixes committed locally as `1a248d1f` (before this documentation-only checkpoint).
  Restored the owner's original state root with the feature on and **no scratch bandit cap**.
  `/transport/readyz` returned `ready`; launcher confirms the original world and one character.
  No push or PR was made. No remaining implementation blocker is known; the live soak below
  remains necessary before describing this feature as fully release-ready.
- Follow-up validation: longer gameplay/balance soak (including complete live convoy/ferry combat)
  and Windows race checking. Native maximum cargo can overwhelm a transport; the implementation
  preserves native damage/spawn behavior. This host has no compatible cgo compiler for `-race`.

### Running test environment and recovery

- Scratch launcher `85448` and its children were stopped. Active original-world launcher at
  checkpoint: `46440`, hidden `node scripts/local/run.mjs`. Confirm process identity before
  stopping; child executables lock build output.
- Scratch state: `E:/CodeProjects/opensro-ai-caravans/.state/caravan-e2e-world`.
  Active original state: `E:/CodeProjects/opensro-issue-hub/apps/server/.state`.
- Enabled: `SRO_AI_CARAVANS=on`, `SRO_AGENT_BRAIN=hardcoded`,
  `SRO_GM_CHARACTERS=global-official:test`. Scratch used `SRO_CARAVAN_BANDIT_CAP=1`;
  that override is absent from the active original-world launcher.
- Game root: `E:/CodeProjects/SRO/AssetImports/OpenSRO-v150`.
  Generated root: `E:/CodeProjects/opensro-arena-main-sync/.state/arena-pinned-clean-20261111/generated`.
  Server data root: `E:/CodeProjects/opensro-issue-hub/apps/server/.generated/game-data/1.150/server`.
  Shared/pinned generated assets were not modified to invent missing transport models.
- Go: `C:/Users/Kresko/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.4.windows-amd64/bin/go.exe`.
  Use `node scripts/task.mjs`; this host's plain `pnpm` shim can return without executing tasks.
- Local UI `http://127.0.0.1:5180/`, transport ready `http://127.0.0.1:8788/transport/readyz`.
  Scratch account `tester`, character `test`; Flora warp `/warp 25000 1675 11 260`.
- Browser automation was recovered through the supported Node browser runtime. Fresh-session
  native riding is verified. All test warps and provisions used the scratch world.


## Live convoy and ferry verification completed (2026-11-17)

**No PR or push.** This checkpoint supersedes the earlier pending live checks. Testing used
an isolated scratch world, the real GameWorld tick, shipped geometry/teleports, real combat actors,
and natural **uncapped** caravan thief spawning. The observer never attacked the test NPCs.
Temporary diagnostics/staging code was removed after testing.

### Defects fixed

1. Traders kept traveling while their escort fought, opening a gap of several thousand units.
   A trader now waits while its assigned hunter fights or is more than 64 units away, including
   before ferry departure. It resumes after the guard catches up. A failing-then-passing regression
   covers fighting, distance, resumed movement and ferry admission waiting.
2. Advanced skills could exhaust affordable MP above the old 20% recovery threshold. Native
   B245 insufficient-MP refusals (`02 04`, native error 0x3004) now request the existing paced
   recovery action. The request survives the next normal vital observation and clears after healing.
   A regression covers this case; native skill cost, damage, cooldown and potion pacing are preserved.

### Live evidence

| Check | Observed result |
| --- | --- |
| Unstaged initial convoy | Mei and Wei left Donwhang together; Mei remained mounted and Wei followed. |
| Natural thief protection | Wei intercepted thief GID 463712 / ref 3105, took its aggro, reduced HP from 19,300 to zero and resumed escort. Mei's transport stayed at 35,500 HP. |
| Native ferry | Both crossed source ref 3824 to destination 16, arriving in region 23436 at X904 Y-23 Z1412. Each paid 500 gold (1,000,000 to 999,500). Cargo and mounting survived. |
| Pacing fix | On the Hotan bank, Mei waited while Wei fought, then both resumed together instead of separating. |
| Final combat with both fixes | Thief GID 462637 / ref 3090 fell from sampled HP 18,159 to zero. The pair stayed 5–15 units apart during combat and returned to a zero sampled gap afterward. |
| Real skills and recovery | Native cooldown groups **709, 712 and 720** confirm all three advanced skills executed: 19095 `SKILL_CH_SPEAR_FRONTAREA_D_08`, 19134 `SKILL_CH_SPEAR_STUN_D_06`, and 19297 `SKILL_CH_SPEAR_SHOOT_C_08`. MP recovered from 1,566 to 3,788, allowing casting to continue. |
| Cargo survival | The final defended transport retained all **12** cargo rows and **35,500 HP**, with the trader still mounted. |

The ferry test staged the existing convoy at the real source ferry to isolate the crossing rather
than waiting through the full desert approach. The native ferry admission, fee, destination,
follow logic, subsequent travel and naturally timed ambushes were not replaced or forced.
This is **not** a claim that a complete uninterrupted town-to-town journey was observed live.
The existing shipped-data route checks separately exercise all six directed and six alternate routes.

Scratch evidence retained locally:

- `.state/caravan-live-natural.stdout.log`: unstaged departure and the first natural thief kill.
- `.state/caravan-live-ferry-before.stdout.log`: crossing and the original escort separation.
- `.state/caravan-live-pacing.stdout.log`: crossing/combat with convoy waiting fixed.
- `.state/caravan-live-final.stdout.log`: final crossing, all three executed skill groups,
  mana recovery, NPC death, intact cargo and resumed close escort.
- `.state/caravan-live-travel.jpg` and `.state/caravan-live-ferry-arrival.jpg`: live visual evidence.
  The latter shows the mounted trader, hunter and native thief on the Hotan bank.
- `.state/caravan-live-final-combat.jpg` missed the moving convoy and is **not** combat proof.

### Final checks and runtime restoration

Final checks after removing diagnostic code and original-world restoration are in progress.
Do not leave fixture control or the scratch state root enabled in the owner's running world.
Remaining limits: no uninterrupted full-length live route soak or Windows race run; native
uncapped waves can still defeat a caravan. No additional native-default rules were changed.
