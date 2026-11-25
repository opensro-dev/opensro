# Optional AI caravans and thieves

Port-only, not native. The switch below is off by default, and off leaves the shard native. When on, AI caravan
and thief agents travel the Jangan - Donwhang - Hotan trade highway, remember the players they meet and decide
what to do next.

The roster contains 10 traders, 20 hunters, and 10 thieves. Four thieves begin road patrols and
six start in the Den for hire. These transient server-owned characters use shipped job suits,
weapons, and trader transports. They share normal targeting, skill damage, region protection,
and cargo scattering.

See [November 19 handoff verification](#november-19-handoff-verification) for the current local test status.

Agents use the configured level cap (native 90, or 110 with the matching expansion data), full
level-appropriate armour/accessories, and alternating ordinary and Seal of Sun sets. Three supported
weapon skills are trained at their highest eligible ranks; casts honor native reuse/cost rules and
fall back to a base attack while those skills cool down. Low mana triggers the paced AI potion lane.
Traders mount through normal ride admission and publish the mount after its spawn for late viewers.
They dismount to fight; hunters acquire and attack opposing native job NPCs as well as player thieves.
Loaded AI transports register with the normal caravan ambush timer. Native thieves spawn only in
battlefield regions, after the ordinary randomized timer; they can kill a transport and scatter cargo.
AI characters enter the population and monster target census without becoming socket viewers.

Routes load specialty shops, town gates, and ferry links from shipped data. Walking legs use the
authoritative navigation geometry and bounded road searches for detours. Ferries require source
proximity, admission, and the native fare. Escorts board their trader's link and resume formation
on the far bank. Route variants include the Dead Ravine, Central-Tarim, Highland Oasis, and Tiger Mountain.

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
the world map shows their positions to GM characters only (never to players, even with
SRO_BETA_PLAYER_MAP on). Use full agent names, e.g. `!hire [AI]Hunter_Kwan`.

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

Status: **requested live combat/ferry checks passed; no PR or push**. The final live checkpoint below supersedes the earlier test limits. This section supersedes earlier verification claims. The original implementation-plan file is not
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
  checkpoint: `85688`, hidden `node scripts/local/run.mjs`. Confirm process identity before
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

Final code commit: `26309f7e`. All four affected Go packages pass after diagnostic removal
(GameWorld, caravan AI, action, simulation); changed-scope vet/lint pass with zero issues. All
18 source-portable checks pass (45.2 s), including TypeScript and 686 Python preparation tests.

Scratch launcher 83940 was stopped. Original world restored from
`E:/CodeProjects/opensro-issue-hub/apps/server/.state`, active launcher PID **85688** at this
checkpoint. AI caravans are on, brain is hardcoded, and both fixture control and the scratch
bandit-cap override are absent. `/transport/readyz` returned **ready**; launcher confirms the
original world and one character. Test observer closed; all test warps affected the scratch copy.
No PR or push was made.
Remaining limits: no uninterrupted full-length live route soak or Windows race run; native
uncapped waves can still defeat a caravan. No additional native-default rules were changed.

Evidence SHA-256 (local logs retained, not shipped as fixtures):

- `caravan-live-natural.stdout.log`: `2b5fa6defead1ab4e461b9149accf7781828de4b222c69a82bad8da1db8a763b`
- `caravan-live-final.stdout.log`: `209b11d81b39a3474128f4594eec40b2ae84c3c9622e9262ad99f59856a93936`


## Uninterrupted live run and thief animation verification

Owner requested a full uninterrupted town-to-town convoy run, including its ferry and naturally
spawned thieves, and verified fix for broken thief skill animations. **No PR or push.**

### Thief bow skill animation fix

Root cause diagnosed live against `[AI]Thief_Kael`: generated bow assets rendered cleanly when
stationary, but `ActionAttackTarget` and `ApproachWorldThreat` kept walking bow agents into melee
range (`<= 20` units) and issuing mid-cast movement updates every 100ms while `caravanCast`
re-triggered `HandleTargetInteract` every 500ms over a 1200ms cast window, cancelling client attack
poses (`OpSkillCastResult` error spam `0xB2CD [1, 2]`).

Implemented fixes:

- `internal/game/action/serveractor.go`: added `ServerActorCasting` (`hasOpenSkillCast`).
- `internal/game/caravanai/runtime.go`, `internal/game/caravanai/world.go`: added `WorldHooks.Casting`,
  weapon-aware `attackStopDistance` (`140` for `_BOW_`/`_CROSSBOW_`, `20` melee) and `InCastRange`
  (`175` bow/crossbow, `45` melee), held movement while casting, and added `StepAgentWithWorldThreat`
  to prevent player-vs-NPC pursuit tug-of-war.
- `cmd/services/sro-gameworld/wiring_caravanai.go`, `wiring_caravancombat.go`: suppressed overlapping
  casts while `ServerActorCasting` is true, gated `caravanCast` with `InCastRange`, and fell back to
  0-MP base attack (`caravanAttackSkill`) on `0x3004`.

Live browser observer verification on `[AI]Thief_Kael` (`SRO_GM_CHARACTERS=global-official:test`,
`/MOVETOUSER [AI]Thief_Kael` in disposable scratch world): Kael held ~140u range, stopped moving
during casts, emitted clean `0xB245` casts (~900ms spacing without `0xB2CD [1, 2]` spam), and drew
and fired bow attacks cleanly (`.state/caravan-kael-before.jpg` -> `.state/caravan-kael-after.jpg`).

### Convoy survival, navigation, and uninterrupted Donwhang -> Hotan run (`v6`)

Additional stability fixes required for the uninterrupted live town-to-town run:

- **Transport & escrow sustain**: `ServerActorTransportVitals`, `HealServerActor` (+35% `ActiveCOS`
  HP/MP alongside actor HP/MP), escorted `RoleTrader` stays mounted and heals at transport HP <= 45%,
  `RoleThief` potions bounded (`PotionCharges=3`), Hunter MP-only recovery preserves battle HP charges,
  dedicated Hunter escorts stay paired across respawns, and `caravanNPCThreat` scans around both the
  Hunter and escorted Trader.
- **Tether, road cache, and unreachable-threat recovery**: `EscortMustRejoin` disengages Hunters >280u
  away or stuck outside town; `waitForEscortLocked` only holds within 600u out of town; `caravanMove`
  evicts failed road searches with backoff and avoids thrashing A* on minor target steps (`<= 64`u);
  `ApproachWorldThreatAt` disengages unreachable world threats after `5000ms` without movement or
  cast progress (`30s` threat ignore cooldown in `wiring_caravancombat.go`).

Uninterrupted live proof in `.state/caravan-uninterrupted-v6.stdout.log` (run against disposable
`.state/caravan-e2e-world` with `SRO_AI_CARAVANS=on`, `SRO_AGENT_BRAIN=hardcoded`, no convoy warps,
no waypoint staging, no forced ferry, no shortened timer, no speed boost, and no bandit cap;
SHA-256 `da647c770670cad5af221d75cbacb18341f71bc2d048e4d0cbafc32661fbde73`):

- `1019` trace snapshots across `33.98m` (`6` native ferry crossings total across actors).
- `[AI]Trader_Mei` departed Donwhang (`reg=26265`), navigated Western China & Tarim Basin through
  natural uncapped mobs (`meiMinCosHP=13873` sustained back up via transport potions), crossed
  Karakoram Ferry (`source=3824 target=16 arrival=23436`) at `28.41m`, and delivered in Hotan
  (`reg=23687`) at `32.84m` with `0` Trader deaths before departing on the `hotan->donwhang` return leg.
- `[AI]Hunter_Wei` escorted `[AI]Trader_Mei` from Donwhang past `25239` (`428` combat casts logged
  before falling in combat around `10.0m` and respawning cleanly in Donwhang). Remaining non-blocking
  limit observed in `v6`: after respawning in town far (>320u) behind a field-progressing leader, an
  escort aims straight toward the leader's coordinates and caught on Donwhang's SW wall (`reg=26265`)
  rather than walking out via highway gate waypoints; `[AI]Trader_Mei` completed the trip solo safely.

### Regressions, cleanup, checks, and world restoration

- Temporary live run trace file `development_caravan_run_trace.go` and its 3 trace calls were removed.
- Regression tests added in `internal/game/caravanai/world_test.go` and
  `cmd/services/sro-gameworld/wiring_caravanai_test.go`.
- Verified checks after edits: `go vet` clean across `cmd/services/sro-gameworld`,
  `internal/game/caravanai`, `internal/game/action`, `internal/game/world/simulation`, and
  `internal/gates`; `go test ./internal/game/caravanai` (`0.726s`), `internal/game/action` (`50.592s`),
  `internal/game/world/simulation` (`13.617s`), `internal/gates` (`13.043s`), and
  `go test -v ./cmd/services/sro-gameworld -run TestCaravan` (`TestCaravanWorldBoundary`,
  `TestCaravanShippedActors` `19.45s`) all pass; `apps/client-next` action tests and `npm run typecheck`
  pass cleanly (`0 failed`).
- Disposable scratch processes stopped and local runtime restored against the owner's original world
  (`E:\CodeProjects\opensro-issue-hub\apps\server\.state`). **No PR or push.**

## Player-hired escort, party, movement, and world-map polish (2026-11-18)

Follow-up fixes addressing owner playtest findings on skills, hired escort catch-up/partying, smooth movement, world map rendering, and test-world provisioning:

- **Skill animations & combo locks:** `simulation/tick.go` now routes tick-emitted frames for all non-monster `viewer.PublishedObjects` GIDs (`SourceGID < MonsterGIDBase || SourceGID > MonsterGIDLimit`), fixing dropped projectile/combo release and `0xB505` finalize frames for AI caravan actors; `action.Runtime.ServerActorCasting` now includes `PlayerAttackLocked` so combo stages are not interrupted mid-chain.
- **Smooth movement, hired escort speed/teleport & town-gate exit:** `caravanRunSpeed` assigns speed `75` to `JobHunter` escorts; `publishCaravanActor` replaces `>32u` jumps with clean despawn+respawn teleports, projects normal `OpMovementAck` destinations `+2*dx/+2*dz` via `worldgeom.Delta`/`worldgeom.NormalizeOutdoor` (emitting `OpObjectStateRefresh` only on movement state transitions), and settles to `MoveStateStand` when stopped. In `caravanai`, `stepEscortFollowLocked` routes town-respawned or lagging escorts through highway gate waypoints (`unit.Route`), `escortMustRejoinLocked` disengages combat for player-hired Hunters >250u (`HiredEscortRejoinDistance`) behind or outside town, and `HiredEscortTeleportDistance` (`500u` / ~100m) teleports lagging hired escorts directly to the hiring player.
- **Party (`pt`) auto-invite & hirer-only auto-accept:** `!hire` and `!accept` trigger `WorldHooks.OnContractFormed` -> `party.Runtime.InvitePlayerFromRuntimeActor` (sending `0x3393` proposal to solo players or joining existing parties); direct `0x70D5`/`0x751A` invites to runtime-only actors resolve via `domain.CharacterLookup`, auto-accept immediately only when `IsContractPartner` holds, refuse non-hirers (`0x0c` / `0x17`), stream `MemberUpdates` via `livePose`, and dissolve bot-only parties when no human member remains.
- **World Map (`M`) polish:** `foundation/ui/world-map.ts` adds optional per-marker `size` (keeping `MAP_MARKER_SIZE = 16` default); `hud/world-map.ts` deduplicates party members from `betaPlayers`, renders AI Traders/Hunters/Thieves and players at crisp sizes (`12`/`8`), and adds `betaRoster` to hoverable `allRoster`; `wiring_caravanai.go` admits `BetaPlayerMapEnabled()` sessions with Title Case town/state labels and disables competing `ticker.PlayerMap` overwrites in `wiring_runtime.go`.
- **Local provisioning & config:** Updated `apps/server/config/local.env` (`SRO_BETA_PLAYER_MAP`, `SRO_STARTING_SILK`, rate/pet/alchemy settings, `SRO_AI_CARAVANS=on`, `SRO_GM_CHARACTERS=global-official:test`) and credited Silk, GM privilege, gold, and Level-90 transport scrolls in both local/scratch `state.db` files.

## Hiring at the specialty shops, roaming escorts, standing runs and the grand caravan (2026-11-18)

Owner request: realistic escorts, hiring any idle agent for one trade run, a bigger roster, standing
runs plus a periodic all-hands caravan, robbed-trader cooldown, and harder-to-find solo routes.
Code: `internal/game/caravanai/convoy.go` (new), `roster.go`, `runtime.go`, `scheduler.go`, `world.go`;
wiring in `wiring_caravanai.go`, `wiring_caravaneconomy.go`, `wiring_quests.go`; party helpers
`MemberNames` / `RemoveRuntimeActor` in `social/party/consent.go`; client label passthrough in `ui.ts`.
Distances use 10 world units per in-game metre (skill ranges: 180 units = 18m); the old
"500u ~= 100m" note above was wrong.

- **Roster:** 20 Hunters, 10 Traders, 10 Thieves spread over Jangan, Donwhang and Hotan. Four Thieves
  patrol the highway (killed ones are replaced by rested town Thieves); everyone else starts idle.
- **Idle agents** stroll within 30m of their town's specialty shop and are the only hireable ones.
  Anyone on a run, mustering, escorting or hired is not offered.
- **Followers** (escorts, grand-caravan members, hired Hunters and Thieves) roam 15-50m around their
  leader, run straight after it past 120m (and stop fighting to do so), and are teleported beside it
  past 200m or when a catch-up makes no progress for 10s. A Trader waits while a follower within 120m
  fights or one 120-200m back catches up.
- **Following Traders** keep their own spot 5m from the leader (the angle comes from the name, so two
  never share one), do not stroll, and never teleport: they run until they are back in the spot. They
  walk the leader's footsteps (one every 3m, the last 400), so they go around the railings and walls
  the leader went around instead of pushing into them. A stuck hired Trader on a route skips the
  waypoint after 20s instead of being teleported onto it. Hunters run at 75 (port-only), the speed
  the 50% speed potion gives everyone else on foot.
- **Loot:** once a second each agent on the move is shown the nearest item within 30m it may take,
  by the native pickup rules, and walks over and picks it up (`action.ServerActorPickup`): a Trader its
  own dropped goods back into its transport, but not during a parley truce it paid for or with a Thief
  in sight; a Thief other caravans' goods (an AI Thief has no cart, so it carries them off), gold and
  items. A walk that does not get there in 15s is dropped.
- **Hunters call for help:** a Hunter under attack below half HP says "Hunters, to me!" and raises a
  call only Hunters answer (an escort answers a fellow guard of its own caravan); it never replaces a
  Trader's own call. A Hunter killed by a Thief respawns in town, rallies up to 3 idle Hunters there
  and leads them back after that Thief, as a killed Thief does after a Trader.
- **Hiring window** (port-only `0x3FCF` S->C JSON, `0x7FCF` C->S `u8 op, u8 stars, name`): the
  specialty shops list "Hire Hunters and Traders" as the first row of their menu, before "Start
  conversation" (the Thief Den's Stolen Goods Dealer: "Hire Thieves"); the server marks those NPCs
  at select with `0x3FD1` (u32 npc gid, label) and the row opens the window. It lists every agent of the kinds hired there,
  available ones first in green, the rest in red with the reason (on a trade run, hired by X, waiting
  in another town, out hunting a caravan, won't work for you, ...). "Show only available" filters the
  list; Previous/Next page and Refresh. Clicking a green name always opens a confirm dialog
  first: a Hunter or Thief shows the fee, a Trader also picks 1-5 stars of goods (100,000 gold per
  star) with the total; nothing is paid until "Hire".
  Hunters and Traders hire Hunters and Traders; Thieves hire Thieves; the player must be dressed and in
  town. Flat fee 15000 gold plus a Trader's goods. At most 7 hires at once. Hunters and Thieves appear
  at the hirer; a Trader is hired only in the town it waits in and walks over. Everyone hired is invited
  to the party. `!hire <name>` still works from chat (a Trader then carries goods of its own rating).
- **Leading:** hired Hunters/Thieves follow the hirer. Hired Traders follow one leader: the hirer or one
  of the hired Traders, which drives the run itself and waits while the hirer is more than 120m away.
  The hirer picks the leader on the party board (port-only, not native): each hired Trader's row and the
  hirer's own panel carry a "Lead" badge, the current leader shows "Leads" (0x7FCF operation 2; the
  server pushes the state as 0x3FD0). With no pick the first hired Trader in party order leads.
  `!leed` (or `!lead`) still toggles the hirer leading.
- **Hire end (hands off):** no command needed. A hired Trader that left town rides to
  the shop of the next town it reaches (its hire town again only after 10 minutes out), sells there and
  stays in that town: the hirer gets the sale (cost + 15% per cargo star). When all of a hirer's Traders
  sold, its other hired agents are released as each reaches a town, where they stay. The hirer
  disbanding the party, or putting an agent out of it, ends that agent's hire (a Trader pays the goods'
  cost back); so do a lost job, `!dismiss`, or the hirer being offline/out of the main world for
  2 minutes. Robbed goods pay nothing. Released agents leave the party; only one released outside a
  town is teleported home.
