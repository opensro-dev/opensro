# Optional 110-cap expansion: checkpoint

The native default remains v1.150 with cap 90. This checkpoint implements
the roadmap's data comparison and optional stopping-level change; it does
not make the expansion ready to release.

## Phase 0: repeatable archive comparison

Run from a worktree with Go and the existing Python requirements installed:

```text
python -B scripts/analysis/compare_cap110.py --native-root <native-client> --vsro-root <later-client>
```

Both folders contain Media.pk2. No executable is needed. The tool reads the
archives without extracting or changing them, follows their shard manifests,
and writes `.state/cap110-comparison.json`. It builds a small offline Go
auditor in `.state/cap110-audit` that calls the production
`enterworld.CompileSkillProgram`; it does not start the server.

Comparison observed on 2026-10-16:

| Input | Media.pk2 SHA-256 |
| --- | --- |
| Native reference | `0bf187785c3cb956f98678c77d4c086fab2c90d70259177d99760960ec28c61a` |
| Owner-supplied VSRO reference | `20c3923e995b8d325dba9b390e63d059f8c495b6d02d6f80a4904698f3f0d390` |

The owner supplied the later client's provenance. Its exact release version
has not been authenticated from an executable.

| Table | Native rows / columns | Later rows / columns | Shared codenames | Added codenames |
| --- | --- | --- | --- | --- |
| Items | 8,439 / 160 | 11,923 / 160 | 8,437 | 3,486 |
| Characters | 12,761 / 120 | 13,713 / 104 | 12,761 | 952 |
| Skills | 27,835 / 118 | 29,334 / 118 | 27,834 | 1,500 |

All counted records in these inputs have service enabled. These are whole
table counts, not a count of content required for level 110. The character
overlap differs from the older handoff's 11,871; the tool now records the
input hashes and manifest shard hashes so that count can be reproduced.

Equal item and skill widths do not prove equal semantics. Item IDs differ
in 3,914 shared rows; skill IDs differ in 22,705. Skill group, chain-next
and prerequisite group fields also differ. Converters must remap references
by stable identity and preserve existing native rows, rather than copying
the newer numeric IDs or replacing the native table wholesale.

The current port compiler decodes all 27,835 native skills and 29,330 of
29,334 later skills. The four first failures are:

| Skill codename | First unsupported instruction | Source column |
| --- | --- | --- |
| `MSKILL_RM_ROC_ATTACK10` | `0x006d6f6d` (`mom`) | 69 |
| `SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_01` | `0x6d736964` (`msid`) | 71 |
| `SKILL_MALL_NASRUN_MOBDEFENCEIGNORE_02` | `0x6d736964` (`msid`) | 71 |
| `SKILL_EVENT_EXP_SP_UP_100_01` | `0x65787075` (`expu`) | 72 |

Tags `defr`, `fitp`, `mscc`, and `stns` additionally appear in successfully
decoded later rows; the existing decoder already recognizes them. This
is framing coverage only. It does not prove that every skill executes,
has correct effects, or is learnable. Unknown blocks stop the audit for
that row; later values are not guessed to be instructions.

Experience differs at every level from 91 through 140:

| Level | Native EXP to next level | Later EXP to next level |
| --- | --- | --- |
| 91 | 305,888,332 | 369,337,595 |
| 100 | 584,251,898 | 1,406,035,568 |
| 110 | 1,249,891,465 | 4,044,607,839 |

## Phase 1: cap option

`SRO_LEVEL_CAP=110` raises the stopping level. Unset, `off`, `false`, `0`
or `90` retains cap 90. Other values fail startup. The flag is port-only,
not native; both the Nomad job and the deployment registry default it off.
The GameWorld runtime, diagnostic fixture API, public rules and exported
level catalogue use the setting. Rebuild the catalogue with the same
setting when changing a deployment.

This implementation deliberately retains the installed v1.150 curve,
including its existing rows above 90. It changes neither mastery budgets
nor content availability, and it does not turn Alexandria on. Importing
the later curve requires publishing that same curve to both client and
server; switching only the server would desynchronize progression.

Turning the cap off stops further upward transitions at or above 90. It
does not lower existing characters, remove their items or undo persisted
level rewards. A release rollback needs its own content/state policy.

## Phase 2 checkpoint: weapons

The optional item importer now stages 112 ordinary and Seal-grade weapons:
40 Chinese and 72 European, spanning all 14 ordinary weapon kinds in
degrees 10 and 11. Existing native item rows, IDs, statistics and names
remain unchanged. New rows receive collision-free IDs; the report records
each source ID and its assigned ID. Statistics, level/STR/INT requirements,
race, weapon kind and ammunition fields retain the later table's values.

The importer is off unless `SRO_CAP110_ITEMS` is `1`, `on` or `true`.
`build_alexandria_variant.py` calls it after the city's NPC/text merge.
Rebuilding that variant with the item flag off unregisters and removes its
generated item shard. Edited or unmarked import shards are refused rather
than overwritten. The native extraction is never the output.

For an isolated staging overlay, run from the worktree:

```powershell
$env:SRO_CAP110_ITEMS = '1'
python -B scripts/cap110_item_import.py --native-extracted <native-client>/extracted --vsro-root <later-client>
```

Output is `.state/cap110-weapons/extracted`; this is an overlay for review,
not a complete game root. The generated report is
`.state/cap110-weapons/extracted/.opensro-cap110-items.json`. The integrated
Alexandria build instead applies these files to its complete optional root.
Neither command changes the running stack's cap or publishes browser assets.

The hash-identified archives from phase 0 produced 203 required render
resources: 170 from the later archive and 33 unchanged native resources.
The Data.pk2 SHA-256 is
`0f6191ba393df32b2258dee38cc5ab100dc91210d1fa121c1d3845fdc74d8c3b`.
The audit follows shared BSR/BMT parsers through meshes, skeletons, animations
and material textures. European paired axes/daggers and female harps use
the existing equipment model resolver, not filename fallback guesses.
Native drop model particle/audio modifiers retain their existing owners;
new later particle/audio modifiers fail the importer. Leaf resources are
checked for presence and hashed; this is not a rendering certificate.

The production `sro-item-catalog -textdata <staged-textdata>` loader accepted
all 112 staged weapons with their assigned IDs and localized names. There
are 98 description symbols with no authored translation; these remain
absent rather than receiving invented text, while every weapon name is
required. No matching stock was found in the supplied `shopitemdata` or
package shop tables for these imported ordinary weapons. No shops, prices
or drop chances were invented or enabled.

Explicit limitations:

- The 376 set rows (56 Egyptian set weapons among them) import dormant
  with the rest, beside their validated group catalogue; set bonuses need
  their own runtime mechanics and are not applied. Event and fortress
  tools are outside the ordinary weapon selection.
- 39 degree-12 item links in degree-11 rare weapon parameter labels are
  trimmed and individually recorded. This is an explicit port-only scope
  inference: the degree-12 conversion feature is outside the v1.150/110-cap
  expansion. Numeric flags are not mistaken for item IDs.
- Armor, shields, accessories, required consumables and full shop conversion
  remain the next item work. Set effects belong with the corresponding
  skill/mechanic implementation.
- Browser asset publication, equipped appearance, attack animations and a
  live combat playthrough have not been verified for these imports.

Verification: the source pipeline passed with the previously documented
cgo race step excluded (the server gate reported up to date). Converter
tests cover native precedence, ID collisions, both races, parameter-link
trimming, required names, optional descriptions, paired/female models,
missing resources, shop dependencies and off/on/off rebuilds without writing
through native hard links. The running local stack was not restarted.

## Next implementation

Import the required consumables and complete authored NPC shop stock, prices
and conditions for the expanded gear. Then import the ordinary skills
needed through 110 and verify their full
execution and learning paths. The framing inventory above is the starting
list, not an execution certificate.

