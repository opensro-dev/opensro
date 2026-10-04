/*
===========================================================================

duplicate.go - the Rogue's Duplicate and Sable Duplicate (msch 2)

The Rogue targets an allied player and takes that player's look: the
persistent instance lives on the Rogue with the player's gid in its
context (CastLifecycle_ProcessPersistent 583BC6), and
CGObjPC_ApplyDupleTransform (4F0040) copies the player's model, record
byte and nine worn slots into the transform block (4F0320) - only when no
transform already holds it. The speeds stay the Rogue's own.

The instance ends with the next skill cast (skc event 2), death, a
loading area or a COS summon; its end clears the block (4F0210) and the
client restores the Rogue's own body (8DD131).

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
duplicateTargetRefusal

TargetValidation_ValidateAllTargets for msch 2 (58D1D3..58D2A0): a player
in a job suit or riding a horse or fellow, one above the level word, a GM
or a murderer (PvP state 2) cannot be copied.
==================
*/
func (rt *Runtime) duplicateTargetRefusal(target *enterworld.Character, maxLevel uint32) uint16 {
	switch {
	case wearsJobSuit(target, rt.statCatalogs().Items), mountedOnCOS(target):
		return 0x3039
	case target.Level != nil && *target.Level > int64(maxLevel):
		return 0x3035
	case target.GMPrivilege, target.PVPState() == 2:
		return 0x3039
	}
	return 0
}

// duplicateLook is 4F0320 on the copied player: its model, the record byte
// (+0x64, taken here as the body shape byte) and the RefObjs in its nine
// worn slots.
func (rt *Runtime) duplicateLook(target *enterworld.Character) (EffectPresentation, bool) {
	models, ok := rt.deps.(interface {
		CharacterModelRef(*enterworld.Character) uint32
	})
	if !ok {
		return EffectPresentation{}, false
	}
	look := EffectPresentation{Phase: 1, TransformRefObjID: models.CharacterModelRef(target)}
	if look.TransformRefObjID == 0 {
		return EffectPresentation{}, false
	}
	if target.BodyShapeByte != nil {
		look.TransformShape = uint8(*target.BodyShapeByte)
	}
	for _, row := range target.MissionInventory {
		if row.Slot >= 0 && row.Slot < int64(len(look.TransformEquipment)) {
			look.TransformEquipment[row.Slot] = row.RefObjID
		}
	}
	return look, true
}

/*
==================
acceptDuplicate

A target out of reach defers the cast behind a support intent, as every
player-targeted command walks first (58D8F0 never refuses for range).
==================
*/
func (rt *Runtime) acceptDuplicate(division string, c, snapshot *enterworld.Character, cast wire.SkillAction, skill enterworld.SkillRow, now int64) OpResult {
	if !skill.Duplicate.Pinned || !cast.HasTarget || cast.HasGroundTarget || cast.TargetGid == 0 ||
		!enterworld.CharacterAlive(snapshot) || !enterworld.SkillLearned(snapshot, skill.ID) {
		return OpResult{DiagnosticRefusal: "duplicate-admission-refused"}
	}
	target := rt.findCharacterByGid(division, cast.TargetGid)
	view := rt.characterSnapshot(division, target)
	if view == nil {
		return offensiveRefusal(0x3006)
	}
	to := rt.liveSpawn(simulation.WorldKey(division, view.Name), view, now)
	spacing, pinned, ok := rt.supportTargetSpacing(snapshot, view, skill)
	if !ok {
		return OpResult{DiagnosticRefusal: "duplicate-spacing-unavailable"}
	}
	from := rt.liveSpawn(simulation.WorldKey(division, snapshot.Name), snapshot, now)
	if pinned && !spacing.Contains(from, to) {
		return rt.beginSupportApproach(division, c, snapshot, cast, spacing, from, to, now)
	}
	if rt.skillCastPostureBlocked(division, snapshot, now) || rt.hasOpenSkillCast(division, snapshot.Name) {
		return OpResult{DiagnosticRefusal: "duplicate-action-busy"}
	}
	if code := rt.skillAdmission(division, snapshot, skill, now, &admitTarget{at: to, player: view}, nil, admitExecution); code != 0 {
		return offensiveRefusal(code)
	}
	look, ok := rt.duplicateLook(view)
	if !ok {
		return OpResult{DiagnosticRefusal: "duplicate-look-unavailable"}
	}

	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	casterGID := enterworld.ObjectIDForCharacter(snapshot)
	var refusal uint16
	var installed []wire.Frame
	if !rt.deps.Update(c, "duplicate", func() bool {
		if !enterworld.CharacterAlive(c) || !enterworld.SkillLearned(c, skill.ID) {
			return false
		}
		cost, code := rt.offensivePhaseCost(division, c, skill, now, nil)
		if refusal = code; code != 0 {
			return false
		}
		// The cast's own event ends an earlier Duplicate first (skc 2).
		rt.startSkillCast(division, c, skill, now)
		rt.commitOffensivePhaseCost(division, c, skill, cost, now, false)
		var ok bool
		installed, ok = rt.commitCharacterEffect(division, c, skill, token, statuseffect.StateActive, false, look, now)
		return ok
	}) {
		if refusal != 0 {
			return offensiveRefusal(refusal)
		}
		return OpResult{DiagnosticRefusal: "duplicate-commit-refused"}
	}

	lifetime, _ := skill.ActionLifecycleMs()
	rt.queueSkillFinalize(division, snapshot.Name, casterGID, now+int64(lifetime), wire.SkillCastFinalizeFrame(token))
	frames := append([]wire.Frame{wire.SkillCastAtTargetFrame(wire.SkillCastSuccess{
		SkillId: skill.ID, CasterGid: casterGID, InstanceToken: token, OwnerOrTargetGid: cast.TargetGid,
	})}, installed...)
	return OpResult{Frames: frames, Broadcast: frames}
}
