/*
===========================================================================

tiers.go - sequential pet skill books, negotiated by tree state version 2

The workbook keeps legacy templates. Three books of one family replace each
row's alternatives. Conversion preserves spent points or refunds excess;
equipment and running cooldowns are retained.

===========================================================================
*/
package companion

import "opensro.online/server/internal/domain"

/*
================
EffectName
================
*/
func (s SkillDef) EffectName() string {
	if s.Family != "" {
		return s.Family
	}
	return s.Name
}

/*
================
SkillsForPet
================
*/
func SkillsForPet(p *domain.CharacterCOS) []SkillDef {
	kind := PetKind(p.Codename)
	if p.Training.TreeVersion < 2 {
		return PetSkills(kind)
	}
	return treeSkillsByKind[kind]
}

/*
================
UpgradeTree
================
*/
func UpgradeTree(p *domain.CharacterCOS) {
	if p.Training.TreeVersion >= 2 {
		return
	}
	old := p.Training.Levels
	p.Training.TreeVersion = 2
	p.Training.Levels, p.Training.Slotted, p.Training.Queued = [15]uint8{}, [3]uint8{}, 0
	for row := range RowCount {
		remaining := int(old[row*3]) + int(old[row*3+1]) + int(old[row*3+2])
		for _, s := range SkillsForPet(p) {
			if s.Row != row {
				continue
			}
			if s.Option > 1 && p.Training.Levels[s.Index()-1] < s.MaxLevel {
				break
			}
			for remaining > 0 && p.Training.Levels[s.Index()] < s.MaxLevel && p.Level >= NextLevelRequirement(s, p.Training.Levels[s.Index()]) {
				p.Training.Levels[s.Index()]++
				remaining--
			}
			if !s.Active && p.Training.Levels[s.Index()] > 0 {
				p.Training.Slotted[row-ActiveRowCount] = uint8(s.Option)
			}
		}
		// Unallocated ranks refund themselves: TrainingPoints subtracts only
		// retained levels. Do not add bonus points and refund them twice.
	}
}
