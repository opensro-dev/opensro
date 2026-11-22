/*
===========================================================================

sro-macro-skill.ts - the SRO Macro's auto-skill loop (port-only)

Port-only, not native (#3907): the v1.696 macro's Auto-Skills, read from
the v1.696 client (research note sro-macro.md, "Auto-skill engine"). The
macro engine runs an ordered step table (0x1549448); two of its steps own
the skill lists:

  buff step (00686bc0)    walks the buff list (tag 5), one entry a tick
  attack step (00687520)  walks the attack list (tag 2) at the target

Each step is gated by the cast re-arm (rule 2 in table 0x17b3688) and
ends the tick whenever it looks at an entry, cast or skipped. A step that
has walked its whole list rewinds and lets the tick go on, so the attack
step looks at one entry after each full buff pass. A cast arms rule 2
for the skill's action time (+0x60 +0x64 +0x68), at least 500 ms.

Before an attack cast the native may equip the Weapon / Shield tab's bag
item (flag bytes tags 3 and 4 via 0068ede0) and arm rule 0x12 for 2000 ms
(0x7d0) instead of casting that tick. This module owns that swap pick;
the worker issues the inventory-move.

This module is pure: the cursors, the rewind, the re-arm and the gear
swap choice. The worker judges each entry (learned, admitted, off
cooldown, buff absent) and sends the cast through the hotbar's skill
command.

===========================================================================
*/
import type { GameplayCommand } from "@/engine/contracts/gameplay";
import { equipmentSocket } from "./item-activation";
import { WRONG_WEAPON } from "./press-admission";
import {
	MACRO_GEAR_SLOT_MAX,
	MACRO_GEAR_SLOT_MIN,
	MACRO_SKILL_FLAG_SHIELD,
	MACRO_SKILL_FLAG_WEAPON,
	type MacroGearKind,
	type MacroSkill
} from "./sro-macro";

// 00686bc0 / 00687520 arm rule 2 for at least 0x1f4 ms after a cast.
export const MACRO_SKILL_REARM_MS = 500;
// 00687520 arms rule 0x12 for 0x7d0 ms after a Weapon / Shield swap.
export const MACRO_SKILL_SWAP_MS = 2000;

const WEAPON_SOCKET = 6;
const SHIELD_SOCKET = 7;

/*
================
MacroSkillLoop

The two steps' cursors ([obj+0x1a4] buff, [obj+0x1a8] attack) and the
time rule 2 lets the next tick run.
================
*/
export interface MacroSkillLoop {
	readonly buff: number;
	readonly attack: number;
	readonly readyAt: number;
}

/*
================
MacroSkillPick

The entry this tick looks at, and the loop with that entry's cursor
already advanced: the native moves past an entry whether it casts it or
skips it.
================
*/
export interface MacroSkillPick {
	readonly loop: MacroSkillLoop;
	readonly list: "buff" | "attack";
	readonly entry: MacroSkill;
}

/*
================
emptyMacroSkillLoop
================
*/
export function emptyMacroSkillLoop(): MacroSkillLoop {
	return { buff: 0, attack: 0, readyAt: 0 };
}

/*
================
pickMacroSkill

One tick of the two steps. Returns the entry to judge, or the loop alone
when the tick ends with nothing to judge: rule 2 still re-arming, both
lists walked to their ends (each rewinds), or the attack step reached
with no living monster targeted (it passes and keeps its cursor).
================
*/
export function pickMacroSkill(
	loop: MacroSkillLoop,
	lists: { readonly buff: readonly MacroSkill[]; readonly attack: readonly MacroSkill[]; },
	now: number,
	monsterTargeted: boolean
): MacroSkillPick | { readonly loop: MacroSkillLoop; } {
	if ( now < loop.readyAt ) return { loop };
	let next = loop;
	const buff = lists.buff[next.buff];
	if ( buff ) return { loop: { ...next, buff: next.buff + 1 }, list: "buff", entry: buff };
	if ( next.buff !== 0 ) next = { ...next, buff: 0 };
	if ( !monsterTargeted ) return { loop: next };
	const attack = lists.attack[next.attack];
	if ( attack ) return { loop: { ...next, attack: next.attack + 1 }, list: "attack", entry: attack };
	return { loop: next.attack === 0 ? next : { ...next, attack: 0 } };
}

