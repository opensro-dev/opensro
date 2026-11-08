# Move the So-ok event into the official Survival Arena

Requested 2026-11-07. Keep the existing PvE waves, eggs, kill-based gold,
level-90 cap, daily entries and safe character return. Change the venue.

## Verified reference

The official Turkish Silkroad operator describes Survival Arena here:
https://silkroad.gamegami.com/guide.php?catid=83

Its in-game screenshot shows the dedicated **Kolezyum / Colosseum**:
https://sroimg.gamegami.com/gg_65b9e6008974ac8f88.png

The official event is PvP (solo or party survival). The owner's requested
adaptation remains PvE; do not replace our rules with the official PvP rules.
Screenshot coordinates are X19479 / Y1444. These identify the reference
view, not a verified teleport destination or a proven region ID.

## Asset audit

Owned v1.150 source:
`E:/CodeProjects/SRO/AssetImports/OpenSRO-v150/extracted`.

The installed world-region catalogue has no entry for 0x63ec, the candidate
outdoor sector inferred from those displayed coordinates. The extracted
map has no `Map_extracted/99/236.m`; its largest installed sector X is 190.
Do not teleport into that inferred sector: geometry and navigation are absent.

The extraction contains `euro_esteuro_colosseum01.bsr` and associated
Constantinople meshes/materials. This is the older world prop, not evidence
that the later dedicated Survival Arena map is present. The former test
amphitheater was region 27493; the active event still uses the validated
Jangan courtyard, region 17989. Neither is being represented as the
official Survival Arena.

The owner supplied `E:/Igre/Silkroad`. Its enabled
`STRUCTURE_POS_COLOSSEUM_SAFEZONE_*` teleport rows prove region **25580
(0x63ec, sector 236/99)**, world 116 in the newer game. The eight surrounding
starts are centred on local X960/Z960, Y0. The port keeps its existing
private definition 30/31 leases instead of importing the newer game's rules.

The guarded PK2 reader verified and indexed Map, Media, Data and Particles
read-only. Their SHA-256 pins are in `scripts/survival_archive_pins.json`.
No retail executable was run and no PK2 was changed.

`scripts/import_survival_colosseum.py` creates a separate game root, using
the Alexandria importer's safe link-and-replace operations. It imports:

- One complete terrain/object sector and its native navigation bitmap.
- All 180 placements, nine BSR models (object IDs 3214–3222), and their
  BMS/BMT/DDJ dependency closure, parsed with production world readers.
- The referenced particle programs, meshes and textures, parsed with the
  existing EasyFX reader, plus minimap `236x99.ddj`.
- Only the required environment 55 entry; native lighting entries and
  region bindings remain byte-for-byte intact.

Conflicting native model/texture paths are rejected. Used terrain tile IDs
use the existing remapper. The resumable manifest records source identity,
file hashes and completion. Failed or unrelated inputs cannot silently
replace a staged import. Generated assets and owned archive contents remain
untracked.

## Reproduce the conversion

Use the documented Python build environment, including FFmpeg on PATH.
Set `SRO_SURVIVAL_COLOSSEUM=on`, then run from the checkout:

```powershell
python scripts/import_survival_colosseum.py --client-root <owned-newer-client> --native-root <owned-v150-root> --variant-root <separate-output-root>
$env:SRO_GAME_ROOT = '<separate-output-root>'
$env:SRO_GENERATED_ROOT = "$env:SRO_GAME_ROOT/.generated"
pnpm assets build world-outdoor -- --region=0x63ec --force --jobs=1
pnpm assets refresh outdoor
```

The local test variant starts with copies of the existing generated client
and image cache; these are ordinary copies, never mutable hard links.
A clean installation must first build the native baseline using the asset
pipeline. Rebuild and publish the shared `environment.json` through
`buildSharedWorldEnvironmentCatalog` and `refreshOwnedPackFiles`, preserving
the existing pack owner. Publish the new minimap tile through the same
owner-preserving publisher before `pnpm assets publish minimap-coverage`.
Do not hand-edit the pack index. Build a matching server projection with
`pnpm task build server-game-data`, setting `SRO_SERVER_GAME_DATA_ROOT` to
a separate absolute directory; deploy its verified manifest digest with it.

The focused outdoor refresh does not own shared terrain images outside
`/assets/world/outdoor`. Publish `/assets/images/Map_extracted/tile2d/*`
through `listPublicAssetFiles` (`build/assetPacks.mjs`) and
`refreshOwnedPackFiles`, with `imagePackGroup` from `assetPackOwnership.mjs`
as the default owner. Otherwise a region can name a newly converted
`.texture` that the installed pack index does not contain. The initial live
test caught this for `pha_rock_01.texture`; the local variant is corrected.

## Current deployment (2026-11-07)

- Game/converted root: `.state/colosseum-variant`, with isolated `.generated`.
  Vite must run with both `SRO_GAME_ROOT` and `SRO_GENERATED_ROOT` pointing
  there; the ordinary main-checkout asset tree intentionally lacks this map.
- Server data: `apps/server/.state/sook-live/progression-v2-game-data`.
- Server manifest: `sha256:7c48d8f38aafd791268249553d569a932328c18e0d14d18cc5177d279d9e3f06`.
- Executable: `apps/server/.state/sook-live/gameworld-colosseum.exe`.
  Superseded after the recovery fix by `gameworld.exe`, hash below.
