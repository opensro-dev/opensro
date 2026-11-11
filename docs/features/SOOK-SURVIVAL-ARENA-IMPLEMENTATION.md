# Survival arena implementation handoff

## Contract and flag

This is a port-only PvE event, not a reconstruction of the official PvP event.
`SRO_SOOK_SURVIVAL_ARENA` defaults to `off`. Configure it on the GameWorld and
restart to enable entry, stages, eggs, rewards, chest decisions and silk use.
The shared shard-setting catalogue also defaults it off. The client presents
only server-owned arena state; it cannot enable the event locally.

With the flag off, So-ok has no arena option, requests cannot enter/resume/
claim, vouchers cannot be consumed, and combat/reward modifiers have no run to
apply to. A state probe returns only a closed, empty projection. Earned items
remain stored. Login recovery may still return a character stranded inside a
previous run to safety; this never starts an event or spends an entry.
Reference decoding and schema support remain available for saved items.

Matched Agent/GameWorld/client protocol **17**, asset schema **4**, authority
schema **42**, layout **12**. Protocol 17 combines main's protocol 16 pet-chat
links with arena checkpoint frames. Old tabs must reload. Schema 42 adds optional
arena fields on top of schema 41's Halloween Gourd count; historical manifests
must not be rewritten. Back up the authority and use the normal schema upgrader.
Disabling the event does not downgrade the authority schema.

## Current mechanics

See [player/operator guide](SOOK-SURVIVAL-ARENA.md) for all numbers. Five stages,
private leases, three entries per UTC day. Default 15-wave budget; fast clears
accelerate Pandoras and finish sooner. Surviving monsters receive timed waves;
spawning stops after five minutes or when the budget is spent. 200 living cap.
After spawning ends, survivors hunt every 10s after one minute. Eggs continue
while clearing; old props are purged at stage end and before the next stage.

Checkpoint choice lasts five minutes. Next stage closes the window and starts
15s of preparation. Character skills are usable then. Skill reuse freezes only
during the decision, and new buffs/cooldowns survive combat resumption. Death
returns after 5s. Disconnect during combat forfeits chest items; checkpoint
reconnect and graceful shutdown preserve earned entitlements. Full bags keep a
saved claim. Existing characters, carried gear and previously rolled items are
not rewritten by reward changes.

Normal floor loot is suppressed only in the exact arena lease. EXP/SP/pet EXP
are 5x. Fresh stage chests contain 3–7/8–15/18–28/32–45/50–70 rows, paged 32 at a time.
Supplies are grains x50 only. Silk stacks are 1–3/1–5/1–10/1–15/1–20; each scroll
credits 10 silk through an atomic bag/wallet transaction, consuming exactly one.
Sun chance per gear roll is approximately 0/0/0.05/0.1/0.25%; no guaranteed Sun reward. Saved earlier
Sun items and supplies are retained. Client item effects use existing native
presentation; arena icons and procedural effects are owned source assets.

## Code owners

- `game/sookarena`: pure clock/admission, wave budgets and stage-level pools.
- `action/sookarena.go`: leased actor lifecycle, admission, return and shutdown.
- `action/sookarena_chest.go`: checkpoint projection, claims and pause/resume.
- `action/sookarena_rewards.go`: fresh stage rolls and reward selection.
- `action/sookarena_powerups.go`, `_reflect.go`, `_transfer.go`: lease-scoped eggs.
- `action/sookarena_silk.go`, `store/arena_silk.go`: use response and atomic debit.
- `enterworld/arena_reward.go`: owned voucher reference, stack limit 20.
- `foundation/gameplay/arena-chest.ts`: bounded client projection parser.
- `runtime/ui/ui.ts`: native-style chest, paging and preparation keyboard input.
- `runtime/characters/effects/survival-effects.ts`: owned arena effects.

## Assets and deployment

The baseline v1.150 assets do not contain the arena sector. Follow
[the guarded map importer](SURVIVAL-ARENA-MAP.md) with legally owned newer-client
archives, then publish a matched client and server data bundle. Generated assets,
retail files, local databases, executables, recordings and browser logs do not
belong in the PR. Enabling without the venue data refuses entry safely.

