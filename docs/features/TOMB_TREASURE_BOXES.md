# Ch'in Tomb treasure boxes

The four treasure boxes on the Ch'in Tomb's second floor, their open event, and the keys tomb monsters drop. Issue
[#4810](https://github.com/opensro-dev/opensro/issues/4810).

The v1.150 client defines all of it: the boxes (characterdata 23261-23264, `NPC_TQ_TREASUREBOX1-4`), the keys
(itemdata 22626-22629, `ITEM_ETC_TREASUREBOX_KEY_*`) and every talk line (`SN_TALK_QET_TQ_TREASUREBOX_01-05`). The
shipped world places no box. The v1.188 server runs each box as its own event. The later client marks the boxes
deleted at one publisher's request, so the switch is off by default (owner, 2026-11-19).

## Switch

| Setting | Read by | What it does |
| --- | --- | --- |
| `SRO_TOMB_TREASURE_BOXES` | Server | Places the four boxes, offers their open row and lets tomb monsters drop keys. Off (the default) leaves the tomb exactly as shipped |

## The open (`action/tombbox.go`)

Talking to a box offers one row, "Open the box". A click checks, in the v1.188 server's order:

1. at least two free bag slots, else `_05` ("inventory is full");
2. the box's own key in the bag, else `_04` ("no key").

It then rolls `rand()/32767*100` and takes the first of nine outcomes whose running sum of weights is not below the
roll. The outcome and the spent key commit together in the character's door. A treasure that does not fit keeps the
key and answers `_05`.

| # | Outcome | Box 1 Courage | Box 2 Trials | Box 3 Challenge | Box 4 Heroes |
| --- | --- | --- | --- | --- | --- |
| 0 | damage, `_02`: maxHP x 10/20/30/40 %, never the last HP | 9.2818 | 8.8849 | 8.6398 | 2.2042 |
| 1 | guards, `_03`: 2 clones / 2 clones / 1 guard / 1 guard within 30 | 30 | 30 | 30 | 5 |
| 2 | gold: 38,000 / 76,000 / 110,000 / 400,000 | 30 | 30 | 30 | 42.5 |
| 3 | potions: 50 `ALL_POTION_05` / 10 `HP_SPOTION_01` / 10 `MP_SPOTION_01` / 50 `ALL_SPOTION_01` | 20 | 20 | 20 | 20 |
| 4 | recipe: shield / accessory / armor / weapon `REINFORCE_RECIPE_*_B` | 10 | 10 | 10 | 23 |
| 5 | top-degree normal item, +4/+5/+6/+7, not repairable | 0.5666 | 0.85 | 1.02 | 5.6666 |
| 6 | top-degree normal item, +1/+2/+3/+4 | 0.0944 | 0.17 | 0.2125 | 1.0625 |
| 7 | top-degree sealed item (Star/Star/Moon/Sun, +0/+3/+0/+0), not repairable | 0.0566 | 0.0944 | 0.1275 | 0.5666 |
| 8 | top-degree sealed item, same grade and plus | 0.0006 | 0.0007 | 0.0002 | 0.0001 |

The top degree is the highest at or below 10 that the drop catalogue holds, after the shard's drop caps
(`loot/treasurebox.go`). Degree 9 is the top on a v1.150 data set. The item is made as any drop makes it
(`prepareSelectedItem`).

## The key drop

Every `MOB_TQ_*` death rolls each box's key once, at 0.5 / 0.25 / 0.16667 / 0.025 %. A party monster's chance is
eight times higher. The roll is `((rand << 15) | rand) % 1e6 / 1e6`, and a key drops at the monster for the kill's
owner.

## Not yet

- The guards (`MOB_TQ_TREASURE_GUARD`, `_CLON`) have no v1.150 characterdata row. Until the build stages them from the
  v1.188 client (#4810), the guard outcome answers `_03` and summons nothing.

## Native claims

| Claim | Evidence | Status |
| --- | --- | --- |
| Open order: two free slots, then the key; the key is spent after the outcome | v1.188 server box open handlers (vtable +0x10 of each box event) | checked (BN) |
| Nine weights, per-box parameters as tabled | the four box init functions | checked (BN) |
| Damage clamps to current HP - 1 | the shared resource-consume helper | checked (BN) |
| Key drop: MOB_TQ_ prefix flag, chance x 8 for a party monster, million-step roll | the box events' monster-death handler; the tomb prefix registration | checked (BN, 2026-11-28) |
| Box positions | ISRO-R Tab_RefNest 24068-24071, region 0x8006 | checked |

The full trace is in the research tree, `investigations/server/tomb-treasure-boxes/README.md`.
