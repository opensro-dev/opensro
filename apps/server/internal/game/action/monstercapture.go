/*
===========================================================================

monstercapture.go - the Monster Mask and Beast Mask skills

The Rogue targets a monster corpse. Within reach the cast absorbs it:
SkillCombat_ApplySkillEffectsToTargets (593F63) has the caster create the
row's item (vtable +0x254, 4AA680) on the ground at the caster's position,
owned by the caster, with the corpse's RefObj as the item data. That
Essence of the Dead is picked up and used like any mask (transform.go).

===========================================================================
*/

package action

import (
	"sync/atomic"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
captureTargetRefusal

58D2F4 for an mcap row, and the corpse test of 594010: the target must be
dead, of normal grade (CGObjMob_GetBaseGrade, else 0x3033), an ordinary
monster (TID4 2, 3 and 4 - thieves, hunters and quest monsters - give
0x3006) and no higher than the cap (0x3035).
==================
*/
func captureTargetRefusal(target monster.Instance, capture enterworld.SkillMonsterCapture) uint16 {
	switch {
	case target.CurrentHP != 0:
		return 0x3006
	case target.Rarity()&0xf != 0:
		return 0x3033
	case target.Ref.TypeID4 >= 2 && target.Ref.TypeID4 <= 4:
		return 0x3006
	case uint32(target.Ref.Level) > capture.MaxLevel:
		return 0x3035
	}
	return 0
}

/*
==================
acceptMonsterCapture

The command: the corpse becomes the Rogue's action target and the capture
runs once it is within reach.
==================
*/
func (rt *Runtime) acceptMonsterCapture(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow) OpResult {
	if !cast.HasTarget || cast.HasGroundTarget || cast.TargetGid == 0 {
		return offensiveRefusal(0x3011)
	}
	if !enterworld.CharacterAlive(snapshot) || !enterworld.SkillLearned(snapshot, skill.ID) || rt.Monsters == nil {
		return OpResult{DiagnosticRefusal: "capture-admission-refused"}
	}
	intent := basicAttackIntent{CaptureCast: true, DivisionID: division, CharacterName: c.Name, TargetGid: cast.TargetGid, SkillID: skill.ID}
	rt.setCombatIntent(intent)
	return rt.advanceCaptureIntent(c, intent, rt.Now().UnixMilli())
}

/*
==================
advanceCaptureIntent

Walks to the corpse like any targeted command, then executes.
==================
*/
func (rt *Runtime) advanceCaptureIntent(c *enterworld.Character, intent basicAttackIntent, now int64) OpResult {
	division := intent.DivisionID
	snapshot := rt.characterSnapshot(division, c)
	skills := rt.deps.SkillData()
	if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) ||
		skills == nil || rt.skillCastPostureBlocked(division, snapshot, now) {
		rt.ClearCombatIntent(division, intent.CharacterName)
		return OpResult{}
	}
	skill, known := skills.SkillByID(intent.SkillID)
	target, found := rt.characterMonster(division, snapshot, intent.TargetGid)
	mover, moving := rt.Monsters.Mover(division, intent.TargetGid)
	if !known || !skill.MonsterCapture.Pinned || !found || !moving {
		rt.ClearCombatIntent(division, intent.CharacterName)
		return offensiveRefusal(0x3006)
	}
	_, loadout, err := rt.playerCombatStats(division, snapshot)
	if err != nil {
		rt.ClearCombatIntent(division, intent.CharacterName)
		return OpResult{}
	}
	spacing, ok := rt.playerToMonsterCombatSpacing(snapshot, target, rt.playerActionReach(division, snapshot, skill, loadout))
	if !ok {
		rt.ClearCombatIntent(division, intent.CharacterName)
		return OpResult{}
	}
	pose := mover.LivePoseAt(now, nil)
	to := simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
	worldKey := simulation.WorldKey(division, snapshot.Name)
	from := rt.liveSpawn(worldKey, snapshot, now)
	if !spacing.Contains(from, to) {
		if !rt.pursuitSteerDue(intent, worldKey, snapshot, to, now) {
			return OpResult{}
		}
		return rt.approachIntentTarget(c, snapshot, intent, spacing, from, to, now)
	}

	rt.ClearCombatIntent(division, intent.CharacterName)
	transition, transitioned := rt.enterBasicAttackRange(c, snapshot, worldKey, to, now)
	if !transitioned {
		return OpResult{}
	}
	return prependOpResult(transition, rt.executeMonsterCapture(division, c, skill, target, to, now))
}

/*
==================
executeMonsterCapture

Admission (execution mask), the cost and cooldown, the cast frames, then
the Essence at the caster's feet.
==================
*/
func (rt *Runtime) executeMonsterCapture(division string, c *enterworld.Character, skill enterworld.SkillRow, target monster.Instance, at simulation.Spawn, now int64) OpResult {
	snapshot := rt.characterSnapshot(division, c)
	if snapshot == nil || rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "capture-action-busy"}
	}
	if code := captureTargetRefusal(target, skill.MonsterCapture); code != 0 {
		return offensiveRefusal(code)
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, &admitTarget{motion: target.Motion.StateAt(now), at: at}, nil, admitExecution); code != 0 {
		return offensiveRefusal(code)
	}
	source, ok := rt.deps.ItemReferences().(interface {
		ItemRefByID(uint32) (*enterworld.ItemRef, bool)
	})
	if !ok {
		return OpResult{DiagnosticRefusal: "capture-item-source-unavailable"}
	}
	ref, ok := source.ItemRefByID(skill.MonsterCapture.ItemRefObjID)
	if !ok || ref == nil || !wire.IsMonsterCapsule(ref.TypeFlags()) {
		return OpResult{DiagnosticRefusal: "capture-item-unavailable"}
	}

	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	casterGID := enterworld.ObjectIDForCharacter(snapshot)
	var refusal uint16
	var essence grounditem.Item
	if !rt.deps.Update(c, "monster-capture", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, nil)
		if refusal = code; code != 0 {
			return false
		}
		rt.startSkillCast(division, c, skill, now)
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, false)
		row := inventory.Item{RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), Quantity: 1, TransformRefObjID: target.Ref.RefObjID}
		planned := PlanItemDrop(row, 1, rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now), c.Name, time.UnixMilli(now))
		planned.OwnerJID = casterGID
		essence = rt.addCharacterGround(division, c, planned)
		return essence.Gid != 0
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "capture-commit-refused"}
	}

	cast := wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: casterGID, InstanceToken: token, OwnerOrTargetGid: target.Gid})
	lifetime, _ := skill.ActionLifecycleMs()
	rt.queueSkillFinalize(division, snapshot.Name, casterGID, now+int64(skill.ActionCastingTimeMs), wire.SkillCastReleaseFrame(token, casterGID))
	rt.queueSkillFinalize(division, snapshot.Name, casterGID, now+int64(lifetime), wire.SkillCastFinalizeFrame(token))
	frames := append([]wire.Frame{cast}, rt.groundReferences([]grounditem.Item{essence})...)
	frames = append(frames, wire.DropBroadcastFrames(essence.SpawnRow(true))...)
	return OpResult{Frames: frames, Broadcast: frames}
}