- **Hunter/Thief hire limits (2026-11-25):** so nobody keeps escorts forever. A Hunter or Thief hired
  with no Trader ends when the hirer is back in a town after at least 20 minutes out. Every Hunter or
  Thief hire lasts one hour at most; the hirer gets a notice 5 minutes before the end, and can renew it
  for another hour (the same fee) from the hire window at a specialty shop (Hunters) or the Den
  (Thieves): their own rows read "Working for you (N min left)" and open "Extend" (0x7FCF operation 3).
  A renewal never runs more than two hours ahead. A Thief carrying a haul is never cut off. Each town
  keeps its last 2 idle Hunters (the Den its last 2 Thieves) free: those rows read "Kept free for
  others" and cannot be hired.
- **Restarts:** hires in flight are mirrored to the `caravan_hires` table every 5 seconds (and at
  shutdown). At boot each one resumes: the agent waits at the hire town's shop for up to 10 minutes,
  then teleports to the hirer and rejoins their party when they log in. A stored hire whose agent is busy or gone pays the goods' cost back.
- **Standing runs:** 2 solo Traders and 2 Traders with 3 Hunters each are kept on the road. Escorts are
  released in the arrival town. Solo Traders pick either ferry crossing at random and bend every open leg
  of 150m+ through a point 60-150m off the road.
