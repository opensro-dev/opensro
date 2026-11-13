# Attack-pet regression audit — 2026-11-12

Follow-up: the fixes and live smoke-test evidence are recorded in [crow rework acceptance](CROW-REWORK-ACCEPTANCE.md). This audit describes the original upstream findings; it does not claim the corrected worktree is already merged upstream.

## Scope and deployment

Reviewed upstream main `c3db7b4` in the isolated `audit/pet-release-regressions` worktree. The running local game uses `dad4a670a9a21ff85f67c69f9d55f7f4716ea378`, with attack-pet expansion enabled; it is not the latest upstream build. No production source, running services, characters, inventory or gear were changed by this audit.

The intended design comes from the accepted pet reworks and canonical `petdesign.json.treeSkills`. The older `skills` list and parts of ATTACK-PET-SYSTEM.md describe obsolete kits.

## Confirmed regression: authored HP balance was removed

PR #2634, commit `e3552e4f125a1d1cfb644407387c28ce99920f42`, restored native HP for all attack pets, including when the expansion is enabled. It removed species HP curves and replaced exact survival assertions with relative-order checks. This reverses the explicitly requested ungeared survival targets: bear three ordinary same-level party-giant hits, wolf two, penguin and crow one.

Reinstating the pre-change benchmark against actual shipped game data fails all 24 species/level cases (levels 10, 20, 40, 60, 80, 90). Normal midpoint damage, no criticals, equipment, passive bonuses or shields:

| Level | Pet | HP | Damage per hit | Hits to die | Intended |
| --- | --- | ---: | ---: | ---: | ---: |
| 20 | Bear | 2,517 | 97 | 26 | 3 |
| 20 | Wolf | 2,517 | 157 | 17 | 2 |
| 20 | Penguin | 2,517 | 216 | 12 | 1 |
| 20 | Crow | 2,517 | 216 | 12 | 1 |
| 90 | Bear | 39,434 | 4,405 | 9 | 3 |
| 90 | Wolf | 39,434 | 5,438 | 8 | 2 |
| 90 | Penguin | 39,434 | 6,470 | 7 | 1 |
| 90 | Crow | 39,434 | 6,470 | 7 | 1 |

Affected paths: `companion/profile.go`, `enterworld/petprofiles.go`, shared pet design data and client `pet-training.ts`. HP-derived shields and healing also change with the larger baseline. Restore the authored curve only with the expansion enabled; preserve native HP and potion behavior when disabled. Reinstate absolute survival tests instead of merely checking species ordering.

## Reproduced visual defect: crow effects can expire before loading

In client `runtime/characters/effects/effects.ts`, crow bursts set their start timestamp before their model is ready and expire after 1.2 seconds. Delaying readiness until two seconds reproduces missing clone wind-burst, red healing and blue mana visuals: all three disappear without rendering. This does not demonstrate lost healing, mana or damage; it demonstrates lost presentation under cold or busy asset loading.

Start the visible lifetime when resources are admitted, following existing effect lifecycle patterns. Add delayed-readiness tests. This is a current-main defect/missing visual fix, not evidence of a newly introduced gameplay regression. PR #3232 addresses retained self-buff casting loops, a separate issue.

## Progression and integration gaps (not proven recent regressions)

- All third books unlock at pet level 90 but require level 94 for learned rank 2 and 126 for rank 10 (`companion.NextLevelRequirement`, four pet levels per rank). With a level-90 cap, their natural progression stops at rank 1; equipment bonuses are separate. Resolve this design conflict explicitly rather than silently removing level requirements.
- `action.petExpansionAllowed` rejects non-default world instances, job suits and free-battle state. Advanced skills/bonuses can consequently be unavailable in a private survival arena. The arena integration is separate from ordinary-world pet behavior; do not enable bonuses in every instance/PvP mode accidentally.
- ATTACK-PET-SYSTEM.md still describes legacy crow rows. Current rows are Rejuvenating Plume, Stormbrood, Mana Spring, Tailwind and Guiding Feathers. Refresh this guide after the gameplay decisions above.

## Checks that passed

Using required shipped data (not silently skipped fixtures), 572 backend pet tests/subtests passed, zero failed and zero skipped. The focused client suite passed 100 tests. The canonical pet-design export check passed.

