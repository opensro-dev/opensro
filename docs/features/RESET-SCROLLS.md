# Stat and skill reset scrolls

Port-only, not native. Two Item Mall scrolls at 690 silk each let a player rebuild a character without
starting over: one returns every stat point, the other every skill and mastery point. Each scroll has its
own switch, off by default. With both switches off the game is v1.150, byte for byte.

## Why

A player who wants another build otherwise needs the level-tier stat recall scrolls, which work only within
their level window, or the herbalist's restoration, which removes one skill or mastery rank at a time for
gold and potions. These two scrolls do the full reset in one use, for silk.

## The scrolls

| Scroll | Codename | Silk | Does |
| --- | --- | --- | --- |
| Stat Recall (All Levels) | `ITEM_MALL_STATS_POINT_RECALL_ALL` | 690 | STR and INT go back to their value for the character's highest level; every point spent returns to the pool. Any level. |
| Scroll of Skill Restore | `ITEM_MALL_SKILL_POINT_RECALL_A` | 690 | Every learned skill is forgotten and every mastery goes back to 0; all the SP they cost returns. |

Both are the real v1.696 items, sold on the Item Mall's scroll tab next to the tier stat recall scrolls.
The skill scroll brings its own icon. The stat scroll uses the same icon as the tier stat recall scrolls,
as it does in v1.696. They are not dropped and not sold by NPCs. The six tier scrolls keep their prices and
their level windows.

The skill reset keeps only the free basic skills: skills whose ranks cost no SP and need no mastery or
other skill. Quick-bar slots on a forgotten skill are emptied. The forgotten skills' buffs end at once.
The refund is exactly what the herbalist's restoration would return rank by rank (each skill rank's SP
cost, each mastery rank's training cost, the free first mastery rank refunding nothing), without its gold,
potions or 20% loss.

## How a player uses one

Right-click the scroll in the bag. The client asks the native question "Will you use the selected item?"
(both items carry the mall item-use confirmation in their data). Yes uses the scroll:
- the stat points are back to spend at once;
- the skill window shows every mastery at 0, the skills forgotten and the SP returned.

No relog is needed.

A scroll with nothing to return is refused and kept: a stat reset when no point was spent, a skill reset
when no paid skill is learned and every mastery is already at 0.

## Switches: on and off at any time

| Switch | Scroll | Default |
| --- | --- | --- |
| `SRO_STAT_RESET_SCROLL` | Stat Recall (All Levels) | `off` |
| `SRO_SKILL_RESET_SCROLL` | Scroll of Skill Restore | `off` |

The switches are on with `1`, `on` or `true`, and off with anything else, unset included. They are shard
settings (`internal/config/shardsettings`), set like any other. The build and the GameWorld both read
them:

- **Build.** While a switch is on, the variant build stages that scroll's item. This needs the pinned
  v1.696 client in `SRO_V1696_ROOT`. Each scroll is independent of the other.
- **GameWorld.** The GameWorld reads the switches once at startup.
  - A scroll that is on and whose item the build staged is listed in the Item Mall and usable.
  - A scroll that is on but whose item is missing from the data is not listed and logs one warning. The
    GameWorld still boots.

To turn a scroll **off**, set its switch off and restart the GameWorld. The scroll leaves the Item Mall at
once. Scrolls players already hold stay in their bags; their use is refused and the scroll is kept. Turning
the switch back on and restarting again makes those scrolls usable again.

To turn a scroll **on**, set its switch on for the build and for the GameWorld, rebuild the variant data,
then restart.

Once a scroll has been sold, keep its item in the build (its build switch on) even while the scroll is off
in the GameWorld. A build without the item removes it from the data while players may still hold it.

## How it is built

- **Data** (`scripts/reset_scroll_import.py`, a variant later layer after the gears stones). Each enabled
  scroll is its pinned v1.696 row, identical except for:
  - its identity, allocated by `published_ids` above every object row of the tree. Once a release ships
    the scroll, its id is pinned in `data/published-identities.json`, and every later build keeps it for
    the items players hold;
  - for the skill scroll, its requirement cells: v1.696 limits it to levels 1-100, and the import lifts the
    limit.

  The rows go in their own shard, `itemdata_300000.txt`. The icons are copied from the later client. The
  English names and descriptions are written by this port: the v1.696 stat name has a typo and the skill
  item has no description. A switch turned off removes exactly what its scroll added, including the copied
  icons.
- **Server.**
  - `commerce/resetscrolls.go` holds the table, the switches, the prices and the mall listing.
  - `sro-gameworld/wiring_resetscrolls.go` turns a scroll on only when its switch is on and its item exists.
  - A switch that is off is refused by codename at use admission.
  - The stat scroll goes through the native stat recall family (`progression/statrecall.go`).
    `commerce.StatRecallListed` leaves it out, so listing it does not turn the tier windows on.
  - The skill scroll (type 3/3/13/13, Param1 5) gets its own family, `itemUseSkillRecall`. Its rule is in
    `progression/skillrecall.go`. It shares the refund sums with the herbalist's withdrawal
    (`skillRankRefund`, `masteryRankRefund`) and the effect retirement with it too (`finishSkillRemoval`).
- **Client.** The SP and stats come on their native frames (0x30B3, 0x303D). The new skill list and the
  mastery levels come on a port-only world control, opcode 21 (`foundation/gameplay/skill-recall.ts`).
  The native withdrawal acknowledgements answer an herbalist request in flight and cannot carry a whole
  build.
- **Release protocol 20.** Older browser tabs end the session on an unknown control, so the protocol moves
  to 20 and an open protocol-19 tab gets the 426 and reloads once.

## Decisions recorded

Port choices and inferences, not native evidence:

- **The skill reset is a full restoration.** Neither binary serves Param1 5. The rule is inferred from the
  item and from the native one-rank withdrawal: the same refund, for the whole build, without gold or
  potions.
- **Free basic skills stay.** They cost nothing, so a reset has nothing to return for them.
- **The import runs only while its switch is on.** With it off the build is byte-identical to native.
- **Authored texts.** "Stat Recall (All Levels)" and "Scroll of Skill Restore" replace the later client's
  strings.

## Known limits

- The tier stat recall scrolls are unchanged: still only sold with the `statrecall` preset, and still held
  to their windows there.
