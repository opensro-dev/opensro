# Wizard skill PR integration — 2026-10-02

PR [#103](https://github.com/opensro-dev/opensro/pull/103) was reviewed at
9b0eb32efd6246a292b3945f27eb3813d18b583e; PR
[#104](https://github.com/opensro-dev/opensro/pull/104) at
bfde46da761e558fffcf3b7c7e5d9a26fd7bd534.
The review covered every changed hunk in the 49 files listed below, their
affected callers and tests, and the relevant native branches. This is a
review of those changes, not a claim that every skill in the game has full
native parity.

The integration retains the contribution and corrects nine findings:

| Finding | Native evidence | Integrated behavior |
| --- | --- | --- |
| R1: absorption classification | Server 410BB0, 40E60B, 40E9B1, 58F312 | Keep initiating attack flags, authored effect flags and selected damage lanes separate. Imbues retain basic/skill classification. Physical flags take precedence in mixed effects, even when a chain or wall masks that lane. Wall helpers 40EBE0/40EEF0 do not apply character absorption to the wall pool. |
| R2: trap results and visibility | Server 59B2A0; client 7756D0, 8D9940, 86C1F0, 86C70F | Send B3C6 mode 3 with trap GID. Publish spawn, result and retirement in one ordered observer batch. Capture the explosion position before despawn. Replicate concealed objects; native hide/dtt comparison owns rendering. The planter decoration uses the trap GID as its instance token. |
| R3: target-follow effects | Client 8DE7A4..8DE81A | AT_TARGET_F creates one target-owned EFP, follows its bone/offset, survives caster closure and retires with the target. It neither accepts BSR nor becomes a projectile because mover fields are populated. |
| R4: Fear acquisition | Server 5299E0, 529A60, 547E04 | Project the Fear slot into the shared hostility predicate so native detection retains precedence. Remembered-opponent lookup keeps its separate observer-status contract; Fear's AI event owns abandonment. |
| R5: HP penalties | Server 59615D | pmhp uses the additive percentage channel. A 20% bonus and 50% penalty on 1000 base HP produce 700; removing the penalty restores 1200. |
| R6: buff replacement | Server 59D870, 59DA30, 59DAD2 | Remove the inferred trailing-letter tier replacement. Keep native group/rank/casting-state conflicts. |
| R7: detached cast closure | Native independent cast/effect lifetimes | Keep the real character owner and explicit presentation-only state. Session cleanup removes the pending close without making it block another command. |
| R8: trap population ownership | Existing division-owned object identity contract | Trap count retirement includes division as well as owner GID and link group. Matching IDs in another division remain untouched. |
| R9: trap and linked-effect tethers | Server 5849F1..584A08, 430BA0, 405270, 405250 | Both live links and trap tethers use the existing native 3D distance helper, including float stores, height, sector displacement and equality. |

## Preserved native branches

The taunt percentage/flat ordering is supported by 590402..5904CF, with
tnt2 fallback. Root's teleport gate at 58E010 tests tele and tel3,
not tel2; the independent context gate still checks all three. Death's
selected-skill retirement preserves the cbuf exemption (59FF80).
Skill-object EFP versus BSR resource handling comes from client 86C440.

The combat request owns impact lane masks; authored reference data is never
rewritten to pass runtime state. Monster acquisition and remembered lookup
live together in monsteracquisition.go. Attack selection and the
pursuit-to-attack handoff live in monsterattack.go, separated from movement
publication on a responsibility boundary.

## Regression coverage

- Native mixed-lane absorption, original basic/skill classification through a
  real Fire Force imbue, chained lanes and partial Force-wall coverage.
- Fear source exclusion, expiry, detecting-body precedence and independent
  remembered-target lookup.
- Trap owner/peer spawn-result-despawn order on the first scan, population
  generation isolation, native mode-3 byte layout, malformed batches,
  post-despawn effect position, concealment and planter-token recognition.
- AT_TARGET sampled placement versus AT_TARGET_F movement, bone/offset,
  caster closure, target removal, resource type and ignored mover fields.
- Vertical, diagonal, sector-boundary and incompatible-plane tether cases.
- HP penalty retirement, native buff conflict, detached-close session cleanup
  and trap count isolation across divisions.

## Binary Ninja persistence

Every unlabeled function exposed during the review was inspected and named.
The server annotations were saved and read back from snapshot 336. Client
annotations were saved and read back through snapshot 272, including
CISkillObj_OnTimerEvent_ExceptionHandler at B8D618. Snapshot numbers are
evidence for this session, not a required future database version.

## Validation and release

The source pipeline passed all 12 tasks, including the full server gate.
The final client and integration checks are recorded in the integration PR.
This change does not deploy a server or publish client/data assets. Release
remains a separate coordinated operation.

## Reviewed files

- apps/client-next/src/engine/runtime/characters/effects/effects.ts
- apps/client-next/tests/runtime/character-effects.test.mjs
- apps/server/internal/game/abnormal/block.go
- apps/server/internal/game/abnormal/lifecycle_test.go
- apps/server/internal/game/action/castclose_test.go
- apps/server/internal/game/action/earthbarrier_test.go
- apps/server/internal/game/action/lifecontrol_test.go
- apps/server/internal/game/action/projectilecast.go
- apps/server/internal/game/action/skilladmit_test.go
- apps/server/internal/game/action/skillcombat.go
- apps/server/internal/game/action/skillcombattrap.go
- apps/server/internal/game/action/skillcombattrap_test.go
- apps/server/internal/game/action/skillimbue.go
- apps/server/internal/game/action/skillobjects.go
- apps/server/internal/game/action/skillposition_test.go
- apps/server/internal/game/action/skilltimedeffect.go
- apps/server/internal/game/action/statuscast_test.go
- apps/server/internal/game/action/statuscastarea.go
- apps/server/internal/game/action/targetinteract.go
- apps/server/internal/game/action/untargetedcast.go
- apps/server/internal/game/action/wall_test.go
- apps/server/internal/game/combat/attributemodifier.go
- apps/server/internal/game/combat/formula.go
- apps/server/internal/game/combat/formula_test.go
- apps/server/internal/game/combat/parametergraph.go
- apps/server/internal/game/combat/stats.go
- apps/server/internal/game/enterworld/skill_storage.go
- apps/server/internal/game/enterworld/skilldata.go
- apps/server/internal/game/enterworld/skilloffense.go
- apps/server/internal/game/enterworld/skilloffense_test.go
- apps/server/internal/game/enterworld/skillposition.go
- apps/server/internal/game/enterworld/skillposition_test.go
- apps/server/internal/game/enterworld/skillstatuscast.go
- apps/server/internal/game/enterworld/skilltimedeffect.go
- apps/server/internal/game/enterworld/skilltrap.go
- apps/server/internal/game/item/statuseffect/registry.go
- apps/server/internal/game/item/statuseffect/replacement.go
- apps/server/internal/game/item/statuseffect/replacement_test.go
- apps/server/internal/game/item/statuseffect/retirementpolicy_test.go
- apps/server/internal/game/item/wire/skillarea.go
- apps/server/internal/game/world/monster/abnormal.go
- apps/server/internal/game/world/monster/fear_test.go
- apps/server/internal/game/world/simulation/monsteracquisition.go
- apps/server/internal/game/world/simulation/monstertick.go
- apps/server/internal/game/world/skillobject/publication.go
- apps/server/internal/game/world/skillobject/registry.go
- apps/server/internal/game/world/skillobject/registry_test.go
- scripts/build/effects/buildEffectPrograms.mjs
- scripts/test/mission/effectProgramClosure.test.mjs
