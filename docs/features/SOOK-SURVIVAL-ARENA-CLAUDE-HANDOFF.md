# So-ok Survival Arena — Claude handoff, 2026-11-09

Worktree `E:\CodeProjects\opensro-sook-arena`, branch `feat/sook-survival-arena`.
Merged upstream through **a5cb558f**, merge commit **ca37f281** (earlier merge
7372233c). Feature changes remain local/uncommitted; no push requested. A safety
stash from the earlier conflicted merge remains; do not pop it over current work.

Matched **protocol 16**, **store schema 42**, layout 12. Upstream protocol 15
pet frames and schema 41 Gourd counts were retained; all historical v41 field
manifests are upstream's immutable versions. New v42 manifests include arena
fields/chest plus Gourd count. Agent, GameWorld and client must deploy together.
No PK2 changes. Source-size fix moved item lookups into itemrefs_lookup.go.

## Current user request implemented

Five stages replace the fixed 20-minute match. Each has five combat minutes,
then a clearing phase. Level bands: −6..−3, −4..−1, −1..+2, +1..+4, +3..+6;
level floor 1. Uniques must also fit the band; missing ones are skipped. Packs
are 3/6/12/24/48, subject to the existing 100-living-monster safety limit.
Opening countdown 20s; Next stage closes the chest immediately and counts down
15s, then spawns the next wave. End-of-stage notice names its stage and tells
players to kill survivors. After 60s, surviving monsters are forced to hunt.

After clearing: five-minute decision, safe timeout extraction. Stage V cannot
continue. Chest shows 32 items/page with native icons/tooltips and navigation.
Accumulated chest targets: 3–7 / 8–15 / 18–28 / 32–45 / 50–70 rows total.
Each stage tops up the retained chest; stage V does not add another 50–70
on top. Capacity stays bounded at 200 for operator overrides.

Rewards: normal/Star/Moon/Sun gear, pet gear including seals, native supplies,
and 10-silk vouchers. Final-stage cycles explicitly include the three seal
tiers where authored variants exist. Pet blue rolls reuse DB affix settings;
normal property/plus/blues use existing generators. Silk vouchers use owned
ref 0x7F531001 and a native scroll icon. Their use atomically commits inventory
consumption and real mall-wallet credit through the existing purchase transaction
owner, with silk-history charge in that same transaction. Replay, missing row,
duplicate slot, bad stack and overflow refuse without another credit. Earned
vouchers remain usable when new arena entry is disabled.

EXP/SP and pet EXP are 5x, ordinary arena floor loot remains suppressed.
Power eggs spawn every 8s rather than 15, including clearing, not during reward
decisions. Fire Pit now removes 3% max HP/sec from non-uniques. So-ok's option
is “Survival Arena (5 stages, EXP x5)”; entry notice no longer promises 20min
or 10x loot. Native overworld loot is unchanged.

## Preserved reward mechanics and fixes

- Exactly-once chest grants, separate idempotent kill-gold payout, full-bag
  saved entitlement at So-ok. Carried gear is never risked.
- Death forfeits chest but keeps kill gold; five-second return remains.
- Combat disconnect forfeits; checkpoint reconnect and graceful shutdown
  preserve earned rewards. Resume costs no daily entry. Recovered UI can close
  so the player can select So-ok before claiming/resuming.
- Fixed the live reconnect race: population membership/town admission could
  retire before socket cleanup, causing end(false) to erase the chest. Both
  paths now call checkpoint-aware cleanup; regressions exercise later ticks.
- Player/pet active and passive reuse, negative-state deadlines and arena
  powers pause during decisions/countdown; pet HUD and power timers resync
  on resume. Late incoming owner/pet hits are rejected during the pause.
- Chest bodies/references publish once per decision rather than every second;
  local countdowns avoid repeatedly shipping up to 200 static reward rows.
- Existing arena-only actor-room/holder cleanup retained. No claim that its
  old 512-actor stress acceptance has been completed merely by removing loot.

## Verification

