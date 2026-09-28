# Recovery consumables, merchant blessing and party selection

This continues PR #26. BUG-001 has an implemented rendering repair. BUG-037
remains partial while the separate quest-trap execution path is investigated.
These changes are not a production deployment.

## Recovery and gold consumers

The timed item compiler now admits complete `irgc` and `gdr` programs. The
common effect registry owns their installation, replacement refusal, logout
checkpoint, restoration and expiry. Item consumption follows installation.

`irgc` writes independent HP/MP percentage additions. Natural recovery now
evaluates the ordinary parameter keeper with the current standing/sitting base,
equipment options, active effects and abnormal state. This also repairs the
older omission of recovery magic options. A 500-percent addition produces six
times the base rate, following native arithmetic; the authored tooltip's
wording is not used as the formula.

`gdr` increases the winning contributor's gold quantity at kill settlement.
It does not change drop probability, consume another random draw, multiply
ordinary item stacks, or apply the final attacker's/picker's bonuses. The
adjusted quantity enters the existing ground-item transaction and publication.

The licensed catalog exercises 57 timed stat/recovery/gold consumables,
including seven newly supported references. The earlier audit incorrectly
described all 84 special-family rows as TID4 1..3: two restoration items have
TID4 0 and no associated skill. There are 82 rows in the actual associated-skill
family; 75 now have supported speed, detection or timed modifier programs.
Seven quest-trap rows still require the separate world-object/quest-event path.
Two of those author an empty target list and cannot trigger in the inspected
native loop. This distinction is evidence, not a claim that the remaining
quest workflows are implemented.

## Party selection

The quick-party selection texture has an opaque interior. It was submitted
after the name, gauges and portrait, covering them on selection. The client now
submits it as row backing before the member's content. A production-UI test
checks the selected member's glyphs and gauges remain after that backing in
the actual draw list. The existing browser overlay test also passes.

## Native evidence

- Server `58893E` stores `irgc` at descriptor +2D0. `595A33..595A93`
  converts its unsigned words and writes PercentSum to parameters 25/26.
- Server `4A9D00` supplies posture source 2: flat 0.8 standing or 8 sitting.
  `4E2990` reads those evaluated parameters, truncates recovery and caps each
  pulse at half the gauge maximum.
- Server `587C58` stores `gdr` at +4C8. `59631F` writes parameter B6.
  `4C40EA..4C41AD` reads the reward owner's B6, spills B6/100 to float32,
  then multiplies gold stock before publication.
- Server `48D900..48D999` matches up to three nonzero quest monster IDs,
  checks quest ownership, dispatches owner event 29, and retires the trap.
  `48CEA0` supplies the world-object scan and lifetime. These are not ordinary
  attached-buff programs and remain outside the new compiler admission.
- Client `5BA262..5BA294` reads the existing member-name control and selects
  its resolved entity. It does not erase the label. The ordering repair is a
  port rendering correction; exact native child draw order is not claimed.

Inspected unnamed functions were labeled, including the native character tick
initializer, gold drop publisher, skill-object recipient/attack helpers, and
quick-party control, selection, portrait and warning helpers. Database saves
use Binary Ninja's database API, never raw executable export.

## Validation

- Focused Go tests with `SRO_REQUIRE_GAME_DATA=1` passed compiler admission,
  all 57 shipped timed programs, recovery cadence, actual HP/MP pulses,
  restoration and expiry. Missing licensed assets therefore fail rather than skip.
- Gold tests passed both effect lifetime and a real fatal-hit settlement where
  the earlier contributor receives its 50-percent bonus on a 28-gold heap.
- `pnpm check source` passed all eight tasks, including the full server gate.
- `pnpm --filter @sro/client-next check` passed all eleven client gates.
- The selected-party UI regression passed. Its first invocation used the
  repository root and failed with an asset-path `ENOENT`; rerunning from the
  client package, as its fixture requires, passed.
- The existing Chrome browser party-overlay test passed against localhost5180.

Changed maintained code was hand-edited and formatted with gofmt/dprint. The
other checkout's in-progress loot work was not edited.
