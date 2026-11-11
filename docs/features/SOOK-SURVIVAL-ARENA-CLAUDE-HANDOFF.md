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
private leases, three entries per UTC day. Default 19-wave budget; fast clears
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