- Authority remains only the copied `sook-live/authority-copy`; backup
  `sook-live/pre-colosseum-20261107-092024/state.db` precedes activation.
- Client and server destination/priority/egg guards now use 25580.
- Two importer unit tests, 17 ground-item tests, the So-ok action tests and
  client quick verification passed. Outdoor publication built one region,
  180 placements and 14 navigation blockers; all 18,249 baseline/new image
  conversions succeeded after adding the installed FFmpeg to PATH.
- GameWorld readiness and ordinary LabWizard world entry were verified.
  The live results below distinguish engine checks from synthetic tests.

## Implementation checklist

1. **Done:** inventory the newer map/terrain/object placements, BSR/BMS/BMT/DDJ
   dependencies, map labels and walk/navigation data. Resolve the actual
   Colosseum region and center from its tables rather than screenshot math.
2. **Done:** reuse the existing asset pipeline to publish that selected region and
   its dependencies; validate source format compatibility and server/client
   navigation agreement. Keep existing archives and shared assets intact.
3. **Done:** change So-ok's destination in `action/sookarena.go`. Keep the private
   definition 30/31 leases, ordinary collision, return anchor and encounter
   validation. Do not use GM permission or a flat-floor substitute to admit
   an unbuilt map.
4. **Done:** update both client arena-region guards: creature/effect admission in
   `runtime/characters/characters.ts` and the power-egg predicate in
   `foundation/gameplay/ground-item.ts`. Update their fixtures/tests so
   the new venue retains visible monsters, crow eggs and native behavior
   outside the private event. Timer HUD has no map-specific guard.
5. **Partially verified:** back up the copied authority, deploy, and live-test entry, walking and
   walls, monster visibility/aggro/spawning, egg pickup/timed buffs, death
   payout and return, disconnect/re-entry and lease isolation. Preserve all
   characters and equipment. Mark the move complete only after this passes.

Record actual live results below before calling the venue move complete.

The later timed-level rework extends the importer with
`characterdata_sook_survival.txt`: nine owned Rocky field variants for
levels 89–97. Run the normal NPC model bake after importing, then refresh
`entity-bsr -- --rebuilt-npc` and build a matching server projection. All
nine aliases reuse the same verified native Rocky resource; there are no
new or substituted skeletons. The manifest records their identities under
`arenaReferences`. See the main event document for the balance rule.

### Live findings and recovery correction

The Colosseum was entered via So-ok's real Event dialog as non-GM LabWizard.
After fixing the shared terrain publication, the live browser rendered the
authored walls, seating, gates, floor and flame particle programs, with the
new minimap and navigation ready, 67 visible groups and no pending textures.
Screenshot: ignored `sook-live/colosseum-live.png`.

Reloading during the initial failed asset load exposed a pre-existing arena
recovery defect: `recoverSookArena` restored the persisted town pose but left
the previous socket's arena pose in `rt.Worlds`. The next admission could
therefore show an empty arena in the default world. Added
`TestSookRecoveryDiscardsArenaPoseCache`: it failed with town region 23644
stored versus arena 25580 live, then passed after successful recovery also
forgot that runtime pose. All So-ok integration tests passed again.

The already-orphaned test LabWizard was repaired using only its town pose
from the pre-Colosseum backup. Its 30 inventory rows and 96 skills were
preserved; only the test daily entries were reset to allow a fresh run.
The correction is deployed as `gameworld.exe`/`gameworld-v2.exe`, built from
`gameworld-colosseum-v2.exe`; the local launcher now pins the matching
Colosseum projection instead of silently returning to native-only data.

### Fresh event acceptance

The corrected build (`C1FDECB244C29E50396E9C0F6C96499F828361CD0A4C1E77BE6ED0812FACB4AB`)
then passed a fresh non-GM So-ok entry into packed world 65566 (definition
30), region 25580. All three initial Niya General/Wing Tribe Guardian
actors were visible, ready and drawn, then approached and damaged LabWizard.
One was killed with the character's Blizzard; actual loot appeared. On
death the notice reported one kill, exactly 10,000 gold was added
(38,773,038 -> 38,783,038), and LabWizard returned alive to So-ok in region
26959. A second entry verified accepted movement from 960/960 to
963.943/985.640, with no texture/model failure or forced arena teleport.
Its death also returned safely. Screenshots `colosseum-wave.png` and
`colosseum-return.png` are ignored local evidence, not committed assets.

The client quick check passed (typecheck, ownership, capabilities, execution
flow). All 109 Alexandria helper/import tests passed, as did the two new
Colosseum importer tests, 17 ground-item tests and the So-ok action suite.
The emitted region and shared object index have no missing public asset
references in the installed pack index.

The third entry tested a reload during an active run. After the old socket's
normal 30-second grace expired, selecting LabWizard again entered town region
26959 in default world 65537, with no stale arena pose. Immediate reload can
still briefly report EnterWorld rejected 2 while that previous socket owns
membership; retry after its grace. This transport lifecycle was not changed.

After these checks, only LabWizard's copied daily entry counter was reset
3 -> 0 again for the owner. The helper confirmed level 90, 30 inventory rows
and 96 skill rows preserved. The server restarted with the corrected build
and matched projection. Source PK2s and native extraction were never edited.

Not yet repeated in this venue: a real-time 30-minute completion, walking
the entire outer wall, two-player lease isolation and every egg powerup.
Existing focused tests cover their unchanged mechanics; do not call these
unperformed live checks passed.
