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
- **Built since (#3456).** The socket (#3477), the awakening clock (#3622), the unique-kill tokens (#3710), the
  token shop (#3741), Nasrun alchemy (below), what the plus gives (below), the extension gear (below), the
  lucky drops (below) and the mall listing (below).
- **Still open (#3456).** The client step that shows the lent skills.

## Nasrun alchemy (#3456 step 3c)

With `SRO_NASRUN` on, Sabakun's jewel reinforces a Spirit in the bag through the native alchemy window, one plus
at a time up to +10 (`internal/game/item/alchemy/nasrun.go`):

| Attempt | +0→+1 | +1→+2 | +2→+3 | +3→+4 | +4→+5 | +5→+6 | +6→+7 | +7→+8 | +8→+9 | +9→+10 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Chance | 70 % | 60 % | 50 % | 40 % | 30 % | 20 % | 15 % | 10 % | 8 % | 5 % |

- A failure spends the jewel. At or below +5 it keeps the plus; above +5 it drops the Spirit back to +5 (the owner's rule, 2026-11-28, #4732). A failure never destroys the Spirit (owner, 2026-11-28).
- The chance is the table alone: Lucky options, avatar luck and the shard's rate table do not change it.
- Refusals use the native codes: a lucky powder staged with the jewel (0x0a), a Spirit at +10 (0x0f), a native
  elixir on a Spirit or the jewel on native gear (0x09). Off, a Spirit is no target (0x10).
- The token shop sells the jewel for 20 tokens beside the Spirits.

## What a worn Spirit gives (#3456, 3c follow-up)

With `SRO_NASRUN` on, a Spirit worn in avatar slot 4 gives its abilities while its clock is awake
(`internal/game/nasrun/ability.go`, `internal/game/action/spiritability.go`). The rule is the iSRO-R shard's
`_RefAbilityByItemOptLevel` (2025-07), read by codename:

| Plus | Skills lent | HP and MP | Block |
| --- | --- | --- | --- |
| +0 to +2 | the colour's Eccentric Demon | +15 % + plus | 0 |
| +3 to +5 | the colour's Devil's Confusion, Monster Defence Ignore 1 | +15 % + plus | 1 |
| +6 to +10 | the colour's Destroy the Devil, Monster Defence Ignore 2 | +15 % + plus | 2 |

The Unique lends the red skills, with +20 % + plus and one more block. The v1.188 export puts the top tier at +5 with
smaller percents; it is a private-server set, so the official shard wins.

- The HP/MP percents land on the STR and INT gauge nodes (0x34, 0x35) and the block on 0x0A, as the v1.188
  server's option pass writes hpna, mpna and brna (498690).
- The lent skills are cast like learned ones, only while the Spirit is attached: each carries reqi 14 1, the
  avatar-socket-4 check (58D480 case 3). Devil skills are 10-minute self buffs of damage (dru) and speed (hst3);
  Monster Defence Ignore is an instant 5-minute buff whose msid adds its percent to the ignore-defence chance (0xBC).
- Taking the Spirit off or its clock running out removes its share at once and ends the running buffs that lean on
  it. The v1.188 server also sends 0x3078 {3, slot}, which the v1.150 client has no handler for; the port sends
  the stats refresh alone.
- The build stages the eleven skill rows from the VSRO client (`add_nasrun_skills`, `skilldata_130000.txt`) under
  fresh ids, with their names and icons. Off, the skill tables are byte-identical.
- Not yet: the client shows the lent skills nowhere (v1.150 has no avatar skill window), so a player casts them
  from a skill bar slot once the client step lands.

## The extension gear (#3456 slice 4)

With `SRO_NASRUN` on, the extension gear (`ITEM_MALL_NASRUN_EXTENSION`, 3/3/13/16) lengthens a woken Spirit's
awakening (`internal/game/action/spiritextend.go`, `internal/game/nasrun/extension.go`):

- The player drops the gear on a Spirit in the bag, as with v1.150's gender tool. The use carries that bag slot, the
  v1.188 server's use shape. A worn Spirit is no target: the reference server reads the bag alone.
- Each gear adds 7 days: from the expiry while the Spirit is awake, from now once it has lapsed. A clock never runs
  more than 28 days ahead; a gear that would pass that is refused and kept. That is the owner's upkeep rule
  (2026-11-16). The VSRO row's own Param1 is four weeks with no ceiling, and it is not used.
- Refusals use the v1.188 codes: a slot outside the bag (0x1882), no Spirit there (3), a Spirit never woken
  (0x18F8). The ceiling has no native code and answers the generic refusal.
- The worn Spirit is refused properly (#3909): the later client stops the use before sending and prints
  `UIIT_MSG_NASRUN_WEAR_EXTENSION_ITEM_NOT_USE` ("You cannot use this while you are wearing it.", imported into
  the text build). This port's client refuses it the same way at the drop and at the armed cursor. The server
  keeps the reference's bag-only read: a sender without that gate gets the slot refusal (0x1882).
- Every honor shop's second tab sells the gear twice: for 10 tokens and for 5,000,000 gold, the owner's upkeep price.
- The Spirits and the gear are staged bound, as v1.150's own honor goods: no trade, merchant sale, warehouse or drop
  (CanTrade, CanSell, CanBorrow, CanDrop all 0).
- The v1.188 server also sends 0x3078 {2, slot, seconds left}, which the v1.150 client has no handler for, so the
  port sends the use answer alone.

## The lucky drops (#3456 step 3d)

With `SRO_NASRUN` on, each eligible contributor to a unique kill rolls a Devil's Spirit and Sabakun's jewel into
their bag, beside the token award (`internal/game/action/nasrun_drops.go`, odds in `internal/game/nasrun/drops.go`):

- A Spirit at 10 % on an easy unique and 20 % on a hard one; the jewel at 90 % on both (owner, 2026-11-27).
  The families are the token ones, below the level band and one per account, as for the tokens.
- The Spirit is the plain A grade row of the winner's sex; the colours stay a shop choice.
- No cap and nothing saved: the roll is per kill, and a full bag keeps neither prize - the notice travels only
  for an item that landed.

## The mall listing (#3456 step 5)

The Devil's Spirit and Sabakun's jewel come from uniques only (owner, 2026-11-25; #4360): while the lucky drops
run, the mall withholds every package holding a Spirit or the jewel (`action/itemmall.go`,
`withholdUniqueOnlyGoods`), and a buy of one gets the existing refusal. The token shop keeps selling the Spirits,
as its tokens are unique rewards too.

| Goods | In the Item Mall | Where they come from |
| --- | --- | --- |
| The six A grade Spirits | withheld | unique kills (10 % / 20 %), the honor token shops |
| Sabakun's jewel | withheld | unique kills (90 %), the honor token shops |
| The extension gear | Premium > Gears, 50 silk, badged new | the mall, the honor token shops |

- The listing itself stays in `internal/game/item/commerce/mallcategories.go` (`nasrunGoods`: Spirits 150, gear
  50, jewel 30). The withholding removes the Spirit and jewel offers when the mall loads.
- The S grade `_UNIQUE` pair is never listed: the later mall sells it only as a Premium Mall copy, and that shop is
  never ported (owner decision on #4240).
- Off, or with the items' rows unstaged, the catalogue is the shipped 91 offers exactly (#3331).
