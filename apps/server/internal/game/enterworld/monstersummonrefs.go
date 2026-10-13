/*
===========================================================================

monstersummonrefs.go - the unique encounter's monster reference closure

Unique encounters summon creatures (and their _L2/_L3 variants) that have
no npcpos nest. The closure joins them to the template's spawnable set
before network admission. What the browser can draw is wider: every
creatable monster rides the public reference file (#369).

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
	var seeds []uint32
	for id, ref := range template.Refs {
		if monster.UniqueSummonPolicy(ref.Codename) != monster.NoSummonPolicy {
			seeds = append(seeds, id)
		}
	}
	refs, err := MonsterSummonClosure(template, skills, seeds)
	if err != nil {
		return template, err
	}
	template.SummonRefs = refs
	return template, nil
}

/*
================
MonsterSummonClosure

The seeds plus the summon children of their default skills, transitively,
in RefObjID order. The client roster seeds it with every unique-policy
reference (WithMonsterSummonReferences); the catalogue seeds it with the
uniques the shard actually places, so a variant no nest holds summons
nothing into the list.
================
*/
func MonsterSummonClosure(template monster.Template, skills SkillDataSource, seeds []uint32) ([]uint32, error) {
	seen := map[uint32]bool{}
	var pending []uint32
	for _, id := range seeds {
		if !seen[id] {
			seen[id] = true
			pending = append(pending, id)
		}
	}
	if len(pending) > 0 && skills == nil {
		return nil, fmt.Errorf("unique encounter roster requires skill data")
	}
	for i := 0; i < len(pending); i++ {
		ref := template.Refs[pending[i]]
		for _, id := range ref.DefaultSkillIDs {
			if id == 0 {
				continue
			}
			skill, ok := skills.SkillByID(id)
			if !ok {
				return nil, fmt.Errorf("monster %s has missing default skill %d", ref.Codename, id)
			}
			if !skill.Summon.Present {
				continue
			}
			for _, entry := range skill.Summon.Entries {
				if entry.RefObjID == 0 {
					continue
				}
				if _, ok := template.Refs[entry.RefObjID]; !ok {
					return nil, fmt.Errorf("monster %s summon %d has missing reference %d", ref.Codename, id, entry.RefObjID)
				}
				if !seen[entry.RefObjID] {
					seen[entry.RefObjID] = true
					pending = append(pending, entry.RefObjID)
				}
			}
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i] < pending[j] })
	return pending, nil
}