/*
================
macroSkillCast

A cast went out: rule 2 holds the loop for the skill's action time, at
least MACRO_SKILL_REARM_MS.
================
*/
export function macroSkillCast( loop: MacroSkillLoop, now: number, actionMs: number ): MacroSkillLoop {
	return { ...loop, readyAt: now + Math.max( MACRO_SKILL_REARM_MS, actionMs ) };
}

/*
================
macroGearAdmits

Weapon tab: equipment that equips to socket 6 (TID3 6, 68c9d0). Shield
tab: equipment that equips to socket 7 (TID3 4 sub 1/2, 68c670). The
CIFEquipment table (equipmentSocket / 594980) is the same gate the bag
drop uses.
================
*/
export function macroGearAdmits( kind: MacroGearKind, typeFlags: number ): boolean {
	const socket = equipmentSocket( typeFlags );
	if ( kind === "weapon" ) return socket === WEAPON_SOCKET;
	return socket === SHIELD_SOCKET;
}

/*
================
macroSkillSwapHelps

True when a press refusal is an equipment gate the Weapon / Shield bag
binds can clear (wrong weapon 0x300d). Other refusals are not swap-worthy.
================
*/
export function macroSkillSwapHelps( code: number ): boolean {
	return code === WRONG_WEAPON;
}

/*
================
macroInventoryAfterSwap

The inventory after one bag→socket equip (items exchange slots). Used to
ask whether the swap would actually clear a wrong-weapon refusal before
spending the 2000 ms re-arm.
================
*/
export function macroInventoryAfterSwap<T extends { readonly slot: number; }>(
	inventory: readonly T[],
	source: number,
	destination: number
): T[] {
	const next: T[] = [];
	let moved: T | undefined;
	let worn: T | undefined;
	for ( const row of inventory ) {
		if ( row.slot === source ) moved = row;
		else if ( row.slot === destination ) worn = row;
		else next.push( row );
	}
	if ( moved ) next.push( { ...moved, slot: destination } );
	if ( worn ) next.push( { ...worn, slot: source } );
	return next;
}

/*
================
MacroSwapKind

Which Auto-Skills gear-bind slots to look at. 68ede0 equips whatever
weapon or shield sits in that bag slot (bind kind does not filter the
item class) — after a shield buff the primary often rests in the Shield
bind slot and must be wearable again from there.
================
*/
export type MacroSwapKind = MacroGearKind;

/*
================
macroSkillSwapCandidates

Weapon / Shield equip candidates (0068ede0): each named bind bag slot
whose item is a weapon (68c9d0) or shield/arrow (68c670), not already on
its socket. Native does not require the item class to match the bind
label — a 2H that landed in the Shield bind after a shield cast is
re-equipped from that same slot on the next attack. The worker still
runs an after-press check so a useless shield equip cannot clear a
weaponKinds refuse and oscillate.
================
*/
export function macroSkillSwapCandidates(
	skillFlags: readonly number[],
	inventory: readonly { readonly slot: number; readonly typeFlags: number; readonly refObjId?: number; }[],
	kinds: readonly MacroSwapKind[] = [ "weapon", "shield" ]
): Extract<GameplayCommand, { kind: "inventory-move"; }>[] {
	const out: Extract<GameplayCommand, { kind: "inventory-move"; }>[] = [];
	const seen = new Set<number>();
	for ( const kind of kinds ) {
		const index = kind === "weapon" ? MACRO_SKILL_FLAG_WEAPON : MACRO_SKILL_FLAG_SHIELD;
		const abs = skillFlags[index] ?? 0;
		if ( abs < MACRO_GEAR_SLOT_MIN || abs >= MACRO_GEAR_SLOT_MAX ) continue;
		if ( seen.has( abs ) ) continue;
		seen.add( abs );
		const item = inventory.find( row => row.slot === abs );
		// 68ede0: 68c670 (shield socket) or 68c9d0 (weapon socket).
		if ( !item || (!macroGearAdmits( "weapon", item.typeFlags ) && !macroGearAdmits( "shield", item.typeFlags )) ) {
			continue;
		}
		const destination = equipmentSocket( item.typeFlags );
		if ( destination !== WEAPON_SOCKET && destination !== SHIELD_SOCKET ) continue;
		const equipped = inventory.find( row => row.slot === destination );
		// Already wearing this exact stack — nothing to do.
		if ( equipped && item.refObjId !== undefined && equipped.refObjId === item.refObjId ) continue;
		out.push( { kind: "inventory-move", source: abs, destination, quantity: 0, activation: true } );
	}
	return out;
}

