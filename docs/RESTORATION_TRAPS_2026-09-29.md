# BUG-037: restoration and remaining quest traps

This change adds the two v1.150 special-family restoration potions to the
server and browser client. It continues PR #26 and does not close BUG-037:
the quest-trap execution path is still unimplemented. No production
deployment was performed.

## Restoration contract

The mall and Old Woman potions open the native Cyclical Growth System pane
from inventory or a hotbar. It reuses the mastery tabs and skill grid, with
the native recycle removal buttons. Selecting a learned skill or mastery
opens the authored 300 x 208 removal box. The spinner starts at zero and
sums the prices of all selected ranks, limited by dependencies and current
potion quantity. Opening/cancelling spends nothing. Inventory display
names are localized; the client identifies the exact 3/3/13/0 type family
instead of comparing those names with reference codenames.

The server validates the exact potion reference and every dependent skill,
then commits consumed stacks, learned ranks, SP and persisted hotbars through
one character transaction. A missing intermediate rank, malformed request,
insufficient potion count or overflow refuses without partial mutation.
The old rank's resident effects retire through the common action owner.
Mastery rank 1 to 0 returns zero SP because training that rank was free.

The client admits receipts only for its pending operation. A duplicate
old-ID receipt therefore cannot be mistaken for a second removal. Timeout
keeps the common uncertain-transaction gate closed until synchronization.

## Native evidence

- Client `701D60` sends `74D6`; `701E40` sends `7606`. Both requests contain
  u32 potion reference, u32 learned identity and u8 absolute destination rank.
- `58DEE0` loads `ifskill.txt` Create plus Withdrawal and resizes control 5
  to 364 x 305. Controls 6 and 19 share the name `GDR_SKILL_BG`; both must
  survive composition. `5841D0` and `588AF0` choose the mastery and skill
  recycle button textures. `5DE8A0` sets the mall confirmation to 300 x 208,
  hides gold controls, and moves its action buttons to y=167. `5DF030`
  starts removal at zero and caps it by available potions and dependencies.
- Client dispatcher `74DD15`/`74DD36` routes `B606`/`B4D6` to `75BDB0`/
  `75BCE0`. The newer research server's response opcodes are not portable.
- Server `5169D0`/`516BB0` explicitly whitelist potion codenames. Mall and
  Old Woman use flags 6: potion consumption and full SP refund, no gold.
  The distinct QSP resuscitation potion uses flags 3 and is not implemented
  by this change; it needs its native gold-cost and reduced-refund UI.
- `59F410`/`59F610` validate the learned dependency graph; `59F7B0` counts
  and consumes across bag stacks; `59F8B0`/`59FD60` execute the reduction.
- `410EA0` sums removed skill costs. `410F10` sums mastery SP costs, excluding
  the free first rank. The latter was incorrectly named as a gold function
  in the database; instructions confirmed and the label was corrected.
- `410C80` and `410DF0` calculate gold, separately from SP refunds. These
  were also relabeled. No gold formula was guessed from their old names.

Understood unnamed functions were labeled in both databases, saved through
the database API and checked in saved symbol snapshots.

## Remaining trap evidence

The seven remaining associated-skill item programs contain `qest`, `efr 3`,
`dura`, `lnks` and `trap`. They create world objects, not attached buffs.

- Server `48CCC0` initializes the skill object at its owner. `48CEA0` owns
  300 ms scans, owner-lifetime checks and duration expiry.
- `48D690` matches up to three monster references, stopping at the first
  zero, checks monster ownership, dispatches owner event 29 and retires the
  trap even when the quest handler refuses the capture.
- `48CE60` writes the dynamic-object envelope, type `0x54`, skill ID and
  ordinary object identity/pose. Client `777220` recognizes the `FFFFFFFF`
  reference sentinel and creates `CISkillObj`; `86C440` reads its skill ID
  before the common `852F80` gid/region/position/heading data. `86C420` reads
  the single-spawn state byte. Native visuals resolve the skill-motion
  metadata's object model.
- `8B03D0`, `8B8520`, `8C19E0` handle the lion, pirate and Hun captures.
  Their random domain is `rand()%101`, with threshold
  `50 - max(requiredLevel-playerLevel,0)*3`. Success grants the captured
  item and starts a 20-minute capture timer. Inventory space, already-held
  captures, death and timer expiry have separate quest transitions.
- `8BB5F0` is the Ivy material timer/reward path. It must not be replaced
  with the other quests' immediate captured-monster reward.
- Three promotion trap item rows exist in media, but their corresponding
  promotion quests were absent from the inspected v1.150 questdata rows.
  Their v1.188 scripts cannot simply be exposed as v1.150 quests. Two also
  have empty target lists. This does not make the four ordinary quest
  workflows implemented.

## Verification

- Focused Go restoration tests passed: unequal rank costs, all dependency
  slots, split stacks, full removal, repeated requests, refusal atomicity,
  hotbar repair and authority-store restart persistence.
- Exact-rank lookup tests passed for linked stages and ambiguous roots.
- Seven client restoration tests passed against the shipped modules,
  including multi-rank quotes, shrinking potion stacks and dependency slots.
- `pnpm check source` passed all eight gates, including full Go tests,
  race subset, vet, lint and vulnerability checks.
- The client suite caught an extra catalog scan in the ordinary Skills
  renderer; withdrawal calculations now run only while restoration is open.
  The final `pnpm --filter @sro/client-next check` passed all eleven gates
  in 115.9 seconds. Final `pnpm check source` passed all eight gates in
  140.2 seconds, including the server checks with test caching disabled.
- The authenticated localhost5180 browser test passed using scratch `asd2`:
  GM ground drop, normal pickup, inventory double-click, visible selection,
  confirmation, one potion consumed, mastery reduction and retraining,
  unchanged gold/SP and no browser errors. Screenshots and trace are under
  ignored `.state/bug-batch/withdrawal-*`.
- Cold opening alongside inventory exposed shared window-cache identity.
  Restoration now owns a separate admission cache. Repeated live tests
  passed after this fix, and screenshots were inspected for native geometry.

The current local Agent and GameWorld were rebuilt and deployed through the
local Nomad workflow; both allocations were healthy. Maintained source
changes were hand-edited and formatted with gofmt/dprint.
