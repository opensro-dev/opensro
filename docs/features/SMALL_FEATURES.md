# Small features

Short notes for features that need no guide of their own.

## Grab pets

Summon the pet from its inventory item, then open its Info command in the pet
command bar. The same window provides Basic Info, Inventory, and Setting.

### Using the window

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

### Clock of Reincarnation

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

## Pet owner nameplates

Every companion whose spawn carries an owner name (attack and loot pets,
trade transports, guild companions) shows that name on a separate, centered
row 20 pixels above its own name, while the Options window's "Monster
names" box is on. With the box off the row is hidden, even on a hovered
pet. The row also follows the pet's name visibility (distance, hidden pets,
ridden companions). Pet status bars stack above the owner row, and drop
back to their usual height when the row is hidden.

The row copies the visible owner's current name color, including party and
special character colors. If the owner is outside the visible entity set,
the spawn's owner name still appears in white. The pet's own name retains
its existing color. Joining, leaving, or dissolving a party updates the
label through the existing entity snapshots; no server or protocol change
is required.

## GM training dummy

Enable `SRO_GM_TRAINING_DUMMY=on` and restart the GameWorld. It defaults to
`off`; the Nomad variable is `gm_training_dummy`. Refresh/reconnect the browser
after enabling it so it receives the new reference catalogue.

As a GM, open the console with **Shift+`** and enter:

```text
/TRAININGDUMMY 21
```

The number is the dummy's level, from 1 to 140. Omitting it spawns level 1.
It appears at your position. Step aside before selecting it.

- Uses the existing wooden Mangnyang body, named **Training Dummy**.
- Has 100,000,000 HP, zero defense and zero elemental resistance.
- Never walks, chases or attacks. Knockback/knockdown cannot displace it.
- Accepts damage, freeze, stun, poison and other normal combat statuses.
- Can die, including through execute, but awards no EXP, SP, loot, berserk
  charge or quest-kill progress. It has no nest or respawn.
- Select it and use `/MOBKILL 1` to remove it. A server restart also removes it.

The native GM command also works:

```text
/LOADMONSTER MOB_GM_TRAINING_DUMMY_021 1 NORMAL
```

For a clean Blizzard test, cast once and order **Follow** immediately after
the initial hit. Keep the target within the storm circle. Expected result:
one initial hit followed by three yellow damage ticks, one second apart.
The dummy does not test pet survival or effects requiring incoming damage.

This is a GM test tool, not a normal monster or a replacement for live combat
balance tests. Original monster references and game archives remain unchanged.

## Beta mastery allowance

The game-world operator switch `SRO_BETA_MASTERY=on` gives both Chinese and
European characters a **5,000 total mastery-level budget**. This is an explicit
beta policy, not a native rule. Individual mastery ceilings, character-level
requirements, skill prerequisites and SP prices remain unchanged.

The Nomad game-world job enables it by default for the current beta through
`beta_mastery = "on"`. Localhost and production use this same job and code.
Directly launched game-world binaries retain native rules unless the environment
switch is enabled.

To return to native rules, put `off` in `<state-dir>/beta-mastery.txt`
(`apps/server/.state/cluster/beta-mastery.txt` with the default layout) and
deploy normally. The deployer passes this persistent setting to the Nomad
job's `beta_mastery` variable; it survives future releases. Put `on` in the
same file to re-enable beta limits. A missing file defaults to on for this beta;
an empty or invalid file refuses deployment. For a directly launched binary, unset
`SRO_BETA_MASTERY` or set it to `off`, then restart. Native total allowances
are Chinese **300** and European **min(2 × character level, 240)**.

The game world reads the switch once at startup and publishes its override
with every world-entry bootstrap. The browser skill window uses that value;
there is no separate client build flag. Missing configuration uses the existing
native display calculation, including level changes.

Changing the switch does **not** delete learned masteries, skills or spent SP.
A character above the native total can still enter the game, but further mastery
training is refused until the total is within the restored allowance. An official
launch character reset, if desired, is a separate operator decision.
