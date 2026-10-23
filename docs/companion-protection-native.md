# Companion attack protection

This is a bounded combat fix found while investigating issue #218, not a claim
that the complete free-PVP transition system has been ported.

## Native evidence

The companion attack validator's instructions establish this order,
independently of the analysis database's function names:

| Step | Behavior |
| --- | --- |
| Missing owner or target | Refuses with `0x3006`. |
| Attacking companion body check | Body mode 2 refuses. |
| Target body check | Body modes 2, 3 and 4 refuse. |
| Protected-body branch | Writes `0x3020`, returns zero. |
| Owner delegation | Only after the body checks, delegate to the owner validator, forwarding the control argument and error pointer. |

## Port and regression

The attack-order path previously consulted only the owner's permission. The
ongoing companion target resolver likewise omitted the attacking companion's
mode and the attack pet's player-victim protection check. Consequently a pet
could accept an invalid order or retain a prepared cast after protection began.

`petAttackBodyAllowed` now supplies those gates to the order and target resolver.
The resolver also serves pursuit, prepared release and secondary area targets.
Its early actor-mode gate includes companion victims. Existing target-specific
guild-soldier and companion body checks remain in place. Existing cancellation
retires the cast token and returns the companion to following its owner.

Tests cover rejected orders, normal-body admission controls, body-mode changes
after a live player-target preparation, and mode-2 transitions on attack pets
and guild soldiers with a prepared monster-target cast. Cancellation must close
the token once and leave monster HP unchanged.

## Remaining #218 scope

### Secondary area victims

The cape-team refusal added in #468 covered command admission and the primary
victim on each combat tick. The secondary area selector still consulted only
the owner's permission or world-enemy predicate. Aggression could therefore
admit a same-cape player and that player's companion as splash victims even
though the primary-target path refused them.

`petAreaTargets` now applies the existing companion team predicate before
either owner-permission query and before collecting the player's companions.
It uses current slot-8 equipment on every selection. The bounded regression
checks attack pets and guild soldiers, all five cape groups, different colors,
joining a team, owner exit and re-entry, and victim exit. Each phase checks
both the player and its summoned companion. Group 5 remains hostile to itself.
With the original production selector overlaid, the same-team and re-entry
phases fail for groups 1 through 4 in both attacker bands; the fixed selector
passes. These are selection tests, not a full connected-client battle replay.

The secondary-owner extension is an inference, not a completed native area
call-chain proof. The validator proves the direct-player team exclusion.
The native area path validates its supplied target vector, then expands
area targets before damage calculation; the around-target selector also
filters relations. That does not establish that every expanded victim
reaches the companion attack validator. Applying the existing team rule
consistently to secondary owners and companions is the bounded inference here.

### Broader transition owner

The later server's `7516` free-battle mode request, its state changer and
its group broadcast are separate from this v1.150 client's slot-8 capes.
They have not been added by this fix. The adjacent validator branch
rejects the owner and target sharing a free-battle team, except group 5.
Its team predicate reads the stored group byte and accepts groups 1
through 5; it does not test mount state. PR #468 ports that same-team refusal.

Transition callers and complete soldier/companion behavior when entering and
leaving cape combat still need a v1.150-scoped audit. Issue #218 remains open.
No new gameplay mode or non-native exception is introduced.
