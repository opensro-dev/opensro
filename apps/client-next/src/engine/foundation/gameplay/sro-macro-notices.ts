/*
===========================================================================

sro-macro-notices.ts - SRO Macro system chat lines (UIIT keys)

The v1.696 client prints UIIT_STT_SRO_MACRO_* rows from 00dade90 (town /
empty-potion ON refuse), the hunt return helpers (68ea80 reasons), Energy
of Life / hammer use, and map-move hunt end (68b330). v1.150 has none of
those keys; the product text catalog completes them
(scripts/build/shared/textResources.mjs) and the worker queues the key
so the HUD resolves English through textuisystem.en.json.

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

// 00dade90 / 006861c0 / return-reason UIIT keys (v1.696 textuisystem).
// TOWN_REFUSE (CANTUSEMACRO) is evidenced by the v1.696 string table
// only: the client pushes no reference to it and its English cell is
// empty, so the town refuse using this row is the port's inference from
// the key's name, not a traced call (the other 13 keys are pushed by
// their handlers).
export const MACRO_NOTICE_TOWN_REFUSE = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_CANTUSEMACRO";
export const MACRO_NOTICE_TOWN_OFF = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_MACRO_OFF";
export const MACRO_NOTICE_POTION_ON_FAIL = "UIIT_STT_SRO_MACRO_AUTO_POTION_ON_FAIL";
export const MACRO_NOTICE_TELEPORT_OFF = "UIIT_STT_SRO_MACRO_AUTO_HUNTING_TELEPORT_OFF";
export const MACRO_NOTICE_HP_POTION_EMPTY = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_HP_POTION_EMPTY";
export const MACRO_NOTICE_MP_POTION_EMPTY = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_MP_POTION_EMPTY";
export const MACRO_NOTICE_ITEM_DURA = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_ITEM_DURA";
export const MACRO_NOTICE_NOT_HAMMER = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_NOT_HAMMER";
export const MACRO_NOTICE_TIME_OVER = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_TIME_OVER";
export const MACRO_NOTICE_NOT_REVIVAL = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_NOT_REVIVAL_SCROLL";
export const MACRO_NOTICE_DIE_TOWN = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_DIE";
// Native key spelling keeps ERORR.
export const MACRO_NOTICE_RETURN = "UIIT_STT_SRO_MACRO_SYSTEM_RETURN_TOWN_ERORR";
export const MACRO_NOTICE_ENERGY = "UIIT_STT_SRO_MACRO_SYSTEM_WHAN_POTION";
export const MACRO_NOTICE_HAMMER = "UIIT_STT_SRO_MACRO_SYSTEM_USE_HAMMER";

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
passes. inTown is macroRegionIsTown(pose.regionId) from region-combat.ts.
Returns the UIIT key to queue, or undefined when the switch may proceed.
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

UIIT key for a hunt action that the native prints when it fires the
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
export type MacroCombatKind = typeof MACRO_SKILL | typeof MACRO_HUNT;

export function macroTownDisableKinds(
	skillOn: boolean,
	huntOn: boolean,
	inTown: boolean
): readonly MacroCombatKind[] {
	if ( !inTown || (!skillOn && !huntOn) ) return [];
	const next: MacroCombatKind[] = [];
	if ( huntOn ) next.push( MACRO_HUNT );
	if ( skillOn ) next.push( MACRO_SKILL );
	return next;
}
