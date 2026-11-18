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
- **Hiring window** (port-only `0x3FC3` S->C JSON, `0x7FC3` C->S `u8 op, u8 stars, name`): the
  specialty shops' talk menu has one row, "Hire Hunters and Traders" (the Thief Den's Stolen Goods
  Dealer: "Hire Thieves"), which opens the window. It lists every agent of the kinds hired there,
  available ones first in green, the rest in red with the reason (on a trade run, hired by X, waiting
  in another town, out hunting a caravan, won't work for you, ...). "Show only available" filters the
  list; Previous/Next page and Refresh. Clicking a green Hunter or Thief hires it; clicking a green
  Trader opens a second dialog to pick 1-5 stars of goods (100,000 gold per star) with the total.
  Hunters and Traders hire Hunters and Traders; Thieves hire Thieves; the player must be dressed and in
  town. Flat fee 15000 gold plus a Trader's goods. At most 7 hires at once. Hunters and Thieves appear
  at the hirer; a Trader is hired only in the town it waits in and walks over. Everyone hired is invited
  to the party. `!hire <name>` still works from chat (a Trader then carries goods of its own rating).
- **Leading:** hired Hunters/Thieves follow the hirer. Hired Traders follow the first hired Trader in
  party order (hire order when not in the party), which drives the run itself and waits while the hirer
  is more than 120m away. `!leed` (or `!lead`) toggles the hirer leading all of them.
- **Hire end:** one trade run, at most 1 hour. The run is done when the hirer, after leaving town,
  enters another town (or the same town after 10 minutes out), or when the leading hired Trader reaches
  its destination shop. Then hired Traders pay the goods' sale (cost + 15% per cargo star); after the
  hour, a lost job, `!dismiss`, or the hirer being offline/out of the main world for 2 minutes they pay
  the goods' cost back; robbed goods pay nothing. A hired Hunter or Thief that dies comes back to its
  hirer; a hired Trader that dies is robbed, its hire ends and it rests 30 minutes.
  Released agents leave the party and go home (teleport if outside town).
- **Restarts:** hires in flight are mirrored to the `caravan_hires` table every 5 seconds (and at
  shutdown). At boot each one resumes: the agent waits at the hire town's shop for up to 10 minutes,
  then teleports to the hirer and rejoins their party when they log in; the hour still counts from the
  original hire. A stored hire whose agent is busy or gone pays the goods' cost back.
- **Standing runs:** 2 solo Traders and 2 Traders with 3 Hunters each are kept on the road. Escorts are
  released in the arrival town. Solo Traders pick either ferry crossing at random and bend every open leg
  of 150m+ through a point 60-150m off the road.
- **Grand caravan:** 30 minutes after boot and then every 3 hours, every idle Trader and Hunter musters
  at the shop of the town most of them are in (announced in chat) and leaves 2 minutes later: the first
  Trader leads, other Traders follow it, Hunters are shared out between the Traders. Arrival releases
  everyone; a robbed leader disbands the caravan home.
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
