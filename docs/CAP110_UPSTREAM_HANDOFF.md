# Optional cap-110 expansion: upstream review handoff

Prepared 2026-10-17 for local review. No push or pull request has been made.

## Result and scope

The optional Alexandria branch adds level-110 progression configuration,
D10/D11 equipment data, Alexandria NPC restoration and ordinary D10 shop stock,
and the CH/EU skill tier through mastery 110. Native defaults remain unchanged.
Regular shops do not sell D11 equipment. Retail assets, database snapshots,
credentials, generated packs and GM character fixtures stay local and ignored.

The skill implementation is in commit `0f2d2372`, with the subsequent focused
icon-publication fix in this handoff's commit. The importer adds 1,518 rows,
preserves 757 existing imported IDs and allocates 761 database-tier IDs. All
1,090 board-eligible imported ranks are published. Review behaviour and
limitations in `CAP110_PROGRESS.md` and `CAP110_SKILL_ASSET_AUDIT.md`; the
detailed provenance is kept outside the repository. This document does not
replace them.

## Validation evidence

- Prior implementation checkpoint: 177 Python tests; progression/enterworld Go
  tests; progression vet; five skill-slot and four native effect/spine tests;
  parameter decoding, stable IDs, isolated off-build and packed-data coverage.
- Icon fix: focused publication refreshed game-data and ui-icons packs; all
  4,888 skill-window rank rows resolve their packed icon references.
- The owner confirmed CH/EU skill-tree presentation, the repaired icons and
  combat operation of the skills they tested. This is manual verification;
  there is no automated exhaustive combat or numerical-balance certificate.
- Source checks for the icon fix: scripts, formatting, encoding, generated-root
  and diff whitespace. The full server/race/client gate was not rerun for this
  asset-publication-only change. Earlier test passes belong to their checkpoint.

## Known limits and review boundaries

- Rogue Sable Duplicate (`SKILL_EU_ROG_STEALTHA_CHANGE_B_03`, ID 33156) is imported
  but refused by cast admission. The owner deferred this issue. Do not present
  the entire skill tier as having exhaustive execution coverage.
- The exact native spine census passes with native inputs and fails with the
  expanded variant's additional records. Keep the native expectations intact.
- Monster/drop progression, cave stability, deferred set items and out-of-scope
  later mechanics remain unfinished. This branch is ready to review in pieces,
  not to describe as a complete production cap-110 release.
- Local builds require owner-supplied clients and an explicit database skill
  snapshot with `SRO_CAP110_SKILLS=1` and `SRO_CAP110_SKILL_DATABASE=<absolute JSON
  snapshot path>`. Omitting a previously published database tier on an on-build
  refuses the build; explicit off removes the owned import.

## Native claims relevant to the icon fix

| Claim | Native evidence | Checked by | Status |
| --- | --- | --- | --- |
| Skill slot icons use source column 61 under the icon root. | Existing CIF collector documents slot bind 0x005891ca and lowercase parsing 0x007f97e2. | Reused that collector and copier; checked delivered references against packs. No new disassembly inspection in this follow-up. | Existing evidence reused; native claim not independently reverified here. |
| New image ownership matches the full asset build. | Repository asset contract, not a native claim. | Reused `imagePackGroup`; focused publication and packed reference audit passed. | Verified locally. |

Keep the icon fix review separate from the later-data gameplay policy: it repairs
publication of already-selected artwork and introduces no new gameplay rule.