- **Grand caravan:** First departure about one hour after boot, then hourly.
  Two traders each haul five stars of cargo and travel together with six hunters,
  three per trader. They muster at a specialty shop for two minutes first.
  The scheduler checks again in one minute if the roster is short or the previous
  grand caravan is still active. The GM command starts the same muster immediately
  when the full group is available.
- **Robbed Trader:** 30-minute cooldown before its next own run (still hireable); its escorts give up
  the run and return to town.
- **Stuck recovery:** an off-road bend that makes no progress for 20s is skipped; any route waypoint
  unreachable for 60s is reached by teleport; a town stroll blocked for 6s picks another spot.
- **Distress calls** are answered only by agents on the road within 300m, never by town or hired agents.
- **Potions and speed:** agents drink real items from their bags through the normal item use
  (`action.ServerActorUseItem`), so amounts, cooldowns and the effect observers see are a player's:
  `ITEM_ETC_HP_POTION_05` at 45% HP or less, `ITEM_ETC_COS_HP_POTION_03` for a Trader's transport at
  45% or less, `ITEM_ETC_MP_POTION_05` when mana runs low; nothing else heals them. Every agent keeps
  `ITEM_MALL_MOVE_SPEED_UP_50` running (checked every 2s) and moves at the buffed speed (75 run, 30
  walk); a mounted Trader rides at the transport's speed, as natively. An empty stack is restocked.
