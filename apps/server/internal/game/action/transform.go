/*
===========================================================================

transform.go - the transform block (msch 1 mask, msch 2 Duplicate)

A filled ITEM_ETC_TRANS_MONSTER capsule holds a monster RefObj in its
item data. Using it (the native capsule use) casts the item's
SKILL_ETC_TRANS_MONSTER_01 as an indirect skill with that RefObj in
the context (+0x20). The cast is admitted by msch word 1 and its
effect installs the transform block:

	skin   everyone near sees the monster's model (0x323A, spawn row)
	speed  walk and run are the monster's own, not the player's
	       modified speeds, unless the player rides

A Duplicate (duplicate.go) fills the same block with another player's
model and worn equipment (the native Duplicate transform), and only
when no transform holds it; its speeds stay the player's own.

The block lives as long as its instance: retiring any msch 1 or 2
instance clears it (the native transform clear). The
client puts the player model back when that instance ends (CIDecoSkill
client 8DD131), so the end needs no packet of its own.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// transformRow reports whether a row is an msch word 1 program.
func transformRow(row enterworld.SkillRow) bool {
	return transformWord(row) == 1
}

// transformWord is the msch word of a row whose effect fills the block
// (1 or 2), else 0.
func transformWord(row enterworld.SkillRow) uint8 {
	if row.CastGate.MschPresent && (row.CastGate.MschMode == 1 || row.CastGate.MschMode == 2) {
		return uint8(row.CastGate.MschMode)
	}
	return 0
}

// applyTransform fills the block from an instance's presentation and
// returns the skin broadcast. The caller holds c's door.
func applyTransform(c *enterworld.Character, word uint8, p EffectPresentation) wire.Frame {
	c.TransformRefObjID, c.TransformMode = p.TransformRefObjID, word
	c.TransformShape, c.TransformEquipment = p.TransformShape, p.TransformEquipment
	skin := enterworld.CharacterTransformSkin(c)
	return wire.Frame{Opcode: wire.OpSkinChange, Payload: wire.SkinChange{GID: enterworld.ObjectIDForCharacter(c), Skin: skin}.Encode()}
}

// clearTransform is the native transform clear for a batch of ended effects: any msch 1
// instance among them ends the transform, even one a newer mask already
// replaced on screen. The caller holds c's door.
func clearTransform(c *enterworld.Character, ended []statuseffect.Effect) bool {
	if c.TransformRefObjID == 0 {
		return false
	}
	for _, e := range ended {
		if e.TransformRefObjID != 0 {
			c.TransformRefObjID, c.TransformMode = 0, 0
			c.TransformShape, c.TransformEquipment = 0, [9]uint32{}
			return true
		}
	}
	return false
}

// transformSpeeds are the walk and run the block holds (RefObj +0xE4,
// +0xE6), which the speed getters return in place of the modified speeds
// while the player is on foot.
func (rt *Runtime) transformSpeeds(c *enterworld.Character) (float32, float32, bool) {
	if c.TransformMode != 1 || rt.Monsters == nil || c.ActiveCOS != nil && c.ActiveCOS.Mounted {
		return 0, 0, false
	}
	ref, ok := rt.Monsters.Reference(c.TransformRefObjID)
	if !ok {
		return 0, 0, false
	}
	return float32(ref.WalkSpeed), float32(ref.RunSpeed), true
}

/*
==================
transformAttackSkill

The native default attack's first arm: while the msch 1 instance holds the block, the
default attack is the RefObj's skill at +0x260 (its first default skill),
not the weapon's base attack, and the weapon record is skipped. The
normal-attack admission (mask 0x64) still runs the equipment check, which
passes: every monster a mask can hold (TID4 1) has a weapon-free basic
skill. Zero when the player is not transformed.
==================
*/
func (rt *Runtime) transformAttackSkill(c *enterworld.Character) uint32 {
	if c.TransformRefObjID == 0 || rt.Monsters == nil {
		return 0
	}
	ref, ok := rt.Monsters.Reference(c.TransformRefObjID)
	if !ok {
		return 0
	}
	return ref.DefaultSkillIDs[0]
}

/*
==================
transformRefusal

The native msch word 1 check. The RefObj must exist (0x3006) and be no higher
than the caster (0x3008); the caster must not ride, be changing motion
(the async job 1 a sit or stand runs) or sit (0x3009).

Only monster RefObjs are looked up: nothing in this port fills a mask
with a player or NPC.
==================
*/
func (rt *Runtime) transformRefusal(division string, c *enterworld.Character, ref uint32, now int64) uint16 {
	var found monster.MonsterRef
	ok := false
	if rt.Monsters != nil && ref != 0 {
		found, ok = rt.Monsters.Reference(ref)
	}
	if !ok {
		return 0x3006
	}
	level := characterEquipRequirements{character: c}
	if uint8(level.characterLevel()) < found.Level {
		return 0x3008
	}
	if c.ActiveCOS != nil && c.ActiveCOS.Mounted {
		return 0x3009
	}
	if rt.Worlds != nil {
		world := rt.Worlds.Snapshot(simulation.WorldKey(division, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) })
		if world.Sitting || now < world.PostureTransitionUntilMs {
			return 0x3009
		}
	}
	return 0
}

