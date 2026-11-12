# Optional GOD temple worlds: Togui Village, Flame Mountain, Shipwreck

Port-only, not native. The default stays v1.150, and the switch below is off by default. The three worlds are
the port's temple bands (the 22 instanced worlds 63-84, #1800 step 3): this import builds their kinds, nests
and regions from the two later sources the #3271 inventory names - the ISRO-R retail shard backup's authored
nests and the v1.696 client's character rows and map archives. Native rows, IDs and names never change.

## Switches

No new switch: the worlds already ride `SRO_CAP110_TEMPLE`, and this import is what the build side of that
switch now runs. The pairing rule is unchanged - the importer and the server loader both refuse
`SRO_CAP110_TEMPLE` without `SRO_CAP110_MONSTERS`.

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_CAP110_TEMPLE` | Build and server | The GOD temple worlds: their kinds, `cap110-temple.txt`, and their regions' terrain. Needs `SRO_CAP110_MONSTERS` |
| `SRO_CAP110_MONSTERS` | Build and server | The cap-110 monster template the temple kinds join |

The build reads the later client from `SRO_V1696_ROOT` (checked against `scripts/v1696_archive_pins.json`) and
the population from the export folder `SRO_GOD_DATABASE` names. The export
(`research/investigations/later-sources-3247`, `extract_god_population.py`) records the shard backup it was
extracted from; an export made from another backup is refused before anything is staged.

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/god_import.py` after the Jupiter merge. Behind the temple
switch: the kinds the population names converted into `characterdata_190000.txt` / `skilldata_190000.txt`, the
population written as `cap110-temple.txt` (the file the server's temple loader already reads), and the worlds'
regions merged from the same client - terrain remapped onto the merged tile catalog, navmesh and availability
bits, minimap tiles, and every Data file the native tree lacks.

| Part | Result (real inputs) |
| --- | --- |
| Regions | 497 distinct region ids across the 22 worlds (the tier copies share geography): Togui's 48 sectors, the Shipwreck's 448, Flame Mountain's single region |
| Interiors | 1 dungeon region (bit 15 set, Flame Mountain): its `ainavdata` rides the Data pass |
| Monsters | 272 kinds: 72 `MOB_GOD_TOGUI_` (levels 36-68), 104 `MOB_GOD_FLAME_` (71-89), 96 `MOB_GOD_GHOST_` (91-109) |
| Placements | 1,616 nests: 696 Togui, 552 Flame Mountain, 368 Shipwreck; 512 carry the database's second spawn kind |
| Worlds | The 22 `INS_GOD_*` bands as already registered (instanced, 50 layers, 4 or 8 residents a layer); this import changes no registration |

## Decisions recorded

These are inferences and port choices, not native evidence:

- **Provenance.** `cap110-temple.txt` was projected from the VSRO test-shard export inside `cap110_monsters.py`
  (its temple family, now retired). `god_import.py` writes the same file from the pinned retail export and the
  v1.696 kinds instead. The two sources agree on the population's shape - 1,616 nests, 512 of the second spawn
  kind - so the swap changes where the data comes from, not what spawns.
- **Admission windows.** Each world admits its own family at its authored band (Togui 36-68, Flame Mountain
  71-89, Shipwreck 91-109) and grades 0, 1, 3, 6, 8, inside the temple loader's wider 35-109 window
  (`monster/temple.go`). Nothing raises the player level cap.
- **NPC rows stay out.** The export also places the worlds' 132 NPC attendants (instance and field NPCs, all
  level 0). They are roster content for the NPC slice, not monsters, and never reach the population or the shard.
- **No authored loot.** The import writes no reward table; the kinds drop on the native posture.
- **Identity band.** The import's character and skill shards own the 190000 band (120000 is the cap-110
  family's, 130000-150000 the pet import's, 160000 the Sky Temple's, 170000 Jupiter's, 180000 the second
  hunting grounds'). Identities are marker-persistent across rebuilds.
- **Region names.** The later client codes all 497 regions `xxx` and its zone-name table carries no rows for
  them; the only authored names are the Korean regioncode names. The merge adds no code or zone rows rather
  than inventing any.
- **Entry and driving rules are the admission slice's** (#1800 step 3 slice 4: entry, the per-band level
  refusal, party layering, and what the second spawn kind does at spawn time - until then it rides the row as
  a preserved fact, as `monster/temple.go` records). Flame Mountain's interior geometry is the later client's
  DOF tree, published into the client's dungeon catalogue by its own slice (the Jupiter interiors' #3295
  posture). Until those land, the bands stand registered with their populations and terrain, and no way in.

The server side is unchanged by this import: `monster.AppendTemple` reads `cap110-temple.txt` behind the same
switch, `world/temple` owns the band identity table, and `instance.TempleDefinitions` registers the worlds.