- **Hunter targets:** a Hunter with an AI or player Thief within sight fights the Thief and skips the
  scan for the world's own monsters (native NPC thieves and mobs) until no Thief is near.
- **Persistence fix:** the caravan runtime's flush loop (memories, route heat, hires) is now started:
  it was handed to the shard ledgers before the runtime existed, so nothing was ever written.
- **Thief revenge:** a Thief that is killed respawns in the Thief Den, calls up to 3 idle Thieves
  there ("Brothers, with me...") and leads them back after the Trader: the attacker if it is a Trader
  agent, the Trader (or hirer) the attacking agent guarded, an attacking player who wears a Trader
  suit, or else the nearest Trader agent within 300m of where it fell. The gang rides to the Trader
  while it can be found outside a town (else to where the Thief fell); within sight the usual ambush
  takes over. The hunt ends when the Trader is robbed or reaches a town, when nothing is found at the
  spot, or after 15 minutes; a leader killed again sends its gang home.

Checks: `go build ./...`, `go vet` on `sro-gameworld`, `caravanai`, `party`; `go test` for those
packages (new `convoy_test.go`; scheduler/escort tests updated to the new ranges and roster); client
`npm run typecheck`. `TestCaravanShippedActors` (now 40 actors) and `TestCaravanPartyAndMovementPolish`
skip on this workstation (no extracted client media). Not yet verified in a live run. **No PR or push.**

