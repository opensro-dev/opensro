/*
===========================================================================

skillfield.go - buff fields: efr kind 3 skill objects that hold recipients

A field skill (Harmony therapy) does not install on its caster. The release
plants a stationary skill object; every pass of 48CEA0 hands each eligible
character standing in it an instance of the same skill and retires the
instance of one that has left. The registry (world/skillobject) owns the
object's lifetime and its tracked set; this file owns recipient selection
and the effect writes. Trap fields (hostile selector, puls) are not here.

===========================================================================
*/
package action

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
)

/*
================
plantSkillField

The release half of a field cast (CastLifecycle_ProcessPersistent reads
+0x294 at 583C33): the object stands where the caster stands. Runs inside
the caster's door; the first recipient pass follows one scan period later.
================
*/
func (rt *Runtime) plantSkillField(division string, c *enterworld.Character, skill enterworld.SkillRow, now int64) bool {
	field := skill.TimedEffect.Field
	lease, present := rt.EntryPopulationLease(division, c.Name)
	if !present {
		return false
	}
	at := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	_, err := rt.SkillObjects.Create(skillobject.Object{
		Division: division, Population: lease, OwnerGID: enterworld.ObjectIDForCharacter(c), OwnerName: c.Name, CreatedMs: now,
		Program: skillobject.Program{SkillID: skill.ID, DurationMs: skill.EffectDurationMs, ScanMs: enterworld.SkillFieldScanMs,
			Radius: field.Radius, Field: true, Select: field.Select, MaxTargets: field.MaxTargets},
		Spawn: wire.SkillObjectSpawn{Region: at.RegionID, X: float32(at.X), Y: float32(at.Y), Z: float32(at.Z), Heading: at.Angle},
	})
	return err == nil
}

/*
================
advanceSkillField

One pass of 48CEA0 for object mode 2. The caller holds the division lock.

Tracked recipients first: one that no longer exists is dropped; one outside
the radius has its instance of the skill retired and is dropped; one inside
whose instance has ended is dropped so the scan below admits it again.
Then the admission scan (48D690): while the set is under MaxTargets, an
eligible character in the radius that holds no instance of the skill gets
one with the row's own duration, and joins the set.

Inferred: retiring the field retires its recipients' instances. The native
retirement goes through a virtual call this port has not traced, and a buff
that outlived its field would contradict the pass that takes the buff from
anyone who steps out.
================
*/
func (rt *Runtime) advanceSkillField(object skillobject.Object, owner *enterworld.Character, ownerPresent bool, nowMs int64) {
	object, due, retired := rt.SkillObjects.Advance(object.Spawn.GID, nowMs, ownerPresent)
	if retired {
		rt.retireFieldRecipients(object, object.Tracked, nowMs)
		return
	}
	if !due {
		return
	}
	skill, known := rt.deps.SkillData().SkillByID(object.Program.SkillID)
	if !known || !skill.TimedEffect.Pinned || !skill.TimedEffect.Field.Present {
		return
	}
	center := fieldCenter(object)
	var kept, left []skillobject.FieldRecipient
	for _, r := range object.Tracked {
		c := rt.findCharacter(object.Division, r.Name)
		if c == nil || enterworld.ObjectIDForCharacter(c) != r.GID {
			continue
		}
		if !partyAreaReach(center, rt.liveSpawn(simulation.WorldKey(object.Division, c.Name), c, nowMs), object.Program.Radius) {
			left = append(left, r)
			continue
		}
		if _, holds := rt.fieldInstance(object.Division, c.Name, skill.ID); holds {
			kept = append(kept, r)
		}
	}
	rt.retireFieldRecipients(object, left, nowMs)
	var admitted []*enterworld.Character
	for _, c := range rt.fieldCandidates(object, owner, nowMs) {
		if object.Program.MaxTargets != 0 && len(kept)+len(admitted) >= int(object.Program.MaxTargets) {
			break
		}
		if _, holds := rt.fieldInstance(object.Division, c.Name, skill.ID); holds {
			continue
		}
		admitted = append(admitted, c)
	}
	rider, _ := rt.skillDurationRider(object.Division, owner, skill)
	for _, c := range rt.installRecipientEffects(object.Division, admitted, skill, rider, nowMs) {
		kept = append(kept, skillobject.FieldRecipient{Name: c.Name, GID: enterworld.ObjectIDForCharacter(c)})
	}
	if !rt.SkillObjects.Track(object.Spawn.GID, kept) {
		rt.retireFieldRecipients(object, kept, nowMs)
	}
}