After main merge: full action tests PASS (38.736s), store PASS (5.013s),
enterworld, arena, domain, release protocol and GameWorld wiring PASS. Scoped
Go vet/build PASS. Updated scheduling/fire-pit/5x assertions pass. Maintained
native-reference bundle test verifies every stage's count range and real item
identities, including silk. Atomic silk test covers failed save and replay.
Client quick/type/ownership/capability checks PASS. Test types retain inherited
ledger errors only (269 files; main changed its count); no new errors. Parser,
tooltip and six effect-budget regressions PASS. UTF-8/LF, size, format and git
whitespace gates PASS. No conflict markers remain.

Earlier live prototype acceptance (before the five-stage revision): real
Antelope kills, no ordinary floor loot, eggs present, checkpoint I, Continue,
countdown, checkpoint II and extraction passed. Fixed metadata omission; two
stage bundles yielded exactly six delivered items (bag 45->51). Reconnect
retained run ID 2:1794227949130:10, stage I and two items after the cleanup fix.
Resuming spent no entry; timeout with a full 92-row bag returned to town with
both items saved. Server restart preserved the same pending ID/items and
GoldPaid=true. Freeing the isolated fixture bag retained the entitlement.
The final post-restart claim was not executed before the user changed scope;
ordinary live claim and atomic full-bag/replay tests passed separately.

**The new five-stage/large paged reward flow has automated coverage, but has not
been played through end-to-end live. The user explicitly asked to test it next.**
Do not claim a five-stage live completion, all seal/pet variants visually
verified, or a new 100-monster crowd acceptance. Fire Pit gameplay/HUD clocks
pause; its existing cosmetic world effect still uses its presentation clock
and may fade during a long decision (cosmetic continuity remains to revisit).

## Preservation and launch

Only `.state/sook-live/authority-copy` is used. Original authority untouched.
Fresh preservation baseline: `reward-chest-live/characters-before.json`, not
older death-effects snapshots. Schema upgrade backup:
`authority-copy/state.before-upgrade-2815539774.db`.

All temporary LabPetTest/Petter records restored before the new deployment:
LabPetTest inventory44/skills212, Petter inventory36/skills16. LabBlader,
BondOwner and BondParty gear/skills compare exactly. **LabWizard changed
independently** during play (NPC sales/buyback counter and online time advanced):
its current inventory26/skills96 is preserved, not overwritten with stale
inventory45. Do not “restore” that older inventory. The reset helper changed
only daily arena counters for LabPetTest and LabWizard to zero for today.
Report: `reward-chest-live/final-preservation-report.txt`; full after-reset
snapshot is `after-final-reset.json`. No test filler gear remains.

Current GM allowlist unchanged: only global-official:LabPetTest. No new GM
grants. Test DBs, helpers, binaries and screenshots stay ignored in .state/temp;
do not commit recordings or .playwright-cli logs.

Operator configuration restored to normal: SRO_SOOK_CHECKPOINT_SECONDS=300,
SRO_SOOK_CHEST_CAPACITY default200, SRO_LEVEL_CAP=90. The checkpoint setting
accepts 30..300 for testing; never leave the local 30-second test value active.
Obsolete SRO_SOOK_REWARD_ROLLS is replaced by the documented stage bundle ranges.

Latest installed binaries:
- GameWorld 591D207709EC722E4CFD538942023F3A77216B9C575254DC0E8FD846BE1CBE2B
- Agent AE3F3B9EAD1DA7A7FD833FFD1D943A109EFC3B554A21D8D7263F2EFA1282954F
Agent restart eval 49dee760-63ff-a7b5-976a-353f9ad5fd45;
GameWorld eval dd3d888c-269d-3a5b-af68-121978ea84f1.
Client URL: http://127.0.0.1:5180/?diagnostics . Final readiness/launch observation
is appended below after startup settles. Current player guide is
SOOK-SURVIVAL-ARENA.md; the old reward-checkpoint plan is historical.
Launch asset repair: merged main requires /assets/data/event-interface.json, absent from the old local variant. Rebuilt buildTextResources with SRO_GAME_ROOT=.state/colosseum-variant and SRO_GENERATED_ROOT=.state/colosseum-variant/.generated; the new file is served HTTP200. Only generated variant text data changed. Do not start an asset build with those overrides omitted (default extracted root is absent here).
The new JSON also needed pack-index publication, not merely a loose HTTP200 file. Published /assets/data/event-interface.json into the existing game-data group through publishLooseFamily, owner event-interface, under withGeneratedAssetsLock. Refreshed web manifest and sidecars; existing unrelated packs retained. HUD missing-resource error then cleared on reload.