Before release, extend level-limited client/server consumers such as party
matching and the quest guide; they still have era-specific limits. Add
monster population, rewards and drops; restore intended teleport conditions
instead of silently dropping unknown kinds. Keep the expansion off until
the full 90-to-110 playthrough and both flag settings pass.

## Evidence scope

No new binary behavior is claimed here. Table counts and curve differences
are direct observations of hash-identified archive payloads. Skill findings
are tests against the current port compiler, not a fresh disassembly of the
later client or the v1.188 server. Existing native provenance stays with its
own implementation; the raised cap is an explicitly optional deviation.

## Verification at this checkpoint

The archive comparison completed against both hashes above. The focused
progression, catalogue, configuration, GameWorld and command tests passed,
as did the 118-test Python suite. Source policy, formatting, encoding,
script checks and typechecking passed. Server vet, lint, ordinary package
tests, module hygiene, vulnerability scan and release contract checks passed.

The full `pnpm check source` command exited 1 because its server race step
reported `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`.
No C compiler was available on PATH or at the checked standard locations.
The remaining server checks were rerun explicitly without that step.
Race coverage and a full licensed-asset build/playthrough remain unverified.
At that checkpoint the running stack had not been restarted or enabled.

## Local deployment and testing, 2026-10-16

The variant asset build and server-game-data export subsequently passed.
All 112 imported weapon equipment entries were present in the compiled
client roster. Nomad deployment completed with `SRO_LEVEL_CAP=110` and
healthy Agent and GameWorld allocations; the local browser is served at
`http://127.0.0.1:5380/`. NPC weapon stock was not imported: use the GM
`/MAKEITEM` command and pick up the spawned item.

The automated browser attempt stopped at character selection before world
entry, so live weapon pickup and equip remain manual verification work.
The complete Python suite subsequently passed 128 tests. The server race
limitation above remains unresolved.

At the owner's request, the local `[GM]100` character was raised from level
90 to 110 through the exclusively locked authority store while GameWorld
was stopped. A pre-change SQLite snapshot was retained under the ignored
local state directory. Existing stat allocations were preserved, normal
automatic STR/INT growth and 60 free stat points were added, and the level
was verified by reopening the persisted store before restarting GameWorld.
This is local test character maintenance, not a shipped gameplay rule.

## Shields, armor and accessories

The optional importer now selects ordinary and Seal D10/D11 shields, all
six Chinese/European armor types for both sexes, and both accessory types
by their item type cells. It imports 824 items: 112 existing weapons,
16 shields, 648 armor pieces and 48 accessories. The original 112 weapon
IDs are retained from the previous import marker; fresh imports also assign
weapons first, preserving the original ID order. Native rows remain intact.
Linked codenames must resolve within the native plus imported item closure.

Authored circlets and accessories may have no worn mesh. They retain their
icon and drop model, and the existing equipment renderer owns their visual
absence. Requirements, sex, race and combat statistics remain authored.
All 824 imported IDs and localized names pass the production server item
loader. Set gear (376 rows) imports dormant beside its chain-validated
group catalogue (refsetitemgroup plus the MAGIC_11_SET_* rows of
refmagicoptgroup); no set bonuses are applied. 279 explicit degree-12 links
are trimmed and recorded. Missing authored descriptions remain absent.

The resource audit verifies 770 files, including ambient shoulder effects
through the shared BSR and EFP parsers, their particle meshes and textures.
Native resources take precedence. Later sound modifiers and unsupported
particle selectors still fail. The later Particles.pk2 SHA-256 is
`5644c43be25a6b45c809a6097766b8be7f5f3d1f7418cf99bca9a59a89fa9a7b`.
The original native particle archive remains the build's base; imported
effect files live only in the optional extraction overlay.

NPC stock is still not imported. The dependency report now identifies
18 authored shop packages to resolve in the shop phase. Manual GM spawning
uses `/MAKEITEM ITEM_CH_SHIELD_10_A 1` or
`/MAKEITEM ITEM_EU_SHIELD_11_A 1`; rare variants append `_RARE`.

Verification: 134 Python tests passed, including ID retention, equipment
statistics, opposite-sex links, model-less accessories, cross-archive
particle closure and off/on/off native hard-link preservation. Script
typechecking, formatting, encoding, size and generated-root checks passed.
Rendered appearance, combat and full progression remain manual checks.

The resource build and server game-data export passed. All 824 imported
items appear in the published equipment catalog, with 832 body/model
bindings and the D11 shoulder effect present. Local HTTP presentation
delivery returned 200. Nomad completed healthy Agent version 2 and
GameWorld version 8 deployments, retaining cap 110 and installing manifest
`sha256:348c4ff7651ea3b7b460fedb8c62cb2a49eed157f63c35ab63457e81938e5b3b`.
The initial cold GameWorld start restarted once after
`healthcheck: check "liveness" unhealthy`; the subsequent start became
healthy and transport readiness returned 200. Player records were preserved.
All 824 GM commands are saved locally in the ignored
`.state/live-cap110/makeitem-equipment.txt` file. No automated game session
was opened or equipment spawned as part of this deployment.

Subsequent Alexandria NPC corrections are recorded in
[ALEXANDRIA_NPC_FIXES.md](ALEXANDRIA_NPC_FIXES.md). They restore the grocery
shop dependencies and supported merchandise, plus missing placement, names
and map labels.

## Ordinary degree 10 shop stock

`scripts/alexandria_shop_d10.py` (called from `alexandria_npc_services.py`,
effective only when the D10 items exist) stocks Alexandria's own shops. The
smiths and armor traders no longer share Khotan's stores: the city gets
`STORE_SD_SMITH` and `STORE_SD_ARMOR`, copied from `STORE_KT_SMITH` and
`STORE_KT_ARMOR`, which stay unchanged. `STORE_SD_ACCESSORY` is restocked in
place. Each degree 9 ordinary (grade A) good gains the degree 10 good of its
family right after it: 93 packages (weapons, shields, armor, accessories; CH
and EU). A tab never exceeds the largest native tab (45 slots), dropping the
lowest degree first. A package's price is its item's price column; v1.150 has
no degree 10 shop, so this is an inference. Degree 11, grades B and C and
rare or seal items are not stocked.

Verified in-game, 2026-10-17: the owner opened Alexandria's own smith, armor
and accessory traders on the local deployment and confirmed the ordinary
degree 10 stock is present and purchasable. This closes the pending in-game
check for this change. It is a manual spot-check, not an automated test; the
built-table checks and the 11D exclusion above are unchanged.

## Phase 3 scope: the later client's skill tier

`scripts/analysis/skill110_scope.py` compares the two skilldata shard sets by
codename, never numeric id, and reports the player ranks the later client adds
whose mastery requirement is above the native cap. It reuses the shared,
manifest-validating `compare_cap110.read_tables`, so a short or undecodable
row fails the tool instead of becoming a scope finding. Run from a worktree:

```text
python -B scripts/analysis/skill110_scope.py --native-root <native-client> --vsro-root <later-client>
```

Observed on 2026-10-17 against the same hash-identified archives as phase 0:

| `SKILL_CH_*` / `SKILL_EU_*` service rows | Native | Later |
| --- | ---: | ---: |
| Mastery requirement at or below 90 | 4,635 | 4,635 |
| Mastery requirement 91 to 100 | 0 | 757 |
| Total | 4,635 | 5,392 |

The later client adds 757 player ranks and removes or renames none of the
shared codenames. Of those, 555 are trainable ranks and 202 are chain sub-rows
the chains point at; both must be imported or a chain link dangles.