/*
==================
useMonsterCapsule

The native capsule use, under the item-use door:

	body mode 4, an empty mask or an item without a skill   error 2
	the item's skill refused natively                       its 0xB070
	                                                        error, then 5

On success the hide ends and the mask is spent. The indirect
skill's cast broadcast is not sent, as for every item skill here.
==================
*/
func (rt *Runtime) useMonsterCapsule(division string, c *enterworld.Character, ref *enterworld.ItemRef, rowIndex int, request wire.ItemUseRequest, now int64, result *OpResult) bool {
	skin := c.MissionInventory[rowIndex].TransformRefObjID
	source, ok := rt.deps.SkillData().(interface {
		SkillByCodename(string) (enterworld.SkillRow, bool)
	})
	if c.NativeBodyStatus == 4 || skin == 0 || !ok || ref.AssociatedSkillCodename == "" {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		return false
	}
	skill, found := source.SkillByCodename(ref.AssociatedSkillCodename)
	if !found || !transformRow(skill) {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		return false
	}
	refused := func(code uint16) bool {
		failure := itemUseFailure(itemUseSkillRefused)
		*result = OpResult{Frames: append(offensiveRefusal(code).Frames, failure.Frames...)}
		return false
	}
	if code := rt.contextSkillAdmission(division, c, skill, now, nil, nil, admitExecution, admitContext{Indirect: true, TransformRef: skin}); code != 0 {
		return refused(code)
	}
	token := atomic.AddUint32(&rt.castTokenCounter, 1)
	if token == 0 {
		token = atomic.AddUint32(&rt.castTokenCounter, 1)
	}
	frames, applied := rt.commitCharacterEffect(division, c, skill, token, statuseffect.StateActive, false, EffectPresentation{Phase: 2, TransformRefObjID: skin}, now)
	if !applied {
		*result = itemUseFailure(itemUseSkillRefused)
		return false
	}
	rt.retireHide(division, c, now)
	remaining := rt.consumeItemUseRow(c, rowIndex)
	*result = OpResult{Frames: append([]wire.Frame{{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(request.Slot, remaining, request.TypeWord)}}, frames...), Broadcast: frames}
	result.Frames = append(result.Frames, rt.updateQuestInventory(c)...)
	return true
}

// itemUseSkillRefused is the item-use result the native capsule gives when
// the indirect skill refuses (5).
const itemUseSkillRefused uint8 = 5

/*
==================
endTransform

Retires the transform instance early, the way three natives do: mounting
(the native mount and broadcast, msch 1 only), summoning a COS
(any word but 4) and a loading area, which both transforms'
descriptions name as an end. The caller holds c's door.
==================
*/
func (rt *Runtime) endTransform(division string, c *enterworld.Character, now int64) bool {
	if c.TransformMode == 0 || rt.effects == nil {
		return false
	}
	var tokens []uint32
	for _, e := range rt.effects.Snapshot(division, c.Name) {
		if e.TransformRefObjID != 0 {
			tokens = append(tokens, e.InstanceToken)
		}
	}
	ended := rt.effects.RetireInstances(division, c.Name, tokens)
	rt.publishEndedEffects(division, c, ended, now)
	return len(ended) != 0
}

// endTransformForLoading runs endTransform in its own door before a
// re-entry builds the character's packets.
func (rt *Runtime) endTransformForLoading(division string, c *enterworld.Character) {
	rt.endTransformForLoadingVia(division, c, rt.deps.Update)
}

// endTransformForLoadingDeferred is endTransformForLoading for the
// tick's return-scroll completion: its commit joins the tick's batched
// commit (#1820). Handler re-entries (portal, relocation) keep the
// immediate door, so their answer still follows its commit.
func (rt *Runtime) endTransformForLoadingDeferred(division string, c *enterworld.Character) {
	rt.endTransformForLoadingVia(division, c, rt.deps.UpdateDeferred)
}

// endTransformForLoadingVia is the shared body behind both doors.
func (rt *Runtime) endTransformForLoadingVia(division string, c *enterworld.Character, door func(*enterworld.Character, string, func() bool) bool) {
	if rt.effects == nil || c == nil {
		return
	}
	hasTransform := false
	for _, e := range rt.effects.Snapshot(division, c.Name) {
		if e.TransformRefObjID != 0 {
			hasTransform = true
			break
		}
	}
	if !hasTransform {
		return
	}
	door(c, "transform-loading-end", func() bool {
		return rt.endTransform(division, c, rt.Now().UnixMilli())
	})
}