Use a separate test authority copy. No production character fixtures, GM grants
or test drop-rate boosts are part of this feature. Machine-specific progress,
backups and full prior transcripts are retained only in the ignored local state.

## Verification and remaining acceptance

Tests cover both flag states; private-population isolation; exact countdown/
expiry/death boundaries; rapid clears and timed waves; hard cap including
ambush; repeated hunt; egg cleanup; reward range/early Sun restrictions; immutable
saved chests; full bag, stale/replayed decisions; and atomic silk stacks/save
failure. Client tests cover projection bounds, tooltips, timers and actor budgets.
Run the contributor checks against the merged branch; the PR description records
actual final commands/results rather than treating old runs as new evidence.

Prior local live tests demonstrated world entry, arena actors, eggs, checkpoint
UI and selected effects, with owner feedback driving changes. A complete live
five-stage regression of the merged protocol 17 build has **not** yet been
recorded. Do not claim that code/unit tests prove full engine playability.

## Post-merge PR preparation (2026-11-10)

Merged official main `f63a41251` with commit `d7f05be4`. Both branches used
protocol 16 for different extensions, so the combined wire contract is protocol
17. Store 42/layout 12 are unchanged. Preserve upstream's field-by-field monster
copy optimization and include the arena marker in that copy.

Verified after merging:

- Full `go test -trimpath -p 2 ./...` passes after the catalogue correction.
- Changed-server gate: regular package tests, build, vet, formatting, lint and
  compatibility checks pass. Its race step fails before execution on this
  Windows host: ThreadSanitizer cannot allocate its shadow-memory region. A
  supported CI race run remains required; this is not a passing race result.
- Client `verify:quick`, arena chest/parameter-job/effect-budget tests and
  `verify:test-types` pass with no new type errors above the existing ledger.
- Client preparation passes 376 Python tests; dedicated Colosseum importer
  tests and the eight-icon asset test pass. Source encoding, size, formatting,
  release, fixture and other fast source checks pass.
- Built and installed matched protocol-17 Agent/GameWorld binaries on the test
  authority copy, with a stopped-database backup. Both readiness endpoints
  return 200. The existing LabWizard session reaches the world in Jangan with
  no frontend error or pending asset loads. No fixture or gear rewrite.

The pipeline-contract suite passes (226 passed, one skipped), including the
new arena icon test registered in the asset suite. The merged catalogue
coverage gate required classifying this private runtime encounter outside its
static-nest/timed-global-event export; the setting and explicit exemption now
agree, and the catalogue tests pass.

The aggregate source pipeline is not green locally because Windows cannot run
the race binary. Its cancelled pipeline-contract task passes independently.
Use the existing isolated asset-tool Python interpreter for preparation and a
supported race toolchain in CI. Full five-stage live acceptance remains
outstanding as described above.

## Cave encounter expansion

Arena-only bootstrap imports Qin-Shi combat references and skills even when
`SRO_QINSHI_TOMB=off`, preserving the public population and closed doors. The
pool admits 30 regular variants from ten families (81–100), with unchanged stage
bounds, and excludes traps/egos/quest variants/chain bosses. Matching asset
builders use the combat overlay for either event; the teleport builder still
uses only the tomb flag. The arena flag invalidates the resource fingerprint.

Regression tests cover preserved native nests/input references, duplicate-ID
refusal, skill overlay on/off, exact regular family count and level bounds,
client combat overlay versus closed teleport posture, and cache invalidation.
Full visual acceptance of every newly admitted cave species remains required;
source existence or passing pool tests do not prove render compatibility.

Cave expansion verification: arena/monster/entry focused Go tests pass; the
changed-server gate passes its regular checks and fails only its unavailable
local CGO race step. Eight overlay/fingerprint tests and two native-texture
contract tests pass; scripts typecheck, encoding, size and formatting pass.
The local GameWorld cave binary is built. Deployment and live rendering status
are recorded in ignored `cave-expansion-status.json`, not inferred from builds.

## Fresh rewards and lower seals (2026-11-11)