Final startup: Agent /readyz and GameWorld /transport/readyz both return ready. Browser loaded protocol16, HUD missing-resource fault cleared after indexed publication; normal game UI observed. Used the native System > Restart flow to return to character selection without consuming an arena entry. No new five-stage match was started; daily caps remain reset for the user's test.

Final wallet concurrency review: BetaSilk voucher wrapper now holds the same mutex and ensures the starter grant as its existing PurchaseMall path; focused arena/beta/wallet tests pass. Latest GameWorld SHA256 F2F5181CA9E2905442C6355DC9D136AD83187C38293774911D4E3A8BB8E8BE87, restart eval c8f8e5af-8fd5-7dec-757b-1d3641876799 (supersedes earlier hash/eval). Agent unchanged. UI stage button is labelled Next stage.

Final reward interpretation confirmed in code/tests: the accumulated stage-V chest is 50–70 items TOTAL, not another 50–70 on top of earlier rewards. Stage targets are 3–7/8–15/18–28/32–45/50–70, topping up immutable saved earlier items. Focused arena regressions pass. Latest GameWorld hash 05812E3276B649C8A1B1BA8DC28DF7688CDCF15A427499247A84277F6B34577F (supersedes earlier hashes); final startup eval follows in status. No entry spent.
Final GameWorld restart eval: 5bbdad5f-cec2-2903-6b13-8029c28c9fad. Client observed at character-select with Create/Cancel and no loading failure; protocol16. LabPetTest/LabWizard daily counters remain zero because no new arena run was entered.

Final authority checks: both readiness endpoints return ready on the final reward-target build. Client stays open at http://127.0.0.1:5180/?diagnostics with no loading-failure screen. Final backend restart returned the client to login; earlier character-selection observation was before that restart. No new arena run entered by this agent. Leave user controls alone for their test.
Latest read-only browser observation after the login transition: phase=world, character=LabWizard, no loading failure. User appears to have signed in while the agent was finishing; do not send further controls or restart the stack during their test.


## 2026-11-09 lighter stages and Chain Reaction (latest)

Implemented and activated in the existing arena worktree; no new merge/push.
- Normal stage waves now retain exactly 60% of the former budget across five
  waves: 9/18/36/72/144 monsters instead of 15/30/60/120/240. Integer carry
  keeps stage I at 2/2/2/2/1; each later stage still doubles the average.
- Five-minute combat deadline verified at the exact boundary. Clearing still
  requires all survivors dead; after 60 seconds, hunt reissues every 10 seconds,
  with its banner only on the first hunt.
- Clear all indexed old eggs/Pandora props at stage resume BEFORE creating the
  fresh stage egg. Active buffs keep their existing paused deadlines.
- New stage-I/II rolls cannot produce Sun character OR pet items. Do not
  rewrite already saved rewards or owned gear.
- Unkillable is 30 seconds, matching server protection and client HUD.
- Chain Reaction egg (kind11/ref0x7f53000b), 30 seconds: player basic/direct skill
  damage is copied to at most ten other live monsters within 10m of the target,
  exact run lease only. Copies actual HP debit (not overkill). AoE uses its first
  damaged monster once per strike. Credited secondary hits cannot recurse.
  Damage-over-time/pet hits do not create copies. Uses existing Damage Surge
  icon; no new bitmap or PK2 edit. Expiry/exit/resume HUD paths are wired.
- Removed obsolete So-ok open-dialog banner (20min/x10 loot). Menu and entry
  now state EXP/SP x5; existing fivefold EXP/SP settlement verified. Corrected
  stale Fire Pit HUD description to its existing 3% damage.

