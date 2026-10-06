/*
===========================================================================

monstercast.go - simulation-owned prepared monster casts

Players and COS share preparation, identity revalidation and cancellation.

===========================================================================
*/

package action

import (
	"fmt"
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
pendingMonsterCast

Preparing -> released or cancelled. Like pendingProjectileCast, this owner
uses the simulation clock and division operation lock, never a goroutine.
Native 58356C / 585BF8 retain positive casting time; 585F69 generates the
release results. Client B245 admits preparation; B505 carries those results.
================
*/
type pendingMonsterCast struct {
	division      string
	instance      monster.Instance
	target        uint32
	characterID   int64
	characterName string
	skill         uint32
	token         uint32
	releaseAtMs   int64
	selfEffect    bool
	cosTarget     bool
	cosRefID      uint32
	cosSlot       uint8
	cosGeneration uint64
	cancelled     bool
}

/*
================
monsterCastRecipient

Retain the character owner separately from the object hit. A COS can leave
or be replaced while its owner remains alive and connected.
================
*/
type monsterCastRecipient struct {
	character *enterworld.Character
	gid       uint32
}

/*
================
prepareMonsterCast

================
*/
func (rt *Runtime) prepareMonsterCast(division string, instance monster.Instance, recipient monsterCastRecipient, skill enterworld.SkillRow, now int64) simulation.MonsterAttackResult {
	rt.pendingSkillFinalizesMu.Lock()
	defer rt.pendingSkillFinalizesMu.Unlock()
	for _, p := range rt.pendingMonsterCasts {
		if p.division == division && p.instance.Gid == instance.Gid {
			return simulation.MonsterAttackResult{TargetAlive: true}
		}
	}
	target := recipient.character
	p := pendingMonsterCast{division: division, instance: instance, target: recipient.gid, characterID: target.ID, characterName: target.Name, skill: skill.ID, token: atomic.AddUint32(&rt.castTokenCounter, 1), releaseAtMs: now + int64(skill.ActionCastingTimeMs) + 1}
	p.selfEffect = skill.MonsterSelfEffect.Pinned
	p.cosTarget = recipient.gid != enterworld.ObjectIDForCharacter(target)
	if pet := target.CompanionByGID(recipient.gid); p.cosTarget && pet != nil {
		p.cosRefID, p.cosSlot = pet.RefObjID, pet.InventorySlot
		p.cosGeneration = pet.SummonGeneration
	}
	rt.pendingMonsterCasts = append(rt.pendingMonsterCasts, p)
	frame := wire.SkillCastUntargetedFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: instance.Gid, InstanceToken: p.token, OwnerOrTargetGid: p.target})
	if p.selfEffect {
		frame = wire.SkillCastSelfFrame(wire.SkillCastSuccess{SkillId: skill.ID, CasterGid: instance.Gid, InstanceToken: p.token})
	}
	return simulation.MonsterAttackResult{Accepted: true, TargetAlive: true, Frames: []simulation.Frame{{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope}}}
}

/*
================
advanceMonsterCasts

================
*/
func (rt *Runtime) advanceMonsterCasts(now int64) []simulation.DivisionFrames {
	rt.pendingSkillFinalizesMu.Lock()
	pending := append([]pendingMonsterCast(nil), rt.pendingMonsterCasts...)
	rt.pendingSkillFinalizesMu.Unlock()
	var out []simulation.DivisionFrames
	for _, p := range pending {
		unlock := rt.lockDivision(p.division)
		attacker, exists := rt.Monsters.Get(p.division, p.instance.Gid)
		target := rt.findCharacter(p.division, p.characterName)
		var snapshot *enterworld.Character
		if target != nil && target.ID == p.characterID {
			snapshot = rt.characterSnapshot(p.division, target)
		}
		valid := !p.cancelled && exists && attacker.CurrentHP > 0 && attacker.Motion.StateAt(now) == 0 && snapshot != nil && !snapshot.DeletePending && enterworld.CharacterAlive(snapshot)
		if p.cosTarget && valid {
			pet := snapshot.CompanionByGID(p.target)
			valid = pet != nil && pet.Summoned && pet.GID == p.target && pet.CurrentHP > 0 &&
				pet.RefObjID == p.cosRefID && pet.InventorySlot == p.cosSlot && pet.SummonGeneration == p.cosGeneration
		}
		if p.selfEffect {
			valid = exists && attacker.CurrentHP > 0 && attacker.Motion.StateAt(now) == 0
		}
		if valid && !p.selfEffect {
			_, valid = rt.characterMonster(p.division, snapshot, attacker.Gid)
		}
		if valid && now < p.releaseAtMs {
			unlock()
			continue
		}
		// Teardown can win between the queue snapshot and the operation lock.
		owned := false
		rt.pendingSkillFinalizesMu.Lock()
		for i, row := range rt.pendingMonsterCasts {
			if row.token == p.token {
				// Retirement may have won after the queue snapshot but before
				// this division lock. Revalidate the canonical cancellation bit.
				valid = valid && !row.cancelled
				rt.pendingMonsterCasts = append(rt.pendingMonsterCasts[:i], rt.pendingMonsterCasts[i+1:]...)
				owned = true
				break
			}
		}
		rt.pendingSkillFinalizesMu.Unlock()
		if !owned {
			unlock()
			continue
		}
		result := simulation.MonsterAttackResult{}
		if valid {
			result = rt.monsterAttackStage(p.division, p.instance, p.target, p.skill, now, &p)
		}
		if !result.Accepted {
			frame := wire.SkillCastFinalizeFrame(p.token)
			result.Frames = []simulation.Frame{{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope}}
		}
		if rt.PushMonsterCast != nil {
			rt.PushMonsterCast(p.division, p.instance.Gid, result)
			unlock()
			continue
		}
		if len(result.Frames) > 0 {
			out = append(out, simulation.DivisionFrames{DivisionID: p.division, SourceGID: p.instance.Gid, Frames: result.Frames})
		}
		for _, private := range result.Private {
			out = append(out, simulation.DivisionFrames{DivisionID: p.division, OnlyCharacterID: private.CharacterID, Frames: private.Frames})
		}
		unlock()
	}
	return out
}

/*
================
monsterCastOwner

================
*/
func monsterCastOwner(gid uint32) string { return fmt.Sprintf("@monster:%d", gid) }

/*
================
cancelCompanionCasts

Retirement and death invalidate incoming casts immediately. The regular cast
sweep still owns the one closing publication to the caster's observers.
================
*/
func (rt *Runtime) cancelCompanionCasts(division string, c *enterworld.Character, gid uint32) {
	rt.petMu.Lock()
	victims := make(map[petOwnerKey]*petSession)
	for key, state := range rt.petSessions {
		if key.division == division && state.combat != nil && (key.gid == gid || state.combat.target == gid) {
			victims[key] = state
		}
	}
	rt.petMu.Unlock()
	for key, state := range victims {
		rt.cancelPetCombat(key, state, rt.Now().UnixMilli())
	}
	// GIDs identify a family, not an item lifetime. A different summoner can
	// reuse both the slot and its initial generation before the next tick.
	// Keep the cast bracket until the normal sweep publishes its cancellation.
	rt.pendingSkillFinalizesMu.Lock()
	for i := range rt.pendingMonsterCasts {
		pending := &rt.pendingMonsterCasts[i]
		if pending.division == division && pending.characterID == c.ID && pending.cosTarget && pending.target == gid && !pending.selfEffect {
			pending.cancelled = true
		}
	}
	rt.pendingSkillFinalizesMu.Unlock()
}
