/*
===========================================================================

styria-view.ts - the Styria Clash windows as draw operations

Port-only, not native (#3648 slice 5). Turns the windows' state
(hud/styria-hud.ts) into a flat list of draw operations; the renderer
(ui.ts) paints them with its ordinary primitives and owns nothing
else of the windows. One panel serves the arena: the registration
standing while the manager's conversation is open, the score while
entered, and the result the clock decided; a small strip over the
world carries the score while the match runs.

===========================================================================
*/

import type { StyriaFeed } from "@/engine/contracts/styria";
import type { UiRect } from "@/engine/contracts/ui";
import { styriaClockText, styriaCountdown, styriaResultWord, styriaSideName } from "@/engine/foundation/ui/styria";
import type { StyriaHudState } from "./styria-hud";

type Color = readonly [number, number, number, number];

export const STYRIA_WIDTH = 440;
export const STYRIA_HEIGHT = 308;
export const STYRIA_STRIP_WIDTH = 320;
export const STYRIA_STRIP_HEIGHT = 40;
const ROW_SKIN = "/assets/images/Media_extracted/interface/option/opt_key02.png";

const WHITE: Color = [ 1, 1, 1, 1 ];
const GOLD: Color = [ 1, .85, .5, 1 ];
const DIM: Color = [ .72, .72, .72, 1 ];
const LOSE: Color = [ 1, .45, .4, 1 ];
const STRIP_WASH: Color = [ 0, 0, 0, .55 ];

/*
================
StyriaOp

One draw operation, in screen pixels. Same shapes as the Job Honor
window's (hud/job-honor-view.ts).
================
*/
export type StyriaOp =
	| { readonly kind: "tile"; readonly path: string; readonly rect: UiRect; }
	| { readonly kind: "rect"; readonly rect: UiRect; readonly color: Color; }
	| {
		readonly kind: "text";
		readonly value: string;
		readonly rect: UiRect;
		readonly color: Color;
		readonly hAlign: number;
		readonly vAlign: number;
	}
	| {
		readonly kind: "button";
		readonly id: string;
		readonly label: string;
		readonly rect: UiRect;
		readonly texture: string;
		readonly color: Color;
		readonly selected: boolean;
	};

/*
================
StyriaViewInput

Everything the panel reads, in one struct: the screen origin, the
option tile's texture ("" draws no plates), the windows' state, the
client's copy and the manager conversation's Styria rows (the
choice numbers the Apply and Cancel buttons send).
================
*/
export interface StyriaViewInput {
	readonly origin: readonly [number, number];
	readonly tile: string;
	readonly state: StyriaHudState;
	readonly copy: ( key: string ) => string;
	readonly choices: { readonly apply?: number; readonly cancel?: number; };
}

