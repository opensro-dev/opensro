/*
===========================================================================

skillEffectStage.ts - the authored 0xa0-byte skill-effect stage record

Owned by the asset pipeline: v1.150 data-format tables recovered from the
native client; keep behaviour identical to the published assets. Grounded in
the v1.150 bytes at sub_91e720 (field producer), sub_8fd510 (copy ctor, the
complete field census) and sub_8d8e30 / sub_8dca40 / sub_8ddde0 (runtime
consumers). The ranges +0x3c..+0x53 and +0x54..+0x6b are complete attachment
records, not four independent bone/offset fields; keeping that boundary
explicit prevents source and target offsets from being swapped.

===========================================================================
*/

type int32 = number;
type uint8 = number;

export type SkillEffectStageVector3 = [number, number, number];

/** Minimal semantic anchor view consumed by sub_8d6330. */
export interface SkillEffectStageAttachAnchor {
	/** Native +0x00 atom/key. `null` anchors at the model root. */
	boneName: string | null;
	/** Native +0x04 points at the authored local vector owned by the record. */
	localOffset: SkillEffectStageVector3 | null;
}

/** Complete browser mirror of one native 0x18-byte attachment record. */
export interface SkillEffectStageAttachmentRecord extends SkillEffectStageAttachAnchor {
	/** Same native +0x00 slot in the matrix-builder vocabulary. */
	boneRef0: string | number;
	/** Same native +0x04 slot in the matrix-builder vocabulary. */
	offset4: SkillEffectStageVector3 | null;
	/** Native +0x08: keep the bone rotation; 8D6880 resets it to identity when
	 * zero. sub_91e720 clears it for an '@Bone' token. */
	keepRotation8: boolean;
	/** Native +0x09: the '*' token; 8D6880 adds CICharactor_GetHeight to Y. */
	addHeight9: boolean;
}

/** Native data_ccc89c, constructed from the literal `Spine_Base`. */
export const SkillEffectSpineBaseSocketNameAddress = 0x00ccc89c;
export const g_skillEffectSpineBaseSocketName = "Spine_Base";

/** Name/id tables registered by sub_bbe6f0..sub_bbea60. */
export const SKILL_EFFECT_PHASE_ID_BY_NAME = new Map<string, number>( [
	[ "READY", 0 ],
	[ "WAIT", 1 ],
	[ "SHOT", 2 ],
	[ "ACT_OS", 3 ],
	[ "ACT_OL", 4 ],
	[ "ACT_OE", 5 ],
	[ "ACT_S", 6 ],
	[ "ACT_L", 7 ],
	[ "DEACT", 8 ],
	[ "S_RETURN", 9 ]
] );
export const SKILL_EFFECT_DAMAGE_TYPE_MASK_BY_NAME = new Map<string, number>( [
	[ "NONE", 0 ],
	[ "NOR", 1 ],
	[ "CRI", 2 ],
	[ "HWAN", 4 ]
] );
export const SKILL_EFFECT_SCALE_MODE_BY_NAME = new Map<string, number>( [
	[ "NONE", 0 ],
	[ "CHAR_BASE", 1 ],
	[ "MOB_BASE", 2 ]
] );
export const SKILL_EFFECT_ACTION_TYPE_ID_BY_NAME = new Map<string, number>( [
	[ "AT_STOP", 0 ],
	[ "AT_ONE_FOLLOW", 1 ],
	[ "AT_LOOP", 2 ],
	[ "AT_TARGET", 3 ],
	[ "AT_TARGET_F", 4 ],
	[ "AT_SOURCE", 5 ],
	[ "AT_DMG_POS", 6 ],
	[ "AT_MOV_1TAR", 7 ],
	[ "AT_MOV_OPTION", 8 ],
	[ "AT_MOV_APIECE", 9 ],
	[ "AT_MOV_SPLASH", 10 ],
	[ "AT_MOV_PIERCE", 11 ]
] );
export const SKILL_EFFECT_MOVE_TYPE_ID_BY_NAME = new Map<string, number>( [
	[ "MOV_NONE", 0 ],
	[ "MOV_STRAIGHT", 1 ],
	[ "MOV_UP", 2 ],
	[ "MOV_UPR", 3 ],
	[ "MOV_ROUND", 4 ],
	[ "MOV_HWAN", 5 ]
] );
export const SKILL_EFFECT_OBJECT_KIND_ID_BY_NAME = new Map<string, number>( [
	[ "NONE", 0 ],
	[ "BSR", 1 ],
	[ "EFP", 2 ],
	[ "WAV", 3 ],
	[ "DDJ", 4 ]
] );
export const SKILL_EFFECT_SCRIPT_FLAG_BY_NAME = new Map<string, number>( [
	[ "NONE", 0x0000 ],
	[ "SCT_ARROW", 0x0001 ],
	[ "SCT_RUT", 0x0002 ],
	[ "SCT_MOVER", 0x0004 ],
	[ "SCT_SHAKECAM0", 0x0008 ],
	[ "SCT_SHAKECAM1", 0x0010 ],
	[ "SCT_SHAKECAM_MOV0", 0x0020 ],
	[ "SCT_SHAKECAM_MOV1", 0x0040 ],
	[ "SCT_SHAKECAM2", 0x0080 ],
	[ "SCT_SHAKECAM3", 0x0100 ],
	[ "SCT_SHAKECAM_MOV2", 0x0200 ],
	[ "SCT_SHAKECAM_MOV3", 0x0400 ],
	[ "SCT_MAT", 0x0800 ],
	[ "SCT_CHAR_SCALE", 0x1000 ],
	[ "SCT_EFFECT_SCALE", 0x2000 ]
] );

