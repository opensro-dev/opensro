# BUG-037: restoration and remaining quest traps

This work adds native restoration UI, the herbalist service and the remaining
quest-trap infrastructure. It continues PR #26. BUG-037 remains open while
live acceptance is being completed. Trap NPC workflows, Ivy gathering,
guardian spawning and service wiring are implemented. No production deployment
was performed.

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
  QSP resuscitation uses flags 3: potions and gold, with a rounded-up 80-percent
  SP refund. The implementation now uses the native `dg.txt` gold minima,
  separate from the unrelated `levelgold.txt` table, and its 300 x 244 mode-2
  confirmation retains the authored gold controls.
- `59F410`/`59F610` validate the learned dependency graph; `59F7B0` counts
  and consumes across bag stacks; `59F8B0`/`59FD60` execute the reduction.
- `410EA0` sums removed skill costs. `410F10` sums mastery SP costs, excluding
  the free first rank. The latter was incorrectly named as a gold function
  in the database; instructions confirmed and the label was corrected.
- `410C80` and `410DF0` calculate gold, separately from SP refunds. These
  were also relabeled. No gold formula was guessed from their old names.

Understood unnamed functions were labeled in both databases, saved through
the database API and checked in saved symbol snapshots.

The herbalist service now admits all five native potion merchants at level
20. Opening the restoration window does not accept or finish the collection
quest. Heart exchange processes all complete ten-heart groups in one atomic
transaction, capped at thirty held restoration potions, including split stacks.

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
- `8BA700` handles the Informant Capture Trap in **Ivy 1**, granting
  `ITEM_QNO_EU_IVY_1_01` and starting a **30-minute** timer. The trap item
  is named for **Ivy 2**, which supplies it; the item name does not identify
  the capture objective. Ivy 1 also requires fifteen guardian kills.
- `8BB5F0` belongs to Ivy 2's **Small Knife / Vine Stalk** material path.
  It is not the capture handler. `8B9B60` advances Ivy 1's capture timer
  and also spawns a guardian near the character outdoors. This now uses the
  real monster population adapter.
- Three promotion trap item rows exist in media, but their corresponding
  promotion quests were absent from the inspected v1.150 questdata rows.
  Their v1.188 scripts cannot simply be exposed as v1.150 quests. Two also
  have empty target lists. This does not make the four ordinary quest
  workflows interchangeable with those unavailable promotion scripts.

## Current trap implementation and remaining integration

- Complete seven-row descriptor admission, native dynamic-object wire format,
  GID ownership, population-generation isolation, scan cadence, strict 3D
  radius, target-owner checks, expiry and reliable scope reconciliation.
- Browser single/group spawn and despawn decoding, plus a presentation owner
  that joins skill IDs to their native object resource. The asset compiler now
  retains animation-set object resources independently of cast-stage models.
  The trap model is published and rendered in the live browser. Its small
  skill-to-object lookup lives in the model manifest: loading the entire
  20 MB effect catalog through a 16 MB generic request failed live acceptance.
  The compact table fixes that ownership mismatch without raising limits.
- Capture transactions for the four ordinary families use the real inventory
  planner and the native `rand()%101` threshold. Monster retirement must succeed
  before any item, objective or timer is committed. Full bag, wrong target,
  inactive quest, repeated capture and failed world retirement are tested.
- Captured-item timers persist through serialization, warn at ten/five minutes,
  remove expired captures without abandoning the quest, and permit recapture.
  Death cleanup is connected to the shared fatal transition. These paths have
  focused tests. Ordinary capture definitions and daily NPC supply workflows
  are published into the runtime catalog, with persistent world-day allowances
  and selection-bound confirmation identity.
- `CaptureQuestTrap` and `CanPlaceQuestTrap` are wired in service composition.
  The actual item-to-world integration test covers refusal without debit,
  one-item placement debit, overlap refusal and exactly-once capture.
- Ivy 2 requires active Ivy 1, supplies the knife/vine objective and awards
  five informant traps. Native ten-second gathering uses `36BD`; client
  `6FFE50` sends `775D` with the four-byte quest ID, and `766950` handles
  `B75D` cancellation. The native collection gauge and cancellation notice
  passed authenticated browser acceptance. Death, disconnect, abandonment
  and explicit cancellation cannot award a late item.
- Live testing caught an unpublished collection-gauge texture. The shared
  runtime CIF image catalog now publishes it. Trap model publication is
  followed by rebuilding the EasyFX closure, including its attached
  `system/system_capture_trap.efp` emitter.

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

The newer quest-trap and batch-exchange build reached healthy local Agent and
GameWorld deployments. An isolated `bug037/TrapProbe` character was created
through store APIs; the existing four test characters were preserved. Native
quest definitions and the inventory planner seeded test inputs, while live
actions remained server-owned. The final herbalist confirmation and clean
attached-effect rerun remain outstanding at this checkpoint. Maintained
source changes were hand-edited and formatted with gofmt/dprint.

Latest validation: `pnpm check source` passed all eight tasks in 79.4 seconds,
including full server tests, race checks, vet, lint and vulnerability checks.
Focused gathering/return tests passed 5/5; object asset/presentation tests
passed 3/3. `verify:quick` and test type checking passed. The latest full
client run passed ten gates and 1964 of 1965 unit tests, but failed with
`Unbound published item /assets/char/weapon/eu_m_darkstaff_01.glb`. Shared
generated assets contain another chat's newer darkstaff output while its
matching source changes remain uncommitted in `rebuild`. Those changes and
assets have been preserved. A current full client pass is still required.

Native annotations were saved with the database API and checked in saved
`PE/symbols`: client snapshot 193 and server snapshot 259. Every exposed
unnamed function, including incidental constructors and exception handlers,
was labeled. Structural labels use a field offset when its narrower semantic
role is not established. `6FFE50` was corrected from an inaccurate skill-arrival
name to the delay-cancellation dispatcher after examining instructions.

The shared checkout's validated Nomad 2.0.4 deployer was used. This branch's
2.0.7 check was not weakened. Temporary node metadata adds `bug037` to local
placement; retire that grant with the acceptance shard after verification.
Evidence is under ignored `.state/bug-batch/`: `gathering-active.png`,
`trap-live.png`, `quest-live.json`, and the earlier `withdrawal-*` artifacts.
