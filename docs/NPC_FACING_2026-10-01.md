# NPC facing recovery â€” 2026-10-01

## Status and defect

Implemented on `codex/npc-facing`, based on public main `8eeb81f`. This document
records source behavior, not a production deployment. No live database change
or asset rebuild is required; the correction is compiled server placement data.

`npcpos.txt` contains identity, region and XYZ, but no facing. The original
`npc_headings_generated.go` recovered directions from later vSRO nests using
codename and exact placement (float32 coordinates normalized to tenths). It
covered 161 of 178 published NPC placements. The roster discarded the lookup's
`found` result, so the other 17 spawned at heading zero. The old test accepted
any coverage above 100 and therefore did not catch Riise or the other gaps.

The shared lookup now supplements the original table with 17 reviewed entries:
12 matching ISRO-R placements and five explicitly inferred relocations. The
original generated file is unchanged and remains owned by its generator.
All 178 current placements resolve. Unknown future placements still return
`found=false`; the exhaustive licensed-data test fails on any such published
placement. This is a build-time coverage requirement, not a new runtime policy
for arbitrary/custom data. Run the licensed test when changing NPC data.

## Evidence and joins

The source is `SILKROAD_R_SHARD202507231313.bak`, SHA-256:

`89c6835f8e51b1a75950242c89cf3ad639ea7d50a22191a90632cca333664a45`

A read-only SQL backup catalog traversal recovered named table allocations;
no SQL server restore, stored procedure execution or database write was used.
Join `_RefObjCommon.ID` through `Tab_RefTactics.dwObjID` and
`Tab_RefNest.dwTacticsID` (also `Tab_RefNest_Back`), then compare the resulting
codename, unsigned region and float32/tenth XYZ to the v1.150 placement.
Never join cross-version runtime IDs directly. The active table has 18,468
rows; the archive has 18,029. They are two tables in one backup, not independent
historical captures.

`wInitialDir` is SQL SMALLINT (signed, two bytes). Its wire meaning is the
unsigned bit pattern: `sqlValue & 65535`. Comparing signed SQL values directly
to unsigned Go headings produces false conflicts. Zero is a valid heading.

Native client raw instructions at `852FC7..853012` read the unsigned word,
divide by 65535, multiply by 360 and convert to radians; `8535A0` adds the
model-axis offset of pi/2. The port already follows this convention. Both
initial object lists and later interest spawns use `BuildNpcCreateRow`, so
repair the data once, not the renderer or separate entry paths.

## Supplemental placements

Offsets below are byte offsets of records in the identified backup. The
runtime keys always use v1.150 coordinates. Both tables agree for all 12 exact
matches. The five other choices are deductions, not claims of an original
v1.150 server capture.

| NPC | Region | Heading | Evidence | Nest ID | Record offsets (active / archive) |
| --- | ---: | ---: | --- | ---: | --- |
| Soldier Sangnam [Teleport] | 25001 | 32767 | Matching placement | 39 | 102441696 / 495002336 |
| Soldier Choiyoung [Teleport] | 25000 | 49333 | Matching placement | 50 | 102441952 / 495002592 |
| Soldier Hogang [Teleport] | 25255 | 0 | Matching placement | 4375 | 102553632 / 495107168 |
| Windy Phantom Thief | 24758 | 8191 | Inferred relocation | 29625 | 111985248 / 495771424 |
| Soldier Jingyo [Teleport] | 25000 | 16019 | Matching placement | 4567 | 102561248 / 495114208 |
| Specialty Trader Payi | 26753 | 13653 | Matching placement | 11107 | 103606880 / 495184800 |
| Specialty Trader Seopok | 23712 | 52792 | Matching placement | 11108 | 103606944 / 495184864 |
| Specialty Trader Hounah | 23445 | 32767 | Matching placement | 11109 | 103607008 / 495184928 |
| Grocery Trader Saha | 27243 | 10922 | Inferred relocation | 11135 | 103608416 / 495186336 |
| Specialty Trader Toson | 27244 | 16201 | Matching placement | 11137 | 103608544 / 495186464 |
| Specialty Trader Osaman | 23411 | 54430 | Matching placement | 14822 | 103610528 / 495188448 |
| Guide Raffy | 26957 | 16565 | Matching placement | 19475 | 106137760 / 495400352 |
| Guide Lipria | 27471 | 24575 | Matching placement | 19476 | 106137824 / 495400416 |
| Guide Riise | 26959 | 21845 | Inferred relocation | 20882 | 107538720 / 495482464 |
| Specialty Trader Tina | 26959 | 16383 | Matching placement | 20986 | 107540128 / 495483872 |
| Event So-Ok | 26959 | 24757 | Inferred relocation | 21006 | â€” / 495484448 |
| Event So-Ok | 26265 | 16201 | Inferred relocation | 21022 | â€” / 495484640 |

## Decisions for the five moved placements

Keep original v1.150 XYZ in every case. A small translation at the same height
is evidence of a moved station, not evidence that its facing changed. Apply
these decisions only to the listed v1.150 key; no nearest-neighbour fallback
runs in the game.

- **Guide Riise â€” 21845 (120 degrees).** ISRO-R active/archive agree on the
  same guide in region 26959 at X -6, Z +22 relative to v1.150, with unchanged
  elevation and zero spawn radius. Retain that recorded station direction.
- **Windy Phantom Thief â€” 8191.** vSRO and both ISRO-R tables agree on this
  heading at X +8.46, Z -4.23, unchanged elevation and zero spawn radius.
  Preserve the consistently recorded facing at the older location.
- **Grocery Trader Saha â€” 10922.** The later station differs only by X +9;
  vSRO and both ISRO-R tables agree on the direction. Preserve it without
  shifting the v1.150 NPC.
- **Constantinople So-Ok â€” 24757.** vSRO and the ISRO-R archive agree at
  X +11, Z +25 with the same elevation. The archived codename/region match
  supplies the older event-NPC station direction. Active nest IDs must not
  substitute for a matching codename/region.
- **Samarkand So-Ok â€” 16201.** The ISRO-R archive preserves the exact Y and Z
  of v1.150 with X +21. The vSRO candidate instead differs in both X and Z
  (about -19.65 and -41.76) and has heading zero. Both candidates have radius
  90, so neither proves a fixed original orientation. Use the archived row
  with unchanged Y/Z as the better station correspondence; this is the least
  certain of the five deductions. Keep it documented and placement-specific.

## Conflicting exact placements

Preserve existing vSRO evidence for Salihap (`NPC_KT_HORSE`, 56432 versus
ISRO-R 24393) and Aryoan (`NPC_CA_ARMOR`, 52063 versus ISRO-R 51881). Both
already match the original client placement. A later changed direction is
not sufficient to replace an earlier matching direction. The lookup checks
the original table before the supplement; tests pin these two decisions.

## Maintenance and verification

- `npc_headings.go` owns key normalization and lookup precedence.
- `npc_headings_supplement.go` holds the reviewed supplemental keys and
  comments describing each inference. Update this file and this evidence
  record together. Never hand-edit the generated vSRO table.
- `TestPublishedNPCHeadingCoverage` requires every loaded placement to
  resolve and verifies the encoded heading at the native spawn field. Its
  previous `matched >= 100` policy is obsolete.
- Supplemental tests pin all 17 directions, including confirmed zero and
  formerly negative SQL values, reject unreviewed nearby placements, and
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
