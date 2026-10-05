export interface BerserkGauge {
	readonly authoritative: number;
	readonly displayed: number;
	readonly pending: number;
}
export interface ItemEffectFeedback {
	readonly kind: "item-effect";
	readonly source: import("./world").EntityState;
	readonly item: number;
	readonly typeFlags: number;
}
export type VisualFeedback =
	| ItemEffectFeedback
	| OrbFeedback
	| { readonly kind: "level-up"; readonly gid: number; }
	| SystemEffectFeedback;
// A SYSTEM_* decoration (an 0x8000xxxx skill id) the native client attaches
// to a character on an event: a pet's appearance, a knockback.
export interface SystemEffectFeedback {
	readonly kind: "system-effect";
	readonly gid: number;
	readonly effect: number;
}
export type OrbFeedback = { readonly kind: "orb-gauge"; readonly value: number; } | {
	readonly kind: "orb-feedback";
	readonly source: number;
	readonly color: 0 | 1 | 2 | 3;
	readonly count: number;
	readonly target: number;
} | { readonly kind: "orb-clear"; };
// The SYSTEM_* skill ids the client attaches itself (the 0x8000xxxx rows of
// skilleffect); the native registers their names in a table at CCDCD0.
// Three more need no port: CICMonster_DeserializeSpawnPacket (861B00) attaches
// SYSTEM_QUEST_MARK (0x8000001F) only when characterdata column 119 (+0x28C)
// is non-zero, and no v1.150 row sets it; SYSTEM_RETURNSCROLLRESULT,
// SYSTEM_APPEAR and SYSTEM_UNTOUCHABLE (05, 07, 18) are registered by name but
// no client code attaches them.
export const SYSTEM_KNOCKBACK = 0x8000001d;
export const SYSTEM_CAPTURE_MARK = 0x80000020;
export const SYSTEM_PET_APPEAR = 0x80000021;