## Test plan and gaps (2026-11-18)

Live setup: a second local instance beside any running `pnpm local` (alternate ports via the uncommitted
runner overrides `SRO_LOCAL_SHARD_CATALOG`, `SRO_LOCAL_VITE_PORT`, `SRO_LOCAL_AGENT_ADDR`, plus
`SRO_AGENT_PROXY_TARGET`), the disposable world `.state/caravan-e2e-world` (`SRO_LOCAL_STATE_ROOT`), the
published client tree of `opensro-arena-main-sync` (`SRO_GENERATED_ROOT`), live shard settings from
`config/local.env`, GM test character `test`.

| # | Check | How |
|---|-------|-----|
| 1 | 40 actors spawn with gear, 2 solo + 2 escorted runs leave, 4 Thieves patrol | server log, `!caravans`, world map |
| 2 | Idle Hunters/Traders stroll (walking) around each city shop, idle Thieves around the Thief Den buyer | browser, screenshots |
| 3 | Idle agents read as available: no job suit, "[Hunter for hire]" row under the name | browser |
| 4 | Specialty shop talk lists Hunters and Traders only, Thief Den buyer lists Thieves only, rows readable | talk window |
| 5 | Hiring charges the fee (+ goods), agent appears beside the player, greets in chat, joins the party | browser, gold, party list |
| 6 | Hired Hunter roams around the player, runs after past 120m, teleports past 200m | `/MOVETOUSER`-free walking, logs |
| 7 | Two hired Traders: first in party leads, `!leed` hands the lead to the player | browser, `!caravans` |
| 8 | Run end in another town pays the Trader's sale; `!dismiss` returns goods cost | gold, chat notices |
| 9 | Grand caravan musters and departs (`!grandcaravan` for GM) | chat, world map |
| 10 | Robbed Trader cools down, escorts return | `!caravans` after a kill |