Every newly completed stage replaces the old preview with an independently
rolled full-size chest; repeated checkpoint saves do not reroll it. Old saved
claims are not rewritten at deployment, and failed stage saves preserve their
previous rewards. Stage I forbids Moon and Sun for both character and pet gear.
Per-gear Star/Moon/Sun targets by stage: 2/0/0%, 3/0.25/0%, 4/0.5/0.05%,
5/0.75/0.1%, 6/1/0.25%. The seal draw uses the full 32768-value native sample
space rather than biased modulo-10000 sampling. Exhaustive tests pin every seal
count; reward tests cover fresh replacement even with a previously full chest,
contiguous slots, idempotence and failed-save preservation. UI/player guide
explain that the next cleared stage replaces the preview.

## Egg weights and checkpoint cleanup (2026-11-11)

Damage Surge and Chain Reaction each receive 39/330 of native 15-bit egg
selection samples (approximately 11.82%, a 30% relative increase over 1/11).
KABUM receives 364/3300 (+30%) and Ambush 196/3300 (−30%); the remaining
seven kinds receive 280/3300 each. The eight-second cadence is unchanged.
Checkpoint entry now purges remaining ground props and cancels owed eggs,
including eggs spawned after the initial spawning cutoff while clearing.
Tests exhaust all 32768 selector values and exercise cleanup at checkpoint
without relying on the earlier StageEnd or next-stage Resume purge.

Stages now cap at 15 waves (shorter operator test timers can reduce that cap).
The five-minute spawning ceiling, immediate-next-wave clears, 15-second between-
stage preparation and 200-live-monster limit are unchanged. Tests verify all
five fast-cleared stages deliver exactly 15 waves and shortened timers still
bound their budget. Personal LabWarrior blue grants are test-authority data
only, backed up and recorded under ignored local state, never product seeds.

## Linear wave density and KABUM/Ambush shares (2026-11-11)

Stage I remains unchanged; stage V uses the former stage-IV wave pattern.
Intermediate densities are linearly interpolated: 1.8 / 4.95 / 8.1 / 11.25 /
14.4 monsters per wave. Across 15 waves this gives 27 / 75 / 122 / 169 / 216
normal wave slots, before uniques, ambushes or population-cap truncation.
Tests pin the totals and unchanged first/final patterns, all five fast clears,
and the exhaustive native 15-bit egg distribution. Damage/Chain weights are
unchanged while KABUM gains 30% and Ambush loses 30% of their previous shares.

## Retained buff casting visual (2026-11-11)

Healing Division (10236) retained its cast token for the five-minute buff,
leaving its READY looping casting glyph at the original position. READY/WAIT
command visuals now release at the authoritative shot time through the existing
fade/drain path. SHOT and attached buff visuals retain their existing lifetimes.
Regression checks cover completed windups, still-charging windups and sustained
SHOT loops; focused effect suites, client typecheck, ownership/capability and
execution checks pass, with no new test-type errors. Live visual confirmation
is pending; applying the client update must wait until the active arena run
ends or the player accepts a reload. No server or character-data change.

## Arena death owns the return (2026-11-11)

Native town/present-point rebirth requests (including the prepared handler)
are refused while an active arena lease owns the corpse. The existing five-
second arena return remains the only relocation, avoiding the intermediate
Jangan rebirth. The native prompt is suppressed only in private survival
Colosseum worlds; ordinary deaths keep it. Bag resurrection scrolls and premium
limited resurrection share an arena refusal before consumption or revival.
These restrictions disappear outside the feature's active runs.

Action and arena package tests, vet, lint, build, client typecheck, ownership/
capability/execution checks and the new UI regression pass. Test-type debt is
unchanged. The broad UI behavior suite has 43 unrelated failures reproducible
on the unchanged parent (plus the new regression failing there); they remain
outside this fix. Live death-return verification is pending deployment.

## Main integration and death confirmation (2026-11-11)

