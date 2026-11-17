# The Roc range

The Roc summit is native to v1.150: its arena regions (`RN_RM_ROCAPEX`) and the entry (`RN_RM_ROCAPEXENTER`)
are in the shipped region table, and so are the `ITEM_QNO_RM_MEETROC_*` items. The kinds that fight there, the
gates and the wait room (25792) are not. They come from the v1.188 client and
are built as one native chain (#3399). The switch below stays off until the whole chain has landed.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_ROC` | Build and server | Build: stages the Roc range's eleven kinds with their skills, names, skill effects and characterinfo, the four gates with their links, the wait room, the six MEETROC quests and the quest kinds' nests (`roc-monsters.txt`). Server: admits those nests (`monster/roc.go`). Build and server must agree |

The build reads the VSRO client (`SRO_VSRO_ROOT`, the variant build's own source).

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/roc_import.py` after every later layer. Behind the switch:

| Kind | Grade | Level |
| --- | --- | --- |
| `MOB_RM_ROC`, the Roc | 3 (unique) | 100 |
| `MOB_RM_DEVILSAYTAN`, Demon God Shaitan | 8 | 100 |
| `MOB_RM_TAHOMET_ROC`, the arena's Tahomet | 8 | 100 |
| `MOB_RM_SPIRITS` and its three clones | 8 | 100 |
| `MOB_RM_WINGTRIBEKNIGHT` | 8 | 100 |
| `MOB_RM_WINGTRIBEDUKE` | 0 | 100 |
| `MOB_QT_RM_FEATHERMASK_01`, a quest kind | 0 | 90 |
| `MOB_QT_RM_ROCKY_CLON_01`, a quest kind | 0 | 90 |

- **Where the rows go:** `characterdata_260000.txt` / `skilldata_260000.txt`.
- **Identities:** fresh, above every published row. `.opensro-roc.json` records them, so a rebuild never
  moves a kind.
- **Names:** the arena's Tahomet reuses the native `SN_MOB_RM_TAHOMET` name. The import records only the names
  it adds, so an off build leaves that native name in place.
- **Switching off:** an off build removes the shards, the skill manifest line, the added names, the skill
  effects and characterinfo rows, the wait room's files and region rows, and the marker.

### The gates (step 1b)

| Gate | Teleport | Building | Arrives in |
| --- | --- | --- | --- |
| `GATE_RC_MOUNTAIN_GATE` | 160 | `STORE_RC_TAHOMET_GATE` (24296) | world 1, region 23406 |
| `GATE_RC_TAHOMET_GATE` | 161 | none | world 41 (`INS_ROC_FRONT`), region 25789 |
| `GATE_RC_ROC_GATE` | 162 | none | world 40 (`INS_ROC_TOP`), region 26045 |
| `GATE_RC_ROC_FRONT_GATE` | 163 | `STORE_ROC_FRONT_GATE` (24297) | world 1, region 25792 |

- **Links:** the summit link, 160 to 162, carries the native conditions: the key item
  `ITEM_QNO_RM_MEETROC_6_01` held (kind 4) and spent (kind 5), level 80 and up (kind 1), and no job (kind 6).
  The server evaluates all four (#3765). The key item is renumbered by codename to the variant's id (24164 in
  v1.150). If a variant lacks it, the link stays out rather than opening without its key. 162 back to 160 is
  unconditioned.
- **Ids:** the gates and buildings keep the client's ids. A building id that the variant already uses for
  another object stops the build.
- **The wait room:** region 25792 (`RN_RM_ROCAPEX_WAIT`). v1.150 ships its zone names, but not its region
  row, sector 100/192 or navmesh. Those are merged the way the other later worlds are (`later_world_import`).

### The quest chain (step 3c)

- **The quests:** the VSRO client's questdata ships exactly six MEETROC quests: `QNO_RM_MEETROC_1`, `_2`, `_6` and
  `_ECT_1` to `_ECT_3`. Their rows join `questdata.txt` under the source ids 91, 92, 96 and 374-376, which are free
  in v1.150. A row the client lacks, or an id or codename the table already holds, stops the build. v1.150 already
  ships their text and their items. The compiled classes are #3796: `QNO_RM_MEETROC_1` projects, and the other
  five wait for their custom slots (step 3b).
- **The quest kinds' nests:** six v1.188 nests in one hive (545) on the Roc mountain, regions 24690, 24691, 24693,
  24947 and 24948, sharing a cap of 20. `scripts/build/import_roc_quest_nests.py` reads them once from the
  v1.188 shard backup the population evidence pins, and commits them to `scripts/data/roc/quest-nests.json`.
  The build writes them as `roc-monsters.txt`. ISRO-R holds the same six nests at the same anchors with other
  tuning; the v1.188 rows win, as for every nest.
- **Switching off:** the six rows leave `questdata.txt` by codename (it is the native table again, byte for
  byte) and `roc-monsters.txt` is removed.

## What is not here yet

- **Spawning and the instances.** The arena kinds stand in no nest, because the reference data has no
  population in the arena regions: the server spawns Roc and the Tahomet bosses when the arena opens. The
  gates arrive in worlds 40 and 41, whose admission and spawns are step 4.
- **Content still to come**, each its own step of #3399:
  - the custom slots of `QNO_RM_MEETROC_2`, `_6` and `_ECT_1` to `_ECT_3` (step 3b);
  - worlds 40 and 41 (step 4);
  - drops and the unique notice (step 5).