| Mastery | Line | Race | Trainable ranks |
| --- | --- | --- | ---: |
| 257 | sword | China | 45 |
| 258 | spear | China | 32 |
| 259 | bow | China | 30 |
| 273 | cold | China | 32 |
| 274 | lightning | China | 18 |
| 275 | fire | China | 29 |
| 276 | water | China | 25 |
| 513 | warrior | Europe | 74 |
| 514 | wizard | Europe | 55 |
| 515 | rogue | Europe | 51 |
| 516 | warlock | Europe | 68 |
| 517 | bard | Europe | 44 |
| 518 | cleric | Europe | 52 |

Every mastery id already exists natively; the tier adds no new mastery. The
race split is 211 China and 344 Europe. Requirements top out at mastery level
100: no rank requires 101 to 110, so in this client the "110 cap" content is a
mastery-100 tier, even though a mastery may train to `min(characterLevel, 120)`.
The learn handler refuses a zero-SP rank, and one of the 555 trainable ranks
has SP cost 0; that rank needs review rather than shipping as-is. All 757 frame
in the production compiler - phase 0's only framing failures were an `MSKILL`,
two `SKILL_MALL_NASRUN` and one `SKILL_EVENT` row - but framing is not
execution: the tier still needs its execution owners, exactly as the item
import needed its drop models and effects.

The scope (every trainable rank with id, group, mastery, level, rank, SP and
race) is written to `.state/skill110-scope.json`. The later client's release
version is still not authenticated; the scope is keyed to the shard hashes the
report records.

## Phase 3 checkpoint: the optional skill import

`scripts/cap110_skills.py` (merge) and `scripts/cap110_skill_import.py` (I/O)
implement the tier behind `SRO_CAP110_SKILLS` (1/on/true; off is native). Every
native row is kept; only later player ranks above the native cap are added,
joined by codename, never the later numeric id. `build_alexandria_variant.py`
calls the importer after the equipment import; turning the flag off removes the
generated shard and restores the native `skilldata.txt` manifest.

Identity follows the later client's own references, all remapped by codename:

- the later group ids are a bijection onto the native ones over shared ranks
  (484 to 484), so a new rank attaches to its native ladder through that map;
- a group no shared rank uses is a genuinely new tier and takes a fresh id
  above the native range - 26 of the 28 new groups would otherwise collide
  with an existing native group id;
- chain-next and the three prerequisite group ids are remapped through the same
  maps; every chain target lies inside the imported set (202 links);
- ids are assigned from one above the highest native or later skill id, and a
  prior marker keeps every published id stable across rebuilds.

Observed on 2026-10-17 against the hash-identified archives, writing one
`skilldata_110000.txt` shard:

| | Value |
| --- | ---: |
| Rows added | 757 |
| Trainable / chain-sub | 555 / 202 |
| New groups | 28 |
| Assigned id range | 31925 - 32681 |

The shard has 118-column rows, unique ids, and no codename or id overlap with
native. Every one of the 283 name symbols already exists in the native
`textdataname`, so no text is imported; the client skill board, the mastery
tables and the tooltip rows are untouched.

Explicit limits carried into the marker:

- Existing native rows, ids and group ids are unchanged.
- A rank with no execution owner does nothing when pressed; this import adds no
  owner, so the tier's effects and learning paths are not yet verified.
- No render, mastery or skill-board display change is made; the client and a
  live playthrough remain the verification steps.
- The later client's release version is still not authenticated.

Verification: the converter's 10 unit tests pass, the full 165-test Python
suite passes, and an isolated overlay run produced the shard above; the off run
removed it and restored the seven-shard native manifest.

## Local deployment, 2026-10-17

The variant was rebuilt with `SRO_ALEXANDRIA`, `SRO_CAP110_ITEMS` and
`SRO_CAP110_SKILLS` on; the resource build published 28,592 projected skilldata
rows from 8 shards and packed 40,068 assets. The server game-data export wrote
4,116 files with manifest
`sha256:6791e2611c8ad2a05ad334574e4bd65d028d2677048dc25c8c6a5b7356b0e8e1`,
and the tier's `skilldata_110000.txt` is in that projection. `sro-nomad`
deployed Agent version 9 and GameWorld version 15, both 1/1 healthy, and the
Agent and GameWorld transport readiness endpoints returned 200.

No skill was learned or pressed, and no visual check was made. The tier's
skill-board display and its execution owners are unverified: a rank with no
owner does nothing when pressed. The client needs a hard reload to read the new
manifests.

## Skill tier: runtime findings, 2026-10-17

The owner tested the tier in-game. Native skills animate normally; the imported
tier does not, and its skill-board display is partial (some lines show one more
rank). Offline checks against the running variant:

- **Execution is data-driven and mostly covered.** Loading the variant through
  the production loader, 555 trainable ranks compile to: offense 286,
  timed-effect 79, passive 78, instant-effect 14, recovery 5, threat 3, position
  2, and 88 unsupported (for example `..._PASSIVE_A_10`, `GIGONGTA_D_14/15`,
  `E_01/02`, `EU_WARRIOR_SHIELDP_ATTACK_A_23`). 462 reach a press owner, 467 carry
  a compiled execution kind. There is no per-skill owner registry to fill.
- **Effect records mostly exist.** 566 of the 757 imported ids have a record in
  the published `effectRecords.json` because their anim-set base name reuses a
  native `skillaniset` row. 191 do not: their anim-set is one of 997 later-only
  entries (73 distinct bases); ~113 of those exist in the later client and could
  be imported, ~78 have no anim-set.
- **The board columns are largely valid.** The imported ranks reuse native tabs
  (0/1/2) and UI rows (0-10); only 171 of 757 are flagged off-board. The partial
  display is therefore in the client pane grouping, not a tab/row mismatch.
- **Observed gap.** The owner reports that even covered imported ranks (a record
  exists, an execution kind compiles) play no animation, while native skills do.
  That points at the client's skill-motion playback for imported ids rather than
  at missing effect data, and an anim-set import would not fix those 566. The
  root cause is not yet established; candidates are the client motion resolver,
  the cast the server emits for the new ids, or the learned-skill/pane path.

Next: root-cause the client skill-motion path for imported ids before importing
later anim-sets; then the later anim-set merge covers the 191, the 88 unsupported
shapes need compiler work, and the pane grouping needs the client skill-board
owner.

## Skill tier: animation import and in-game check, 2026-10-17

Temporary client diagnostics finished the runtime investigation: a covered
imported rank casts and animates (id 32210 resolved `native:default:*` phases),
while an uncovered rank logged no effect record. So the cause was the missing
animation sets, not execution or the board columns.

`scripts/cap110_skill_effects.py` merges the later client's `skillaniset` rows
for imported ranks whose animation set the native table lacks, inside the right
section with native rows winning; a later row naming an unknown native animation
state is refused rather than guessed. `cap110_skill_import.add_skill_effects`
wires it into the variant build behind `SRO_CAP110_SKILLS` and restores the
native `skilleffect.txt` when off.

Against the real archives it merged 27 rows for 28 uncovered bases; the resource
build then published 26,370 effect records (679 in the imported id range, up
from 566) with 5,753 skill-data joins. The owner confirmed the previously-silent
ranks now animate in-game. The temporary client diagnostics were removed.

Still open: about 46 uncovered bases have no animation set in either client and
stay silent; 88 ranks compile to no execution shape; and the skill window's
board listing is unchanged - the new ranks appear as a mastery rises, and the
display path was not traced to the code that builds it.

These three were re-measured on 2026-10-26; see the next section.

## Skill tier: execution, silent bases and board, 2026-10-26