Verification: full action suite (41.553s), pure arena suite, scoped go vet,
GameWorld build, verify:quick, 8 parameter-job/HUD tests, source size/encoding/
formatting and diff whitespace checks PASS. New tests cover transfer 10-target
cap, actual HP debit, isolation, expiry; old egg cleanup; 30s protection boundary;
repeating hunt and exact 5min spawning stop; 600 early-stage reward rolls.
One stale entry-banner assertion and an old HUD test sending a 60s Unkillable
packet failed initially and were updated to the changed contract; reruns pass.

Deployment: stopped ONLY sro-gameworld-global-official after read-only browser
confirmed LabPetTest outside arena in Jangan (region23687). Backed up stopped
state.db + previous executable to .state/sook-live/pre-egg-update-20261109-185114.
No character/gear/reward/entry-counter edits or fixture restores. Original
(non-test-copy) authority untouched. Agent unchanged, protocol16/store42/layout12.
Installed GW SHA256 28E4633664F21F67359D27C0B2A333B68C72C43635D0EF02C5DF67DF90CBA426,
PID25456; Nomad start eval caf1beee-949c-f8ee-5832-6d7a83c24670. Both readiness
endpoints ready; checkpoint environment remains300 seconds. Reloaded pet13
browser http://127.0.0.1:5180/?diagnostics for the owner's testing.

Not yet verified in a live full five-stage playthrough: new Chain Reaction egg,
repeated hunts during extended clearing and visual old-egg removal. Do not
claim those passed live based on synthetic/runtime tests. Saved earlier Sun
rewards are intentionally retained. No recordings committed.

Final post-reload observation: client reconnected into world in Jangan23687, native HUD visible, frontend error none, zero failed UI images/world textures. No arena entry or further game controls sent. Leave session to the owner.


## 2026-11-09 final egg/reward balance (supersedes prior egg timing/odds)

- Chain Reaction now uses an authored cyan linked-node icon. Added Fleetfoot
  egg kind12/ref0x7f53000c: +200% movement speed (3x normal) for30s, with a
  distinct green boot icon. Shared live movement projection and native speed
  packet update on pickup, expiry and checkpoint resume; roots/slows preserved.
  Mounted motion retains native mount ownership, like ordinary player haste.
- Mirror Shell reflects500% of actual HP debits, capped at wire maximum;
  absorbed/blocked damage and overkill still do not reflect. Does not prevent
  incoming damage or multiply again through Damage Surge.
- Stage-end removes ALL indexed eggs/Pandoras immediately, cancels owed eggs,
  and suppresses periodic, boss-half-HP and kill-earned egg spawns while clearing.
  Resume cleanup remains defensive. Expiry now sends a client despawn even if
  another ground owner already removed the registry row (stale ghost fix).
- Clearing notices report actual remaining count from10 down to0, once per
  changed count, alongside the existing repeating hunt.
- Removed guaranteed stage-V seal tiers. New per-gear Sun odds: I0%, II0%,
  III0.1%, IV0.5%, V1%. Applies equally to ordinary/pet gear. Earlier immutable
  saved rewards/owned Sun gear are NOT rerolled or deleted.

Checks: full action suite39.941s and pure arena suite, scoped go vet, GameWorld
build, client verify:quick,9HUD tests,8-icon art uniqueness/64px test, source
format/size/encoding/diff checks PASS. Further focused tests passed for stale
client despawn, zero remaining count and no eggs during clearing. Exact Sun
roll test enumerates all10000 probability buckets per stage. Fleetfoot test
checks authoritative walk/run speed3x, native speed publication, owner isolation
and29999/30000ms expiry. Reflection tests cover physical/magic/periodic/lethal/
foreign/expired/zero debits at500%.

Both new art files are source assets under docs/features/assets/survival;
regeneration extends scripts/build_survival_buff_icons.py. Full CIF builder
copies all8icons. Published ui-icons through buildSurvivalBuffIcons and
publishLooseFamily under the generated-assets lock, preserving pack ownership
and refreshing manifests/sidecars. No PK2 edits.

