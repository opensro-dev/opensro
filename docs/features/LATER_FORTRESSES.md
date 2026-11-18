# Optional Bandit and Hotan fortresses

Port-only, not native. The default stays v1.150, and both switches below are off by default (#3886). v1.150
ships both fortresses' siege worlds (`INS_FORT_BJ`, `INS_FORT_HT`), their emblems and every fortress staff
character, but no siegefortress row, event zone, structure or staff standing for either. The imports add
the v1.188 client's, on top of the native tables, joined by codename and region id. The flag-off build is
byte-identical to the build without these imports.

## Switches

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_FORT_BIJEOKDAN` | Build and server | The Bandit fortress near Jangan: 16 regions, 43 event zones, its fort stone and small gate, its siegefortress row (id 6) and its staff |
| `SRO_FORT_HOTAN` | Build and server | The Hotan fortress: 40 regions, 52 event zones, its fort stone and big gate, its siegefortress row (id 3) and its staff |

The build reads the v1.188 client from `SRO_VSRO_ROOT` (checked against `vsro_archive_pins.json`).

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/bandit_fortress_import.py` and then
`scripts/hotan_fortress_import.py` right after the Roc import. Both are specs over one engine,
`scripts/later_fortress_import.py`, which Constantinople (`scripts/constantinople_import.py`, from v1.696)
now uses too. Each import stages:

- the fortress regions' terrain, object catalogue and navmesh;
- the region code and zone name rows;
- the model closure of the placed objects and the new structures;
- the event-zone rows and the siegefortress row;
- the structure characters the shipped tables lack, in the import's own character shard;
- the fortress staff's v1.188 `npcpos` standings, by the shipped character ids;
- the zones' placements as `Data_extracted/navmesh/objectstring.<fortress>.portonly.ifo`.

The server bundle's structure-zones projection merges every `objectstring.*portonly.ifo` beside the native
`objectstring.ifo`. A zone two files place differently stops the build.

| Part | Bandit (real inputs) | Hotan (real inputs) |
| --- | --- | --- |
| Regions | 16 (`RN_FORT_BIJEOKDAN`, sectors x 77-80, z 67-70; they already exist natively and are replaced) | 40 (`RN_FORT_HOTAN`) |
| Zones | 43 | 52 |
| Characters | `STRUCTURE_FORT_STONE_BJ_01`, `STRUCTURE_SMALL_GATE_BJ_01` in `characterdata_290000.txt` | `STRUCTURE_BIG_GATE_HT_01`, `STRUCTURE_FORT_STONE_HT_01` in `characterdata_300000.txt` |
| Staff | `NPC_CH_FORTRESS_MANAGER2`, `_BATTLEAIDE2`, `_SMITH2` | `NPC_KT_FORTRESS_OFFICIAL` (in Hotan town), `_MANAGER`, `_BATTLEAIDE`, `_SMITH`, `_TRAINER` |
| Registration | siegefortress id 6, fee 500,000, tax targets 96 | siegefortress id 3, fee 1,000,000, tax targets 63 |

The off path is exact. It removes every row the marker (`.opensro-bandit-fortress.json`,
`.opensro-hotan-fortress.json`) names, and puts back the native bytes of every file it replaced, such as
the Bandit sectors. A marker is written before the first change, so a run that stops partway still leaves
the off path what it needs.

The fortresses then run on the port's existing fortress line. The authority catalog binds each siege world
to its fortress by the world's first string column, `appendFortressStructures` stands each zone's default
structure, and `enterworld.siegeFortressRows` adds the rows to the client's fortress list in id order
(Jangan 1, Hotan 3, Constantinople 4, Bandit 6).

## Decisions recorded

These are inferences and port choices, not native evidence:

- **Two Bandit model paths no client ships.** The Bandit structure rows name `fort_stone_bj.bsr` and
  `small_gate_bj.bsr`. Neither is in the `Data.pk2` of v1.150, v1.188, v1.657 or v1.696. Each client ships
  the fortress's own models under other names:
  - **Fort stone:** every client ships exactly eight fort stones (`bd, ct, dw, er, hd, ht, ja, sk`) for
    exactly eight siege worlds (`INS_FORT_BJ, CT, DW, ER, HM, HT, JA, SK`). The two "-dan" fortresses
    carry the "d": Bijeokdan's stone is `fort_stone_bd`, Heukmakdan's `fort_stone_hd`.
  - **Gate:** `Bldg\guild\bj\` holds one gate, `big_gate_bj.bsr`. The later maps' `object.ifo` (v1.188,
    v1.657 and v1.696 alike) places it as object 1979. The Bandit rows author no big gate, only the
    small one.

  Only the drawn model changes. If a source ever ships the authored file, the import stops and asks for
  the mapping to be removed.
- **One misfiled model.** Hotan's big gate names `Bldg\guild\ht\big_gate_ht.bsr`, but both clients ship
  that file in `Bldg\guild\hotang\`, beside the fortress's bridge, plaza and castle gate. The import uses
  the file where it is shipped.
- **Unparsable shipped models pass.** The v1.188 sectors place models the dependency reader cannot parse.
  These are v1.150's own `tre_bamboo04.bsr` and the palms that stage 1 already copies into every variant.
  The walk lets through only files the variant already ships. A missing file, or an unparsable one that
  only this walk brought in, stops the build.
- **The Bandit fortress shares Jangan's official.** v1.188 stands one `NPC_CH_FORTRESS_OFFICIAL`, at
  Jangan's fortress, and both siegefortress rows name it.
- **Identities.** One importer per shard file: 290000 is the Bandit fortress's and 300000 Hotan's. Both
  allocate after Roc, so their structure identities stay above every earlier layer's rows. They are kept
  in the marker across rebuilds.

Both switches are registered in `shardsettings`. Neither is a catalogue setting, because the catalogue
publishes no fortress state. Both are described on the public rules page.
