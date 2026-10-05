/*
===========================================================================

auto-potion.ts - native automatic recovery settings and admission predicates

The client checks current vitals, not the server's remaining potion pulses.
The ordinary item-use lane owns pending requests and category cooldowns.

===========================================================================
*/
import type { WireFrame } from "@/engine/contracts/network";
/*
================
AutoPotionSettings
================
*/
export interface AutoPotionSettings {
	readonly hp: number;
	readonly mp: number;
	readonly cure: number;
	readonly timing: number;
}
/*
================
AutoPotionEntry
================
*/
export interface AutoPotionEntry {
	readonly enabled: boolean;
	readonly percent: number;
	readonly slot: number;
}
/*
================
defaultAutoPotion
================
*/
export function defaultAutoPotion(): AutoPotionSettings {
	return { hp: 0x3211, mp: 0x3212, cure: 0x0013, timing: 0x8a };
}
/*
================
autoPotionSettings
================
*/
export function autoPotionSettings( value: unknown ): AutoPotionSettings {
	if ( !value || typeof value !== "object" ) throw Error( "Invalid auto-potion settings" );
	const r = value as Record<string, unknown>;
	for ( const [key, max] of [ [ "hp", 65535 ], [ "mp", 65535 ], [ "cure", 65535 ], [ "timing", 255 ] ] as const ) {
		if ( typeof r[key] !== "number" || !Number.isInteger( r[key] ) || r[key] < 0 || r[key] > max ) {
			throw Error( "Invalid auto-potion " + key );
		}
	}
	return { hp: r.hp as number, mp: r.mp as number, cure: r.cure as number, timing: r.timing as number };
}
// 7783E0 substitutes all defaults for a zero timing byte, and HP alone for zero HP.
/*
================
admittedAutoPotion
================
*/
export function admittedAutoPotion( value: unknown ): AutoPotionSettings {
	const r = autoPotionSettings( value );
	return r.timing === 0 ? defaultAutoPotion() : { ...r, hp: r.hp || defaultAutoPotion().hp };
}
/*
================
autoPotionBootstrap
================
*/
export function autoPotionBootstrap( value: unknown ): AutoPotionSettings {
	const raw = (value as { character?: { autoPotion?: unknown; }; })?.character?.autoPotion;
	return raw === undefined ? defaultAutoPotion() : admittedAutoPotion( raw );
}
// 70C3D0: byte decrement wraps before zero extension; malformed page zero caps at 40.
/*
================
autoPotionEntry
================
*/
export function autoPotionEntry( word: number ): AutoPotionEntry {
	if ( !Number.isInteger( word ) || word < 0 || word > 65535 ) throw Error( "Invalid auto-potion word" );
	const low = word & 15, high = word >>> 4 & 15;
	return {
		enabled: !!(word & 0x8000),
		percent: word >>> 8 & 127,
		slot: low === 0 && high === 0 ? 0 : Math.min( 40, low + ((high - 1) & 255) * 10 )
	};
}
/*
================
autoPotionWord
================
*/
export function autoPotionWord( entry: AutoPotionEntry ): number {
	const { enabled, percent, slot } = entry;
	if (
		typeof enabled !== "boolean" || !Number.isInteger( percent ) || percent < 0 || percent > 127 ||
		!Number.isInteger( slot ) || slot < 0 || slot > 40
	) throw Error( "Invalid auto-potion entry" );
	const low = slot === 0 ? 0 : ((Math.floor( (slot - 1) / 10 ) + 1) << 4) | ((slot - 1) % 10 + 1);
	return low | (percent << 8) | (enabled ? 0x8000 : 0);
}
/*
================
autoPotionDelay
================
*/
// A00B30 clamps a zero timer period to one millisecond.
export function autoPotionDelay( settings: AutoPotionSettings ): number {
	return settings.timing & 128 ? Math.max( 1, (settings.timing & 127) * 100 ) : 500;
}
/*
================
autoPotionSave
================
*/
export function autoPotionSave( value: unknown ): WireFrame {
	const s = autoPotionSettings( value ), payload = new Uint8Array( 8 ), v = new DataView( payload.buffer );
	payload[0] = 2;
	v.setUint16( 1, s.hp, true );
	v.setUint16( 3, s.mp, true );
	v.setUint16( 5, s.cure, true );
	payload[7] = s.timing;
	return { opcode: 0x7541, payload };
}
/*
================
AutoPotionFacts
================
*/
export interface AutoPotionFacts {
	readonly alive: boolean;
	readonly hp: number;
	readonly mp: number;
	readonly maxHp: number;
	readonly maxMp: number;
	readonly abnormal: number;
}
// 70C550/70C5D0/70C640 keep the repeating timer armed while blocked by an abnormal bit.
/*
================
autoPotionActive
================
*/
export function autoPotionActive( kind: 0 | 1 | 2, entry: AutoPotionEntry, facts: AutoPotionFacts ): boolean {
	if ( !entry.enabled || !facts.alive ) return false;
	if ( kind === 2 ) return facts.abnormal !== 0;
	const current = kind === 0 ? facts.hp : facts.mp, max = kind === 0 ? facts.maxHp : facts.maxMp;
	// 70C28F stores percent / 100 to float32 before the x87 multiply.
	// 70C2BF truncates to int64 and 70C2C7 retains the low unsigned dword.
	const threshold = Math.trunc( max * Math.fround( entry.percent / 100 ) ) >>> 0;
	return current <= threshold;
}
/*
================
autoPotionEligible
================
*/
export function autoPotionEligible( kind: 0 | 1 | 2, entry: AutoPotionEntry, facts: AutoPotionFacts ): boolean {
	return autoPotionActive( kind, entry, facts ) &&
		(kind === 0 ? (facts.abnormal & 0x20) === 0 : kind === 2 ? (facts.abnormal & 0x4000) === 0 : true);
}
// 70C300 -> 5729E0 -> 5503A0: only bag binding 46, consumable bands 1 or 2.
/*
================
autoPotionItemSlot
================
*/
export function autoPotionItemSlot(
	binding: import("./quickslots").QuickSlot,
	inventory: readonly { slot: number; typeFlags: number; }[]
): number | null {
	if ( binding.kind !== 0x46 ) return null;
	const slot = binding.payload + 13, item = inventory.find( row => row.slot === slot ), tid = item?.typeFlags;
	if (
		tid === undefined || (tid & 2) !== 0 || (tid & 0x1c) !== 0xc || (tid & 0x60) !== 0x60 ||
		!([ 1, 2 ].includes( tid >>> 7 & 15 ))
	) return null;
	return slot;
}

