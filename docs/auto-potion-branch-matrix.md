# Auto-potion branch and lifecycle verification

This matrix records the application-level audit completed on 2026-10-06.
The scope is the v1.150 client's auto-potion entry points and its complete
shipped recovery/cure catalog, with v1.188 server mechanics restricted to
those references. It includes the downstream item-use, receipt, recovery,
configuration and actor-lifetime boundaries. It does not claim formal
whole-program equivalence of the native executables or their runtime libraries.

The binary hashes and the earlier repair/stall integrations are in
[auto-potion-parity.md](auto-potion-parity.md). Native instructions, oracle
sources and run records are retained in the research evidence directory
`docs/work-items/world/auto-potion-evidence-2026-10-05` in the sibling research
checkout. Runtime code does not import that directory.

## Entry, configuration and dispatch

| Branch or transition | Native evidence | Port owner and verification |
| --- | --- | --- |
| Default configuration and zero-field substitution | 7783E0, 70C3D0 | `auto-potion.ts`; bootstrap/default and every packed-word corpus tests. |
| HP, MP and cure enabled bits, slot decoding and unsigned thresholds | 70C3D0, 70C220 | All 65,536 packed words, all channels; float32 rounding and unsigned threshold cases. |
| HP/MP below, equal to and above threshold; LIFE bit; abnormal inhibition | 70C550, 70C5D0, 70C640 | Original check-function branch partitions; `auto-potion.test.mjs`. |
| Active channel flag differs from registered retry timer | 70C550..70C640, 77A080 | Matching vitals notifications arm inactive channels; frame/save alone do not. Recovery/damage cannot bring an armed retry forward. |
| Custom versus default delay, zero delay and changed configuration | A00B30, A00BE0 | Timer registration/scheduler oracle; 1 ms zero clamp; active, registration and configured-period transition tests. |
| One due callback per update, callback clock movement and unsigned wrap | A00BE0 | Earlier original scheduler oracle (1,536 cases) and registration oracle (49,152 cases); timer transition tests. |
| Draft open/edit/cancel/apply, disabled sliders, re-enable and empty cure combos | Native option/potion control evidence in the archived UI audit | `auto-potion.ts`, `auto-potion-input.ts`, production `game-options.test.mjs`: clamp, committed-value restoration, cancel, save and persistence. |
| Save enqueue failure, unchanged save, character bootstrap and durable restart | 70C3D0 and native save/bootstrap receivers | Failed enqueue does not commit; unchanged settings suppress traffic; server `autopotion_persistence_test.go` reopens the store. |
| Binding is empty, skill, equipment, other consumable or a valid potion | 70C300, 5729E0, 5503A0, 572770 | Every 65,536 type word; `autoPotionItemSlot`; runtime refusal tests. |
| Exhausted stack replacement | 573390 | Same-reference native rule documented. Strongest usable HP/MP replacement remains the user's explicit exception. |
| Selected/dragged control, Item Mall, NPC shop/storage, visible stall, return/repair delay | 572770, 561D50, 6961B0 and archived interaction-helper instructions | Existing UI/inventory owners supply gates; refusal notices and armed-retry retention tested. Stall naming/network are separate from visible owner/visitor stalls. |
| Disabled item/CanUse and special high-bit confirmation | 561D50 | The complete 64-reference census has nonzero CanUse. The high-bit confirmation branch additionally requires TID3=13, excluded by the auto-potion filter. |
| Flag-war marker gate | 561DAA, 4B1589 | Dormant in the audited v1.150 lifecycle: reset writes FF; all state references and event receiver inspected; no non-FF activation writer found. |
| Inventory request pending, lost reply, failure, successful final unit and stale receipt | 6961B0, 755E40 | Inventory is the one transaction owner. Pending/lost-receipt tests, exact success debit validation, cooldown tests and connected capture. |
| Authored cooldown group, zero-group item reference, final unit, icons and expiry | 80B870, 755E40, 565400 | All 20 description pairs, last declaration wins; category 18 receipt/icon tests. Recovery use admission still follows native potion categories. |

The older core oracle's 203,520 comparisons stop at use-dispatch and timer
service boundaries. They are evidence for the core predicates, not evidence
that all downstream services ran inside that oracle. The remaining rows here
identify their separate evidence and tests.