| Pet | Current rows covered by focused tests |
| --- | --- |
| Bear | Challenging Roar; Iron Hide; Unbroken Spirit; Guardian’s Bond; Crushing Paw |
| Wolf | Hunting Howl; Savage Rend; Execute; Bloodscent; Blood Hunt |
| Penguin | Ice Lance; Blizzard; Shatter; Arcane Resonance; Frozen Hourglass |
| Crow | Rejuvenating Plume; Stormbrood; Mana Spring; Tailwind; Guiding Feathers |

Coverage includes owner/party effects, revival HP/EXP/level limit/cooldown, counters, five Blizzard pulses, controlled-target Shatter, echoes, Hourglass, egg hatch/expiry/clone cap/wind damage, mana and cooldown recovery, gear stats/seals/procs/cooldown cap, tooltips, quickslots and passive selection. Current UI and quickslot manual casts encode the full 11-byte targeted command, including target zero; the legacy seven-byte queue branch is not evidence that the current client toggles actives.

Passing tests are not full live acceptance. No fresh in-game replay of every pet/book, party death, sound and animation was completed in this audit. In particular, broad visual correctness and player-perceived timing remain unverified.

## Reproduction and next steps

Local evidence is in the audit worktree’s ignored `.state/pet-audit/`: `original-survival-contract.log`, `survival-measurements.json`, the original-balance reproduction source, `cold-recovery-results.log`, its reproduction snippet, and the backend JSON test results. These files are machine-local evidence, not runtime dependencies.

For shipped-data tests set all of `SRO_SERVER_GAME_DATA_ROOT`, `SRO_GENERATED_ROOT`, `SRO_GAME_ROOT` and `SRO_REQUIRE_GAME_DATA=1`; the last setting prevents missing-data tests from silently skipping. Run focused companion/action pet tests and client runtime pet/companion tests. The intentionally failing reproduction checks are separate from the passing upstream suite.

Fix order: restore expansion-only survival contract; correct cold crow effect lifetime; decide third-book progression and arena admission; refresh docs; then run a fresh live four-pet/three-book matrix on training dummies, using controlled retaliation/death fixtures for cases dummies cannot exercise. Preserve characters/gear and restore temporary permissions or fixtures. Do not call the current green suite proof of the original design: the HP test itself was weakened when that design changed.
# Follow-up: party/expiry and Bond visual routing

**2026-11-13 remaining checks:** Live level-90 owner/level-79 bear correctly refused revival for over 26 seconds without spending cooldown; the level-80 bear accepted the exact +10 boundary and restored full HP/EXP. Owner and party revival chimes were verified in captured game-output PCM, with the actual asset voice playing; the isolated party recording's energy envelope correlated 0.725 with the chime asset. The user muted the page, so physical speaker audibility is deliberately not claimed. No further product fix was needed. The temporary capture hook was removed, original deployment restored, disposable login disabled and all seven original characters/gear verified unchanged. See the dated handoff section and `.state/pet-audit/boundary-sound-20261113/` for precise evidence and limits.

The evening live follow-up reproduced a further client regression: combat consumes `0x3fb4` for yellow paid-damage feedback, preventing entity animation admission. The shared world dispatcher now forwards that same validated packet to entities too. Both transfer and revival packet forms have a dispatch regression test; it fails before the fix and passes after it. Focused client tests: 53 passed; `verify:quick` passed.

Actual live checks now include a natural ten-minute crow clone expiry (observed 599,994 ms, 200 ms polling), wind-burst animation and 14,231 dummy damage; owner full-HP/EXP revival and cooldown refusal; party full-HP/EXP revival with gold animation on recipient and observer; party Howl, Tailwind, Guiding Feathers, shields, Plume/Mana Spring visuals, and approximately 500 ms party pet cooldown endpoint reductions.

The precise setup, failed/interrupted attempts, evidence filenames, cleanup, deployment limits and remaining unverified cases are in `CLAUDE-HANDOFF-2026-11-12.md`, under “Evening follow-up.” All live work used copied databases. Seven original characters and their gear/skills/EXP/gold were verified unchanged, and the temporary account was disabled. This is expanded live coverage, not a claim that every skill rank and sound passed fresh release acceptance.