/*
================
styriaView

The panel's content, back to front: the standing result when one
stands, else the registration or the entered score.
================
*/
export function styriaView( input: StyriaViewInput ): StyriaOp[] {
	const ops: StyriaOp[] = [];
	const [ox, oy] = input.origin;
	const { state, copy } = input;

	/*
 ================
 text

 One line of the window's text, aligned as asked.
 ================
 */
	function text( value: string, rect: UiRect, color: Color, hAlign = 0, vAlign = 0 ) {
		ops.push( { kind: "text", value, rect, color, hAlign, vAlign } );
	}

	/*
 ================
 button

 One skinned choice.
 ================
 */
	function button( id: string, label: string, rect: UiRect ) {
		ops.push( { kind: "button", id, label, rect, texture: ROW_SKIN, color: WHITE, selected: false } );
		text( label, rect, WHITE, 1, 1 );
	}

	if ( input.tile ) {
		ops.push( { kind: "tile", path: input.tile, rect: [ ox + 16, oy + 44, STYRIA_WIDTH - 32, 120 ] } );
	}

	const result = state.result;
	if ( result ) {
		styriaResultOps( result, text, button, copy, ox, oy );
		return ops;
	}
	const feed = state.feed;
	if ( !feed ) {
		text( copy( "UIIT_STT_ARENA_JOB_WAR" ), [ ox + 28, oy + 56, STYRIA_WIDTH - 56, 16 ], GOLD, 1 );
		text( copy( "UIIT_PORT_STYRIA_WAITING" ), [ ox + 28, oy + 120, STYRIA_WIDTH - 56, 14 ], WHITE, 1 );
		return ops;
	}
	if ( feed.phase === "match" ) {
		styriaScoreOps( feed, state.serverNowMs, text, copy, ox, oy );
		return ops;
	}

	// Registration: the arena's rule, the schedule, the queue and
	// the viewer's own standing in it.
	text( copy( "UIIT_STT_ARENA_JOB_WAR" ), [ ox + 28, oy + 52, STYRIA_WIDTH - 56, 16 ], GOLD, 1 );
	const desc = copy( "UIIT_PORT_STYRIA_DESC" ).split( "\n" );
	desc.forEach( ( line, index ) => {
		text( line, [ ox + 28, oy + 84 + index * 15, STYRIA_WIDTH - 56, 14 ], WHITE, 1 );
	} );
	const countdown = styriaCountdown( feed, state.serverNowMs );
	if ( countdown ) {
		text(
			copy( countdown.opens ? "UIIT_PORT_STYRIA_OPENS_IN" : "UIIT_PORT_STYRIA_STARTS_IN" ) +
				" " + styriaClockText( countdown.ms ),
			[ ox + 28, oy + 132, STYRIA_WIDTH - 56, 15 ],
			GOLD,
			1
		);
	}
	text(
		copy( "UIIT_PORT_STYRIA_APPLICANTS" ) + ":  " +
			styriaSideName( 0 ) + " " + feed.hunters + "  ·  " + styriaSideName( 1 ) + " " + feed.thieves,
		[ ox + 28, oy + 168, STYRIA_WIDTH - 56, 14 ],
		WHITE,
		1
	);
	if ( feed.ownState === 1 ) {
		text(
			copy( "UIIT_PORT_STYRIA_YOUR_SIDE" ) + ": " + styriaSideName( feed.ownSide ),
			[ ox + 28, oy + 196, STYRIA_WIDTH - 56, 14 ],
			GOLD,
			1
		);
		text(
			copy( "UIIT_PORT_STYRIA_APPLIED" ),
			[ ox + 28, oy + 214, STYRIA_WIDTH - 56, 14 ],
			WHITE,
			1
		);
	} else {
		text(
			copy( "UIIT_PORT_STYRIA_SUIT_HINT" ),
			[ ox + 28, oy + 196, STYRIA_WIDTH - 56, 14 ],
			DIM,
			1
		);
	}
	const choice = feed.ownState === 1 ? input.choices.cancel : input.choices.apply;
	if ( choice !== undefined ) {
		button(
			"npc-choice:" + choice,
			copy( feed.ownState === 1 ? "UIIT_PORT_STYRIA_CANCEL" : "UIIT_PORT_STYRIA_APPLY" ),
			[ ox + STYRIA_WIDTH / 2 - 90, oy + STYRIA_HEIGHT - 52, 180, 24 ]
		);
	}
	return ops;
}

/*
================
styriaScoreOps

The entered standing in the panel: the score and the clock.
================
*/
function styriaScoreOps(
	feed: StyriaFeed,
	serverNowMs: number,
	text: ( value: string, rect: UiRect, color: Color, hAlign?: number, vAlign?: number ) => void,
	copy: ( key: string ) => string,
	ox: number,
	oy: number
) {
	text( copy( "UIIT_STT_ARENA_JOB_WAR" ), [ ox + 28, oy + 52, STYRIA_WIDTH - 56, 16 ], GOLD, 1 );
	text(
		styriaSideName( 0 ) + " " + feed.scoreHunters + "  -  " + feed.scoreThieves + " " + styriaSideName( 1 ),
		[ ox + 28, oy + 96, STYRIA_WIDTH - 56, 20 ],
		WHITE,
		1
	);
	text(
		copy( "UIIT_PORT_STYRIA_TIME_LEFT" ) + " " + styriaClockText( feed.phaseEndsMs - serverNowMs ),
		[ ox + 28, oy + 132, STYRIA_WIDTH - 56, 15 ],
		GOLD,
		1
	);
	if ( feed.ownSide >= 0 ) {
		text(
			copy( "UIIT_PORT_STYRIA_YOUR_SIDE" ) + ": " + styriaSideName( feed.ownSide ),
			[ ox + 28, oy + 168, STYRIA_WIDTH - 56, 14 ],
			WHITE,
			1
		);
	}
}

