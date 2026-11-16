# Optional Pharaoh Temple

Port-only, not native. v1.150 has no temple, and the switch is off by default. The job temple in the Storm and Cloud
Desert is seven dungeons (the Temple and six sanctums) behind two desert gates: one for the merchant and hunter jobs,
one for the thief job. With the switch on, the dungeons, their gates and their monsters are on the shard.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_PHARAOH_TEMPLE` | Build and server | The seven dungeons (regions `0x800A`-`0x8010`), the four gates, and the 24 kinds and 101 nests in them, `pharaoh-temple.txt` |

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
| Monsters | 24 kinds in `characterdata_260000.txt`, 83 skills in `skilldata_260000.txt` |
| Placements | 101 nests across the seven dungeons, joining the default world |
| Keepers | Selkis, Neith, Anubis, Isis and Seth (grade 3), one each in their sanctums (Anubis two), on a 3-hour respawn |

The server loader (`monster.AppendPharaohTemple`) admits a nest only in one of the seven dungeon regions, and only
when its source world is the one its dungeon is grouped under (51-56, and 60 for the Temple). Every other population
keeps refusing dungeon regions.

## Native evidence and decisions

- **Source.** The v1.188 server places the same 24 kinds in these regions, with the same nest counts and the same
  keeper counts and respawns (research `tactics-recovered/Tab_RefNest.json`, 105 nests joined through
  `Tab_RefTactics.json`). Its recovered tactics keep 10 of the fields a nest's controls carry, so the rows come from the
  ISRO-R export, whose tactics are whole.
- **Shared hive.** The Sanctum of Punishment's healing, power and guard priests are four nests in one hive, with a
  total of 3 alive (v1.188 hive 639). The export carries the cap but not the hive id, so the import keys the hive by
  world, region and cap. The server caps the four nests as one (`HiveKey` / `HiveMaxCount`).
- **NPC rows.** The export's guild agents and the Mentuhotep and Berenice stands are roster content and stay out of
  the population.
- **Not yet here.**
  - `MOB_SD_OSIRIS_3`: v1.188 has one nest of it, a world-1 hive in the Temple with an 8-hour respawn. The ISRO-R
    export doesn't carry it, and its controls need the v1.188 database's full tactics row.
  - Haroeris: neither source places it by a nest.
