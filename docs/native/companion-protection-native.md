# Companion attack protection

This is a bounded combat fix found while investigating issue #218, not a claim
that the complete free-PVP transition system has been ported.

## Native evidence

Authority: v1.188 `SR_GameServer.exe`, SHA-256
`bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290`.
Image base: `0x400000`. Addresses below are VAs; subtract the image base for RVAs.

The instructions in `528F40`, the companion attack validator, establish this
order independently of the database's function name:

| Instructions | Behavior |
| --- | --- |
| `528F44..528F57` | Missing owner or target refuses with `0x3006`. |
| `528F5D..528F69` | Read attacking companion vtable `+0x100`; body mode 2 refuses. |
| `528F6F..528FA5` | Read target vtable `+0x100`; body modes 2, 3 and 4 refuse. |
| `5290E6..5290F3` | The protected-body branch writes `0x3020`, returns zero. |
| `529003..52901D` | Only after the body checks, delegate to owner vtable `+0x628`, forwarding the control argument and error pointer. |

SHA-256 of the 107 original instruction bytes `[528F40,528FAB)`:
`17a792b9aaa0f424da0e75eff96a3b1838f849dcdcc6d757c5cf1eda33a24a78`.
No binary or database mutation was needed for this finding.

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
call-chain proof. `528FAB..529000` proves the direct-player team exclusion.
`58CC70` validates its supplied target vector, but `585A53` / `585AB5` later
expand area targets before damage calculation at `585AD1`. The around-target
selector also filters relations at `58AAD1..58AB15`; that does not establish
that every expanded victim reaches `528F40`. Applying the existing team rule
consistently to secondary owners and companions is the bounded inference here.

### Broader transition owner

The later server's `7516` free-battle mode request, `4E0D50` state changer and
`4F0E90` group broadcast are separate from this v1.150 client's slot-8 capes.
They have not been added by this fix. The adjacent branch at `528FAB..529000`
rejects the owner and target sharing a free-battle team, except group 5. Its
`4959D0` predicate reads group byte `+0x21EC` and accepts groups 1 through 5;
it does not test mount state. PR #468 ports that same-team refusal.

Transition callers and complete soldier/companion behavior when entering and
leaving cape combat still need a v1.150-scoped audit. Issue #218 remains open.
No new gameplay mode or non-native exception is introduced.
