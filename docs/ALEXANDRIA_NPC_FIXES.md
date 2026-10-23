# Alexandria NPC corrections

The optional Alexandria data overlay restores the reported missing services
and names. Native extraction files are unchanged, and the variant remains
behind `SRO_ALEXANDRIA`.

- Chunmoo's `sd_t_area_smith_part1.bms` has seven referenced vertices with
  skin records `ff 0000 ff 0000`. The exporter formerly emitted all-zero
  weights, which the character loader correctly rejected. It now represents
  native unbound vertices with an explicit independent identity joint under
  a common model-space root. Existing bone transforms remain unchanged.
- Viviana's position is absent from the supplied client's `npcpos.txt`. A
  later shard export kept outside the repository supplies her nest placement
  and model reference; the optional importer joins by codename, retaining
  the client's reference ID, and uses that model and position. This replaces
  the earlier inferred placement beside Chunmoo and the male
  `sd_t_area_armor.bsr` model.
- Alexandria placements now resolve facing through exact position keys. The
  later export supplies most headings; the four event NPCs absent from it
  retain the earlier table's headings. Asagon's heading now resolves,
  rather than the previous unresolved lookup's zero. Valid authored zero
  headings are retained. NPCs keep their authored facing; this does not
  make them track players.
- Tausert's German translation contains an unquoted physical newline.
  Joining that short record with its continuation recovers the authored
  English name, `Thief Union President Tausert`.
- Kapra and his main-town counterpart regain the complete shop mapping,
  tab and available package dependencies. Native merchandise and prices win;
  packages with missing items are deferred whole. Conditions and rewards
  remain intact for the production commerce loader's admission rules.
- The existing M window receives native kind-1 labels projected from the
  imported NPC positions through the authored Alexandria page bounds.
  Names come from the NPC translation symbols, including restored Viviana.

## Native mesh evidence

NPC placement/model sources are local evidence kept outside the product
checkout. Spawn joins follow the shard's nest -> tactics -> object chain,
filtered to the main game world; signed headings preserve their unsigned
16-bit wire values. Existing native placement headings take precedence, so
the default world is unchanged with the Alexandria overlay off.

| Claim | Evidence | Checked | Status |
| --- | --- | --- | --- |
| Bone byte `ff` skips the influence | VA A4EF0C..A4EF10 in the actual PE bytes | Capstone 5.0.9, pefile 2024.8.26 in an isolated Python environment | Observed |
| Two missing influences retain packed palette indices zero | A4EE00 initialization, A4F100 weight test and A4F170..A4F17B index packing | Bounded disassembly of A4ECB0..A4F276 | Observed |
| Reserved palette zero preserves model-space geometry | Zero-index path above, represented by a separate glTF identity joint | Synthetic mixed-weight regression; native draw capture remains a next verification step | Explicit inference |

Binary identity, bounded disassembly and the inference are saved outside the
product checkout in the owner's local research tree, keyed by the full hash
and RVA `0064ecb0`. No research dependency is imported by the product.

## Verification

The production NPC roster and commerce loaders find all four reported NPCs.
Kapra has six tabs and 45 admitted offers; Viviana has twelve tabs and 360
offers from the existing native armor store. Tausert resolves to the authored
English name. The optional shop overlay adds 50 goods rows and defers eight
packages whose item definitions are absent. Alexandria has 32 NPC name labels.

The 138-test Python suite passed. A synthetic GLB test checks that unbound
vertices use model-space identity, retain positive weights and share a valid
common root with the original animated skeleton. All eleven focused Node
mesh, animation-timeline and locomotion tests passed. Script typechecking,
formatting, encoding, size and generated-root checks passed.

The resource build, final NPC/VAT rebuild and standard asset repack passed.
Published Chunmoo geometry has 1,674 vertices with positive weight sums,
including the seven previously unbound vertices, and a valid shared skin
root. The published map decoder resolves all four reported NPC names.
Server game-data export completed. An in-game visual and purchase spot-check
remains manual.

## Authored placements update, 2026-10-17

The updated production loader matches all 39 city placements against their
authored positions, headings and model references. Viviana's published GLB
uses `sd_guild_agent3`, with 850 positive-weight vertices, three available
textures and 162 VAT frames. Inspection of the source face/body texture
confirms a female character. Her M-map label now projects to `(634, 578)` on
page 12. Existing cap-110 equipment translations survive the NPC restage.

The 139-test Python suite, focused NPC heading/roster tests against the new
bundle, full simulation suite against the native bundle, simulation `go vet`,
script typecheck, formatting, encoding, source-size, generated-root and diff
checks passed. The complete asset rebuild and server-data export passed.

The earlier full simulation run against the Alexandria variant exposed an
existing Magic Pop test assumption: `machine id 200197 outside its band`.
That test assumes every machine is synthesized in the reserved ID band;
Alexandria also imports an authored `npcpos` machine in the regular NPC band.
The product identity allocation and that separate test are unchanged here.
The same run's stale Viviana heading failure was resolved by exporting the
new bundle; the updated published-roster heading test passes.

On Windows, the first export could not rename the running bundle (`EBUSY`).
Stopping the local Agent/GameWorld services released it and the export then
succeeded. Deployment uses the same game root and preserves operator flags.
In-game appearance and shop interactions still require the owner's spot-check.

## Sharon restoration, 2026-10-17

Armor Trader Sharon (`NPC_SD_M_AREA_ARMOR`) was also absent from the client's
position table. A later shard export supplies her placement, heading and
`npc\sd\sd_m_area_warehouse.bsr` model. The importer joins by codename and
retains the client reference ID. The source texture shows a female character.
The restore obeys imported-region scope and preserves explicit client poses.

Her existing `GROUP_STORE_SD_M_ARMOR -> STORE_KT_ARMOR` mapping now reaches
the production roster, with twelve tabs and 360 admitted armor offers.
The optional city roster now has 40 placements and 33 projected map labels.
This coverage concerns the restored client NPC set; two later commerce
NPCs (`NPC_SD_COMMERCE1/2`) remain outside this change and are not imported.

The 140-test Python suite, native simulation suite, updated-bundle NPC
heading/roster tests, simulation vet, script typecheck, formatting, encoding,
size, generated-root and diff checks passed. Focused NPC/VAT, presentation,
country and text builders passed, followed by the standard complete pack
rebuild. Verification reads the packed model, animation, name and map label;
the production commerce loader admits Sharon's offers from the new bundle.
No in-game visual or purchase test was automated.
