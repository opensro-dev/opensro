# Optional Pharaoh Temple

Port-only, not native. v1.150 has no temple, and the switch is off by default. The job temple in the Storm and Cloud
Desert is seven dungeons (the Temple and six sanctums) behind two desert gates: one for the merchant and hunter jobs,
one for the thief job. With the switch on, the dungeons, their gates and their monsters are on the shard.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_PHARAOH_TEMPLE` | Build and server | The seven dungeons (regions `0x800A`-`0x8010`), the four gates, and the 26 kinds and 103 nests in them, `pharaoh-temple.txt` |

The live shard profile (`apps/server/config/shard-settings.json`) turns it on. The code default stays off.

The build reads:
- the VSRO client (`--vsro-root`) for the dungeons and gates (`scripts/pharaoh_temple.py`);
- the later client from `SRO_V1696_ROOT`, checked against `scripts/v1696_archive_pins.json`, for the kinds;
- the population export from the folder `SRO_PHARAOH_TEMPLE_DATABASE` names (`temple-population.json`, from the
  pinned ISRO-R shard backup).

An export made from another backup is refused before anything is staged.

## What is imported

`scripts/pharaoh_temple_import.py` runs with the later-world layers (`variant_later_layers.py`). It is the shared
monster pass alone (`later_world_import.add_monsters`): the dungeons themselves are VSRO Data the variant build already
stages.

| Part | Result (real inputs) |
| --- | --- |
| Monsters | 26 kinds admitted, in `characterdata_270000.txt` (the Osiris kind rides the Pharaoh Tomb pass's standing row when that pass staged it first), 104 skills in `skilldata_270000.txt` |
| Placements | 102 nests across the seven dungeons, joining the default world |
| Keepers | Selkis, Neith, Anubis, Isis and Seth (grade 3), one each in their sanctums (Anubis two), on a 3-hour respawn |
| Uniques | Osiris (`MOB_SD_OSIRIS_3`, grade 8), one nest in the Temple on an 8-hour respawn; Haroeris (`MOB_SD_HAROERIS`, grade 3), his kind and skills only: no nest places him (below) |

The server loader (`monster.AppendPharaohTemple`) admits a nest only in one of the seven dungeon regions, and only
when its source world is the one its dungeon is grouped under (51-56, and 60 for the Temple); the one exception is
the Osiris nest, which v1.188 groups under the field world and which is admitted from there alone. Every other
population keeps refusing dungeon regions.

## Native evidence and decisions

- **Source.** The v1.188 server places the same 25 kinds in these regions, with the same nest counts and the same
  keeper counts and respawns (research `tactics-recovered/Tab_RefNest.json`, 105 nests joined through
  `Tab_RefTactics.json`). Its recovered tactics keep 10 of the fields a nest's controls carry, so the rows come from the
  ISRO-R export, whose tactics are whole.
- **Shared hive.** The Sanctum of Punishment's healing, power and guard priests are four nests in one hive, with a
  total of 3 alive (v1.188 hive 639). The export carries the cap but not the hive id, so the import keys the hive by
  world, region and cap. The server caps the four nests as one (`HiveKey` / `HiveMaxCount`).
- **NPC rows.** The export's guild agents and the Mentuhotep and Berenice stands are roster content and stay out of
  the population.
- **The Osiris exception.** v1.188's one `MOB_SD_OSIRIS_3` nest (40999) is grouped under the field world
  (hive 2761), not under the Temple's world 60, so the ISRO-R export carries no row for it and the projection
  would drop the one it was handed. The import names it as a single authored row with the v1.188 database's
  full tactics row (57573, read whole), and the server admits it alone from the field world into the Temple
  region, at the grade its tomb brothers carry (8). The kind is the Pharaoh Tomb's too: whichever pass stages
  it first, the other places its nest on the standing row (one codename, one row).
- **Haroeris has no nest (#3787).** Natively he is never placed by a nest. The Haroeris gate job spawns him once
  when his sanctum opens, and his death opens Seth's (research `unique-encounters/haroeris-seth-chain.md`). The
  import claims his kind by name, so his character and skills are staged, and places no row. The server's sanctum
  schedule spawns him (#4270).
- **Not yet here.**
  - His skill summons (`ssou`) and decoys, on the iSRO-R counts and `_RefGameWorldNPC` points.
