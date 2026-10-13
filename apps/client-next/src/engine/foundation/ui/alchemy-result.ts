/*
===========================================================================

alchemy-result.ts - what the alchemy window shows when a reinforcement ends

CIFAlchemyReinforce's result arm (62B0B0, from the 0xB373 and 0xB651
answers) swaps its effect sprite to the success or failure atlas, plays it
once and writes the outcome to the chat. The worker records the outcome
(AlchemyOutcome); the window draws and words it from here.

A successful magic or attribute stone is worded by what it changed, not by
the native enhancement-level line (stoneResultLines): port-only, not
native; owner-approved deviation (2026-10-13).

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { AlchemyOutcome } from "@/engine/contracts/item-process";
import { itemTooltipMagic } from "./item-tooltip-magic";
import { itemTooltipStats } from "./item-tooltip-stats";

// 62B0B0's flag bits: 0x10 success, 0x20 failure (with 1 the enhancement
// level fell and 2 the durability fell), 0x40 the item was destroyed, 0x80
// a stone's roll failed (0xB651 [2, 0x23]; the stone is spent).
export const ALCHEMY_SUCCESS = 0x10;
export const ALCHEMY_FAILURE = 0x20;
export const ALCHEMY_DESTROYED = 0x40;
export const ALCHEMY_STONE_FAILED = 0x80;
const FAILURE_LEVEL = 0x01;
const FAILURE_DURABILITY = 0x02;
const FAILURE_DETAIL = 0x0f;
// CIFAlchemyReinforce_OnCreate (625600): the effect is 64 px cells, sixteen
// of them (a 4x4 atlas), drawn into control 50; its state timer 0xA steps
// one cell every 50 ms and hides the sprite once it wraps (62C350).
export const ALCHEMY_EFFECT_CELL = 64;
export const ALCHEMY_EFFECT_FRAMES = 16;
export const ALCHEMY_EFFECT_FRAME_MS = 50;
const ALCHEMY_EFFECT_COLUMNS = 4;

/*
================
alchemyEffectTexture

The atlas 62B0B0 loads: success for 0x10, the failure atlas otherwise.
================
*/
export function alchemyEffectTexture( flags: number ): string {
	return flags & ALCHEMY_SUCCESS ? "interface/alchemy/alcm_effect_success" : "interface/alchemy/alcm_effect_fail_1";
}

/*
================
alchemyEffectCell

The atlas cell shown elapsedMs after the result, as UV [u, v, w, h], or
null once the single play has ended.
================
*/
export function alchemyEffectCell( elapsedMs: number ): [number, number, number, number] | null {
	if ( !(elapsedMs >= 0) ) return null;
	const frame = Math.floor( elapsedMs / ALCHEMY_EFFECT_FRAME_MS );
	if ( frame >= ALCHEMY_EFFECT_FRAMES ) return null;
	const size = 1 / ALCHEMY_EFFECT_COLUMNS;
	return [ (frame % ALCHEMY_EFFECT_COLUMNS) * size, Math.floor( frame / ALCHEMY_EFFECT_COLUMNS ) * size, size, size ];
}

/*
================
alchemyResultLines

62B0B0's chat lines, in its order. Success names the new enhancement
level. A failure without detail says it failed; a fallen level names the
new level (or says it is gone) and the drop; a fallen durability names the
new value and the drop. A destroyed item says so, and a stone whose roll
failed says the alchemy failed (its 0x80 arm).
INFERENCE: native reports the item's maximum durability; the port's item
rows carry the current durability, which a failed reinforcement lowers
with it, so the drop is read from that.
================
*/
export function alchemyResultLines( outcome: AlchemyOutcome, copy: ( key: string ) => string ): string[] {
	const format = ( key: string, ...values: number[] ) => {
		let text = copy( key );
		for ( const value of values ) text = text.replace( "%d", String( value ) );
		return text;
	};
	const { flags } = outcome;
	if ( flags & ALCHEMY_SUCCESS ) {
		const stone = outcome.stone ? stoneResultLines( outcome.stone.before, outcome.stone.after, copy ) : [];
		return stone.length ? stone : [ format( "UIIT_MSG_REINFORCERR_SUCCESS", outcome.plus ) ];
	}
	if ( flags & ALCHEMY_DESTROYED ) return [ copy( "UIIT_MSG_REINFORCERR_BREAKDOWN" ) ];
	if ( flags & ALCHEMY_STONE_FAILED ) return [ copy( "UIIT_MSG_REINFORCERR_FAIL" ) ];
	if ( !(flags & ALCHEMY_FAILURE) ) return [];
	if ( !(flags & FAILURE_DETAIL) ) return [ copy( "UIIT_MSG_REINFORCERR_FAIL" ) ];
	const lines: string[] = [];
	if ( flags & FAILURE_LEVEL ) {
		lines.push(
			outcome.plus === 0 ?
				copy( "UIIT_MSG_REINFORCERR_FAIL_RESULT_OPTLV_ZERO" ) :
				format(
					"UIIT_MSG_REINFORCERR_FAIL_RESULT_OPTLV_DOWN",
					outcome.plus,
					outcome.previousPlus - outcome.plus
				)
		);
	}
	if ( flags & FAILURE_DURABILITY ) {
		lines.push(
			format(
				"UIIT_MSG_REINFORCERR_FAILDOWN_DURABILITY",
				outcome.durability,
				outcome.previousDurability - outcome.durability
			)
		);
	}
	return lines;
}

/*
================
stoneResultLines

Port-only, not native; owner-approved deviation (2026-10-13). Natively
62B0B0 answers every successful reinforcement with the enhancement-level
line, which for a stone names the item's plus rather than what the stone
did. A magic or attribute stone instead names each stat it changed, from
the old value to the new, in the item tooltip's own words: the white stats
(55B540's variance rows) and the magic options are compared separately,
as both lists can name the same stat. A stat the item did not have before
changed from 0.
================
*/
export function stoneResultLines(
	before: InventoryItem,
	after: InventoryItem,
	copy: ( key: string ) => string
): string[] {
	// A tooltip row is the stat's name, then its value: the value starts at
	// the first number or bracket ("Str 3 Increase (+50%)", "Luck(2 times)").
	const statRow = /^(.*?)\s*((?:[+-]?\d|\().*)$/;
	const lines: string[] = [];
	for ( const rows of [ itemTooltipStats, itemTooltipMagic ] ) {
		const previous = new Map<string, string>();
		for ( const row of rows( before, copy ) ) {
			const match = statRow.exec( row.value );
			if ( match ) previous.set( match[1]!, match[2]! );
		}
		for ( const row of rows( after, copy ) ) {
			const match = statRow.exec( row.value );
			if ( !match ) continue;
			const old = previous.get( match[1]! ) ?? "0";
			if ( old !== match[2] ) lines.push( `${match[1]} changed from ${old} to ${match[2]}` );
		}
	}
	return lines;
}
