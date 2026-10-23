# NPC facing recovery — 2026-10-01

## Status and defect

Implemented on `codex/npc-facing`, based on public main `8eeb81f`. This document
records source behavior, not a production deployment. No live database change
or asset rebuild is required; the correction is compiled server placement data.

`npcpos.txt` contains identity, region and XYZ, but no facing. The original
`npc_headings_generated.go` recovered directions from later shard nests using
codename and exact placement (float32 coordinates normalized to tenths). It
covered 161 of 178 published NPC placements. The roster discarded the lookup's
`found` result, so the other 17 spawned at heading zero. The old test accepted
any coverage above 100 and therefore did not catch Riise or the other gaps.

The shared lookup now supplements the original table with 17 reviewed entries:
12 matching later-shard placements and five explicitly inferred relocations.
The original generated file is unchanged and remains owned by its generator.
All 178 current placements resolve. Unknown future placements still return
`found=false`; the exhaustive licensed-data test fails on any such published
placement. This is a build-time coverage requirement, not a new runtime policy
for arbitrary/custom data. Run the licensed test when changing NPC data.

## Evidence and joins

The supplemental headings come from a later shard export kept outside the
repository. Placements join by codename through the shard's nest/tactics
chain, then compare the resulting codename, unsigned region and
float32/tenth XYZ to the v1.150 placement. Never join cross-version runtime
IDs directly. Two generations of the nest table exist in the export; they are
two tables in one source, not independent historical captures.

Headings are stored as signed 16-bit values. Their wire meaning is the
unsigned bit pattern: `sqlValue & 65535`. Comparing signed values directly
to unsigned Go headings produces false conflicts. Zero is a valid heading.

Native client raw instructions read the unsigned word, divide by 65535,
multiply by 360 and convert to radians; the model-axis offset of pi/2 is
added. The port already follows this convention. Both initial object lists
and later interest spawns use `BuildNpcCreateRow`, so repair the data once,
not the renderer or separate entry paths.

## Supplemental placements

The 17 reviewed supplemental entries (12 exact placement matches, five
inferred relocations) live in `npc_headings_supplement.go` with their keys;
the per-placement evidence record is kept outside the repository. The
runtime keys always use v1.150 coordinates.

## Decisions for the five moved placements

Keep original v1.150 XYZ in every case. A small translation at the same height
is evidence of a moved station, not evidence that its facing changed. Apply
these decisions only to the listed v1.150 key; no nearest-neighbour fallback
runs in the game.

- **Guide Riise — 21845 (120 degrees).** Both generations of the later table
  agree on the same guide in region 26959 at X -6, Z +22 relative to v1.150,
  with unchanged elevation and zero spawn radius. Retain that recorded
  station direction.
- **Windy Phantom Thief — 8191.** The earlier table and both later
  generations agree on this heading at X +8.46, Z -4.23, unchanged elevation
  and zero spawn radius. Preserve the consistently recorded facing at the
  older location.
- **Grocery Trader Saha — 10922.** The later station differs only by X +9;
  all sources agree on the direction. Preserve it without shifting the
  v1.150 NPC.
- **Constantinople So-Ok — 24757.** The earlier table and the later archive
  generation agree at X +11, Z +25 with the same elevation. The archived
  codename/region match supplies the older event-NPC station direction.
  Active nest IDs must not substitute for a matching codename/region.
- **Samarkand So-Ok — 16201.** The later archive generation preserves the
  exact Y and Z of v1.150 with X +21. The earlier-table candidate instead
  differs in both X and Z (about -19.65 and -41.76) and has heading zero.
  Both candidates have radius 90, so neither proves a fixed original
  orientation. Use the archived row with unchanged Y/Z as the better station
  correspondence; this is the least certain of the five deductions. Keep it
  documented and placement-specific.

## Conflicting exact placements

Preserve existing earlier evidence for Salihap (`NPC_KT_HORSE`, 56432 versus
the later source's 24393) and Aryoan (`NPC_CA_ARMOR`, 52063 versus 51881).
Both already match the original client placement. A later changed direction
is not sufficient to replace an earlier matching direction. The lookup checks
the original table before the supplement; tests pin these two decisions.

## Maintenance and verification

- `npc_headings.go` owns key normalization and lookup precedence.
- `npc_headings_supplement.go` holds the reviewed supplemental keys and
  comments describing each inference. Update this file and the external
  evidence record together. Never hand-edit the generated table.
- `TestPublishedNPCHeadingCoverage` requires every loaded placement to
  resolve and verifies the encoded heading at the native spawn field. Its
  previous `matched >= 100` policy is obsolete.
- Supplemental tests pin all 17 directions, including confirmed zero and
  formerly negative stored values, reject unreviewed nearby placements, and
  retain both original headings when sources disagree.
- Run `go test ./internal/game/world/simulation -run 'TestNPC|TestPublishedNPCHeadingCoverage|TestNpcTravelPublicationLifecycle' -count=1 -v`
  from `apps/server`, with licensed game data available. Set
  `SRO_REQUIRE_GAME_DATA=1` to make missing data an error rather than a skip.
- Run `pnpm task check:server` and `pnpm check source` before release.

The focused test passed with `resolved headings for 178/178 published NPC
placements`. The full server gate passed (vet, lint, tests, race, govulncheck
and release contract); `pnpm check source` passed all 12 tasks, including a
second server pass on the final source. `git diff --check` passed. This verifies
source data through wire output, not a live browser capture or production
publication. No client rendering code changed.
