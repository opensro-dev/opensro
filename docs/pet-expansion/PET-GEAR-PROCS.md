# Pet equipment spell procs

Implemented 2026-10-24. These blues have separate **pet dealing damage** and
**pet taking damage** versions:

|Spell|Effect|
|---|---|
|Chain Lightning|Up to 5 distinct monsters;8 m hops,20% less damage per hop.|
|Blizzard|5 m field,4 damage pulses across 4 seconds. Uses the penguin storm visual.|
|Fire Burst|3 m burst, up to 8 monsters.|
|Poison Cloud|5 m field,4 damage pulses across 4 seconds.|
|Mend|Heals the owner for 5% maximum HP.|

Attack versions generally roll on Collar/Charm; retaliation versions on
Harness/Charm. Existing attack-Mend stays Charm-only. Proc families start
atD3, with stronger chance tiers atD6/D7/D9, matching their existing chance
progressions. Each direction is a separate blue group and may coexist.

## Degree power

|Degree|Base proc damage|
|---|---:|
|D1|100|
|D2|213|
|D3|325|
|D4|438|
|D5|550|
|D6|663|
|D7|775|
|D8|888|
|D9|1000|

This is damage per target for Fire Burst and the first Chain Lightning hit.
Blizzard/Poison Cloud split that total across their four pulses, preserving
the remainder on the last pulse. Mend retains its existing 5% heal. This
proc damage is separate from flat fire/cold/lightning/poison damage blues.
Higher roll chance and degree power are independent. Multiple items add the
same proc's chance (capped 100%) and use its strongest contributing degree.

## Limits

- Only committed damaging pet actions or nonfatal hits taken from ordinary
  population monsters qualify. Physical and magic hits both qualify.
- Full blocks, fully shielded hits, player/PvP hits and brood clones do not.
- At most one proc fires per action, sharing the existing 2 second internal
  cooldown across both directions. Selection order: Chain, Blizzard, Fire,
  Poison, Mend. Chances still roll independently in that order.
- Fixed proc hits use normal credited settlement for loot/EXP. They do not
  generate more attacks, proc rolls, leech or learned-skill stacks.
- Only the correct population, range and line-of-sight are admitted. Area
  pulses hit at most 8 monsters. No player targets.
- At most 4 fields per pet; missed ticks do not replay a damage backlog.
  Death, owner death, town entry, separation or unsummon discard fields.
- Proc state is transient; logout/restart clears cooldowns/fields.
- Dedicated new visuals were not authored for the other gear spells.

## Configuration and stable IDs

`pet_affix_rules` controls min/max **proc chance**, selection weight and
roll power, loaded at shard startup. Setting weight 0 disables future rolls.
Already saved blue rolls retain their values.

Existing attack codes 84–86(Chain),90–91(Fire),96–97(Mend) remain stable.
Previously planned codes 87–89 now implement Blizzard when struck;
92–93 implement Poison Cloud when struck. New paired blues use 220–231
and seed automatically. The local DB metadata for 87–89 and 92–93 was updated
to match their new groups; configured chance ranges and weights were kept.
No schema migration is needed.

Power is authored perdegree in `gear[].procDamage` in `pet-design.json`;
regenerate with `scripts/tools/pet_design_export.py` and rebuild the server
and client after editing. Recovery uses `rules.procInternalCooldownMs`.
