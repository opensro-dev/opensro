/*
===========================================================================

monstersummonrefs.go - the unique encounter's monster reference closure

Unique encounters summon creatures (and their _L2/_L3 variants) that have
no npcpos nest. The GameWorld seeds them into the refObjSnapshot, and the
asset bake exports the same closure through sro-evidence
spawnable-monsters, so a reference the server can stream always has a
model (#369). One function owns the closure; both call it.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"sort"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
WithMonsterSummonReferences

Sets template.SummonRefs to every unique-policy reference plus the summon
children of their default skills, transitively. Computed before network
admission: omitting a summoned creature from the initial mirror makes a
valid later spawn disappear in the client.
================
*/
func WithMonsterSummonReferences(template monster.Template, skills SkillDataSource) (monster.Template, error) {
	seen := map[uint32]bool{}
	var pending []uint32
	for id, ref := range template.Refs {
		if monster.UniqueSummonPolicy(ref.Codename) != monster.NoSummonPolicy {
			seen[id] = true
			pending = append(pending, id)
		}
	}
	if len(pending) > 0 && skills == nil {
		return template, fmt.Errorf("unique encounter roster requires skill data")
	}
	for i := 0; i < len(pending); i++ {
		ref := template.Refs[pending[i]]
		for _, id := range ref.DefaultSkillIDs {
			if id == 0 {
				continue
			}
			skill, ok := skills.SkillByID(id)
			if !ok {
				return template, fmt.Errorf("monster %s has missing default skill %d", ref.Codename, id)
			}
			if !skill.Summon.Present {
				continue
			}
			for _, entry := range skill.Summon.Entries {
				if entry.RefObjID == 0 {
					continue
				}
				if _, ok := template.Refs[entry.RefObjID]; !ok {
					return template, fmt.Errorf("monster %s summon %d has missing reference %d", ref.Codename, id, entry.RefObjID)
				}
				if !seen[entry.RefObjID] {
					seen[entry.RefObjID] = true
					pending = append(pending, entry.RefObjID)
				}
			}
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i] < pending[j] })
	template.SummonRefs = pending
	return template, nil
}