/*
================
AutoPotionTimer

CAutoPotion's active byte is separate from CIObject's registered timer.
Changing a channel clears the former without deleting the latter.
================
*/
export interface AutoPotionTimer {
	readonly active: boolean;
	readonly due: number | null;
	readonly period: number;
}

/*
================
AutoPotionCheck
================
*/
export interface AutoPotionCheck {
	readonly kind: 0 | 1 | 2;
	readonly settings: AutoPotionSettings;
	readonly facts: AutoPotionFacts;
	readonly now: number;
	readonly event: "vitals" | "timer";
}

/*
================
emptyAutoPotionTimer
================
*/
export function emptyAutoPotionTimer(): AutoPotionTimer {
	return { active: false, due: null, period: 0 };
}

/*
================
checkAutoPotionTimer

77A080 only enters inactive channels on a matching vitals notification.
70C550/5D0/640 leave registered timers alone while disabled; A00BE0 advances
one callback per host frame. A00B30 refuses to replace an existing timer.
================
*/
export function checkAutoPotionTimer(
	timer: AutoPotionTimer,
	check: AutoPotionCheck
): { timer: AutoPotionTimer; use: boolean; } {
	const { kind, settings, facts, now, event } = check;
	if ( event === "vitals" && timer.active || event === "timer" && (timer.due === null || now < timer.due) ) {
		return { timer, use: false };
	}
	let next = event === "timer" ? { ...timer, due: now + timer.period } : timer;
	const entry = autoPotionEntry( [ settings.hp, settings.mp, settings.cure ][kind]! );
	if ( !entry.enabled ) return { timer: next, use: false };
	if ( !autoPotionActive( kind, entry, facts ) ) return { timer: emptyAutoPotionTimer(), use: false };
	if ( !next.active ) {
		const period = autoPotionDelay( settings );
		next = { active: true, due: next.due ?? now + period, period: next.due === null ? period : next.period };
	}
	return { timer: next, use: autoPotionEligible( kind, entry, facts ) };
}

