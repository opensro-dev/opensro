# Sealed pet equipment

Collars, harnesses and charms have ordinary, Seal of Star, Seal of Moon and
Seal of Sun versions at every degree. They share the degree's icon and the
native sealed-item animated slot shine and yellow tooltip heading.

| Quality | White stats and rolled blue values |
| --- | --- |
| Ordinary | 100% |
| Seal of Star | 150% |
| Seal of Moon | 200% |
| Seal of Sun | 250% |

Blue values are multiplied once when dropped and rounded down to integers.
The stored values survive pickup, equip, trade, storage and relog without
being multiplied again. Per-item blue count limits remain unchanged. Global
cooldown reduction remains capped at 80%; proc chances remain capped at 100%.
Seals also increase the degree's proc damage, but do not change proc recovery.

The three slots reserve IDs `301xx`, `302xx` and `303xx`. The units digit is
degree 1–9; tens digit 0/1/2/3 is ordinary/Star/Moon/Sun. Existing ordinary
references and saved rolls are unchanged. Sealed codenames append `_STAR`,
`_MOON` or `_SUN` to the ordinary codename. No database schema change is needed.

## Drop configuration

Pet equipment still drops only from monsters, through the existing
`pet_gear_drop_factor` policy described in [PET-LOOT-DB-CONFIG.md](PET-LOOT-DB-CONFIG.md).
It inherits the triggering normal-equipment drop's degree and seal tier.
Thus native monster loot tables control seal rarity; there is no second,
independent seal roll and no NPC source. Changing the pet factor changes the
number of pet drops, not their relative seal odds.

## Elemental blues

Fire, cold and lightning add separate fixed-damage hits to each successfully
damaged monster, displayed through the native yellow damage-number packet.
They are no longer hidden inside the ordinary physical/magic damage formula.
They do not recursively trigger counters, leech or chance-to-cast blues.
Their tooltip lines describe these fixed hits, without obsolete 5% status
chances or a random lightning range. Chance-to-cast proc blues remain separate.

Poison's displayed value is its **total damage over four seconds**. It ticks
once per second: 12 means four ticks of 3. Remainders are distributed over the
four ticks without losing damage. Another hit refreshes this pet's poison on
that monster without postponing the next tick or stacking another copy.
Different pets retain independent poison applications. Death, recall, town
entry, logout or an invalid target discards pending ticks. Late simulation
ticks never replay a backlog.

## Building

`scripts/build/shared/portItemRows.mjs` creates all 108 reference rows. Rebuild
the server game-data bundle and refresh the client `pet-loot` asset family
together, then deploy the server and reload the client. Sealed items use the
same owned icons and models; original PK2 archives are unchanged.