export type SkillEffectStageNativeField = {
	offset: number;
	width: 1 | 2 | 4 | 24;
	field: string;
	source: string;
};

/**
 * Machine-readable source -> native projection contract. Padding is omitted;
 * the two 24-byte attachment rows cover their pointer/flag/inline storage.
 */
export const SKILL_EFFECT_STAGE_NATIVE_FIELDS: readonly SkillEffectStageNativeField[] = [
	{ offset: 0x00, width: 1, field: "phase00", source: "animationPhase" },
	{ offset: 0x01, width: 1, field: "startEvent01", source: "startEvent" },
	{ offset: 0x02, width: 1, field: "flags02", source: "damageEvent" },
	{ offset: 0x03, width: 1, field: "damageTypeMask03", source: "damageTypes" },
	{ offset: 0x04, width: 1, field: "scaleMode04", source: "scale" },
	{ offset: 0x05, width: 1, field: "slot05", source: "id" },
	{ offset: 0x06, width: 1, field: "attachSlot06", source: "attach" },
	{ offset: 0x07, width: 1, field: "stagedCmdKey07", source: "trade" },
	{ offset: 0x08, width: 1, field: "releaseSlot08", source: "kill" },
	{ offset: 0x09, width: 1, field: "batchCount09", source: "createCount" },
	{ offset: 0x0a, width: 2, field: "fadeDurationMs0a", source: "fadeInMs" },
	{ offset: 0x0c, width: 2, field: "fadeOutMs0c", source: "fadeOutMs" },
	{ offset: 0x0e, width: 1, field: "byte0e", source: "actionType" },
	{ offset: 0x0f, width: 1, field: "kind0f", source: "move.kind" },
	{ offset: 0x10, width: 2, field: "value10", source: "move.delay" },
	{ offset: 0x12, width: 2, field: "dstRegion12", source: "move.startSpeed" },
	{ offset: 0x14, width: 2, field: "blendIdMax14", source: "move.endSpeed" },
	{ offset: 0x18, width: 4, field: "param18", source: "param[0]" },
	{ offset: 0x1c, width: 4, field: "param1c", source: "param[1]" },
	{ offset: 0x20, width: 4, field: "param20", source: "param[2]" },
	{ offset: 0x24, width: 1, field: "rotateFlag24", source: "actionOptions.enabled" },
	{ offset: 0x28, width: 4, field: "rotateAngle28", source: "actionOptions.direction" },
	{ offset: 0x2c, width: 4, field: "rotateRadius2c", source: "actionOptions.distance" },
	{ offset: 0x30, width: 4, field: "residualDistance30", source: "actionOptions.residualDistance" },
	{ offset: 0x34, width: 2, field: "actionLifeMs34", source: "actionOptions.lifeMs" },
	{ offset: 0x36, width: 1, field: "simultaneousRelease36", source: "actionOptions.simultaneousRelease" },
	{ offset: 0x37, width: 1, field: "kind37", source: "objectResourcePath" },
	{ offset: 0x38, width: 4, field: "nameOrId38", source: "objectResourcePath" },
	{ offset: 0x3c, width: 24, field: "attach3c", source: "startBone,startOffset" },
	{ offset: 0x54, width: 24, field: "attach54", source: "targetBone,targetOffset" },
	{ offset: 0x6c, width: 1, field: "rotationAxis6c", source: "rotate" },
	{ offset: 0x70, width: 4, field: "rotationAngle70", source: "rotate" },
	{ offset: 0x74, width: 4, field: "secondaryResource74", source: "secondaryObjectPath" },
	{ offset: 0x78, width: 4, field: "flags78", source: "scripts[0]" },
	{ offset: 0x7c, width: 4, field: "pitch7c", source: "scripts" },
	{ offset: 0x80, width: 4, field: "materialColorFrom80", source: "scripts" },
	{ offset: 0x84, width: 4, field: "materialColorTo84", source: "scripts" },
	{ offset: 0x88, width: 4, field: "renderParam88", source: "scripts" },
	{ offset: 0x8c, width: 4, field: "characterScale8c", source: "scripts" },
	{ offset: 0x90, width: 4, field: "characterScaleDuration90", source: "scripts" },
	{ offset: 0x94, width: 4, field: "effectScale94", source: "scripts" },
	{ offset: 0x98, width: 4, field: "soundIdA98", source: "soundBegin" },
	{ offset: 0x9c, width: 4, field: "soundIdB9c", source: "soundEnd" }
] as const;

