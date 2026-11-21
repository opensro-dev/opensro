/*
===========================================================================

sro-macro-notices.ts - SRO Macro system chat lines (port-only)

Port-only, not native (#3907): the v1.696 client prints UIIT_STT_SRO_MACRO_*
rows from 00dade90 (town / empty-potion ON refuse), the hunt return helpers
(68ea80 reasons), Energy of Life / hammer use, and map-move hunt end
(68b330). v1.150 has none of those keys, so the port keeps the English
text from textuisystem (research media/macro_rows.txt) the same way the
settings lane already hard-codes "ON/OFF Error" / "Save Error".

===========================================================================
*/
import {
	MACRO_HUNT,
	MACRO_POTION,
	MACRO_POTION_SLOTS,
	MACRO_SKILL,
	type MacroKind,
	potionSlot
} from "@/engine/foundation/gameplay/sro-macro";
import type { MacroHuntAction } from "@/engine/foundation/gameplay/sro-macro-hunt";

// 00dade90 / 006861c0 / return-reason strings (English column).
export const MACRO_NOTICE_TOWN_REFUSE = "You cannot start the skill/hunting macro in town.";
export const MACRO_NOTICE_TOWN_OFF = "The skill/hunting macro is disabled in towns.";
export const MACRO_NOTICE_POTION_ON_FAIL =
	"There is no registered item in auto potion. Please register items and try again.";
export const MACRO_NOTICE_TELEPORT_OFF = "You've moved out of the map, Hunting Macro will end.";
export const MACRO_NOTICE_HP_POTION_EMPTY = "Insufficient HP Potions.  Using Return Scroll.";
export const MACRO_NOTICE_MP_POTION_EMPTY = "Insufficient MP Potions.  Using Return Scroll.";
export const MACRO_NOTICE_ITEM_DURA = "Item durability has reached 0.  Using Return Scroll.";
export const MACRO_NOTICE_NOT_HAMMER = "No Repair Hammer.  Using Return Scroll.";
export const MACRO_NOTICE_TIME_OVER = "Auto-Hunt time expired.  Using Return Scroll.";
export const MACRO_NOTICE_NOT_REVIVAL = "No Resurrection Scroll.  Using Return Scroll.";
export const MACRO_NOTICE_DIE_TOWN = "Character is dead.  Resurrecting in town.";
export const MACRO_NOTICE_RETURN = "Using Return Scroll.";
export const MACRO_NOTICE_ENERGY = "Using Energy of Life";
export const MACRO_NOTICE_HAMMER = "Using Repair Hammer";

/*
================
macroPotionHasRegisteredItem

00dade90 potion arm: any of the nine RefItem slots non-zero.
================
*/
export function macroPotionHasRegisteredItem( potion: readonly number[] ): boolean {
	for ( let i = 0; i < MACRO_POTION_SLOTS; i++ ) {
		if ( potionSlot( potion, i ).item !== 0 ) return true;
	}
	return false;
}

/*
================
MacroSwitchGateFacts
================
*/
export interface MacroSwitchGateFacts {
	readonly inTown: boolean;
	readonly potionHasItem: boolean;
}

/*
================
macroSwitchRefuseNotice

Client-side refuse before 0x7554 (00dade90). Town blocks skill/hunt ON;
potion ON with every row empty prints AUTO_POTION_ON_FAIL. OFF always
passes. inTown needs a region-combat fact the worker does not have yet —
callers pass false until that lands.
================
*/
export function macroSwitchRefuseNotice(
	kind: MacroKind,
	on: boolean,
	facts: MacroSwitchGateFacts
): string | undefined {
	if ( !on ) return undefined;
	if ( kind === MACRO_POTION ) {
		return facts.potionHasItem ? undefined : MACRO_NOTICE_POTION_ON_FAIL;
	}
	if ( (kind === MACRO_SKILL || kind === MACRO_HUNT) && facts.inTown ) {
		return MACRO_NOTICE_TOWN_REFUSE;
	}
	return undefined;
}

/*
================
macroHuntNoticeText

Chat line for a hunt action that the native prints when it fires the
matching return / use helper. undefined when the action is silent.
================
*/
export function macroHuntNoticeText( action: MacroHuntAction ): string | undefined {
	switch ( action.kind ) {
		case "return-scroll":
			switch ( action.reason ) {
				case "timer":
				case "vip-time":
					return MACRO_NOTICE_TIME_OVER;
				case "potion-hp":
					return MACRO_NOTICE_HP_POTION_EMPTY;
				case "potion-mp":
					return MACRO_NOTICE_MP_POTION_EMPTY;
				case "repair":
					return action.noHammer ? MACRO_NOTICE_NOT_HAMMER : MACRO_NOTICE_ITEM_DURA;
				default:
					return MACRO_NOTICE_RETURN;
			}
		case "resurrect-town":
			return action.noScroll ? MACRO_NOTICE_NOT_REVIVAL : MACRO_NOTICE_DIE_TOWN;
		case "energy-of-life":
			return MACRO_NOTICE_ENERGY;
		case "repair-hammer":
			return MACRO_NOTICE_HAMMER;
		default:
			return undefined;
	}
}

/*
================
macroTownDisableKinds

006861c0: while skill or hunt is on and the player is in town, switch both
OFF and print MACRO_OFF. Returns the kinds to turn off (empty when none).
================
*/
export function macroTownDisableKinds(
	skillOn: boolean,
	huntOn: boolean,
	inTown: boolean
): readonly MacroKind[] {
	if ( !inTown || (!skillOn && !huntOn) ) return [];
	const next: MacroKind[] = [];
	if ( huntOn ) next.push( MACRO_HUNT );
	if ( skillOn ) next.push( MACRO_SKILL );
	return next;
}
