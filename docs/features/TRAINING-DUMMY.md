# GM training dummy

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