## Complete authored item census

`TestAutoPotionEntireShippedCatalog` enumerates the licensed catalog through
its real reference loader. It checks every admitted reference, rejects any
new unaudited family, and validates recovery amounts at levels 1, 5, 20 and 90.
The census passed with `SRO_REQUIRE_GAME_DATA=1`: **64 references, 11 families**.

| Type IDs | References | Downstream behavior and tests |
| --- | ---: | --- |
| 3/3/1/1 | 12 | Player HP: absolute and percentage amounts, race reuse, full gauge, reductions, Zombie and queued recovery. |
| 3/3/1/2 | 12 | Player MP: independent lane, percentage/absolute recovery, client icon expiry before server reuse. |
| 3/3/1/3 | 17 | Dual HP/MP: both queues and reuse-lane interaction; food/event/quest-named items stay in their authored family. |
| 3/3/1/4 | 3 | Companion HP; selected owned live COS, class exclusions, pet keeper/reductions and refusal codes. |
| 3/3/1/6 | 2 | Companion revival requires the summoner-drop context; automatic activation without it refuses. Manual server revival, identity, dormant/live pet and result ordering covered. |
| 3/3/1/8 | 3 | Berserk recovery; active berserk refusal and successful point change. |
| 3/3/1/9 | 1 | Companion food; selected owned live pet, authored percent and the native 99-percent display refusal boundary. |
| 3/3/1/10 | 3 | Fortress repair; existing war/holder/role/structure authorities, target/refusal ordering, all grades, linked pulses and delay UI. |
| 3/3/2/1 | 5 | Player universal cure; grade/level limits and its distinct reuse lane. |
| 3/3/2/6 | 4 | Player authored abnormal-mask cure; successful empty cure still consumes and locks its lane. |
| 3/3/2/7 | 2 | Companion cure; owned selected pet's abnormal block, with player state unchanged. |

Family tests are in `itemuse_admission_test.go`, `cure_test.go`,
`cositem_test.go`, `cosfeeding_test.go`, `cosabnormal_mechanics_test.go`,
`berserk_test.go`, and `fortress_repair_test.go`; client targeting and receipt
coverage is in the auto-potion, COS item-use/command/clean and cooldown suites.
Families 3/3/1/5 and 3/3/1/7 have native dispatch arms and port handlers but no
shipped references in this catalog; they must not be counted as missing items.

## Queue algorithms and lifetime

The expanded oracle executes **35,424 calls across 2,528 scenarios** using
unaltered bytes from the pinned server executable. Original routines run for
admission (49A5B0), front-only ticking (49A510), pulse sizing (49A0C0), promised
credit (49A110), node construction (4A0F30), capacity increment (4A0FE0), erasure
(4A0AF0) and list clearing (695450). The Go corpus test checks the credited HP/MP
and both complete queue contents after every admission and tick. All matched.

The harness intercepts allocation/free, actor vital getters, the final keeper
credit sink (4A86A0) and diagnostic dumping. It does not intercept the queue
or container algorithms. Every emulation must reach its return sentinel;
instruction-budget exhaustion fails. Linked-list forward/back links and counts
are validated. Seeded lists include empty, single and multiple entries with
remaining counts 1..4, exact-full and overflow boundaries, independent HP/MP,
integer step limits and mixed deterministic random cases. Keeper reductions
are checked separately through the action/combat tests.

