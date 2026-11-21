# Equipment switch scrolls

Port-only, not native. Three Item Mall scrolls let a player turn an item into its counterpart, keeping the
work already put into it: a weapon into another weapon type of either race, a piece of armor or a shield
into the same piece of the other race, and an accessory into the same accessory of the other race. Each
scroll has its own switch, off by default; with every switch off the game is v1.150, byte for byte.

## Why

A player who changes build or race otherwise starts the gear over: the item's plus, its blue options and
its white stats are lost with it. The scrolls keep them, for silk, so the change costs a scroll instead of a
new set.

## The scrolls

| Scroll | Codename | Silk | Turns | Into |
| --- | --- | --- | --- | --- |
| Weapon Switch Scroll | `ITEM_MALL_WEAPON_SWITCH_SCROLL` | 1490 | a weapon in the bag | the weapon of the chosen type and race, same degree, grade and seal |
| Armor Switch Scroll | `ITEM_MALL_ARMOR_SWITCH_SCROLL` | 990 | a piece of armor or a shield in the bag | the same piece of the other race |
| Accessory Switch Scroll | `ITEM_MALL_ACCESSORY_SWITCH_SCROLL` | 990 | an earring, necklace or ring in the bag | the same accessory of the other race |

The armor pairs: Chinese clothes (robe) and European garment, Chinese light and European protector,
Chinese heavy and European armor, the Chinese and European shields. An armor piece keeps its part and its
sex. The accessories pair Chinese 3/1/5/x with European 3/1/12/x (earring, necklace, ring).

Kept: the plus, the blue options, the white stats and the durability (lowered to the new item's maximum
when it is above it). Only the item's reference changes.

The scrolls are sold in the Item Mall, on the scroll tab, beside the name-change scroll. They are not
dropped and not sold by NPCs.

## How a player uses one

1. Right-click the scroll: the item-target cursor appears.
2. Click the item in the bag (not an equipped item: take it off first).
3. Weapon scroll: a box asks for the race, then the weapon type. Armor and accessory scrolls: a Yes/No
   confirmation.
4. The item changes in place in its slot and one scroll is spent.

A refusal keeps the scroll and says why: not an item the scroll switches, no counterpart of that type
(odd quest, event, trade or PvP gear has none; in the shipped data the twelve `ITEM_CH_M_HEAVY_07_*_C_SUPER`
pieces and `ITEM_EU_W_HEAVY_05_HA_C_RARE` have no counterpart), or blue options that could not follow.

## Switches: on and off at any time

Each scroll has one environment switch, used by both the build and the GameWorld:

| Switch | Scroll | Default |
| --- | --- | --- |
| `SRO_WEAPON_SWITCH_SCROLL` | Weapon Switch Scroll | `off` |
| `SRO_ARMOR_SWITCH_SCROLL` | Armor Switch Scroll | `off` |
| `SRO_ACCESSORY_SWITCH_SCROLL` | Accessory Switch Scroll | `off` |

`1`, `on` and `true` turn a scroll on; anything else, unset included, is off. They are shard settings
(`internal/config/shardsettings`), so the operator sets them like any other.

- **Build.** While a switch is on, the variant build stages that scroll's item. The switches are
  independent: one scroll can exist without the others.
- **GameWorld.** The GameWorld reads the switches once at startup. A scroll on whose item the build staged
  is listed in the Item Mall and usable. A scroll on whose item is missing from the data lists nothing and
  logs one warning; the GameWorld still boots.

To turn a scroll **off**: set its switch off and restart the GameWorld. The scroll leaves the Item Mall at
once. Scrolls players already hold stay in their bags; using one is refused and the scroll is kept. Turning
it back on (restart again) makes those scrolls usable again.

To turn a scroll **on**: set its switch on for the build and for the GameWorld, rebuild the variant data,
restart.

Once a scroll has been sold, keep its item in the build (the build switch on) even while the scroll is off
in the GameWorld: a build without it removes the item from the data while players may still hold it.

## How it is built

v1.150 has no such items, and the shipped reinforce scrolls whose icons fit stay untouched, so each scroll
is an item of its own:

- **Data** (`scripts/switch_scroll_import.py`, a variant later layer after the gears stones). Each enabled
  scroll is a copy of a shipped reinforce scroll's itemdata row (the +2 weapon, +1 armor and +1 accessory
  reinforce scrolls) under its own codename, name symbols and identity, so it wears that scroll's icon,
  type (3/3/13/11) and stack. The rows go to their own shard, `itemdata_290000.txt`. Each identity is
  allocated above every object row of the tree and remembered in `.opensro-switch-scrolls.json`, so a rebuild
  never moves an item a player holds. The English name and description join `textdataname.txt`. A switch
  off removes exactly what its scroll added.
- **Server** (`internal/game/item/commerce/equipswitch.go`: the table of scrolls, switches, prices and mall
  listing; `cmd/services/sro-gameworld/wiring_equipswitch.go`: which scrolls are on). The use is admitted by
  codename (`itemuse_admission.go`) and served by `action/weaponswitch.go` (weapons) and
  `action/raceswitch.go` (armor, shields, accessories). The counterpart is found by codename: the shipped
  data names both races' items and every weapon type alike after the race and type segments, so the server
  swaps those segments and requires the result to exist with the expected TypeIDs and race. It also
  requires the same blue-option family and equipment socket, the only inputs of alchemy's blue-option rule,
  so every blue stays valid. The answer is the armor gender tool's: the reference changes in place, a
  reference row for the new item, then 0x3645 [slot][1][u32 ref].
- **Client** (`foundation/gameplay/weapon-switch.ts`, `race-switch.ts`, the cos HUD cursor and `ui.ts`
  boxes). A scroll is known by its codename, which the bootstrap and the commerce reference deltas carry,
  so a scroll bought during the session works without a relog. The use request is 0x75BD [slot][type]
  [target slot] for the race scrolls and [target slot][weapon type] for the weapon scroll. The refusal bytes
  0xF5 to 0xF8 read as the scroll's own lines only while its use is in flight.

## Decisions recorded

Port choices and inferences, not native evidence:

- **Bag only.** An equipped item is refused; the player takes it off first.
- **Enhanced items switch.** The plus and the blues are kept (owner, 2026-11-21).
- **The counterpart is the same tier.** Both races and every weapon type carry the same degree, grade and
  seal suffixes with the same required level per degree, so the swapped codename is the same tier.
- **Durability.** Kept, but never above the new item's maximum: the races and types of one degree author
  different maxima, and a row above its maximum could never be repaired.
- **New items, not renamed ones.** Reusing the reinforce scrolls under a new name would have taken them
  from the game; copies keep them (owner, 2026-11-21).

## Known limits

- An item offered in an open exchange is not refused at use time.
- Icons are the reinforce scrolls'; dedicated art may come later.