Re-measured against the same later archive (hash above), loading native plus
the imported shard through the production loaders (#1746):

- **Execution.** The 88 ranks with no execution plan are not ranks that do
  nothing: the plan only gates offensive sequences. The press router
  (`action.SkillPressHandled`) handles 540 of the 555 trainable imported ranks
  (handled or passive). The other 15 belong to native families the catalogue
  under-reported (imbues and instant skills, #1771) and to the cleric's
  special-rebirth buff companions, which are applied, not pressed.
- **Silent bases.** The 46 bases with no animation set are passive families
  (and the warrior's frenzy defence line) whose native ranks name the same
  bases, absent from the native `skilleffect.txt` too: silent in the original
  as well, so nothing is missing.
- **Board.** The v1.150 client's board build never creates a cell for a skill
  whose first mastery requirement is above 90 (`cmp byte [record+0xac], 0x5a`
  at 0x7F1151). The native tables have no such rank; the imported tier has 586
  on the board. The browser client does not port the gate, so the new ranks
  show as the mastery reaches them (recorded in `skill-slot.ts`). The 18 new
  groups on the board land in free cells under existing group headers.
- **Program decoder.** `CompileSkillProgram` accepts all 757 imported rows.
- **Learning.** A mastery is bounded by `MasteryLevelCap` (120) and the
  character's level, so with the level cap at 110 masteries reach 110.

Still open: the owner's in-game pass with the tier on (learn a rank per
mastery from 91 to 110; press one offence, heal, buff and passive).

## Database tier through mastery 110, 2026-10-17

The client-only import above stopped at mastery 100. The owner's VSRO shard
contains another 761 enabled player rows for mastery 101-110. The optional
importer now accepts an explicit `_RefSkill` JSON snapshot through
`--database-export` or `SRO_CAP110_SKILL_DATABASE`. No SQL connection or
research file is required by the running product.

`scripts/cap110_skill_database.py` projects 118 explicitly named SQL columns;
JSON property order is irrelevant. Missing fields, invalid cells and a snapshot
without the requested tier are refused. The client tier is merged first. The
database tier then joins against native plus already-published client rows by
codename, with independent source IDs and group mappings. A prior marker keeps
every published ID stable. Losing the database input on a later on-build fails
instead of silently removing learned skills; explicit off still removes the
owned shard. New rows above mastery 110 are excluded.

Observed staged/published results:

| Item | Result |
| --- | ---: |
| Added rows, mastery 91-110 | 1,518 |
| Existing client-tier IDs preserved | 757 (31925-32681) |
| New database-tier rows | 761 (32682-33442) |
| New tier root candidates / chain stages | 536 / 225 |
| New database groups | 20 |
| Total board-eligible imported ranks | 1,090 |
| Active database-tier candidates with effect records | 474 / 474 |
| Added name/description/study symbols | 104 |

The root count is not a learnability guarantee. One zero-SP Cleric resurrection
buff helper is authored off-board, explaining the 1,091 root candidates versus
1,090 displayed ranks. Its two absent study/description symbols remain absent;
no player-facing name was invented. Character level 110 does not waive mastery,
prerequisite or SP gates. A regression test checks refusal at mastery 100 and
successful paid training at mastery 110.

Animation merging now preserves other overlays' sections, especially Alexandria
NPC `characterInfo`, and owns its inserted bases in a separate marker so off
removes only those rows. The production effect builder successfully handles the
bow's `MOV_PIERCE` through the existing native-family adapter, with evidence from
`SKILL_CH_BOW_PIERCE_A/B/C` selecting `MOV_STRAIGHT`. The earlier asset audit used
the variant table as the comparison reference; production uses the preserved
v1.150 `resinfo` table. No guessed enum or new movement implementation was added.

A reused D10 shop clone now reconnects its NPC groups after source NPC mappings
are restaged. Previously the clone-exists early return could leave Alexandria
pointing back at Khotan stock. This preserves the already-approved D10 shops and
does not modify Khotan or add D11 stock.

Database snapshot SHA-256:
`15a709b6b84b0ab70dc96772f36a74f1f2af6f30782d48ee4558c936a7f4c843`.
Source shard SHA-256 remains
`b4d85a043ebce36c8704768dce0dfa63ff9418c8489241d8441b7902e28fa37a`.
Local input: `.state/vsro-database/exports/vsro_inspect-player-skills.json`.
Set its absolute path in `SRO_CAP110_SKILL_DATABASE` along with
`SRO_CAP110_SKILLS=1` when rebuilding the full variant.

The three switches that make the tier learnable in game must agree, and
nothing joins them automatically: `SRO_CAP110_SKILLS` publishes the
client tier at build time, `SRO_CAP110_SKILL_DATABASE` adds the 101-110
database tier, and `SRO_LEVEL_CAP=110` on the server admits the levels.
Publishing the database tier while the cap stays at 90 leaves the
101-110 ranks board-visible but unlearnable under the generic mastery
refusal, so the gameworld warns once at startup when the loaded table
requires a mastery above the configured cap.

Verification: 177 Python tests, production parameter decoding of all 1,518
rows, progression/enterworld Go tests, progression vet, five skill-slot tests,
and four native effect/spine tests passed. Repeated on-builds preserved IDs;
the isolated off-build removed its owned shard and marker. Packed-asset checks
verified every eligible board rank, row group, display name and active new-tier
effect record. Focused skill-data/audio, effect/stage-model/EFP, text and skill-UI
builders and the standard pack rebuild passed. Source format, encoding, size,
generated-root and script checks passed. No in-game cast test was automated.

The native spine test's exact whole-table census fails against the expanded
variant (`484/300/11` versus native `373/239/11`); it passes against the native
root. The variant's added records were verified separately rather than changing
the native census expectation.

Execution limits remain explicit: a generic press audit must not classify
passives or the instant imbue path as missing casts. Of the new 536 root
candidates, 62 are passives and 20 use the instant path. The one ordinary
queued candidate that used to be refused, `SKILL_EU_ROG_STEALTHA_CHANGE_B_03`
(33156), is admitted since #1746: tracing the reference server showed its
Duplicate branch takes the row's own target classes and gates only the
target's suit, battle mode, level, GM privilege and murderer state, so the
parser now admits the enemy-player envelope and the press path puts no
relation gate on the family. More generally,
decoded parameters, an effect record and an available command owner are not an
in-game execution certificate; test representative CH/EU casts and learning.

Server export manifest:
`sha256:a8195527550d8ab6ec8a325c76b807bf5b1090c0b19b06662101990b432436bd`.

## Owner verification and icon publication, 2026-10-17

The owner confirmed the CH/EU skill trees and reported that the tested skills
work in combat. This is manual owner verification, not an automated exhaustive
test of every rank, target combination, status interaction or numerical value.
Sable Duplicate (33156) was then a known cast-admission issue, explicitly
deferred by the owner; #1746 later traced the reference server's handling
and admitted the rank, so the exception is closed.

The delivered skill-window catalogue referenced 20 missing packed images:
19 Chinese icons and one Rogue icon, covering 51 rank rows. Their source DDJs
and converted PNGs already existed. Focused `skill-ui` publication rebuilt the
data without publishing its image dependencies. It now uses the existing CIF
skill-icon collector/copy owner and assigns new images through `imagePackGroup`.
Native absent-icon handling is unchanged; no replacement artwork was invented.

`pnpm assets publish skill-ui` published 572 files and refreshed the game-data
and ui-icons groups. A direct packed-asset audit checked all 4,888 skill-window
rank rows and found zero missing icon references after publication. The owner
then confirmed that the icons resolve in-game. Scripts, formatting, source
encoding, generated-root and `git diff --check` passed for the icon change.

Local test fixtures are excluded from source control: `[GM]100` on `gm100` is
level 110 with all seven CH masteries at 110; `[GM]101` was created on the same
account at level 110 with all six EU masteries at 110 and 2 billion SP. Both
are GM-allowlisted. Character data was backed up and reopened for verification;
Agent and GameWorld readiness returned 200 after restart. These fixture grants
do not alter normal character creation or native progression defaults.

Upstream review preparation is in `docs/RETIRED.md`.
The expansion remains optional; monster/drop progression and cave stability
remain separate work before a complete cap-110 release.

## Field monsters deployed, 2026-10-17

The database-authored Alexandria field population is staged and live behind
`SRO_CAP110_MONSTERS` (default off): 29 monster types, 1,519 nests, 80 monster
skills and the database equipment-drop catalog through level 110. The variant
manifest lists the characters, skills, nests, regions and the source-backup
hash for each input.

The asset build published 29,431 skilldata rows from 9 shards, 27,147 skill
effect records, 1,903 mission NPC/monster entries with 20 new VATs, and 40,913
assets in 248 packs (browser manifest `696c4ec22e9e`). The server game-data
export wrote 4,250 files with manifest
`sha256:fd0042fe3ca9a2897c5f9336856033728b281d0b9b4d28e1c614c3906835181f`.

The fleet was redeployed from the saved pre-stop job flags with
`SRO_CAP110_MONSTERS=on` and `SRO_LEVEL_CAP=110`: Agent version 19 and
GameWorld version 25, both 1/1 healthy, readiness 200 on the Agent and the
GameWorld transport.

Verification: 180 Python tests passed, plus the loot, monster, sro-gameworld
and shardsettings Go packages. Not verified: any in-game spawn, combat, drop or
progression check, and the three monster EasyFX programs the build flagged as
non-v11 version 13 (it continued rather than guess their layout).

## Apis spawn check, 2026-10-17

The owner GM-spawned `MOB_SD_APIS` (character reference 26859, skills 33521 to
33525) through the console in the Holy Water Temple. It renders at its authored
scale and grade, with its model, materials and horn-flame effect intact.

Combat, its five skills and its drops remain unverified. The earlier staging
collision is fixed: the importer now reserves every retained monster identity
before allocating new ones. 188 Python tests and the monster and loot Go
packages pass.

## Owner field test and commit review, 2026-10-17

The owner reports successful botting against level-101 field monsters, including
equipment and gold drops, with no observed problem. This supersedes the pending
in-game check above for that tested slice; levels 102-110 and every monster's
effects, numerical rewards and rare drops are not exhaustively certified.

Reviewed commits `c4347c9e`, `d48bb641` and `8c1ea61f`. Live Agent and GameWorld
readiness both returned 200. The installed population has 29 types and 1,519
nests; the import marker contains 78 added skill rows, correcting the earlier
80-skill summary. Five Go packages and 40 cap-110 Python tests passed in this
review. No gameplay implementation was changed during review.

Two follow-ups were identified (both resolved below):

- `cap110_monsters.merge_skills` does not reserve all previous target IDs before
  allocating new ones. Replaying the real snapshot with Apis's attack references
  added to the ordinary field set fails with `monster skill identity collision:
  MSKILL_SD_UNEG_ATTACK01`. Existing deployments work; extending the import can
  fail. Reserve retained identities before allocating new skills and characters.
- `sro-catalogue` still calls `monster.LoadTemplate` without `AppendCap110`, and
  does not initialize the optional loot catalog. Regenerated public monster/drop
  data therefore does not describe the expanded live shard. Reuse the same
  optional startup loaders before building those catalogue projections.

The F3 GM item picker is planned in `GM_ITEM_PICKER.md`, not implemented. Caves
and Rogue Sable Duplicate remain deferred by the owner.

## Import identity and catalogue fixes, 2026-10-17

The monster importer now reserves every published character and skill identity
before assigning additions, including identities absent from the selected set.
Invalid or colliding marker identities fail explicitly. Replaying the actual
Apis extension now retains all 78 published skill IDs and adds five dependency
rows without a collision. This was a read-only replay; Apis was not added to the
live population.

The catalogue command now initializes optional equipment loot and appends the
optional monster population using the same loaders and flag as GameWorld.
Its setting defaults to off. Actual CLI exports verified 29 expansion types
with regions when on (212 total monsters), versus none when off (183 total).
The level-101 table contains 94 normal and 200 rare equipment entries with
positive probabilities, including degree-11 equipment; that table is absent
with the expansion off. Verification output lives in the ignored
`.state/cap110-review/catalogue-{on,off}` directories, not the published site.

Verification: 42 cap-110 Python tests and the catalogue, shardsettings,
catalogue projection, loot and monster Go packages passed. Script typechecking,
formatting, source encoding, generated-root and source-size checks passed.
The first attempt to batch task names failed with `TS5042: Option 'project'
cannot be mixed with source files on a command line`; rerunning each task
separately passed. No live server, character or licensed data was modified.
The full `pnpm task check:server` gate failed its race step with
`go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`. This shell has
CGO disabled and no GCC on PATH; the full gate is not reported as passing.

## Outdoor Alexandria dimensional gates, 2026-10-17

Restored four source-authored teleport buildings and four directed links in
the optional Alexandria variant:

| Delta side | Remote side | Fee | Admission from Delta |
| --- | --- | --- | --- |
| Abundance Ground (`GATE_DELTA_GATE_E`) | Storm and Cloud Desert (`GATE_STORMDESERT_GATE`) | 0 | Level 95+ |
| Forbidden Plain (`GATE_DELTA_GATE_W`) | Kings Valley (`GATE_KINGSVALLEY_GATE`) | 0 | Level 95+ |

Both pairs have free return links without a level condition. Abundance Ground
is the source name of the Delta East endpoint, not an additional shortcut.
The importer keeps the source teleportbuilding interaction positions/bounds
separate from teleportdata arrival positions. No cave entrances were added.
The existing `SRO_ALEXANDRIA` opt-in still owns the variant. With the cap-110
monster flag on, gate projection now uses the 110 ceiling, preserving the
source's level-95 conditions on imported links (including city/ferry access).
With that flag off the existing level-90 projection ceiling remains.

Evidence is the owner's VSRO client Media archive's `teleportdata.txt`,
`teleportbuilding.txt`, `teleportlink.txt` and `textdata_object.txt`, joined by
codename. The read-only SQL helper was unavailable because Docker's Linux
engine pipe was absent; no fresh database comparison is claimed. Source fees,
positions, bounds and supported conditions were copied, not inferred.

Published the updated text and teleport projections through their builders
and the standard pack owner: one pack rebuilt, 247 reused, web manifest
`8526d2263624`. Verified both directions and resolved names from delivered
pack bytes. The server export has 4,250 files, manifest
`sha256:21dbb3e83fd7cc59a8f58886d5d34cec5ee3ef4ec25a8c4aa0a37695efcab3c6`.
Local Agent version 21 and GameWorld version 27 deployed healthy; both
readiness endpoints returned 200. Configuration and an authority snapshot
were saved under ignored `.state/alexandria-portals/` before restart. Native
input hashes are unchanged; other item, skill and NPC overlays were retained.

Verification: 63 Alexandria Python tests and 47 cap-110 Python tests passed,
as did script typechecking, formatting, encoding, generated-root and size
checks. The expanded-data Go simulation/action run was not green: examples
include `NPC_SD_M_AREA_SPECIAL3 ... silently falls back to zero`,
`machine id 200198 outside its band`, and the native ferry census test's
`refused a near transport: 0x15` / `census: 4 town gate links, 36 crossings`.
That ferry fixture uses level 90 and native counts; imported ferry entry now
correctly retains its source level-95 requirement. Do not rewrite native
census expectations to certify a variant. The focused gate roster/wire,
selected-NPC transaction/rollback, structure transaction and far-transport
refusal tests passed against the expanded data. In-game clicking, travel and return
through these four gates still need the owner's playthrough after a hard reload.

## Outdoor navigation index repair, 2026-10-17

The owner's first portal trip failed with `Missing navigation BSR 2076`.
Reproduced that exact error through the production client navigation loader
using delivered pack bytes. The source BSR exists; the outdoor shared object
index had only 1,687 BSR records and omitted objects used by the expanded
regions. Both the Kings Valley and Storm and Cloud Desert endpoints referenced
missing entries. The earlier portal validation checked links, names and gate
transactions, but did not exercise destination navigation loading.

`buildOutdoorSharedObjectResources` now gathers the current complete sector
usage before reusing its index. Retention requires every used object to match
its source path, flags and placement count; builder code stamps alone do not
notice an expanded source region set. Regression tests cover newly referenced
objects, changed catalog paths/flags/counts, duplicate identities and native
path case folding. Source assets and the client's strict missing-resource
checks were not changed.

Rebuilt the shared object resources and the four gate region bundles through
the outdoor builder: 1,741 BSRs, 7,102 BMS meshes, 2,363 enabled outdoor regions.
Published through the outdoor pack owner: seven packs rebuilt, 116 reused.
The production navigation loader then loaded all four endpoint regions from
delivered packs successfully, including their neighboring navigation regions:
19001 (35 collision placements), 21569 (23), 23608 (19) and 21812 (19).
The two new cache regression tests, script typecheck, format, source encoding,
generated-root, source-size and diff checks passed. No full-suite pass is claimed.

The matching server collision projection was rebuilt: 4,296 files, manifest
`sha256:81c366852d53d43acee275b001b6311da1f0c42becc69b555dc9ec6108d494c7`.
The browser serves the rebuilt pack manifest (SHA-256
`99418ea6bf46c716d46aa9df11d18d3e953a0c3b4889db4d323a65b7645d9ccb`).
Backups, build logs and the exact client-loader verification script are in
ignored `.state/alexandria-navigation/`. A hard reload is required to discard
the failed session's old pack manifest before retrying.
Local Agent version 23 and GameWorld version 29 were deployed; Agent and
GameWorld transport readiness returned 200 after startup.

## Untextured world materials and scenery effects, 2026-10-17

The owner reported `Invalid geometry material` when loading regions 0x4c48
and 0x4c4a, anchored at 0x4b42, plus missing effect
`dun/red_orange_flame_sb_s.efp`. Both failures were reproduced from packed data.

The shared Egyptian entrance BSR `res/dun/asiam/r2_cv/st_egyptent.bsr` has an
authored untextured `st_lv100_doorsill` material. Its resource projection carries
`texturePublicPath: null`; `worldObjectMaterial` forwarded that null into a
renderer contract which accepts an absent texture, not null. The conversion
now represents both absent source forms as undefined while keeping the original
diffuse color and draw flags. Strict renderer admission remains unchanged.
The source material interface now describes the nullable published field.

The outdoor index rebuild also introduced reachable scenery EFP references
without rebuilding the effect program catalogue. Outdoor pack refresh now
builds and publishes the complete effect dependency closure through the
existing effect family/pack owner before publishing outdoor packs. The missing
flame file is a version-11 EFP and already exists in the extraction; no effect
layout was guessed or substituted. The rebuilt catalogue has 974 programs,
133 effect meshes and 475 textures, with one newly copied texture.

Verification against delivered pack bytes: both reported regions decoded their
full object scenes at the reported anchor (183 and 60 groups respectively),
all 9,999 shared outdoor material instances passed renderer admission, and
the flame program decoded into nine primitives with all seven image
dependencies present in packs. Fourteen focused world material/decoder/compound
tests passed with the variant roots configured. Initial asset-reading test
failures from missing default roots were corrected by configuring those roots.
Script typecheck, formatting, encoding, generated-root, source-size, client
TypeScript and client test-type checks passed.

The full client check was attempted but did not pass: it used absent default
assets (`ENOENT ... C:\opensro\.generated\client-public\assets\packs\manifest.json`)
and also reported unchanged world-map ownership violations
(`shared module state must be an immutable scalar constant`). The quick check
likewise reached that ownership failure after passing TypeScript. These broader
failures are not claimed resolved. Build/audit scripts and logs are retained
in ignored `.state/alexandria-materials/`. No server/authority data was changed;
hard reload the client to replace its worker code and effect pack manifest.

## Complete outdoor terrain components, 2026-10-17

The owner reported gray missing ground at Chaos Maze near minimap coordinates
X -12969, Y -3462, also seen in the mountain area. The source availability
bitmap enables region 0x4a43 beside the photographed area, but the installed
variant did not. The existing region's terrain itself had all 36 blocks and
no missing texture-association quads: the absent neighbor, rather than an
invalid material or texture, explains the visible boundary gap.

The stage-1 import previously flood-filled VSRO-only terrain from the city,
then appended the exact monster-spawn regions. This admitted the hunting
patches but omitted empty connecting regions and mountain terrain around
them. Field regions now seed the same connectivity traversal as the city.
Selection is still bounded by the source VSRO availability bitmap; unrelated
components are excluded, and the flag-off city selection is unchanged.

The complete import has 334 outdoor regions instead of 240: 94 source-authored
regions added, without changing monster populations or inventing terrain.
Native input hashes were verified unchanged. Region text, minimap resources,
the existing tile-ID remap and the source navigation files are included in
the rebuild. Raw source/archive hashes and backups are recorded under ignored
`.state/alexandria-terrain/`.

For a future variant rebuild, rerun stage 1 with `SRO_CAP110_MONSTERS=on` and
the existing `SRO_CAP110_MONSTER_DATABASE` export directory, or use the fresh
isolated stage-1 root
`.state/alexandria-terrain/source-native/Alexandria_extracted`. Do not reuse
the older 240-region stage-1 snapshot: it predates this selection fix.

The expanded-seed regression and 64 Alexandria Python tests passed, along
with script typecheck, formatting, encoding, generated-root, size and diff
checks. The regression also preserves city-only selection and excludes an
unrelated candidate component.

The rebuilt browser has 2,457 globally addressable outdoor regions. A check
using the production client decoders and delivered pack bytes verified all
334 imported regions: every region has all 36 terrain cells at each of the
four LODs, and all 334 navigation neighborhoods loaded completely. The
previously absent 0x4a43 is now included. The 23 published tile remaps were
preserved. Outdoor publication rebuilt 21 packs and reused 104; the final
text/minimap repack rebuilt four packs and reused 247, web manifest
`c7528671ced6`. This establishes data/decoder coverage, not a complete visual
playthrough of every terrain edge.

The matching server projection has 4,390 files, manifest
`sha256:679a6fe85abbe565a5b87ad7e589ea26485ae81e5c21766085f06fc99ab59e1e`.
Agent version 25 and GameWorld version 31 deployed successfully; both readiness
endpoints returned 200, and the browser serves the updated pack manifest.
Configuration and authority snapshots were saved before restart. Hard reload
and log back in to replace the old region catalogue.

### 2026-10-17: restore desert overview artwork and Alexandria map markers

The map importer kept v1.150 overview textures whenever their filenames
already existed, even where the imported outdoor regions need later artwork.
It also selected only Alexandria landmarks/gates and four Delta labels,
excluding the authored shop icons, NPC dots and remaining field labels.

The variant now replaces overview textures covering imported regions with
the VSRO source bytes, unlinking variant hard links before writing. All 35
selected tiles match the source archive; six existing tiles changed artwork.
Native input hashes stayed unchanged. Complete source-authored city markers
and overview labels located in imported regions are projected into the
native 20-column table. Blank lookup-only NPC records and zero-height glyphs
are excluded; their positions are not inferred. The map table projection
and tile selection now live in `scripts/alexandria_map_tables.py`, keeping
the variant assembly below the source-size limit.

Published to the running browser's variant tree through text publication,
`pnpm assets repack`, then `pnpm assets refresh world-map`. The first focused
refresh reported `xy_guild.png has no asset-pack owner`; the full repack
registered the new source glyphs under the existing native-ui owner, and
the subsequent focused refresh passed with zero remaining asset deltas.
The repack rebuilt two packs and reused 249; web manifest `b96e9301f815`.
No server restart or player-state changes were needed.

Delivered pack bytes passed the production icon decoder for 48 Alexandria
icons, including Chunmoo's NPC dot and weapon-shop markers; every icon's
image is packed. The imported city/field labels passed the production label
decoder, including Blacksmith, Storage, Chaos Maze, Red Ground and all
selected desert labels. Packed overview PNGs 66x85 and 66x89 match the new
published conversion. HTTP port 5380 serves the updated pack catalogue.
Backups, native hash audit and verification scripts are in ignored
`.state/alexandria-map/`.

Verification: 67 Alexandria Python tests passed; scripts, formatting,
encoding, generated-root, source-size and diff checks passed. The initial
source pipeline stopped at the size limit before the map module extraction;
the standalone size gate passed afterward. The broader native map/actions
suite passed six tests and failed three with `Missing map label
SN_ZONE_15200`. This failure was incorrectly classified as existing debt:
although its text key was already absent, its referencing map row was newly
imported. It blocked client startup and was repaired in the follow-up below.
This is not a complete visual playthrough; hard reload before checking the M
map in game.

### 2026-10-17: repair startup after the map publication

The overview import also selected source rows 15200 and 15201, Pharaoh's
Tomb entrance/exit labels whose symbols exist in neither the native name
table nor the VSRO textdata_object catalogue. The production decoder correctly
rejected them. The import now admits new symbolic labels only when their
text key resolves in the native catalogue or the imported source text rows.
The regression includes an otherwise valid positioned field label with an
unresolvable key and proves it is excluded. No runtime fallback was added.

Rebuilt text resources and repacked: one pack rebuilt, 250 reused, web
manifest `f956a8be8209`. The complete delivered map table now passes the
production label decoder, all 48 Alexandria icons remain packed, and HTTP
port 5380 returns neither incomplete label. All nine native map/actions
tests and 67 Alexandria Python tests passed. Scripts, size, formatting,
encoding and diff checks passed. Reload the browser to replace the failed
session's older map table.

### 2026-10-17: repair Apis target HP gauge

Owner confirmed Apis appears, then reported an empty target HP bar. Reviewed
DeepSeek commits `2e8d27b9` and `ce7a2c8a`: initializing current HP and logging
the inputs did not address `monsterMaximumHp` returning undefined for grade
8. The target renderer consequently omitted the gauge even with valid HP.

HP calculation now uses the reference base for grades without an authored
grade icon/title, matching the server's existing `EffectiveMaxHP` default
arm. Icon/title handling remains independent. The idle-HP initialization
now lives beside the effective maximum in `targetStatus`, so champions,
giants and party monsters also start full rather than dividing base HP by
a promoted maximum. Real current HP, including zero, always takes priority.
Removed the temporary target debug state and logging from the UI owner.

Eight target/compact-target tests passed, including Apis at full, half and
zero HP, idle promoted monsters, and unknown maximum HP. Formatting,
encoding and diff checks passed; TypeScript typecheck passed. HTTP port
5380 serves the updated target module. No asset rebuild or service restart
is required for this source-only change.

The full client check completed with four failing gates: runtime tests,
ownership, item-sounds and audio audit. It ran 577 test files, with 11 files
failing on expanded catalogue counts and an effect decoder issue
(`Unsupported particle rotation assignment` for
`hiteffect/hit_4_cold_hit_e.efp`). Representative census failures include
`29436 !== 27835` skills and `105 !== 85` portal links. Ownership reports
`world-map.ts: shared module state must be an immutable scalar constant`;
item sounds reports `ITEM sound catalog drift`, and the audio audit reports
`item-catalog-drift`. Those owners/assets were not changed by this HP fix.
Typecheck, test types, execution, capabilities, delivery, layout and retail
panels passed. The full client gate is not green; no exhaustive live fight
was run for the gauge repair.

The owner confirmed the HP gauge now renders, then reported the missing
Unique title/icon. Added a port-only grade-8 presentation entry using the
existing Unique text and icon; Apis's source grade and HP stay unchanged.
The source hive 584 describes Apis as a unique, while the imported record
retains grade 8. All eight target/compact-target tests passed with explicit
grade-8 icon/title assertions. The packed English `Unique` label and icon
were verified present, and port 5380 serves the updated target module.

**Root cause and review (2026-10-21):** the server already sends the monster's
current HP when it is selected (0xB45A, `EncodeMonsterObjectSelectResult`), and
the client already stores it (`hp-seed`). The bar was missing only because
grade 8 was not in the client's grade table, which left the maximum HP unknown.
So the only client change is the grade-8 entry (unique title, icon, x1 HP). No
"assume full HP" fallback and no Experimental row: the earlier fallback was
removed because the native client reads HP from the server (it has no such
default), and the entry is unreachable on a native build (native monster
references are grades 0 and 3; nests add 1, 4, 5 and 6; none is 8).

## Pharaoh Tomb 1-3 (SRO_PHARAOH_TOMB), 2026-10-20

Port-only, not native; off by default; needs `SRO_CAP110_MONSTERS` (its equipment
drops come from the cap-110 loot catalogue) and refuses to start without it.

Kings Valley has one dimensional gate, `GATE_TOMB_GATE_IN`, linked both ways and
free to three tomb exits, `GATE_TOMB_GATE_OUT_1..3` (Beginner, Intermediate,
Advanced). The later server's database holds the tombs as game worlds 57, 58
and 59 (30 nests each, 17 monster types each, levels 100-110, with the
Sphinx, Sekhmet, Osiris, Horus and Nephthys bosses). Unlike the Donwhang cave or
the Qin-Shi floors they are ordinary outdoor regions (all 90 nests and the three
gate arrivals have no dungeon-region sign bit), so they reuse the field pipeline.

Port decisions, each inferred rather than evidenced:

- Each tomb is its own permanent world (#1750, owner 2026-10-27). The v1.150
  world table has no 57-59 (the server stops with `names unknown world 57`), so
  the port registers the three shipped ids port-only (`INS_PHARAOH_TOMB_1..3`)
  behind the same switch and the tomb population joins its own world instead of
  `INS_DEFAULT`. The last `teleportdata` column names the destination world;
  the importer keeps the three exits' authored worlds 57, 58 and 59, and the
  portal catalogue resolves them once the tomb worlds are registered (the
  bootstrap registers before the catalogue loads). A variant built with the
  tomb gates therefore only boots with `SRO_PHARAOH_TOMB` on; off, the server
  refuses the catalogue at `names unknown world 57` instead of silently
  flattening three tombs into the field.
- Bosses are single-spawn nests with their authored respawn delay. The database
  carries no trigger data, so the original's instance triggers are not ported.
- The unplaced `GATE_TOMBOSIRIS_*` rows (all-zero positions) are not carried.

Implementation: `scripts/cap110_monsters.py` admits worlds 57-59 and grades 6
(elite) and 8 (unique) when the flag is on, sharing the field overlay's character
and skill shards and marker so one allocator keeps every identity stable; nests
go to `cap110-tomb.txt`. Two basic skills the database projection omits
(`SKILL_PUNCH_01`, `SKILL_CH_SWORD_SMASH_A_01`) join native skills by codename
through the later client's own rows; no database export was needed. Tomb nest
regions seed the terrain import (+120 regions, 334 -> 454); the tile remap is
unchanged. `scripts/build_alexandria_variant.py` adds the four gates. The server
side is `monster/pharaoh_tomb.go` (`AppendPharaohTomb`), sharing the nest
validation in `cap110.go`, with the flag in `shardsettings.go`, the Nomad job,
`devdeps.go`, the catalogue tool and the public rules list.

New check: `scripts/analysis/check_identity_parity.py` joins the client's and the
server's characterdata, skilldata and itemdata by codename and fails on any
differing ID or one-sided codename. Run it after every import and server export.
The Weneg-drawn-as-serpent bug was exactly this drift: the Oct 19 variant rebuild
allocated the field monsters from scratch while the server data was the Oct 17
export. Identity retention depends on `extracted/.opensro-cap110-monsters.json`
surviving a rebuild.

Verified on this machine: 10 monster and 30 variant Python tests, the monster,
loot, shardsettings, catalogue, nomad and gameworld Go packages, a throwaway
real-data load through the production Go loader (90 nests, 39 regions, grades
25/50/15 by nest), the identity check (0 differences in all three tables), the
delivered packs (51 monster IDs match the server and every model is delivered;
the gate links; navigation for all 120 added regions; 14,596 object material
groups admitted with 0 failures), and a healthy Agent and GameWorld with 10,389
nest rows (10,299 + 90). Formatting and encoding gates pass.

Owner report, 2026-10-20: all portals work in game (the two outdoor gate pairs
and the tomb gates). This covers travel only, not what is inside the tombs.

Not verified: any in-game spawn, combat, boss respawn or drop in
the tombs; the visual result of five new models baked to float16 (max error
0.016-0.031, warned above 0.01, float32 kept above 0.05); two new version-13
effects (`skill_khepri_ac3_powder_shot`, `skill_horus_ac4_ready`) that the
parser accepts with its "verify layout" warning (Apis's version-13 head flame
rendered correctly in game); level-100 tomb monsters and non-equipment drops
(the equipment catalogue covers levels 101-110); `pnpm check` and the full
client check.

## Monster rewards and tomb gear rate, 2026-10-21

The owner reported that tomb uniques dropped only potions, pills, gold and one
piece of gear. Cause: the loot catalogue treats the native v1.150 client as its
content boundary, so every reward rule of a monster the native client lacks was
recorded as excluded (`absent-client-monster` in `scripts/data/loot/*-rewards-source.json`).
The Alexandria field monsters, Apis and the 51 tomb monsters therefore had no
assigned rewards at all, and the website catalogue showed empty drop lists for
them.

What the database holds for them (`_RefMonster_AssignedItemDrop`,
`_RefMonster_AssignedItemRndDrop`, `_RefDropItemGroup`, exported read-only by
`.state/vsro-database/export_assigned_rewards.py`): 162 fixed rules, which are
monster-specific alchemy materials (`ITEM_ETC_ARCHEMY_MATERIAL_SD_<MONSTER>1/2`,
3.8% on ordinary monsters, 41.6% and 20.8% on the tomb bosses and Apis), and 30
random rules over two single-item groups, the SD tokens (`ITEM_ETC_SD_TOKEN_01`
and `_03`, 12% for most of the 15 monsters that have them, 20% for Osiris 3).
None are gear. The database's gear odds are only the level class tables
(about 0.04% normal and 0.3-0.43% rare per roll at levels 105-110, all
degree 11), which is why tomb gear is rare by design.

Implementation: `scripts/cap110_rewards.py` (run by the variant build after the
monsters) imports the 96 items these rules name that the native table lacks (94
materials, 2 tokens) as shard `itemdata_110001.txt` with retained identities
from 42770, their names, and the few asset files they need (both token icons and
`drop_token.bsr`), and writes `cap110-rewards.txt`. Group ids are offset by
1,000,000 so they never meet a native group. The server reads it in
`loot.InitCap110` (`loot/cap110_rewards.go`), joining onto the pristine native
catalogue; it refuses a rule that names a native monster or group. Rewards ride
`SRO_CAP110_MONSTERS`; the file is required when that flag is on.

Port-only gear boost: `SRO_PHARAOH_TOMB_GEAR_RATE` (1 is native, 2 to 20; unset
is 1) multiplies the equipment rolls of tomb elites (grade 6) and uniques
(grade 8): the unique prepass equipment count and the ordinary equipment passes.
The rate is stamped on the tomb monsters' references when the tomb loads
(`MonsterRef.PharaohGearRate`), so no other monster is affected. The website's
drop lists describe the native rule. The local stack runs it at 5.

Important interaction: `SRO_ALCHEMY_MATERIAL_DROPS_OFF` (owner, 2026-10-14) is
on, and `prepareSelectedDrop` discards every type 3/3/11/4 item, assigned drops
included. While it is on, the 94 imported materials never drop; only the tokens
do. Turning the flag off would also bring back the old grey-bag junk everywhere.

Verified: 6 + 10 + 4 Python tests (rewards, monsters, identity), the loot, monster,
action, enterworld, shardsettings, catalogue, nomad and gameworld Go packages
(loot tests include a repeatable `InitCap110` and refusals), a real-data load
and a 200,000-kill simulation of `AssignedDrops` that reproduced the authored
rates (Sphinx materials 41.4% and 20.9%, tokens 12%; Osiris 3 tokens 0.4 per
kill; ordinary monsters 3.8%), the delivered packs (all 96 items have a name,
icon and drop model), identity parity (0 differences in characters, skills and
items), and the source gates: scripts, scripts-tests, typecheck, formatting,
encoding, size, generated-root, loot-catalog, compiled-quests, shared-fixtures,
console, release, client-preparation and pipeline-contracts. The server race
step cannot run here (`go: -race requires cgo`, no C compiler).

Not verified: any in-game drop. Whether `ITEM_ETC_SD_TOKEN_*` (type 3/3/5/1, no
native item of that kind) picks up, stacks and shows correctly; whether anything
in the variant accepts tokens; and the effect of the gear rate on real kills.
The token descriptions are absent in both clients and stay absent.

### 2026-10-21: SD token pickup ended the session

Picking up `ITEM_ETC_SD_TOKEN_01` (TID 3/3/5/1) failed with "A game error
interrupted your session" (Agent log: `Truncated inventory item`, opcode
0xB06D). TID3 5 is the gold-heap group: native holds only `ITEM_ETC_GOLD_01..03`
(3/3/5/0) there, and `CSOItem` 78C830 reads a u32 amount for that group while
every other ETC group has a u16 stack count. The server wrote the u16 body for
all ETC rows. `ItemBody` now writes and reads the u32 amount for the gold
group; other groups are unchanged (`TestGoldGroupItemBodyCarriesU32Quantity`,
`TestOtherEtcGroupsKeepU16Quantity`). Gold never travels as an inventory item
natively, so native flows do not reach this branch.

## Database export pin, 2026-10-21

Every imported field nest, tactic, monster skill and drop row comes from one
database export, made from this backup (author, PR #1285, re-hashed
2026-10-21):

- File: `SRO_VT_SHARD.bak`, 74,291,712 bytes
- Backup set: `SRO_VT_SHARD-Full Database Backup`, database `SRO_VT_SHARD`,
  server `VSRO_TESTIN` (SQL Server 2022 backup of a VSRO test shard),
  taken 2025-04-08 18:18:27
- SHA-256: `b4d85a043ebce36c8704768dce0dfa63ff9418c8489241d8441b7902e28fa37a`

It is the same VSRO shard the city NPC headings name
(`npc_headings_alexandria.go`). `cap110_monsters.py` refuses an export whose
`summary.json` records any other `backup_sha256`
(`require_pinned_export`); the import marker keeps the per-file hashes of the
export it used (`snapshotHashes`).