/*
================
fieldCandidates

The characters 48CEA0 hands to admission, in identity order. Selector bits
(efr +0x14, 48D1CB..48D3FF): 1 admits the owner, 4 the owner's party, and
2 every other entity the skill may not strike
(CSkillManager_IsHostileTargetEligible refuses it); a hostile one needs bit
8, which a buff field never sets. worldPlayerEnemy stands in for the
hostility check. Inferred: only characters are candidates; the native
collector also returns monsters, but no monster holds a player buff in this
port.
================
*/
func (rt *Runtime) fieldCandidates(object skillobject.Object, owner *enterworld.Character, nowMs int64) []*enterworld.Character {
	if owner == nil || rt.deps == nil {
		return nil
	}
	party := rt.auraParty(object.Division, owner)
	world := domain.CharacterWorldInstance(owner)
	center := fieldCenter(object)
	sel := object.Program.Select
	var out []*enterworld.Character
	for _, c := range rt.deps.CharactersForDivision(object.Division) {
		if c == nil || c.DeletePending || !enterworld.CharacterAlive(c) || c.NativeTeleportMode != 0 ||
			domain.CharacterWorldInstance(c) != world {
			continue
		}
		if rt.RewardActorPresent != nil && !rt.RewardActorPresent(object.Division, c.Name) {
			continue
		}
		gid := enterworld.ObjectIDForCharacter(c)
		var admitted bool
		switch {
		case gid == object.OwnerGID:
			admitted = sel&enterworld.SelectCaster != 0
		case party[gid]:
			admitted = sel&enterworld.SelectParty != 0
		default:
			admitted = sel&enterworld.SelectCharacter != 0 && !rt.worldPlayerEnemy(object.Division, owner, c)
		}
		if !admitted || !partyAreaReach(center, rt.liveSpawn(simulation.WorldKey(object.Division, c.Name), c, nowMs), object.Program.Radius) {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return enterworld.ObjectIDForCharacter(out[i]) < enterworld.ObjectIDForCharacter(out[j])
	})
	return out
}

/*
================
fieldInstance

CSkillManager_FindActiveBuffBySkillID: the recipient's live instance of the
field's skill, whoever installed it.
================
*/
func (rt *Runtime) fieldInstance(division, name string, skillID uint32) (uint32, bool) {
	for _, e := range rt.effects.Snapshot(division, name) {
		if e.SkillID == skillID && e.State == statuseffect.StateActive {
			return e.InstanceToken, true
		}
	}
	return 0, false
}

/*
================
retireFieldRecipients

tagActiveSkillInstance_RequestRetirement for each recipient that still holds
the field's skill. Publication runs outside every character door.
================
*/
func (rt *Runtime) retireFieldRecipients(object skillobject.Object, recipients []skillobject.FieldRecipient, nowMs int64) {
	for _, r := range recipients {
		c := rt.findCharacter(object.Division, r.Name)
		if c == nil || enterworld.ObjectIDForCharacter(c) != r.GID {
			continue
		}
		token, holds := rt.fieldInstance(object.Division, c.Name, object.Program.SkillID)
		if !holds {
			continue
		}
		rt.publishEndedEffects(object.Division, c, rt.effects.RetireInstances(object.Division, c.Name, []uint32{token}), nowMs)
	}
}

/*
================
fieldCenter
================
*/
func fieldCenter(object skillobject.Object) simulation.Spawn {
	return simulation.Spawn{RegionID: object.Spawn.Region, X: float64(object.Spawn.X), Y: float64(object.Spawn.Y), Z: float64(object.Spawn.Z)}
}