| Lifetime or timing boundary | Verification |
| --- | --- |
| Absolute potion: one immediate pulse and four queued pulses | Native corpus plus `TestPotionPulsesUseLiveKeeperAndResidentTick`. |
| HP accepts an exactly full promised gauge; MP requires room | Native corpus, including all front/successor list shapes. |
| Percentage potion clears an already overflowing gauge or retains the first overflowing queued entry and removes successors | Native corpus executes original erasure and clear helpers. |
| Second use accepted before first potion finishes; 1,099 ms refusal / 1,100 ms acceptance for Chinese absolute recovery | `TestPotionReuseCanPrecedeFinalRecoveryPulse`; real WebSocket `TestPotionTransportConsumptionAndResidentLifetime`. |
| Client/server reuse differences for races, percentage recovery and cures | `TestItemUseRecoveryCooldownLanesAndExactExpiry`, independent-lane/concurrent replay tests, client cooldown corpus. |
| Late host update advances one pulse; same timestamp cannot replay; overdue phase survives | `TestPotionQueueOverdueTicksAdvanceOnePulsePerHostUpdate`. |
| Cure/reduction changes between pulses; Zombie admission versus ordinary queued pulses | `potionrecovery_test.go`, `cure_test.go`, combat recovery tests. |
| Death, disconnect, replacement actor/session and final retirement | `TestPotionQueueRetiresWithCharacterLifetime`; repair lifecycle scenarios; WebSocket/store test. |
| Same-resident re-entry preserves queue; displaced session close cannot retire replacement | `TestPotionQueuePreservesSameResidentAndRejectsStaleRetirement`, using production actor binding before recovery binding. |
| Durable HP, inventory debit and reuse lock survive store reopen; transient queue does not | `TestItemUseCooldownSurvivesStoreRebootAndSnapshotMutation`; native-profile WebSocket/store test. |
| Client death/reset/dispose/reconnect replaces transient timers and pending state | Runtime auto-potion tests, repair-delay tests, connected browser reconnect. |
| Companion representative removal, unknown despawns, selection changes and class zero | COS selection/command/clean runtime suites and archived native lifecycle evidence. |

The lifecycle fixture initially omitted the actor binding that owns stale
session-close rejection. It failed, was corrected to follow
`wiring_sessions.go` (BindPetSession before BindRecoverySession), and passed.
No runtime change was warranted by that incomplete fixture.

## Connected verification and profile boundaries

`auto-potion-live.test.mjs` uses the normal authenticated dock/world entry and
passive Playwright WebSocket events. No served source, headers or server
responses are rewritten. On the existing beta server it observed three
accepted automatic MP uses, three exact stack debits and category-2 cooldowns.
The accepted receipt intervals were 1,988 and 2,020 ms with a 1,000 ms retry:
the server's additional 100 ms reuse guard refuses the intervening attempt.
Disabling stopped consumption across three retries; reconnect retained disabled
settings and did not restart consumption. The full-gauge case deliberately
checks native accepted use even when no MP recovery can be queued.

The existing beta server refills HP/MP stacks on entry (`starterrefill.go`,
introduced in #87). This is an operator profile, not a newly added parity
exception. The probe requires `SRO_POTION_EXPECT_BETA_REFILL=1` to expect it;
without that setting, any refill fails the persistence assertion. Strongest
replacement is still the only exception authorized by this auto-potion task.

The deployed server identifies source revision `ab43b4a5` with modified build
inputs, so that browser capture alone does **not** certify the PR's server.
The independent loopback transport test runs this checkout's Go action owner,
real WebSocket envelope/receipt delivery and durable store, with no beta refill.
It checks immediate and queued HP, exact reuse boundaries, overlapping use,
retirement, and the persisted 20-to-18 stack debit. Its authentication callback
and actor binding are fixture boundaries; it does not retest the login service.

## Reproduction and conclusion

- `SRO_REQUIRE_GAME_DATA=1 go test ./internal/game/action ./internal/game/enterworld ./internal/game/item/recovery -run 'Test(AutoPotion|Potion|ItemUse|MPRecovery|ShippedPet|Cure|Zombie|StructureRepair|OriginalMachineQueue)' -count=1`
- `go test -race ./internal/game/action -run 'TestPotionTransport|TestPotionQueue|TestPotionReuse' -count=1`
- `pnpm --filter @sro/client-next check`
- `pnpm check source`
- From `apps/client-next`, with the owned Vite URL in `SRO_PROBE_CLIENT_NEXT_BASE_URL`: `SRO_AUTO_POTION_LIVE=1 SRO_POTION_EXPECT_BETA_REFILL=1 node --test tests/browser/auto-potion-live.test.mjs` (set environment variables using the host shell).
- `node --test tests/browser/game-options.test.mjs` against that same Vite server.

The catalog, mapped application branches and listed lifecycle transitions have
no known unported behavior after the existing fixes. The additional pass found
no new runtime defect. This conclusion is supported by instruction review,
finite exhaustive core inputs, expanded queue comparisons and independent
integration tests; it is not an unbounded whole-call-graph machine proof or a
live original-client/original-server session capture.
