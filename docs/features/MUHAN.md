# Optional Muhan, the Battlefield of Infinity

Port-only, not native. The default stays v1.150, and the switch below is off by default. The import adds the
later client's Muhan wave arenas on top of the native tables, joined by codename. Native rows, IDs and names
never change.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_MUHAN` | Build and server | The eight Muhan wave arenas (worlds 103-106, 108-111), their two regions' terrain and navmesh, 36 monster kinds, and the wave director that runs their twenty waves |

The build reads the later client from `SRO_V1696_ROOT` (checked against `scripts/v1696_archive_pins.json`) and the
wave export from the folder `SRO_MUHAN_DATABASE` names (`muhan-population.json`, extracted from the ISRO-R shard
backup by `research/investigations/later-sources-3247/extract_muhan_population.py`). The export records the backup's
sha256; an export made from another backup is refused before anything is staged.

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/muhan_import.py` after the Jupiter pass. Behind the switch:
the two wave regions' (27091, 27092) terrain remapped onto the merged tile catalog, their object catalog, navmesh
and minimap tiles, every Data file the native tree lacks, and the monster import:

| Part | Result (real inputs) |
| --- | --- |
| Regions | 2 (27091, 27092), the halls every arena binds |
| Monsters | 36 `MOB_MUHAN_*` kinds in `characterdata_200000.txt` / `skilldata_200000.txt`: one combat kind per level a wave can name (72-80, 82-90, 92-100, 102-110) |
| Waves | 160 rows (8 arenas × 20) in `muhan-waves.txt`, Version 2: point, delay, count, level offset, radius, monster type |
| Worlds | `GROUP_MUHAN_SOLO_75/85/95/105` and `GROUP_MUHAN_PARTY_75/85/95/105`, registered as permanent worlds (type 0, one layer) while the switch is on |

The four unbanded worlds (99-102) bind no region and author no waves, so they stay unregistered. The 24 authored
nests standing in the wave regions are the arenas' manager and change NPCs, and the 16 transform-mask rows are
recorded in the export; no monster joins them here (see "Left for the entry slice").

## How a run works

The wave director (`simulation/muhan_chain.go`) is the only owner of a run, the way the Qin-Shi tomb chain owns
its triggers. A division opens each arena at boot, and the run starts on the population's first tick: twenty waves
in sequence, each spawning its count of the resolved kind scattered inside the wave's radius at its point, the
next wave only once the whole wave is dead, and after the finale the arena rests out a rearm window and runs
again. The director owns these lives - wave instances never respawn on their own and a wave never refills.

## Decisions recorded

These are inferences and port choices, not native evidence:

- **Kind selection (inference).** No surviving binary this port may cite implements the wave system, and the wave
  table names no kind - only a level offset. The v1.696 kinds are level-stamped `MOB_MUHAN_*` clones, exactly one
  combat kind per level, so a wave spawns the kind at (world band + offset): the solo and party 75 arenas spawn
  levels 72-80, and so on. The importer refuses a build where a named level resolves to zero or two kinds, and the
  server refuses the same at install. The `MOB_MUHAN_CHANGE_*` transform forms are excluded on both sides.
- **Delay (inference).** A wave's `DelayMs` counts from the previous wave's clearance (the first wave's from the
  run's start). The rows' delays are not monotonic in sequence, so they cannot be absolute offsets from a start;
  clearance-relative is the reading that makes every row meaningful.
- **Rarity.** A wave's monster type rides the spawn as its rarity override (0 ordinary, and the authored 1/6/7 on
  the waves that carry them), the same override the nest schema's rarity byte provides.
- **Rearm (inference).** After wave 20 falls, the arena re-arms after 1800-3600 s - the Floor 1-3 unique respawn
  window the tomb chain's rearm already stands on - since the port keeps these worlds permanently open instead of
  closing and reopening an instance on a schedule.
- **Ambient runs.** The arenas are permanent worlds whose timers run whether or not anyone stands inside (the
  tomb chain's posture: the world owns its timers, including regions without viewers). Nothing raises the player
  level cap, and solo versus party rides the world codename alone until the entry slice gives it meaning.
- **No authored drops.** The import writes no reward table: the kinds keep the native loot posture.

## Left for the entry slice

Recorded so the follow-up needs no second pass over the sources (#3314 tracks it):

- **Entry.** The manager NPCs (`NPC_MUHAN_<SOLO|PARTY>_<band>_MANAGER`) and the gates/teleports that admit a
  player or party to an arena, with the solo/party rule. The port has no entry machinery for port-only instanced
  content yet (the same gap as the Jupiter temples' admission and the #1800 step-3 slice-4 work).
- **Transforms.** The 16 `_RefMuhanMonsterTransMask` rows (change NPC, `SKILL_MUHAN_*_BUFF01`, `MOB_MUHAN_CHANGE_*`
  form) the export records.
- **Rewards.** Any wave-clear or run rewards: the source's reward tables were not imported.
- **The unbanded shells.** Worlds 99-102 carry no data in the source and stay out unless the source grows some.