Gaps found while planning (being implemented): Thieves were hired at city shops instead of the Thief Den;
idle agents looked identical to working ones; town strolls moved at half speed under a run animation;
dialog rows showed raw `[AI]` names; no hire/farewell lines from the agents; no way to start a grand
caravan on demand for testing.

### Implemented from the gap list

- Thieves live in the Thief Den (Burglar Town, region 24758): idle ones stroll around the Stolen Goods
  Dealer (`NPC_TD_THIEF_BUY`), are hired only there, and only by Thieves; replacements for killed
  highway Thieves leave from there. Hunters and Traders are hired only at the city specialty shops.
- Idle agents drop the job suit and carry a guild-row label "[Hunter for hire]" / "[Trader for hire]" /
  "[Thief for hire]"; the suit returns when they are hired or sent out (respawn of the appearance).
- Town strolls walk (walk mode, 20 units/s) on a ring 8-30m around the shop, pausing between walks.
- Talk rows read "Hire Kwan, Hunter from Jangan (15,000 gold)", agents of the current town first; the
  answer line and a notice give the result; agents greet on hire and say goodbye when released.
- GM-only `!grandcaravan` starts the muster now.
- Live fixes: the store capped runtime actors at 16 (now 64); idle agents no longer scan for monsters
  every tick (caravan hook ~25-60ms -> ~5-15ms per tick); the payout door has an economy reason.

### Live test results (2026-11-18, second local instance, disposable world)

Passed in the browser: 40 actors spawn; idle Hunters/Traders stroll the Jangan shop with "for hire"
labels and no suit; world map labels (Hireable / Escorting); specialty shop dialog lists the agents with
prices; hiring from the dialog and from `!hire` charges 15,000 (Trader 215,000 incl. 200,000 goods),
teleports the agent in, greets, invites/joins the party, re-suits it; a busy agent is refused;
`!leed` toggles the lead; `!dismiss` pays the goods back, says goodbye and dissolves the party;
`!grandcaravan` mustered 6 Traders and 14 Hunters in Donwhang with an announcement; the Thief Den shows
"[Thief for hire]" Thieves and offers no Thieves to a Trader.

Not verified live (unit-tested only): a full hired run to another town and its sale payout, roaming and
200m teleport on the open road, grand caravan departure and arrival, Thief-only rows for a dressed Thief,
robbed-Trader cooldown.

Settings deviation for local runs: this workstation's server game data has no variant overlays, so
`SRO_ALEXANDRIA`, `SRO_CAP110_ITEMS/MONSTERS/TEMPLE`, `SRO_ARABIA`, `SRO_JUPITER(_TEMPLE)` and
`SRO_PHARAOH_TEMPLE` were forced off for the test instance (the GameWorld refuses to boot otherwise).

Known gaps: a hire made in the last 5 seconds before a crash is lost (the flush interval); a payout
to a hirer who is not loaded on the server is lost (logged); idle agents gather on a 30m ring, so a
busy shop still looks crowded.

## November 19 handoff verification

- Preserved Gemini's uncommitted patch and separate respawn source under the ignored local state directory before editing. Reviewed the November 19 prompts in the handoff.
- Started an isolated local world with the shipped assets and the normal scheduler. All four highway thieves left their initial regions. Viper and Kael each crossed several regions. Six other thieves were available in the Den. The authority reported mounted level-90 transport ref 3914 and loads of 41, 71, or 101 stacks.
- Baseline GameWorld tests still assumed the old roster positions and hire dialogue. The corrected fixtures and the caravan, action, GameWorld, and architecture suites passed. Client TypeScript and 145 targeted client tests passed.

The first scratch run exposed two ferry and convoy failures. Hunters boarded other crossings while their trader was still approaching the river. A late escort held the entire convoy on the road, even with other hunters fighting beside the transport. After the ferry fix, Bao, Wei, Ming, and Feng all used source 3827 and destination 19 to reach region 23183. Bao boarded on the level-90 transport at 87,829 HP. The run later failed in South-Tarim after the wait rule held the horse under repeated native thief attacks.