/*
================
macroSkillWantSwap

The first candidate from macroSkillSwapCandidates (Weapon then Shield).
The worker walks the full list with an after-press check so a shield
equip that would not clear the refuse is skipped.
================
*/
export function macroSkillWantSwap(
	skillFlags: readonly number[],
	inventory: readonly { readonly slot: number; readonly typeFlags: number; readonly refObjId?: number; }[],
	kinds?: readonly MacroSwapKind[]
): Extract<GameplayCommand, { kind: "inventory-move"; }> | null {
	return macroSkillSwapCandidates( skillFlags, inventory, kinds )[0] ?? null;
}

/*
================
scrubMacroGear

Clear a Weapon / Shield bind whose bag slot is empty or no longer holds
the admitted class (same scrub idea as scrubMacroPotion).
================
*/
export function scrubMacroGear<T extends { readonly skillFlags: readonly number[]; }>(
	settings: T,
	inventory: readonly { readonly slot: number; readonly typeFlags: number; }[]
): T {
	const flags = settings.skillFlags.slice();
	while ( flags.length < 4 ) flags.push( 0 );
	const kinds: readonly MacroGearKind[] = [ "weapon", "shield" ];
	for ( const kind of kinds ) {
		const index = kind === "weapon" ? MACRO_SKILL_FLAG_WEAPON : MACRO_SKILL_FLAG_SHIELD;
		const abs = flags[index]!;
		if ( abs < MACRO_GEAR_SLOT_MIN || abs >= MACRO_GEAR_SLOT_MAX ) {
			flags[index] = 0;
			continue;
		}
		const item = inventory.find( row => row.slot === abs );
		if ( !item || !macroGearAdmits( kind, item.typeFlags ) ) flags[index] = 0;
	}
	if ( flags.every( ( value, i ) => value === (settings.skillFlags[i] ?? 0) ) ) return settings;
	return { ...settings, skillFlags: flags };
}

/*
================
MacroBuffSkill

The cast skill's identity for the buff-present gate: its id, its skilldata
group, and the published replace descriptor when the row has one.
================
*/
export interface MacroBuffSkill {
	readonly id: number;
	readonly group: number;
	readonly replace?: {
		readonly category: number;
		readonly group: number;
		readonly cbuf?: boolean;
	};
}

/*
================
macroBuffAlreadyOn

The buff step's "already applied" gate (00686bc0 → 00abd630): skip when
the caster already holds this buff. Exact skill id matches the native
list walk; the port also treats the same skilldata group or the same
SkillUiReplace group as present, so a live shield (or any self-buff)
whose attached effect id is a sibling rank is not re-cast every pass.
================
*/
export function macroBuffAlreadyOn(
	skill: MacroBuffSkill,
	effects: readonly { readonly skill: number; }[],
	catalog: readonly MacroBuffSkill[]
): boolean {
	if ( effects.some( effect => effect.skill === skill.id ) ) return true;
	if (
		skill.group && effects.some( effect => {
			const row = catalog.find( entry => entry.id === effect.skill );
			return !!row && row.group === skill.group;
		} )
	) {
		return true;
	}
	const replace = skill.replace;
	if ( !replace || replace.cbuf ) return false;
	return effects.some( effect => {
		const old = catalog.find( entry => entry.id === effect.skill )?.replace;
		return !!old && !old.cbuf && old.category === replace.category && old.group === replace.group;
	} );
}