/*
================
autoPotionChannelChanged

63DD60 compares enabled, selected quickslot and (HP/MP only) percentage.
A delay-only edit never resets channel flags or an existing timer period.
================
*/
export function autoPotionChannelChanged(
	before: AutoPotionSettings,
	after: AutoPotionSettings,
	kind: 0 | 1 | 2
): boolean {
	const a = autoPotionEntry( [ before.hp, before.mp, before.cure ][kind]! );
	const b = autoPotionEntry( [ after.hp, after.mp, after.cure ][kind]! );
	return a.enabled !== b.enabled || a.slot !== b.slot || kind !== 2 && a.percent !== b.percent;
}

/*
================
AutoPotionDraft

Cure combos can independently have no selection. The packed word alone
cannot represent that UI state; 63DBF0 converts it only when applying.
================
*/
export interface AutoPotionDraft extends AutoPotionSettings {
	readonly curePage: number;
	readonly cureKey: number;
}

/*
================
autoPotionDraft

63E050 loads a disposable UI copy. The HP/MP sliders clamp to 1..100,
542E40 clamps the delay spin to 500..9500 ms, and empty HP/MP bindings
leave the newly created combo boxes at F1/key1 (63E690). Cure combos
start at -1 (51DB40); byte arithmetic in 63DBF0 clamps that selection to 40.
================
*/
export function autoPotionDraft( settings: AutoPotionSettings ): AutoPotionDraft {
	const hp = autoPotionEntry( settings.hp ),
		mp = autoPotionEntry( settings.mp ),
		cure = autoPotionEntry( settings.cure );
	return {
		...settings,
		hp: autoPotionWord( { ...hp, percent: Math.max( 1, Math.min( 100, hp.percent ) ), slot: hp.slot || 1 } ),
		mp: autoPotionWord( { ...mp, percent: Math.max( 1, Math.min( 100, mp.percent ) ), slot: mp.slot || 1 } ),
		cure: autoPotionWord( { ...cure, slot: cure.slot || 40 } ),
		curePage: cure.slot ? Math.floor( (cure.slot - 1) / 10 ) : -1,
		cureKey: cure.slot ? (cure.slot - 1) % 10 : -1,
		timing: (settings.timing & 128) | Math.max( 5, Math.min( 95, settings.timing & 127 ) )
	};
}

/*
================
autoPotionDraftChoice

63DBF0 casts each combo index to a byte before composing the cure slot.
================
*/
export function autoPotionDraftChoice(
	draft: AutoPotionDraft,
	key: "hp" | "mp" | "cure",
	part: "page" | "key",
	value: number
): AutoPotionDraft {
	const entry = autoPotionEntry( draft[key] );
	if ( key === "cure" ) {
		const curePage = part === "page" ? value : draft.curePage;
		const cureKey = part === "key" ? value - 1 : draft.cureKey;
		const slot = Math.min( 40, (curePage & 255) * 10 + ((cureKey + 1) & 255) );
		return { ...draft, curePage, cureKey, cure: autoPotionWord( { ...entry, slot } ) };
	}
	const page = Math.floor( (entry.slot - 1) / 10 ), keySlot = (entry.slot - 1) % 10 + 1;
	const slot = part === "page" ? value * 10 + keySlot : page * 10 + value;
	return { ...draft, [key]: autoPotionWord( { ...entry, slot } ) };
}