- Escorts now use the leader's precise ferry link. They catch up directly on the road, take the leader's gate route when leaving town, and move the convoy while nearby cover remains.
- Active thieves start on four distinct directed routes. Native combat uses one movement decision per tick. A cast holds route movement, and a verified cast start clears the projected peer path before the skill animation.
- Grouped traders retain their cargo while the living horse waits for resurrection. A failed run clears the cargo and releases surviving escorts. Entering sight of a corpse receives its native life state.
- Full AI loads use native trade tiers. A genuinely one-star caravan keeps the native PvP protection. The shipped data tests use named actors and check actual damage against a loaded trader and an NPC thief.
- In the browser, Fen was seated on the armored horse at the Jangan specialty shop. The client uses the shipped DHORSE1 visual for missing DHORSE3 geometry. The authority retains DHORSE3 level, HP, capacity, and cargo.
- Warlock signed in to the existing world at level 90 and opened the seeded equipment inventory. The saved inventory initially held 13 ninth-degree Sun pieces at +12. Replaced its seeded options with 117 legal maximum values calculated by the native forge, verified the one-character transaction, and kept a local backup. The main merge is `275b3f3e` and includes both the caravan hire and market UI handlers.
- The next uninterrupted run (`.state/handoff-run-soak.log`) confirmed all three escorts used Bao's ferry and cast over 130 skills at native thieves. It exposed another post-ferry bug: escorts still tried to board for the entire outbound leg after they had crossed, so none could catch up once separated. The ferry retry now ends as soon as the leader leaves the landing area. A regression test covers regular following after the landing.

