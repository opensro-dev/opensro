/*
===========================================================================

sro-macro.ts - the SRO Macro settings and their wire codec (port-only, #3907)

Port-only, not native: the SRO Macro (auto-potion, auto-skill, auto-hunt)
is a v1.696 feature that v1.150 does not have. The contract is read from
the v1.696 client (sro_client.exe 8af7afc2..., research note sro-macro.md):

	0x7554 C->S  u8 0 (switch) u8 kind u8 state           (006947f0, 00694ab0)
	             u8 1 (save)   u8 kind section            (00694170)
	0xB554 S->C  u8 action u8 kind u16 result [u8 state]  (00cfca30)
	0x3555 S->C  u8 mode ...                              (00cfcd80)

The potion and hunt sections are opaque fixed blocks on the wire (0x48 and
0xC bytes); the accessors below name their slots and bytes. The skill
section is four flag bytes and two skill lists (attack and buff).

This module is pure: it decodes, encodes and commits settings values; the
worker's sro-macro lane owns the live copy.

===========================================================================
*/
import { viewOf } from "@/engine/foundation/binary/byte-view";

export const OP_MACRO_REQUEST = 0x7554;
export const OP_MACRO_ANSWER = 0xb554;
export const OP_MACRO_CONFIG = 0x3555;

export const MACRO_POTION = 0;
export const MACRO_SKILL = 1;
export const MACRO_HUNT = 2;
export type MacroKind = typeof MACRO_POTION | typeof MACRO_SKILL | typeof MACRO_HUNT;

export const MACRO_ACTION_SWITCH = 0;
export const MACRO_ACTION_SAVE = 1;

// The only result the 0xB554 handler commits on; 0x9806 prints "ON/OFF
// Error" (00cfcbb3), any other failed save "Save Error".
export const MACRO_RESULT_OK = 1;
export const MACRO_RESULT_SWITCH_FAILED = 0x9806;

const CONFIG_FULL = 0;
const CONFIG_AVAILABILITY = 2;

export const MACRO_POTION_BYTES = 0x48;
export const MACRO_POTION_SLOTS = 9;
const POTION_SLOT_STRIDE = 8;
// The character rows the auto-potion readers serve (004c2ce0). Row 3 is the
// buff-item / speed-scroll gate (006930a0), not an anomaly check. Pet rows
// 4..8 are not automated yet.
export const MACRO_ROW_HP = 0;
export const MACRO_ROW_MP = 1;
export const MACRO_ROW_CURE = 2;
export const MACRO_ROW_BUFF_ITEM = 3;
export const MACRO_GAUGE_SLOTS = [ 0, 1, 4, 5 ] as const;
export const MACRO_HUNT_BYTES = 0x0c;
export const MACRO_SKILL_FLAGS = 4;
// skillFlags indices for the Weapon / Shield bag slots (save tags 3 and 4
// at 00694170; the attack step reads them at 0x17b37de / 0x17b37df).
export const MACRO_SKILL_FLAG_WEAPON = 2;
export const MACRO_SKILL_FLAG_SHIELD = 3;
// Absolute bag slots the Weapon / Shield UI accepts (00687520: >= 0x0d, < 0x70).
export const MACRO_GEAR_SLOT_MIN = 0x0d;
export const MACRO_GEAR_SLOT_MAX = 0x70;
// No list cap is read from the client; 32 matches the server's store
// (domain.MacroSkillsPerList) and is far above macro_2.2dt's slot grid.
export const MACRO_SKILLS_PER_LIST = 32;

export type MacroGearKind = "weapon" | "shield";

const POTION_VERSION = 3;
const SKILL_VERSION = 2;
const HUNT_VERSION = 2;
const SKILL_RECORD_BYTES = 5;
const EMPTY_LIST_BYTES = 1;
// The save's skill record tags (00694170): four flag bytes, two lists.
const SKILL_FLAG_TAGS = [ 0, 1, 3, 4 ] as const;
const SKILL_LIST_TAGS = [ 2, 5 ] as const;

export interface MacroSkill {
	readonly slot: number;
	readonly skill: number;
}

