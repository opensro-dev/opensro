# Companion order callback audit

This bounded finding belongs to #219. It does not establish equivalence of the
complete companion AI graph.

## Native evidence

v1.188 `SR_GameServer.exe`, SHA-256
`bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290`,
image base `0x400000`. Addresses below are VAs; subtract the base for RVAs.

| Owner | Instructions and behavior | Coverage |
| --- | --- | --- |
| COS attack dispatcher `4D2200` | `4D2568..4D2578` submits AI event `0x19` with the target GID. `4D257A..4D2582` checks the owner pointer; `4D2588..4D2592` retires the owner's effect mask 2. The event return value is not tested before retirement. | Owner-effect side effect implemented and tested for first and repeated orders. Existing admission/refusal predicates remain separate. |
| Attack event `5595C0` | Calls `53FFE0` to set the target, then `53FF30` to enter state 3 when target admission succeeds. | Traced, not a complete state-transition proof. |
| Follow event `559600` | Enters state 8 through `53FF30`. | Existing follow/cancel tests retained; no new follow behavior in this fix. |
| State transition `53FF30` | Invokes old-state exit and new-state entry callbacks. | Complete callback bodies and repeated-order timing remain to audit; this patch does not claim the port's retained strategy is equivalent. |

The original 55 bytes `[4D2560,4D2597)` have SHA-256
`3d3f77d4eb4297fd116713d17ff78d627d071a871d390193ebe4e7bc14ae3dc2`.
The raw instructions, not the function labels, establish the unconditional
owner-effect retirement after event dispatch. No database labels were changed.

## Defect and fix

`orderPetAttack` previously returned immediately when its pet already targeted
the requested GID. If the owner acquired an effect cancelled by skill-cast
event bit 2 after the first order, repeating that order left the effect active.
The first accepted order correctly removed the same effect.

The existing character update and effect-retirement operation now runs on
return from every admitted order, including that early return. It still runs
after command processing and under the caller's division lock. Invalid targets
and missing sessions return before scheduling the operation.

Eight behavioral cases cover first/repeated orders, valid/missing targets and
event masks 1/2. The original production code fails only the repeated, valid,
mask-2 case. The corrected code passes all eight. The tests install the effect
through its normal application owner, with a pinned active replacement and
self-application phase, so the event mask is actually live.

## Remaining audit

Issue #219 remains open for the complete callback graph, target-switch timing,
state-entry/exit effects and lifecycle cleanup. This fix changes only owner
effect retirement for repeated admitted attack orders. Native follow steering
and formation fixture boundaries remain those in `guild-soldier-native.md`.