The owner confirmed the corrected arena death return works in game. Main
through 9a5909e4 is merged: projectile waiting verdicts retain main's external
stat-modifier reads and the arena mana adjustment; arena returns explicitly
use handler commit doors to preserve their synchronous payout/return ordering.
Post-merge action/arena tests, vet, lint (zero issues), client typecheck,
ownership/capability/execution checks, targeted arena/effect/death-dialog
tests, test-type gate, encoding, formatting and source-size checks pass.
PR #3189 targets opensro-dev/opensro main. CI/race verdicts remain pending.
The live local game was left on the verified pre-merge build/source to avoid
interrupting play; use the merged worktree for the next deployment.

## PR #3189 CI follow-up (2026-11-11)

Renamed the PR to `feat(server): add solo survival arena`; the server feature
remains off by default. The failed client-source gate reported a stale UI
census after merging main's chat changes. Merged main through b5931e19 and
regenerated the census with the canonical tool. Census verification and its
architecture test, arena client tests, arena Go tests, formatting and encoding
checks pass. Previous CI server tests/static/lint/race/policy and source checks
passed. New CI results must be checked on the pushed head. The running game
was neither reloaded nor restarted.

## Repository Colosseum inputs; v1.150-only base (2026-11-11)

At the owner's request, the 113 missing Colosseum source inputs (5,725,252
bytes) are included under docs/features/assets/survival/colosseum, with a
per-file SHA-256 manifest and byte-preserving Git attributes. No PK2,
executable, downloadable bundle or generated runtime output is included.
The existing importer defaults to these repository inputs, then uses the
existing Alexandria catalog merges and normal world/model/image converters.
The original v1.150 extraction remains the base and stays read-only.

A fresh import using only that base and the repository inputs matched the
inspected archive-source import exactly, including all 114 nonshared staged
files. Five importer/integrity/path-safety tests pass; all staged input bytes
match the manifest. The standard outdoor build converted region 0x63ec
successfully (18,250 images, zero failures; world objects 1587 BSR/5880 BMS).
Only this region was built in the isolated test tree: its global routing was
correctly not published with 2123 other region bundles absent. A production
release still needs the documented full build, server projection and normal
pack/release gates. The running game and its data were left untouched.

## Asset handoff merge follow-up (2026-11-11)

Main advanced through 8a6e98de while the repo-only arena build was being
verified. Its new catalogue exemptions are preserved alongside the arena
exemption, and the generated UI census was rebuilt. Arena silk redemption now
uses main's betaSilkStarterSilk helper rather than the removed starter constant.
These changes are isolated from the running local game.

## Full v1.150-based build and launch verified (2026-11-11)

Built all 2,124 outdoor regions and the full client resource/pack pipeline
from the original v1.150 extraction plus the checked-in Colosseum inputs.
No newer PK2 or external asset bundle was used. The resulting browser asset
manifest contains 42,518 files; publication-ledger verification reports zero
unclaimed packed assets. The matching server projection contains 3,935 files
(379,277,663 bytes), with an 84,840,523-byte archive. Its manifest digest is
`sha256:672f85bfaf1bd7c7f869acc45c73f3b89ebcfe60398b1450a10f96fdfb42362e`.

The matched Agent/GameWorld binaries and local browser client were started
from the b09deaed source snapshot. Both backend readiness endpoints returned
HTTP 200. The served client pack manifest was verified byte-for-byte against
the fresh build, and the browser reached the world. All seven test characters
retained their inventories/pet gear, skills, stats, gold and quickslots; two
arena test-character entry counters were reset after backup. Local backup,
launch and deployment details remain in ignored state, not product defaults.

This verifies the complete local asset/server-data build and successful world
entry. User arena testing is pending; no additional full-stage playability or
production beta packaging/deployment result is claimed.

## Easier monster-level progression (2026-11-11)

Shifted all five stage bands down two levels. Relative ranges are now
−8..−5, −6..−3, −3..0, −1..+2 and +1..+4; a level-90 player therefore meets
82–85, 84–87, 87–90, 89–92 and 91–94 monsters. The final stage matches the
previous stage-IV range. Normal waves, ambush eggs and uniques continue to
share StageBounds, with the existing level-one floor. Tests cover all five
ranges, pool filtering and low-level floors; focused arena/action tests pass
against the fresh v1.150 server dataset. No asset rebuild is needed. The
change requires a GameWorld restart and is not yet active in the running game.

