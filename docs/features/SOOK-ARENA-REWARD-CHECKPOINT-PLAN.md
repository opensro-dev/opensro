# Survival arena: reward chest and extraction checkpoints

Historical design: the implemented rules and current operator settings are in [SOOK-SURVIVAL-ARENA.md](SOOK-SURVIVAL-ARENA.md).

Status: historical initial proposal, superseded by the five-stage changes requested later on 2026-11-09. Current rules: SOOK-SURVIVAL-ARENA.md. Current evidence: SOOK-SURVIVAL-ARENA-CLAUDE-HANDOFF.md.

## Reference and goal

The [Fortis Colosseum](https://oldschool.runescape.wiki/w/Fortis_Colosseum)
offers accumulated rewards between waves: claim and end the run, or continue
and risk them for better rewards. Its
[reward chest](https://oldschool.runescape.wiki/w/Rewards_Chest_%28Fortis_Colosseum%29)
collects the wave rewards. The source page could not be fetched directly;
the linked wiki's indexed descriptions confirmed these mechanics.

Our adaptation should remove ordinary ground loot during combat, preserve
the beneficial and harmful eggs, and introduce a clear cash-out decision.
The following numbers and rules are proposals for OpenSRO, not OSRS rules.

## Recommended player experience

1. Keep private instances, three daily entries, the 20-second opening,
   the 20-minute combat timer, level progression and the 15-minute DOOMS DAY.
2. Ordinary arena kills grant EXP/SP and kill credit, but create no ground
   gold, equipment, materials or consumables. Eggs remain real pickups.
3. Use checkpoints around combat minutes 5, 10 and 15. At a boundary, stop
   new waves and let the player clear the remaining monsters. Clearing still
   uses combat time; never open a blocking rewards dialog during combat.
4. Once clear, suspend spawns and pause the combat timer for a maximum
   30-second decision. Freeze arena power-up/buff clocks consistently during
   this screen; do not let waiting recharge skills or generate more eggs.
   Therefore wall-clock duration can exceed 20 minutes by the decision time.
5. Show a native-style rewards window: stage, kills, time remaining, earned
   gold, chest contents with ordinary item tooltips, next-stage difficulty
   and an explicit warning about rewards at risk.
6. Offer **Claim & Leave** or **Continue**. A decision timeout safely claims
   and leaves. Continue starts a 10-second countdown, then resumes spawning.
   The final stage uses the existing DOOMS DAY pacing and population cap.
7. Surviving the 20-minute combat deadline automatically claims and returns
   the player. No requirement to kill every final-stage monster.

Suggested screen:

```text
SURVIVAL ARENA                         Checkpoint 2
Kills: 143                       Combat time left: 10:00

Guaranteed gold: ...             Chest at risk: ...
[item] [item] [item] [item]       Hover for full stats

Next: stronger monsters and better chest rewards.
Death forfeits chest items. Guaranteed kill gold remains.

[Claim & Leave]                         [Continue]
```

## Rewards and risk

- Preserve the existing kill-bracket gold reward on death or completion.
  Pay it once on extraction too; do not add a second payout path alongside
  `endSookArena`.
- Chest items and bonus rewards accumulate after each completed stage.
  Continuing risks the entire unclaimed chest; death forfeits it. This
  differs deliberately from forfeiting the existing guaranteed gold.
- Never risk the character's carried equipment or inventory.
- Roll a bounded stage reward bundle from existing appropriate item/degree
  pools, rather than banking every discarded monster drop. Later stages
  offer more/better rolls. Keep rolls stable across reconnects and previews.
- Preserve configured item/seal rules; do not silently guarantee Sun items.
- Preview the next stage's possible reward quality, not a reroll button.
- Keep reward weights, gold bonuses, chest capacity and checkpoint timings
  server-owned and configurable through the existing configuration pattern.
  Start with no additional gold bonus until economy testing is complete.
- If inventory is full, keep the entitlement claimable through So-ok after
  returning. Do not drop the chest, discard rewards or force-delete items.

## Implementation order

1. **Settlement interception:** inspect all ordinary monster drop producers,
   including pet kills, reflect, KABUM and Fire Pit. Suppress ordinary loot
   only for the exact active arena lease/generation. Keep kill/EXP/SP and
   egg scheduling. Ordinary overworld drops must remain unchanged.
2. **Run state:** add explicit combat, clearing, checkpoint, countdown and
   ended phases to `internal/game/sookarena`. Pause relevant clocks through
   the existing simulation clock; no wall-clock sleeps or parallel timers.
3. **Reward ownership:** persist run identity, completed stages, rolled chest
   and claim state through the typed store. Introduce the next schema only
   after checking current main; preserve historical schema manifests.
4. **Exactly-once delivery:** one authoritative transaction settles the chest,
   gold and run completion. Reject stale/repeated Continue/Claim requests.
   A reconnect at a checkpoint restores its decision; a server restart must
   preserve earned entitlements, not reroll or duplicate them. Keep current
   mid-combat abort behavior initially and document it in the UI.
5. **Client UI and wire:** reuse the character inventory grid, tooltips and
   native dialog styling. Server sends a versioned snapshot and validates
   every decision. Coordinate any protocol change with Agent and GameWorld.
6. **Integration:** remove obsolete per-kill ground-loot multiplication in
   arena settlement and update So-ok instructions, progress notices, end-run
   cleanup and the feature guide. Leave ordinary gameplay and PK2s alone.

## Acceptance tests

- No ordinary arena ground drops from any kill source; all egg types still
  spawn, can be picked up and retain their effects. Overworld loot unchanged.
- Checkpoints cannot open with live attackers; no spawns, damage, recharge
  advantage or time loss during the bounded decision screen.
- Continue resumes the same isolated run with correct scaling and DOOMS DAY;
  claim, timeout and final survival deliver the exact previewed chest once.
- Death keeps guaranteed gold, loses only unclaimed chest rewards and still
  uses the five-second return delay. Original inventory remains untouched.
- Full inventory, disconnect, restart, duplicate requests and stale run IDs
  cannot destroy items, reroll rewards or pay twice.
- Two simultaneous entrants cannot see or claim each other's rewards.
- Long-run crowd test stays below ground-loot pressure; confirm combat and
  egg animations remain visible at the 100-monster limit.

Defer optional difficulty-modifier choices and endless mode until this loop
is stable. They are not required to deliver the requested chest mechanic.