export interface MacroSettings {
	// bit 0 potion, bit 1 skill, bit 2 hunt (017b37d9, 017b3805, 017b3813)
	readonly available: number;
	readonly potionOn: boolean;
	readonly skillOn: boolean;
	readonly huntOn: boolean;
	readonly potion: readonly number[];
	readonly hunt: readonly number[];
	readonly skillFlags: readonly number[];
	readonly attack: readonly MacroSkill[];
	readonly buff: readonly MacroSkill[];
}

// One potion row: the item's RefItem ID (0068b4c0 compares it with each
// bag item's reference) and its two bytes. A gauge row (0, 1, 4, 5) is on
// by b and fires at a percent (readers 00692e00, 00692f60; loader 00db2070);
// a check-box row (2, 3, 6, 7, 8) is on by a (00692d20, 006930a0).
export interface MacroPotionSlot {
	readonly item: number;
	readonly a: number;
	readonly b: number;
}

export interface MacroAnswer {
	readonly action: number;
	readonly kind: number;
	readonly result: number;
	readonly state?: boolean;
}

/*
================
defaultHuntBlock

Native ctor 006938a0: range In place (0), byte 1 = 1 (not a UI control;
also the save's leading u8), every other option at its first choice.
================
*/
export function defaultHuntBlock(): number[] {
	const hunt = new Array<number>( MACRO_HUNT_BYTES ).fill( 0 );
	hunt[1] = 1;
	return hunt;
}

/*
================
defaultMacroSettings

Nothing available. Potion and skill blocks zeroed (empty rows / first
choice). Hunt uses defaultHuntBlock (006938a0).
================
*/
export function defaultMacroSettings(): MacroSettings {
	return {
		available: 0,
		potionOn: false,
		skillOn: false,
		huntOn: false,
		potion: new Array<number>( MACRO_POTION_BYTES ).fill( 0 ),
		hunt: defaultHuntBlock(),
		skillFlags: new Array<number>( MACRO_SKILL_FLAGS ).fill( 0 ),
		attack: [],
		buff: []
	};
}

/*
================
macroAvailable
================
*/
export function macroAvailable( settings: MacroSettings, kind: MacroKind ): boolean {
	return (settings.available & (1 << kind)) !== 0;
}

/*
================
macroOn
================
*/
export function macroOn( settings: MacroSettings, kind: MacroKind ): boolean {
	return kind === MACRO_POTION ? settings.potionOn : kind === MACRO_SKILL ? settings.skillOn : settings.huntOn;
}

/*
================
withMacroOn
================
*/
export function withMacroOn( settings: MacroSettings, kind: MacroKind, on: boolean ): MacroSettings {
	if ( kind === MACRO_POTION ) return { ...settings, potionOn: on };
	if ( kind === MACRO_SKILL ) return { ...settings, skillOn: on };
	return { ...settings, huntOn: on };
}

// ============================================================================

/*
================
potionSlot
================
*/
export function potionSlot( potion: readonly number[], index: number ): MacroPotionSlot {
	const at = index * POTION_SLOT_STRIDE;
	const item = (potion[at]! | (potion[at + 1]! << 8) | (potion[at + 2]! << 16) | (potion[at + 3]! << 24)) >>> 0;
	return { item, a: potion[at + 4]!, b: potion[at + 5]! };
}

/*
================
withPotionSlot

The block with one row replaced; the row's two padding bytes stay as
they were.
================
*/
export function withPotionSlot( potion: readonly number[], index: number, slot: MacroPotionSlot ): number[] {
	const next = potion.slice();
	const at = index * POTION_SLOT_STRIDE;
	next[at] = slot.item & 0xff;
	next[at + 1] = (slot.item >>> 8) & 0xff;
	next[at + 2] = (slot.item >>> 16) & 0xff;
	next[at + 3] = (slot.item >>> 24) & 0xff;
	next[at + 4] = slot.a & 0xff;
	next[at + 5] = slot.b & 0xff;
	return next;
}

/*
================
potionGauge
================
*/
export function potionGauge( index: number ): boolean {
	return (MACRO_GAUGE_SLOTS as readonly number[]).includes( index );
}

