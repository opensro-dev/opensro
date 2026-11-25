/*
===========================================================================

styria.ts - the Styria Clash windows' words and shaping

Pure shaping for the Styria Clash windows (port-only, #3648 slice 5):
the side and result words, the countdowns the schedule and the
match clock speak, and the arena manager's conversation rows the
registration window's buttons ride. The texts themselves are the
client's copy (ui.ts's Styria table); this file only shapes what
the server and the conversation said.

===========================================================================
*/

import type { StyriaFeed } from "@/engine/contracts/styria";
import type { NpcConversation } from "@/engine/foundation/gameplay/npc-dialogue";

// The wire's sides.
export const STYRIA_HUNTERS = 0;
export const STYRIA_THIEVES = 1;

// The manager conversation's Styria rows, by their copy symbols
// (the server's styria.ServiceApply / ServiceCancel titles).
export const STYRIA_APPLY_SYMBOL = "UIIT_PORT_STYRIA_APPLY";
export const STYRIA_CANCEL_SYMBOL = "UIIT_PORT_STYRIA_CANCEL";

/*
================
styriaSideName

The word a side number speaks; anything else is no side.
================
*/
export function styriaSideName( side: number ): string {
	switch ( side ) {
		case STYRIA_HUNTERS:
			return "Hunters";
		case STYRIA_THIEVES:
			return "Thieves";
		default:
			return "";
	}
}

/*
================
styriaClockText

A duration as the windows speak it: minutes and seconds, never
negative, hours folded into the minutes.
================
*/
export function styriaClockText( ms: number ): string {
	const total = Math.max( 0, Math.floor( ms / 1000 ) );
	const minutes = Math.floor( total / 60 );
	const seconds = total % 60;
	return `${minutes}:${seconds < 10 ? "0" : ""}${seconds}`;
}

/*
================
styriaCountdown

The registration window's countdown: while applications are shut
it counts to their opening, then to the match start. Null when the
schedule says nothing (no start carried).
================
*/
export function styriaCountdown(
	feed: StyriaFeed,
	serverNowMs: number
): { readonly opens: boolean; readonly ms: number; } | null {
	if ( feed.nextStartMs <= 0 ) return null;
	if ( feed.windowOpensMs > 0 && serverNowMs < feed.windowOpensMs ) {
		return { opens: true, ms: feed.windowOpensMs - serverNowMs };
	}
	return { opens: false, ms: Math.max( 0, feed.nextStartMs - serverNowMs ) };
}

/*
================
styriaResultWord

The result window's verdict for this viewer: their side won, lost,
or the clock drew. Sides the viewer does not hold read by the
winning side's name instead (styriaOutcomeText).
================
*/
export function styriaResultWord( feed: StyriaFeed ): "win" | "lose" | "draw" {
	if ( feed.outcome === 3 ) return "draw";
	const winner = feed.outcome === 1 ? STYRIA_HUNTERS : feed.outcome === 2 ? STYRIA_THIEVES : -1;
	if ( winner < 0 ) return "draw";
	return winner === feed.ownSide ? "win" : "lose";
}

/*
================
styriaConversationChoices

The arena manager's rows in an open conversation: the choice
numbers of the Apply and Cancel rows, by their copy symbols. The
registration window's buttons send these same choices, so the
window and the talk list can never disagree about what a click
means.
================
*/
export function styriaConversationChoices(
	conversation: NpcConversation | undefined
): { readonly apply?: number; readonly cancel?: number; } {
	if ( !conversation || conversation.phase !== "ready" ) return {};
	let apply: number | undefined, cancel: number | undefined;
	for ( const option of conversation.dialogue.options ) {
		if ( option.symbol === STYRIA_APPLY_SYMBOL ) apply = option.choice;
		if ( option.symbol === STYRIA_CANCEL_SYMBOL ) cancel = option.choice;
	}
	return {
		...(apply !== undefined ? { apply } : {}),
		...(cancel !== undefined ? { cancel } : {})
	};
}