Deployment backup (stopped GW): pre-final-egg-balance-20261109-201019/state.db
and previous executable. Original authority, character gear and entries not
edited; no fixture restore. Read-only client observed in Jangan23687 before
restart. Installed GW SHA256906E6AECAF49FF5270542CCDCD03364CFAFA9649DDD7612D4ACA32DEBEE5E6EC,
PID49580, Nomad evale2211bb5-0e2b-143f-d89e-81c85bc25d69. Protocol16/store42,
Agent unchanged; checkpoint300sec. GameWorld ready; client reload follows.

Live five-stage/egg visual verification still belongs to the owner; code and
packet tests are not a claim of a completed live playthrough. No commit/push.

Final: client reload reconnected to world/Jangan23687, no disconnect screen. Type-check followup fixed optional HUD-help narrowing in the test; verify:test-types PASS with unchanged269-file/2892-error legacy ledger,9HUD tests pass again. No new errors accepted into the ledger. Source format/diff checks PASS. Owner can test now; no arena entry consumed by the agent.

2026-11-09 owner requested daily reset, clarified LabWizard (not LabTester). Stopped test GameWorld, backed up state.db and all character JSON under wizard-daily-reset-20261109, ran rebuilt entries-only helper. LabWizard entries3->0; zero is omitted by store JSON. Normalized full-record comparison verifies all other fields and every other character unchanged. No level/gear/skill/wallet/reward changes. Restart evalb96a9bb5-94a7-6323-b1cd-43662b27ef1f using same final egg-balance executable, checkpoint300. Verification report saved in that backup folder.


## 2026-11-09 preparation countdown and native-style chest (latest)

Owner reversed the clearing egg suppression: fresh eggs now continue every8s
while remaining monsters are cleared, including kill/boss earned eggs. Existing
eggs are still purged at stage end and on resume. Egg spawns/pickup still stop
for the reward decision and15s preparation countdown (no monsters then).

Countdown now allows character skills through BOTH client keyboard handling
and server skill admission. Continue restores frozen reuse once, including pets,
then discards the pause snapshot. Reuse ticks normally during preparation;
StageResume cannot overwrite newly cast buffs/cooldowns. Native MP, equipment,
status, learned-skill and cooldown validation remains. Negative states and arena
power deadlines keep their previous pause through preparation. Client cooldown
projection uses live time during countdown, frozen time only during decision;
closed/resumed projections include fresh cooldown state. resumeSookSkills clones
saved maps, so a retried failed store update cannot double-shift the snapshot.

Reward window is460x432, uses shared main-frame/title, native tiled message
background and buttons, a compact32-item grid with existing item effects/help,
gold summaries and concise labels. Pagination and server-owned reward decisions
remain unchanged. Native main-frame paths explicitly join resource admission.

Tests: real shipped Chinese movement buff refuses during decision, casts during
countdown and survives resume with its cooldown unchanged; old reuse freezes
only for decision, not countdown. Egg cadence/decision-pause tests updated.
Full action40.053s and pure arena suite, scoped go vet, GameWorld build, chest
parser tests and client verify:quick pass (final resource-path quick rerun follows).
Source encoding/size/format and diff checks pass. No live chest screenshot or
full-stage playthrough claimed; owner visual review remains needed.

Deployment: backed up stopped state.db + prior executable to pre-preparation-update-20261109-213354. Installed GW1DCB6EF8552DFD0D7DD199A3BD3BD4FD2006BCEB5FE0D80BA6F0CAFA76B0EBDA, PID28780, evalf847b6d4-e995-cf1d-fcb5-edefa4701bb2. Agent/protocol16/store42/300s checkpoint unchanged. No character/gear/reward/entry-counter edits or restores. Final client verify:quick and source gates PASS. Readiness/reload observation follows.

Final startup: Agent and GameWorld readiness both ready. Refreshed pet13 client, native Silkroad game interface reappeared successfully. No arena entry consumed. Leave controls to owner for reward-window visual review and live preparation casting.


