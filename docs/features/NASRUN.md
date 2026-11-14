# Devil's Spirit (Nasrun) item rows

Port-only, not native. The default stays v1.150, and the switch below is off by default. This is the first
step of #3348: the item rows land, inert, so the Devil's Spirit's mechanics (its socket, awakening clock and
alchemy) can follow without another data pass.

## Switch

| Switch | Where | Effect |
| --- | --- | --- |
| `SRO_NASRUN` | Build | The fifteen Nasrun item rows, their name texts and their resources |

The build reads the VSRO client (`SRO_VSRO_ROOT`, the variant build's own source). The v1.696 client is not
needed.

## What is imported

`scripts/build_alexandria_variant.py` runs `scripts/nasrun_import.py` after the mall avatars. Behind the switch:

| Rows | Type | VSRO ids |
| --- | --- | --- |
| The eight `ITEM_MALL_AVATAR_{M,W}_NASRUN` wearables (red, yellow, blue, unique) | 3/1/14/1 | 24405–24412 |
| `ITEM_MALL_NASRUN_EXTENSION`, the extension gear | 3/3/13/16 | 24415 |
| `ITEM_ETC_NASRUN_ARCHEMY_UPGRADE_STONE`, Sabakun's jewel | 3/3/10/1 | 24417 |
| `ITEM_ETC_NASRUN_ARCHEMY_RECIPE` | 3/3/11/3 | 24418 |
| `ITEM_ETC_NASRUN_ARCHEMY_STONE_01..04` | 3/3/11/5 | 24419–24422 |

The rows are staged in `itemdata_190001.txt` under fresh identities above every published row of the merged
item tables, remembered in `.opensro-nasrun.json` so a rebuild never moves an item. Their `SN_` names join
`textdataname.txt`. Their icons and drop models are copied from the VSRO archives. Off removes the shard, its
manifest line, the names it added and the marker, so the tables are byte-identical to a build without it.

## Why the rows are inert

- The wearables fit no equipment or avatar socket the port admits (avatars are 3/1/13 only), so they can be
  carried but not worn.
- The extension gear, the recipe and the four stones match no use or alchemy handler.
- Sabakun's jewel has an elixir's type, but its class parameters admit only item class 14, the Spirit itself:
  reinforcing any v1.150 item with it is refused. The import refuses a jewel row that names any other class.
- Nothing lists them: no mall goods, drop or event reward. They arrive by GM grant alone.

## Decisions recorded

These are inferences and port choices, not native evidence:

- **No worn model.** Every 3/1/14 row of the VSRO client carries a drop model and an icon but no worn model,
  so the Spirit adds no body geometry. Earlier notes (the mall avatar import, `CAP110.md`) read that empty
  column as absent resources.
- **Left out.** The event copies (`ITEM_EVENT_*`) belong to events this port does not have. The Umdu magic
  stone (`ITEM_MALL_NASRUN_ARCHEMY_MAGICSTONE_UMDU`) names `MATTR_NASRUN_UMDU`, which has no row in the
  v1.150 magicoption table.
- **Still open (#3348).** The socket, the awakening clock (2,419,200 seconds on the wearable and the extension
  gear), the Nasrun alchemy outcome and the mall listing.