/** One authored 0xa0-stride stage record. */
export interface SkillEffectStageRecord {
	nativeAddress?: int32;
	phase00: uint8;
	startEvent01: uint8;
	/** +0x02: bit0 is the authored damage-event gate; runtime may add flags. */
	flags02: uint8;
	damageTypeMask03: uint8;
	scaleMode04: uint8;
	slot05: uint8;
	attachSlot06: uint8;
	stagedCmdKey07: uint8;
	releaseSlot08: uint8;
	batchCount09: uint8;
	fadeDurationMs0a: number;
	fadeOutMs0c: number;
	byte0e: uint8;
	kind0f: uint8;
	value10: number;
	dstRegion12: number;
	blendIdMax14: number;
	param18: number;
	param1c: number;
	param20: number;
	rotateFlag24: uint8;
	rotateAngle28: number;
	rotateRadius2c: number;
	residualDistance30: number;
	actionLifeMs34: number;
	simultaneousRelease36: uint8;
	kind37: uint8;
	nameOrId38: string | number;
	attach3c: SkillEffectStageAttachmentRecord;
	attach54: SkillEffectStageAttachmentRecord;
	rotationAxis6c: uint8;
	rotationAngle70: number;
	secondaryResource74: string | number | null;
	flags78: int32;
	pitch7c: number;
	materialColorFrom80: readonly [uint8, uint8, uint8];
	materialColorTo84: readonly [uint8, uint8, uint8];
	renderParam88: int32;
	characterScale8c: number;
	characterScaleDuration90: number;
	effectScale94: number;
	soundIdA98: number;
	soundIdB9c: number;
}
