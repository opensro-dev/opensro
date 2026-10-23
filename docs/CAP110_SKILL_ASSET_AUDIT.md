# Mastery 101-110 skill asset audit

Checked 2026-10-17 against a later shard export and client archives kept
outside the repository, the extracted Alexandria variant, and the production
skill-effect parsers. No skill import or live game-data change was made.
The counts and source hashes behind this summary are kept with the local
audit results.

## Findings

- The later shard has 761 enabled Chinese/European skill rows requiring
  mastery 101-110. Chain references classify 536 as root candidates and 225
  as chain sub-rows. This classification does not prove learning/execution.
- All 536 root candidates have their English display-name symbols and icon
  files available in the variant or later client text/archive data.
- Root activity fields identify 62 passive candidates and 474 non-passive
  candidates. All 474 reference animation sets present in the later client.
  The current variant covers 423; the remaining 51 share 20 missing bases.
- Production parsers read 223 distinct animation bases for the non-passive
  candidates, all with effect stages. Each base also has effect-stage rows
  in the current variant. Animation-set coverage and effect-stage coverage
  are separate facts; neither should be inferred solely from an icon or cast.
- All 458 unique directly referenced effect/model/sound files are present
  in both the later archives and the extracted variant. Sound paths were
  resolved below `prim/snd`, as the production parser resolves them.
- No unrecognized animation-name tokens or animation-group names were found
  in the selected source rows.
- One stage has an unsupported discriminator: `MOV_PIERCE` in
  `SKILL_CH_BOW_PIERCE_E`. The existing adapter refuses it with:
  `newer move kind MOV_PIERCE has no unique v1.150 family homolog`.
  This is a code/evidence gap, not a missing file. Do not assign a guessed
  movement enum to bypass the refusal.

## Conclusion and limits

There is no demonstrated need to fetch another client before starting the
database-tier conversion. The later client contains presentation resources
beyond the mastery-100 skill rows in its skilldata manifest.

This audit checks direct resource presence and the existing parsers' names
and discriminators. It does not prove every transitive EFP/model dependency,
character motion binding, server parameter, learning gate or in-game cast.
Those checks remain part of implementation. Preserve current published ids
and remap database group, chain and prerequisite references by codename.

The local audit tools and results live in ignored local state (`.state/`),
with no product dependency.

## Implementation follow-up

The full database tier is now projected by the optional importer; see
`CAP110_PROGRESS.md`. The `MOV_PIERCE` diagnostic above used the variant
itself as the native comparison. The production builder uses the preserved
v1.150 `resinfo` table and its existing adapter resolves this row from the
unanimous A/B/C family stages. The actual effect build succeeds; no new enum
or guessed trajectory was required.