## 2026-11-10 arena stack restarted after host/session shutdown

Client5180/Nomad4646/Agent8787/GameWorld8788 were all stopped. Started existing
arena-nomad.exe with explicit original hub dev-windows.hcl config and original
hub .state/nomad/dev-agent data-dir; worktree lacks its default config path.
Started existing .state/Start-ArenaClient.ps1 hidden, using Colosseum overrides.
Nomad dev server lost its in-memory jobs/variables. Restored ONLY original
Agent credential values from last rendered allocation and verified its private
keyring matches the central operator file. Account chunks and GameWorld public
verifier were restored through Nomad Variables (not printed). Parsed original
Agent HCL with current agent-arena.exe and existing accounts.db. Correct
SRO_AGENT_DIRECTORY_STATE_PATH is cluster/agent/shard-leases.json, NOT the
cluster/agent directory (first attempt failed before serving). Latest restored
job JSONs saved under .state/sook-live/startup-recovery for future recovery.
Restored saved GW job stopped, then used Start-ArenaTest.ps1 to select existing
copied schema42 authority and the latest preparation-countdown binary.

Agent allocation a9b9d63a-22b5-8104-ef01-b854ba62a30c; GW33564367-c40b-b199-28ca-3018285ec42f.
Agent eval78b21d56-416a-e9cf-7b1b-afdde5d4068b; GW5429ce25-d270-9ee5-102c-46dd51835805.
GW binary unchanged SHA2561DCB6EF8552DFD0D7DD199A3BD3BD4FD2006BCEB5FE0D80BA6F0CAFA76B0EBDA.
Both readiness endpoints ready after world warmup. Opened headed pet13 browser
at5180/?diagnostics and reloaded after readiness. No character/gear/reward/
entry edits, DB restore, new fixture or source changes. Existing accounts DB
retained. Owner will log in/test; no arena run started by this agent.


## 2026-11-10 fixed wave budgets / 200 monsters - BUILT, activation pending

Owner requests fast clears finish stages before five minutes while timed
reinforcement continues with survivors. Implemented a budget equal to the
normal timed programme: WaveLimit=1+(CheckpointInterval-1)/(15000+1200),
default19waves. Empty arenas still immediately start the next Pandora after a
committed kill, respecting1200ms windup. After the wave budget, enter clearing/
checkpoint early instead of requiring five elapsed minutes. Five minutes stays
the maximum spawning window if the cap/survivors prevent spending the budget.
Eligible unique is attempted near the last waves or the original late-stage
time, whichever first; early completion requires that attempt (failed spawning
can retry). MaxAlive=200 shared by waves, uniques and ambush eggs. A cap filled
during a pending Pandora does not spend an empty wave.

Tests: full action40.952s and purearena suites PASS; scopedvet and build PASS;
final purearena rerun includes pending-cap budget test. Other new tests cover
fast completion through all5stages with exactly19waves and one unique attempt,
no extra wave after checkpoint, and timed reinforcement with no kills. Updated
hard-cap/ambush tests for199/200. Source encoding/size/diff checks PASS.

Built .state/sook-live/gameworld-fast-stage.exe SHA256 21BF9F63A25C786229F25AF89661D68250C34DB78E482053BA1FFDC6E65A9BB6.
NOT installed yet: browser owner is actively fighting in Colosseum25580. Asked
whether to restart now (ends run) or wait for completion. Preserve the existing
running instance until response or until character safely returns to town.
Current installed build remains preparation/countdown build1DCB6E..., no entry
or gear edits. Player guide updated. No commit/push requested.


## 2026-11-10 fast stages and grain rewards ACTIVATED

Owner explicitly approved Restart now. Installed fast-stage build21BF9F63...
(evald25fa0bf-7027-d2d0-9de7-dc89cb66fd91), then owner added the reward change
before handoff. No new run was active (browser signed-out) for the second deploy.
New reward supply selection keeps native _SPOTION_ grains only, Quantity50;
regular HP/MP potions and universal/cure pills fall back to normal gear rewards.
HP, MP and vigor grains are retained. Existing saved rewards/carried items are
not rerolled or deleted. Native reward regression sampled3000 supply attempts,
found grain rewards, verified every grain stack50 and no potion/cure output.
Focused action/arena tests PASS, then built final gameworld-fast-stage-grains.exe.

