/*
===========================================================================

fortress_structure_appearance.go - a structure's standing skill in its
create row

A fortress structure casts its authored skill as it appears (the
appearance cast slices 1 and 2 gave effects to, fortress_structure_
pulse.go and fortress_structure_aura.go), and a viewer reads the cast
in the structure's create row: the shared actor block's skill list is
read for structures as for any actor, and the client seeds the listed
skills as the object's standing effects. This lane keeps that list
true: a standing structure carries its standing-area skill, a fallen
one carries nothing. The attachment stands outside the war too - the
cast is the structure's appearance, not one of its war acts, whose
target checks gate the pulses and auras instead.

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
structureStandingSkill

The structure's standing-area skill (its tower pulse or its holder
aura): the first of its authored default skills whose row carries the
kind-3 standing area. Doors and gates carry none and show nothing.
================
*/
func (rt *Runtime) structureStandingSkill(structure monster.Instance) (uint32, bool) {
	skills := rt.deps.SkillData()
	if skills == nil {
		return 0, false
	}
	for _, id := range structure.Ref.DefaultSkillIDs {
		if id == 0 {
			continue
		}
		if row, ok := skills.SkillByID(id); ok && row.Abnormal.PersistentArea.Present {
			return row.ID, true
		}
	}
	return 0, false
}

/*
================
advanceFortressStructureAppearances

Keep every fortress structure's create-row skill list true: install
the standing skill on a structure that stands without it, take it
from one that has fallen. Runs on the siege tick, ungated by the war;
the install validates the instance is alive, so a race with the fall
only skips a tick.
================
*/
func (rt *Runtime) advanceFortressStructureAppearances() {
	if rt.Fortresses == nil || rt.Monsters == nil {
		return
	}
	for _, division := range rt.Fortresses.Divisions() {
		for _, world := range rt.fortressWorlds() {
			for _, structure := range rt.Monsters.WorldStructures(division, world) {
				skillID, ok := rt.structureStandingSkill(structure)
				if !ok {
					continue
				}
				token, attached := uint32(0), false
				for effect := range structure.LinkedEffects.Entries() {
					if effect.SkillID == skillID {
						token, attached = effect.Token, true
					}
				}
				switch {
				case structure.CurrentHP == 0 && attached:
					rt.Monsters.RemoveMonsterLinkedEffect(division, structure.Gid, token)
				case structure.CurrentHP != 0 && !attached:
					token = atomic.AddUint32(&rt.castTokenCounter, 1)
					rt.Monsters.InstallMonsterLinkedEffects(division, []simulation.MonsterLinkedEffect{{
						GID:    structure.Gid,
						Effect: monster.AttachedSkill{SkillID: skillID, Token: token},
					}})
				}
			}
		}
	}
}
