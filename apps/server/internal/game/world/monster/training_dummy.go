/*
===========================================================================

training_dummy.go - optional GM targets using the existing wooden doll mesh.

Port-only, not native. Separate references preserve native monster stats;
the ordinary GM spawn, combat, statuses and despawn doors still own them.
===========================================================================
*/
package monster

import (
	"fmt"
	"os"
	"strings"
)

const EnvTrainingDummy = "SRO_GM_TRAINING_DUMMY"
const TrainingDummyRefBase uint32 = 5000100

/*
================
TrainingDummyEnabled
================
*/
func TrainingDummyEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvTrainingDummy))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
AppendTrainingDummies

No nests: only an authenticated GM can create these targets. They are
killable for execute tests, but settlement never awards a training kill.
================
*/
func AppendTrainingDummies(template Template, enabled bool) (Template, error) {
	if !enabled {
		return template, nil
	}
	base, ok := template.Refs[1933]
	if !ok || base.Codename != "MOB_CH_MANGNYANG" {
		return Template{}, fmt.Errorf("training dummy: wooden Mangnyang reference missing")
	}
	refs := make(map[uint32]MonsterRef, len(template.Refs)+140)
	for id, ref := range template.Refs {
		refs[id] = ref
	}
	for level := uint32(1); level <= 140; level++ {
		id := TrainingDummyRefBase + level
		if _, exists := refs[id]; exists {
			return Template{}, fmt.Errorf("training dummy: reference %d already exists", id)
		}
		ref := base
		ref.RefObjID, ref.TrainingDummy = id, true
		ref.Codename = fmt.Sprintf("MOB_GM_TRAINING_DUMMY_%03d", level)
		ref.Name, ref.NameStrID = "Training Dummy", "SN_MOB_GM_TRAINING_DUMMY"
		ref.Level, ref.MaxHP, ref.ExpToGive, ref.MonsterType = uint8(level), 100000000, 0, 0
		ref.WalkSpeed, ref.RunSpeed = 0, 0
		ref.CombatPinned = true
		ref.PhysicalDefense, ref.MagicalDefense, ref.ParryRate, ref.MagicalParry = 0, 0, 0, 0
		ref.EvasionRate, ref.BlockRate, ref.ElementResist = 0, 0, [6]uint8{}
		ref.DefaultSkillIDs = [10]uint32{}
		refs[id] = ref
	}
	template.Refs = refs
	return template, nil
}
