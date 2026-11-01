# Grab pets

Summon the pet from its inventory item, then open its Info command in the pet
command bar. The same window provides Basic Info, Inventory, and Setting.

## Using the window

- Basic Info shows the saved pet name (or No name), remaining rental time, and
  the retail no-stats message. Grab pets do not level or fight.
- Inventory holds the pet's collected items. The initial bag has 28 slots.
  Drag items between the pet bag and player inventory to transfer them. The
  existing inventory authority validates capacity and stack limits.
- Setting has Grab function ON/OFF, Grab only my items/Grab all items, and
  Gold/Equipment/Other items. Confirm saves the selection; Cancel restores
  the last saved selection. New pets initially have grabbing OFF, as in the
  provided reference. Turn it ON and Confirm to start collecting.

Pets follow independently of whether auto-grab is enabled. Following and
pickup approach use the server's terrain and wall checks. Items go into the
pet bag; gold credits the player's gold balance without occupying bag slots.
Only legally available drops can be picked: All items does not override a
different player's temporary ownership reservation. Party-shared drops honor
the existing party pickup rules. Quest-special drops are excluded.

The bag, name, and settings remain attached to the summoner item when the pet
is dismissed or the player logs out. A full pet bag disables automatic pickup;
clear space and enable grabbing again. Expiry dismisses the pet and keeps its
stored items. Lease renewal restores access to that same pet.

Summoned item icons show the retail animated edge; dead or expired item icons
have the retail blue wash. As in the original server, monsters ignore grab
pets (their hostility check refuses a grab pet first), so direct and area
monster attacks cannot damage them.

One server option is port-only and off by default; the game server reads it
once at startup. Set `SRO_PET_PACING=1` to move grab and growth pets at 80% of
their run speed; walking and mount speeds are unchanged, and the native follow
and battle speed rules still compare the unpaced speed. Unset, pets move at
their native speeds.

## Evidence and implementation

The provided screenshots and the v1.150 client's window resources define the
window: `resinfo/ifcos.txt`, `ifcosinfo.txt`, `ifcosinventory.txt`, and
`ifcossetup.txt` in the extracted client media. Character data classifies grab pets
as COS band 4 and supplies their movement speeds. The existing implementation
cites native command, record, filter, and slot-overlay handlers; those prior
disassembly claims were not independently re-disassembled for this change.

[Joymax's October 2007 pet announcement, reproduced by MMORPG.com](https://www.mmorpg.com/news/new-pets-2000063909)
describes ability pets collecting nearby items into their own inventory.
The packet and UI details here come from the extracted client and existing
repository contracts, rather than later private-server bot features.

The movement repair compares the terrain height at the integer precision used
by destination packets. It still revalidates the rounded horizontal endpoint
against collision, so the repair does not bypass walls. Combat exclusion
follows the original server.

## Quick in-game check

1. Refresh the browser after the server update, log in, and summon the pet.
2. Walk away: the pet should follow across ordinary slopes.
3. Open Setting, choose ON and the desired categories, then Confirm.
4. Drop an ordinary potion and equipment item nearby; check the pet Inventory.
5. Disable Other items and repeat the potion drop: it should stay on the ground.
6. Transfer a collected item to your own inventory, dismiss and resummon, and
   verify the remaining bag contents and settings survive.

## Clock of Reincarnation

Right-click the clock to arm the retail yellow cursor (A6), then left-click
a previously summoned grab-pet item in your bag and confirm Yes. Escape or
right-click cancels targeting without consuming the clock. The
standard ITEM_COS_P_EXTENSION adds 28 days; ITEM_COS_P_EXTENSION_1D adds one
day, using the retail item parameter in minutes. A valid use consumes one
clock and adds time to max(current expiry, now). It preserves the pet name,
settings and stored items; a dormant pet stays dormant until summoned.

The clock accepts grab pets only. Clicking any occupied item slot opens the
confirmation; confirming an unsuitable item produces the native chat refusal
without sending an item-use packet. Grass of Life requires a dead attack pet
and cannot renew a grab pet. Grab pets retain their native target health gauge
but ignore periodic damage and resource debits. Rental expiry still dismisses
them. Renewal changes rental time without resetting health or mana.

The [historical official Q&A reproduced here](https://www.silkroadonline.de/silkroadonline-allgemein/anleitungen-guides/1878-alle-q-a-von-sro-net-zusammengefasst/)
describes the clock renewing the monkey for four weeks and Grass of Life
reviving the wolf. Durations and item families above are checked against the
installed v1.150 textdata, rather than inferred from that forum alone.