## One extra stage-I monster per wave (2026-11-11)

Stage I now adds exactly one monster to each normal wave: 3/3/3/3/2 instead
of 2/2/2/2/1, totaling 42 normal slots across 15 waves. Stage-II through
stage-V counts are unchanged; stage V remains 15/14/15/14/14 (216 slots).
Tests pin every stage total, the first/final patterns, the five-wave budget
and real initial spawn counts. Focused arena/action tests and vet pass.
This is committed source only; the currently running game has not restarted
to activate this spawn-count change. Monster level bands remain unchanged.

## CI wave-count assertions corrected (2026-11-11)

CI caught two arena tests omitted by the earlier focused name filter. Their
opening and follow-up wave assertions now expect three monsters, matching
the requested stage-I increase. The entire sookarena and action packages pass
locally, not just filtered stage tests. Merged main through c85e47bd and
regenerated the UI census. Client quick checks and focused arena/census tests
also pass. No runtime behavior or running-game deployment was changed.

## Retail-asset distribution correction (2026-11-11)

This supersedes the repository-input instructions above. Retail Colosseum
models, maps, textures and effects have been removed from the source tree;
the PR branch history is being rewritten to omit these retail inputs from
every feature commit while retaining the code changes. Earlier local conversion results remain useful
evidence, not permission to distribute extracted Joymax media.

The importer now requires `--client-root` pointing to an operator-supplied
v1.696 client (Media.pk2 SV.T build 696), alongside the original v1.150
extraction passed as `--native-root`. `--help` prints the folder layout and
all four pinned SHA-256 hashes. Every archive is verified before directory
parsing, extraction or output writes. Missing, empty and mismatched files
fail with actionable errors. Normal staging/conversion/publication paths
are unchanged; no download, retail execution or new asset delivery system
was added. See SOOK-SURVIVAL-ARENA.md for complete build commands.

Synthetic regression tests cover matching archives, missing/empty input,
last-archive corruption and refusal to parse/write on a mismatch. The donor
build number was read statically from Media.pk2 SV.T; no executable ran.
The existing running game, generated assets, databases and characters are
untouched by this source correction.

Verification: all four real donor archives matched their pinned hashes and a
fresh archive-only import completed for region 25580 / sector 236,99 (83
model/data entries and 11 particle entries). Four focused importer tests,
source encoding, formatting, source-size and diff-whitespace checks pass.
The existing published runtime was not rebuilt, restarted or reloaded.

Merged main through c1e2a412 in the isolated source worktree; regenerated the
sole conflicted file, the UI census, from the merged UI source. Client quick
checks and 11 focused census/arena tests pass. Full Go sookarena, action and
GameWorld wiring packages pass against the existing matched test data. The
rewrite preserves the final source tree and removes the retail-input path
from every feature commit; a force-with-lease push updates only this PR's
feature branch. The running game continues on its prior deployed snapshot.

## Review scope and release coordination (2026-11-11)

The shared retained-buff casting fix is split into its own client-fix branch;
it no longer changes ordinary casting through this event PR. Cold-model
one-shot admission is restricted to the arena cue, preserving existing crow
recovery timing. The eight real 64px PNGs are the source of truth; the duplicate
Python art generator was retired, and source-art validation now rejects empty
or incorrectly sized PNG source inputs. Operator docs spell out the unconditional protocol
17/store 42 migration, matched rollout and backup-dependent rollback, plus
the intentional combat-only cave asset dependency and its independent world
gate. The implementation notes now have a contributor-neutral filename.

A clean archive-only v1.150 + pinned-v1.696 build passed locally before this
review follow-up: 42,534 browser assets, zero unclaimed packed assets and a
matched 379,277,663-byte server projection. Both services reached readiness;
all seven existing characters retained inventory/pet gear, skills, stats,
gold and quickslots. This review work uses a separate checkout and does not
change that running deployment. No new full arena playthrough is claimed.
