package wire

// Native 8E0440 consumes owner GID and steering byte even with no target
// rows. Zero steering is a complete self-cast body, not an empty attack.
func SkillCastSelfFrame(cast SkillCastSuccess) Frame {
	cast.OwnerOrTargetGid = cast.CasterGid
	return Frame{Opcode: OpSkillCastResult, Payload: cast.writePrefix(NewWriter(19)).U8(0).Payload()}
}

// SkillCastAtTargetFrame is the same body aimed at another object: the
// caster acts on OwnerOrTargetGid without a damage record (the corpse a
// Monster Mask absorbs).
func SkillCastAtTargetFrame(cast SkillCastSuccess) Frame {
	return Frame{Opcode: OpSkillCastResult, Payload: cast.writePrefix(NewWriter(19)).U8(0).Payload()}
}