A published [phBot caravan route runner](https://github.com/Vette1123/phbot-plugins#xcaravan) also treats boarding as a distinct step. OpenSRO's implementation uses the server's native ferry admission and arrival.

The final uninterrupted run used an ignored Go overlay for observation only. The regular live
scheduler dispatched Lin from Jangan. Lin and Kwan, Lei, and Gang crossed ferry source 2119 to
destination 10 and reached Elder Leegak's specialty shop in Donwhang at **23:35:28**, after
**18m 38s**. All four survived. Lin carried the 41-stack, three-star load and was mounted
through every travel sample. The trader's only position jump was the native ferry. The
escorts used their configured catch-up placement inside Donwhang. The trace contains **540
accepted casts against 17 distinct native thieves** by those three hunters before delivery.
The observer was disconnected throughout the run. Evidence is in `.state/handoff-run-final.log`.

The trace also shows Viper using a skill against an AI hunter and Kael summoning a transport,
loading one stolen stack, and damaging a native hunter. Kael arrived back in the Thief Den
and became idle after the native five-minute scroll countdown. The shipped-data test
independently covers both named thieves damaging a native hunter. The November 20 browser check below
covers both thieves' animation playback. Bao's separate five-star convoy lost its transport after
its escorts fell, which confirmed that native ambushes and cargo loss still apply.

One thief revenge gang remained at a blocked highway entry after combat. The final shared build
returns a gang home after 60 seconds without movement. Its regression passed independently,
and the shared world on port 15180 was restarted with the fix. The Go caravan, action,
GameWorld, and architecture suites passed on that build. The main merge and feature changes
remain local.

## November 20 browser animation verification

- Used the current branch in a disposable local GameWorld. A Go build overlay placed the two
  named thieves in South-Tarim and spawned a native NPC hunter for repeatable casts.
  The browser received the normal combat and movement packets. The observer sent no attacks.
- Viper showed spear wind-up, swing and thrust poses, visible hit effects, and recovery.
  Kael drew and released the bow from range, with visible projectile travel and impact.
  Repeated samples showed both returning to grounded combat poses. The browser console
  showed zero errors and no cast or animation warnings.
- The server log recorded 40 accepted Viper casts (base 40 and trained skills 19095, 19134,
  19297) and 31 accepted Kael casts (base 70 and trained skills 19330, 19391, 19430).
  The cast log is `.state/thief-animation-browser.log`. The visual check sampled repeated
  combat sequences. The skill ID coverage comes from the server trace.
- The reported animation problem did not reproduce on the current branch. This verification
  changed the implementation notes only. The scratch overlay was excluded from production
  source, and the shared caravan world remained in service.

## November 20 caravan balance and controls

- Solo dispatch caps the trader's cargo at two stars. The transport loader checks
  the actual native or gold-based cargo rating and trims any heavier load left
  over from a previous run. Escorted and grand runs restore the trader's roster
  cargo profile.
- A trader that loses its own run asks for one more hunter, up to four. A
  successful delivery asks for one fewer. A robbed solo trader waits for a
  hunter before dispatching again. The scheduler prioritizes these traders
  for escorted slots. Escort preferences are held in the running shard.
- Escorts near a stationary player change their position more often. At 30 m
  they start catching up when out of combat. Hired followers and automated
  convoy escorts can fight within 100 m of their leader. Past 100 m they
  rejoin and ignore targets until they are within 30 m again. Existing ferry
  boarding rules apply on ferry legs.
- AI dialogue uses normal white chat, scoped to players who can see the
  speaking character. Dialogue is delivered after actor appearances and stays
  out of the global chat history. System replies to caravan commands keep
  their existing channel.
- With AI caravans enabled, dismounting a moving trade transport stops and
  parks it in one request. The native stop-first rule still applies with the
  feature off and to ordinary riding horses.
- AI thieves load stolen goods into a trade transport through normal pickup,
  ride at the same transport pace as traders, and use the native Bandit Den
  Return Scroll. The return uses the shard's configured countdown.

The regression suite covers loaded cargo shrinking from five stars to two,
an actual thief defeat followed by an escorted dispatch, a successful delivery
reducing that escort to zero, straight-line catch-up at 30 m, the combat tether,
the normal chat packet, and trade-transport dismount with the feature off and on.

Shipped-data tests also verify that a thief can collect real cargo, start the
native scroll, mount during the countdown, and match a trader's transport speed.
The action, caravan AI, chat, GameWorld, and architecture suites passed. The
shared local shard on port 15180 returned ready after rebuilding from the worktree.

Browser checks used the isolated Jangan shard on port 25180. Before the fix,
Dismount was refused during a ride and worked after stopping. On the updated
build, one click during a ride stopped the horse, put the character on foot,
and restored the Board control. Hired Hunter Tian moved around the stationary
trader, and Tian's farewell appeared in white in the All tab and as a speech
bubble. The later chat ordering change for greetings was covered by the server
suite. A second full town-to-town ferry run was not part of this balance check.

## November 20 thief cargo pickup before return

The Bandit Den scroll blocks native trade-goods pickup while its countdown is
active. Thieves now resume looting after combat, prioritize trade goods within
30 m, and check the current ground again before starting the scroll. A full
transport can still take goods that fit an existing stack. Once the scroll is
active the thief stops looking for cargo.

The shipped-data GameWorld regression drives the actual loot scan and pickup
hooks, clears three cargo drops, and checks that the scroll stays off until the
last pickup. It then advances the native countdown and verifies arrival in the
Bandit Den. The GameWorld, caravan AI, inventory, action, and architecture suites
passed.

## November 20 escort combat distance

Regression checks cover both a player-hired hunter and an autonomous convoy
hunter. The autonomous escort fights at 100 m, runs toward the trader at
110 m, keeps following at 65 m, and can fight again at 30 m and on a later
chase. The hired hunter keeps its normal straight-line catch-up behavior at
30 m and uses the same combat limits around its hirer.

The combined loot and escort build reported ready on ports 15180 and 25180.

## November 20 hourly grand caravan verification

The simulated clock test verified the first departure and the next hourly
muster, two traders, three hunters per trader, and overlap prevention.
The shipped-data GameWorld test verified five-star markers for both loaded
transports. The caravan AI, action, GameWorld, and architecture suites passed.
The isolated local shard on port 25180 reported ready.

## November 22 grand caravan fixes

Both escort groups stage at the lead trader’s ferry. A trader that crosses
as a follower passes the exact native ferry link to its own hunters while
it is near the landing. Hunters already on the landing side resume following.

The cargo loader trims the actual retained cargo when the requested rating
falls and preserves the solo two-star limit. The five-star marker publisher
reads server-controlled transports alongside connected players’ transports.

The caravan AI, action, GameWorld, and architecture suites passed. Regressions
cover the second escort group at the ferry, the five-to-three-to-two cargo
transition, and five-star marker packets for AI actors without player sessions.

### Live Donwhang to Hotan run

The isolated browser shard ran the normal two-minute muster and then the
full Donwhang–Hotan route. The observer only moved their own character and
stayed invisible and out of combat. No route, combat, or ferry behavior was
changed for the test. The server overlay recorded route and ferry events.

- Hua rode with Shan, Hao, and Lan. Zhou rode with Rui, Bo, and Tian. Both
  mounted traders left Donwhang at about 17:57 with five-star cargo. The
  browser showed hunters fighting thieves in Earth Ghost Canyon. Shan was
  knocked out in the ravine and rejoined Hua before the crossing.
- At about 18:26, both traders and all six hunters crossed Tarim Ferry
  through native link `3825 → 17`, landing in region `23180`. The browser
  showed both mounted traders at the approach and all eight still on the
  grand route after the crossing.
- Both traders reached Specialty Trader Samok in Hotan at about 18:31,
  roughly 34 minutes after departure. The last active route sample showed
  both carrying five-star cargo beside the shop. The browser map then showed
  both back at their usual four-star rating in Hotan and all six hunters
  hireable there.

The five-star overhead icons were not independently resolved in the browser.
The GameWorld regression verified the marker packet for both loaded AI
transports. See `.state/grand-browser-live.log` for the live ferry trace and
`.state/grand-browser-tests.log` for the regression results.