/*
================
potionRowOn
================
*/
export function potionRowOn( potion: readonly number[], index: number ): boolean {
	const slot = potionSlot( potion, index );
	return (potionGauge( index ) ? slot.b : slot.a) !== 0;
}

/*
================
withPotionRowOn

A gauge row switched on with no percent yet takes percent; check-box rows
ignore it.
================
*/
export function withPotionRowOn( potion: readonly number[], index: number, on: boolean, percent: number ): number[] {
	const slot = potionSlot( potion, index );
	if ( !potionGauge( index ) ) return withPotionSlot( potion, index, { ...slot, a: on ? 1 : 0 } );
	return withPotionSlot( potion, index, { ...slot, a: on && slot.a === 0 ? percent : slot.a, b: on ? 1 : 0 } );
}

/*
================
withPotionRowPercent
================
*/
export function withPotionRowPercent( potion: readonly number[], index: number, percent: number ): number[] {
	return withPotionSlot( potion, index, { ...potionSlot( potion, index ), a: percent } );
}

/*
================
withPotionRowItem

Binds a RefItem ID to a row; 0 clears it.
================
*/
export function withPotionRowItem( potion: readonly number[], index: number, item: number ): number[] {
	return withPotionSlot( potion, index, { ...potionSlot( potion, index ), item } );
}

/*
================
withHuntByte
================
*/
export function withHuntByte( hunt: readonly number[], index: number, value: number ): number[] {
	const next = hunt.slice();
	next[index] = value & 0xff;
	return next;
}

/*
================
macroGearFlagIndex

The skillFlags index that holds the Weapon or Shield bag slot.
================
*/
export function macroGearFlagIndex( kind: MacroGearKind ): number {
	return kind === "weapon" ? MACRO_SKILL_FLAG_WEAPON : MACRO_SKILL_FLAG_SHIELD;
}

/*
================
macroGearBagSlot

The absolute bag slot bound to Weapon or Shield, or 0 when unset / out of
the native 0x0d..0x6f range.
================
*/
export function macroGearBagSlot( skillFlags: readonly number[], kind: MacroGearKind ): number {
	const slot = skillFlags[macroGearFlagIndex( kind )] ?? 0;
	if ( slot < MACRO_GEAR_SLOT_MIN || slot >= MACRO_GEAR_SLOT_MAX ) return 0;
	return slot;
}

/*
================
withSkillFlag

One of the four skill flag bytes replaced; 0 clears a Weapon / Shield bind.
================
*/
export function withSkillFlag( skillFlags: readonly number[], index: number, value: number ): number[] {
	if ( index < 0 || index >= MACRO_SKILL_FLAGS ) throw new Error( "Invalid SRO Macro skill flag" );
	const next = skillFlags.slice();
	while ( next.length < MACRO_SKILL_FLAGS ) next.push( 0 );
	next[index] = value & 0xff;
	return next;
}

/*
================
withMacroGearSlot

Binds an absolute bag slot to Weapon or Shield; 0 clears it.
================
*/
export function withMacroGearSlot(
	skillFlags: readonly number[],
	kind: MacroGearKind,
	bagSlot: number
): number[] {
	return withSkillFlag( skillFlags, macroGearFlagIndex( kind ), bagSlot );
}

// ============================================================================

/*
================
createReader

A bounds-checked little-endian cursor; any read past the end is a
protocol error, never a partial settings value.
================
*/
function createReader( payload: Uint8Array ) {
	const view = viewOf( payload );
	let offset = 0;
	const need = ( bytes: number ) => {
		if ( offset + bytes > payload.length ) throw new Error( "Invalid SRO Macro frame" );
	};
	return {
		u8(): number {
			need( 1 );
			return payload[offset++]!;
		},
		u16(): number {
			need( 2 );
			const value = view.getUint16( offset, true );
			offset += 2;
			return value;
		},
		u32(): number {
			need( 4 );
			const value = view.getUint32( offset, true );
			offset += 4;
			return value;
		},
		bytes( count: number ): number[] {
			need( count );
			const out = Array.from( payload.subarray( offset, offset + count ) );
			offset += count;
			return out;
		},
		done: () => offset === payload.length
	};
}

