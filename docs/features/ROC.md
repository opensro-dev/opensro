# The Roc range

The Roc summit is native to v1.150: its arena regions (`RN_RM_ROCAPEX`) and the entry (`RN_RM_ROCAPEXENTER`)
are in the shipped region table. The kinds that fight there are not. They come from the v1.188 client and
are built as one native chain (#3399). The switch below stays off until the whole chain has landed.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_ROC` | Build | Stages the Roc range's nine kinds with their skills, names, skill effects and characterinfo |

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

- **Where the rows go:** `characterdata_260000.txt` / `skilldata_260000.txt`.
- **Identities:** fresh, above every published row. `.opensro-roc.json` records them, so a rebuild never
  moves a kind.
- **Names:** the arena's Tahomet reuses the native `SN_MOB_RM_TAHOMET` name. The import records only the names
  it adds, so an off build leaves that native name in place.
- **Switching off:** an off build removes the shards, the skill manifest line, the added names, the skill
  effects and characterinfo rows, and the marker.

## What is not here yet

- **Spawning.** The kinds are inert. No nest places them, because the reference data has no population in the
  arena regions: the server spawns Roc and the Tahomet bosses when the arena opens. That is worlds 40 and 41,
  a later step.
- **Content still to come**, each its own step of #3399:
  - the summit gate (teleportdata 162 and 163, building 24297, the waiting room 25792);
  - the gate's link conditions;
  - the `QNO_RM_MEETROC` quest chain and its `ITEM_QNO_RM_MEETROC_*` items;
  - drops and the unique notice.
