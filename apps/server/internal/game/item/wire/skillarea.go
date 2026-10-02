/*
===========================================================================

skillarea.go - area-result publication for targeted and caster-centered casts

One token owns every victim. Steering identity is separate from the result
list, so an untargeted taunt does not invent a selected primary target.

===========================================================================
*/

package wire

const maximumAreaResultCount = 255

/*
================
SkillAreaTarget

One committed victim, carrying each authored impact in order.
================
*/
type SkillAreaTarget struct {
	GID     uint32
	Impacts []SkillCastTargetImpact
}

/*
================
SkillCastAreaFrame

The required primary is first in the target list and owns actor steering.
================
*/
func SkillCastAreaFrame(cast SkillCastSuccess, primary uint32, targets []SkillAreaTarget) Frame {
	if len(targets) == 0 || targets[0].GID != primary {
		panic("wire: invalid area target set")
	}
	cast.OwnerOrTargetGid = primary
	return skillAreaFrame(cast, targets)
}

/*
================
SkillCastUntargetedAreaFrame

Keep the result list while preserving the native zero steering target.
================
*/
func SkillCastUntargetedAreaFrame(cast SkillCastSuccess, targets []SkillAreaTarget) Frame {
	cast.OwnerOrTargetGid = 0
	return skillAreaFrame(cast, targets)
}

/*
================
skillAreaFrame

Client 8E0190 reads impact count, target count, then target-major records.
Every target must carry the same number of impacts under the shared token.
================
*/
func skillAreaFrame(cast SkillCastSuccess, targets []SkillAreaTarget) Frame {
	if len(targets) == 0 || len(targets) > maximumAreaResultCount {
		panic("wire: invalid area target set")
	}
	impacts := len(targets[0].Impacts)
	for _, target := range targets {
		if len(target.Impacts) != impacts || impacts == 0 || impacts > maximumAreaResultCount {
			panic("wire: invalid area impact set")
		}
	}
	w := cast.writePrefix(NewWriter(21 + (4+9*impacts)*len(targets))).U8(skillCastSteeringTargets).U8(uint8(impacts)).U8(uint8(len(targets)))
	for _, target := range targets {
		w.U32(target.GID)
		for _, impact := range target.Impacts {
			impact.writeTo(w)
		}
	}
	return Frame{Opcode: OpSkillCastResult, Payload: w.Payload()}
}

/*
================
SkillCastAreaReleaseFrame

Retain the original action token and use the same target/result grammar.
================
*/
func SkillCastAreaReleaseFrame(cast SkillCastSuccess, primary uint32, targets []SkillAreaTarget) Frame {
	start := SkillCastAreaFrame(cast, primary, targets)
	return Frame{Opcode: OpSkillEffectControl, Payload: append(NewWriter(5).U8(1).U32(cast.InstanceToken).Payload(), start.Payload[14:]...)}
}

/*
================
SkillCastUntargetedAreaReleaseFrame

The release of a prepared caster-centred area (Lightning Impact): the same
result grammar as SkillCastAreaReleaseFrame with the zero steering target.
================
*/
func SkillCastUntargetedAreaReleaseFrame(cast SkillCastSuccess, targets []SkillAreaTarget) Frame {
	start := SkillCastUntargetedAreaFrame(cast, targets)
	return Frame{Opcode: OpSkillEffectControl, Payload: append(NewWriter(5).U8(1).U32(cast.InstanceToken).Payload(), start.Payload[14:]...)}
}