type Reader = ReturnType<typeof createReader>;

/*
================
readSkillList

u16 bytes, then bytes/5 entries; bytes 1 is the empty list and carries
one zero byte.
================
*/
function readSkillList( reader: Reader ): MacroSkill[] {
	const size = reader.u16();
	if ( size === EMPTY_LIST_BYTES ) {
		reader.u8();
		return [];
	}
	if ( size % SKILL_RECORD_BYTES !== 0 || size / SKILL_RECORD_BYTES > MACRO_SKILLS_PER_LIST ) {
		throw new Error( "Invalid SRO Macro skill list" );
	}
	const list: MacroSkill[] = [];
	for ( let i = 0; i < size / SKILL_RECORD_BYTES; i++ ) {
		const slot = reader.u8();
		list.push( { slot, skill: reader.u32() } );
	}
	return list;
}

/*
================
decodeMacroConfig

Applies one 0x3555 frame to current. Mode 0 replaces everything: the ON
states reset, and a section whose version or length the client does not
know loads the defaults (00cfcd80). Mode 2 only updates availability, and
a kind that loses it is switched off (00cfdb77).
================
*/
export function decodeMacroConfig( payload: Uint8Array, current: MacroSettings ): MacroSettings {
	const reader = createReader( payload );
	const mode = reader.u8();
	if ( mode === CONFIG_AVAILABILITY ) {
		const available = reader.u8();
		if ( !reader.done() ) throw new Error( "Invalid SRO Macro availability" );
		return {
			...current,
			available,
			potionOn: current.potionOn && (available & 1) !== 0,
			skillOn: current.skillOn && (available & 2) !== 0,
			huntOn: current.huntOn && (available & 4) !== 0
		};
	}
	if ( mode !== CONFIG_FULL ) throw new Error( "Invalid SRO Macro config mode" );
	const defaults = defaultMacroSettings();
	let next: MacroSettings = { ...defaults, available: reader.u8() };
	const sections = reader.u8();
	for ( let i = 0; i < sections; i++ ) {
		const kind = reader.u8();
		const on = reader.u8() === 1;
		const version = reader.u16();
		if ( kind === MACRO_SKILL ) {
			if ( version !== SKILL_VERSION ) throw new Error( "Unsupported SRO Macro skill version" );
			const skillFlags = [ reader.u8(), reader.u8() ];
			const attack = readSkillList( reader );
			skillFlags.push( reader.u8(), reader.u8() );
			next = { ...next, skillOn: on, skillFlags, attack, buff: readSkillList( reader ) };
			continue;
		}
		const length = reader.u16();
		const block = reader.bytes( length );
		if ( kind === MACRO_POTION ) {
			const known = version === POTION_VERSION && length === MACRO_POTION_BYTES;
			next = { ...next, potionOn: on, potion: known ? block : defaults.potion };
		} else if ( kind === MACRO_HUNT ) {
			const known = version === HUNT_VERSION && length === MACRO_HUNT_BYTES;
			next = { ...next, huntOn: on, hunt: known ? block : defaults.hunt };
		} else {
			throw new Error( "Invalid SRO Macro section" );
		}
	}
	if ( !reader.done() ) throw new Error( "Invalid SRO Macro config" );
	return next;
}

/*
================
decodeMacroAnswer
================
*/
export function decodeMacroAnswer( payload: Uint8Array ): MacroAnswer {
	const reader = createReader( payload );
	const action = reader.u8(), kind = reader.u8(), result = reader.u16();
	if ( action === MACRO_ACTION_SWITCH && result === MACRO_RESULT_OK ) {
		const state = reader.u8() === 1;
		if ( !reader.done() ) throw new Error( "Invalid SRO Macro answer" );
		return { action, kind, result, state };
	}
	if ( !reader.done() || (action !== MACRO_ACTION_SWITCH && action !== MACRO_ACTION_SAVE) ) {
		throw new Error( "Invalid SRO Macro answer" );
	}
	return { action, kind, result };
}