Normal five-wave repeating patterns by stage:2,2,2,2,1 /4,4,3,4,3 /8,7,7,7,7 /
15,14,15,14,14 /29,29,29,29,28. Full19-wave totals35/69/137/274/548 (excluding
eligible unique/ambush). Same timed15s+1.2s Pandora all stages; empty arenas
start Pandora immediately.19wave budget can finish early; five-minute spawning
cutoff and200living cap remain. This supersedes pending-activation note.

Final stopped-GW backup: pre-grain-rewards-20261110-185521/state.db + prior exe.
Installed B7B2DF9E5D5B8C48A2F7330B1C7F268C9DB596854CEE0A7B83CA2F1D51145350;
start eval3c0e06fc-0a41-ab63-543c-07b97b1c9554. Original authority untouched,
no character/gear/reward/entry-counter edits or fixture restore. Agent unchanged,
protocol16/store42/checkpoint300. Readiness/client refresh follows below.
No commit/push requested. New whole-stage live verification not claimed.

Final startup: Agent8787 and GameWorld8788 ready on final grain build PID22640. Client pet13 refreshed for owner testing; no new arena entry consumed by agent.


## 2026-11-10 stage-scaled silk stacks ACTIVATED

Owner repeated grain-only supplies and requested silk1..3 in first stage,
1..20 in final, then explicitly requested restart. Existing grain-only filter
and50-stack rule verified unchanged. New silk quantity per voucher reward stack
is1..3/1..5/1..10/1..15/1..20 for stagesI..V. Still10silk per scroll. Existing
saved/owned rewards are not rerolled or deleted. Multiple voucher reward slots
can exist in a chest; these are PER-STACK ranges, not a cap on whole-chest silk.

ArenaSilkScroll reference maxStack is20. Store redemption accepts1..20 only,
clones the bag and atomically decrements ONE (or removes the last) with wallet
credit/history. Failed saves preserve the full stack. Item-use response now
reports the actual remaining stack rather than always0, preventing client
removal/desync after using one voucher from a stack. No schema/wire change.

Tests: sampled all quantity outcomes per stage, metadata cap20,20scroll claim
fits one bag slot, response reports19remaining, durable20-use loop and replay,
failed-save rollback. Focused Sook/ArenaSilk tests PASS, complete store suite
4.684s PASS, scoped action/store/enterworld vet and GameWorld build PASS. Source
encoding/size/diff checks PASS. Initial range test used an item fixture without
the voucher reference; corrected fixture to use actual ArenaSilkScroll metadata.
No product fallback was added to hide missing references.

Stopped only test GW, backup pre-silk-stacks-20261110-190408/state.db+oldexe.
Installed2B3703E3878FF9CB520D2BEEB8777C4CB8719FA3BC7401220CF85684E2D5B8EA,
evalf417969a-2355-2ffd-4e8a-0165f4e2e15b. Agent unchanged. Uses same copied
schema42authority, protocol16, checkpoint300, fast19-wave stages and200cap.
No character/gear/entry reset, fixture restore or manual wallet edits. Final
readiness and browser refresh follow below. Live reward/redemption UI test
remains for owner. No commit/push requested.

Final: both Agent and GameWorld ready; final silk-stack build PID15100. Refreshed pet13 client for owner testing. No live redemption performed by agent.

2026-11-10 owner requested entries reset again (LabWizard per prior clarification). Checked inactive run; stopped only test GW; backed up full state.db and character JSON under latest-reset-backup.txt path. Entries3->0, normalized full-record comparison verifies all other fields and every other character unchanged. Existing final silk-stack build retained. Restart evalcf6a107f-490b-3934-7442-de5211e96880. No gear/skills/level/reward/wallet changes.
