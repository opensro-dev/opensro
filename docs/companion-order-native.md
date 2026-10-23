# Companion order callback audit

This bounded finding belongs to #219. It does not establish equivalence of the
complete companion AI graph.

## Native evidence

The rules below were read from the original machine code; the raw
instructions, not the function labels, establish each behaviour.

| Owner | Instructions and behavior | Coverage |
| --- | --- | --- |
| COS attack dispatcher | Submits AI event `0x19` with the target GID, checks the owner pointer, then retires the owner's effect mask 2. The event return value is not tested before retirement. | Owner-effect side effect implemented and tested for first and repeated orders. Existing admission/refusal predicates remain separate. |
| Attack event | Sets the target, then enters state 3 when target admission succeeds. | Traced, not a complete state-transition proof. |
| Follow event | Enters state 8 through the same transition. | Existing follow/cancel tests retained; no new follow behavior in this fix. |
| State transition | Invokes old-state exit and new-state entry callbacks. | Complete callback bodies and repeated-order timing remain to audit; this patch does not claim the port's retained strategy is equivalent. |

The raw instructions establish the unconditional owner-effect retirement
after event dispatch.

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

### Direct orders against another companion

The native dispatcher explicitly handles a COS victim: it checks whether
the target is a COS and substitutes that object's owner for permission
checking, while the attacking companion's own validator still runs. Once
admitted, the retained target GID goes to event `0x19`, so permission
through an owner does not redirect damage to that owner.

The command path used the player/monster-only resolver, silently rejecting a
companion GID, while ongoing combat already had a companion-aware resolver.
Command admission now uses that existing resolver and preserves its target
identity. Existing attacker, victim, owner-body and owner-permission gates
still apply. The order fixture sends the real command, asserts the stored
companion GID, then advances combat until that companion loses HP while its
owner's HP stays unchanged. Eleven controls refuse dead, dismissed, mounted,
pickup, protected, hidden, owner-protected, own, missing, party and same-cape
targets. The original command resolver fails the live-companion case.

The v1.150 refusal-response wire and the complete callback graph are outside
this change; existing silent-refusal behavior is retained.

### Broader callback graph

Issue #219 remains open for the complete callback graph, target-switch timing,
state-entry/exit effects and lifecycle cleanup. The bounded fixes above cover
owner-effect retirement and direct companion-target admission. Native follow steering
and formation fixture boundaries remain those in `guild-soldier-native.md`.