// ============================================================================

/*
================
encodeMacroSwitch
================
*/
export function encodeMacroSwitch( kind: MacroKind, on: boolean ): Uint8Array {
	return Uint8Array.of( MACRO_ACTION_SWITCH, kind, on ? 1 : 0 );
}

/*
================
skillListBytes
================
*/
function skillListBytes( list: readonly MacroSkill[] ): number[] {
	if ( list.length === 0 ) return [ EMPTY_LIST_BYTES, 0, 0 ];
	const size = list.length * SKILL_RECORD_BYTES;
	const out = [ size & 0xff, size >>> 8 ];
	for ( const { slot, skill } of list ) {
		out.push( slot & 0xff, skill & 0xff, (skill >>> 8) & 0xff, (skill >>> 16) & 0xff, (skill >>> 24) & 0xff );
	}
	return out;
}

/*
================
sameSkills
================
*/
function sameSkills( a: readonly MacroSkill[], b: readonly MacroSkill[] ): boolean {
	return a.length === b.length && a.every( ( skill, i ) => skill.slot === b[i]!.slot && skill.skill === b[i]!.skill );
}

/*
================
sameBytes
================
*/
function sameBytes( a: readonly number[], b: readonly number[] ): boolean {
	return a.length === b.length && a.every( ( value, i ) => value === b[i] );
}

/*
================
encodeMacroSave

One 0x7554 save of kind, or null when nothing in it changed. The potion
save carries the whole block after one unread byte (00694170; zero here).
The hunt save carries pending[1] then the 0xC block. The skill save
carries only the changed records, as the native onlyIfChanged save does.
================
*/
export function encodeMacroSave( kind: MacroKind, next: MacroSettings, current: MacroSettings ): Uint8Array | null {
	const head = [ MACRO_ACTION_SAVE, kind ];
	if ( kind === MACRO_POTION ) {
		if ( next.potion.length !== MACRO_POTION_BYTES ) throw new Error( "Invalid SRO Macro potion block" );
		if ( sameBytes( next.potion, current.potion ) ) return null;
		return Uint8Array.from( [ ...head, POTION_VERSION, 0, 0, ...next.potion ] );
	}
	if ( kind === MACRO_HUNT ) {
		if ( next.hunt.length !== MACRO_HUNT_BYTES ) throw new Error( "Invalid SRO Macro hunt block" );
		if ( sameBytes( next.hunt, current.hunt ) ) return null;
		// 00694170: u16 version, then pending[1] (the non-UI flag byte), then the 0xC block.
		return Uint8Array.from( [ ...head, HUNT_VERSION, 0, next.hunt[1]! & 0xff, ...next.hunt ] );
	}
	const body: number[] = [];
	SKILL_FLAG_TAGS.forEach( ( tag, i ) => {
		if ( next.skillFlags[i] !== current.skillFlags[i] ) body.push( tag, next.skillFlags[i]! & 0xff );
	} );
	const lists = [ [ next.attack, current.attack ], [ next.buff, current.buff ] ] as const;
	SKILL_LIST_TAGS.forEach( ( tag, i ) => {
		const [wanted, held] = lists[i]!;
		if ( wanted.length > MACRO_SKILLS_PER_LIST ) throw new Error( "Invalid SRO Macro skill list" );
		if ( !sameSkills( wanted, held ) ) body.push( tag, ...skillListBytes( wanted ) );
	} );
	if ( body.length === 0 ) return null;
	return Uint8Array.from( [ ...head, SKILL_VERSION, 0, ...body ] );
}

/*
================
commitMacroSave

The 0xB554 save success: the pending section becomes live (the native
memcpy of the pending block, 00cfca30). The ON states are not part of
a save.
================
*/
export function commitMacroSave( live: MacroSettings, kind: MacroKind, pending: MacroSettings ): MacroSettings {
	if ( kind === MACRO_POTION ) return { ...live, potion: pending.potion };
	if ( kind === MACRO_HUNT ) return { ...live, hunt: pending.hunt };
	return { ...live, skillFlags: pending.skillFlags, attack: pending.attack, buff: pending.buff };
}