/*
================
styriaResultOps

The clock's verdict in the panel: the word for this viewer, the
final score, and the reward figures the drain pays (the later
client's result labels; the arena coin is not among them - the
group's config pays none).
================
*/
function styriaResultOps(
	feed: StyriaFeed,
	text: ( value: string, rect: UiRect, color: Color, hAlign?: number, vAlign?: number ) => void,
	button: ( id: string, label: string, rect: UiRect ) => void,
	copy: ( key: string ) => string,
	ox: number,
	oy: number
) {
	text( copy( "UIIT_PORT_STYRIA_RESULT" ), [ ox + 28, oy + 52, STYRIA_WIDTH - 56, 16 ], GOLD, 1 );
	const word = styriaResultWord( feed );
	text(
		copy(
			word === "win" ?
				"UIIT_PORT_STYRIA_WIN" :
				word === "lose" ?
				"UIIT_PORT_STYRIA_LOSE" :
				"UIIT_PORT_STYRIA_DRAW"
		),
		[ ox + 28, oy + 84, STYRIA_WIDTH - 56, 22 ],
		word === "win" ? GOLD : word === "lose" ? LOSE : WHITE,
		1
	);
	text(
		styriaSideName( 0 ) + " " + feed.scoreHunters + "  -  " + feed.scoreThieves + " " + styriaSideName( 1 ),
		[ ox + 28, oy + 122, STYRIA_WIDTH - 56, 16 ],
		WHITE,
		1
	);
	text( copy( "UIIT_PORT_STYRIA_REWARDS" ), [ ox + 28, oy + 164, STYRIA_WIDTH - 56, 14 ], GOLD, 1 );
	text(
		copy( "UIIT_PORT_STYRIA_SKILL_EXP" ).replace( "%d", String( feed.skillExp ) ),
		[ ox + 28, oy + 188, STYRIA_WIDTH - 56, 14 ],
		WHITE,
		1
	);
	text(
		copy( "UIIT_PORT_STYRIA_JOB_EXP" ).replace( "%d", String( feed.jobExp ) ),
		[ ox + 28, oy + 206, STYRIA_WIDTH - 56, 14 ],
		WHITE,
		1
	);
	button( "styria-dismiss", copy( "UIIT_PORT_STYRIA_CONFIRM" ), [
		ox + STYRIA_WIDTH / 2 - 70,
		oy + STYRIA_HEIGHT - 52,
		140,
		24
	] );
}

/*
================
styriaStripOps

The score over the world while the match runs: the two sides, the
score and the clock, on a washed strip.
================
*/
export function styriaStripOps( input: {
	readonly origin: readonly [number, number];
	readonly state: StyriaHudState;
} ): StyriaOp[] {
	const feed = input.state.feed;
	if ( !feed || feed.phase !== "match" ) return [];
	const [ox, oy] = input.origin;
	return [
		{ kind: "rect", rect: [ ox, oy, STYRIA_STRIP_WIDTH, STYRIA_STRIP_HEIGHT ], color: STRIP_WASH },
		{
			kind: "text",
			value: styriaSideName( 0 ) + " " + feed.scoreHunters + "  -  " + feed.scoreThieves + " " +
				styriaSideName( 1 ) + "   ·   " + styriaClockText( feed.phaseEndsMs - input.state.serverNowMs ),
			rect: [ ox, oy, STYRIA_STRIP_WIDTH, STYRIA_STRIP_HEIGHT ],
			color: WHITE,
			hAlign: 1,
			vAlign: 1
		}
	];
}

/*
================
styriaPictures

Every picture the windows may draw this frame, for the window's
resource admission: the plates and the buttons' skin.
================
*/
export function styriaPictures( ops: readonly StyriaOp[] ): string[] {
	const out = new Set<string>();
	for ( const op of ops ) {
		if ( op.kind === "tile" ) out.add( op.path );
		else if ( op.kind === "button" && op.texture ) out.add( op.texture );
	}
	return [ ...out ];
}
