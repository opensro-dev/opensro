/*
===========================================================================

job-honor-view.ts - the Job Honor Rank window as draw operations

Port-only, not native (#3530). Turns the window's state
(hud/job-honor-hud.ts) into a flat list of draw operations; the
renderer (ui.ts) paints them with its ordinary primitives and owns
nothing else of the window. My honor sits over the ranking table
(tabs All, Trader and Thief; periods Current and Previous; columns
#, Name, Job, Points and Tier). All times are the feed's server time.

===========================================================================
*/

import type { JobHonorFeed } from "@/engine/contracts/job-honor";
import type { UiRect } from "@/engine/contracts/ui";
import {
	honorCountdownText,
	honorMonthText,
	honorRows,
	honorToday,
	jobName,
	JOB_HONOR_TABS,
	tierName,
	type JobHonorPeriod,
	type JobHonorTab
} from "@/engine/foundation/ui/job-honor";

type Color = readonly [number, number, number, number];

export const JOB_HONOR_WIDTH = 560;
export const JOB_HONOR_HEIGHT = 520;
// The rows one page of the ranking table holds.
export const JOB_HONOR_ROWS = 12;
const ROW_SKIN = "/assets/images/Media_extracted/interface/option/opt_key02.png";

const WHITE: Color = [ 1, 1, 1, 1 ];
const GOLD: Color = [ 1, .85, .5, 1 ];
const DIM: Color = [ .72, .72, .72, 1 ];

/*
================
JobHonorOp

One draw operation, in screen pixels. "tile" repeats its picture
over rect; "rect" is a flat wash; "button" is a hit area whose
pixels are its own skinned rect (ui.ts admits the texture and draws
it while the resource lasts). Same shapes as the Event Calendar's.
================
*/
export type JobHonorOp =
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
JobHonorViewState

The parts of the state owner's state() this window draws. Declared
here, not imported from the hud: the two are siblings under ui.ts,
and the renderer hands the state across.
================
*/
export interface JobHonorViewState {
	readonly feed: JobHonorFeed | null;
	readonly tab: JobHonorTab;
	readonly period: JobHonorPeriod;
	readonly page: number;
	readonly serverNowMs: number;
}

/*
================
JobHonorViewInput

Everything the window reads, in one struct: the screen origin, the
option tile's texture ("" draws no plates) and the window's state.
================
*/
export interface JobHonorViewInput {
	readonly origin: readonly [number, number];
	readonly tile: string;
	readonly state: JobHonorViewState;
}

/*
================
jobHonorView

The whole window's content, back to front: the My honor plate and
its lines, the tab and period buttons, the table head, one page of
rows (or the period's empty line) and the pager.
================
*/
export function jobHonorView( input: JobHonorViewInput ): JobHonorOp[] {
	const ops: JobHonorOp[] = [];
	const [ox, oy] = input.origin;
	const state = input.state;
	const feed = state.feed;

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

 One skinned choice: the tabs, the periods and the pager.
 ================
 */
	function button( id: string, label: string, rect: UiRect, selected: boolean ) {
		ops.push( {
			kind: "button",
			id,
			label,
			rect,
			texture: ROW_SKIN,
			color: selected ? GOLD : WHITE,
			selected
		} );
		text( label, rect, selected ? GOLD : WHITE, 1, 1 );
	}

	if ( input.tile ) {
		ops.push( { kind: "tile", path: input.tile, rect: [ ox + 16, oy + 44, JOB_HONOR_WIDTH - 32, 106 ] } );
	}
	text( "My honor", [ ox + 28, oy + 52, 300, 14 ], GOLD );
	if ( !feed ) {
		text( "Waiting for the ranking...", [ ox + 28, oy + 96, JOB_HONOR_WIDTH - 56, 14 ], WHITE, 1 );
	} else if ( !feed.own.present ) {
		text( "You are not in this ranking.", [ ox + 28, oy + 96, JOB_HONOR_WIDTH - 56, 14 ], WHITE, 1 );
	} else {
		const own = feed.own;
		text(
			`Today ${honorToday( feed )} points - daily cap left ${own.capLeft}`,
			[ ox + 28, oy + 72, JOB_HONOR_WIDTH - 56, 13 ],
			WHITE
		);
		text(
			"This month: " + honorMonthText( feed ),
			[ ox + 28, oy + 88, JOB_HONOR_WIDTH - 56, 13 ],
			WHITE
		);
		text(
			own.place > 0 ? "Place #" + own.place + " (" + jobName( own.job ) + ")" : "You are not in this ranking.",
			[ ox + 28, oy + 104, JOB_HONOR_WIDTH - 56, 13 ],
			own.place > 0 ? WHITE : GOLD
		);
		text(
			"Tier " + tierName( own.tier ) +
				(own.tonight !== own.tier ? " - " + tierName( own.tonight ) + " after tonight" : "") +
				" · Tiers change 05:00 (" + honorCountdownText( feed, state.serverNowMs ) + ")",
			[ ox + 28, oy + 120, JOB_HONOR_WIDTH - 56, 13 ],
			DIM
		);
	}

	JOB_HONOR_TABS.forEach( ( tab, index ) => {
		button(
			"job-honor-tab:" + tab.id,
			tab.label,
			[ ox + 28 + index * 96, oy + 160, 88, 22 ],
			state.tab === tab.id
		);
	} );
	button(
		"job-honor-period:current",
		"Current",
		[ ox + JOB_HONOR_WIDTH - 196, oy + 160, 80, 22 ],
		state.period === "current"
	);
	button(
		"job-honor-period:previous",
		"Previous",
		[ ox + JOB_HONOR_WIDTH - 108, oy + 160, 80, 22 ],
		state.period === "previous"
	);

	const headY = oy + 192;
	text( "#", [ ox + 28, headY, 28, 13 ], DIM );
	text( "Name", [ ox + 60, headY, 190, 13 ], DIM );
	text( "Job", [ ox + 254, headY, 70, 13 ], DIM );
	text( "Points", [ ox + 328, headY, 70, 13 ], DIM, 2 );
	text( "Tier", [ ox + 402, headY, 130, 13 ], DIM );

	const rows = feed ? honorRows( feed, state.tab, state.period ) : [];
	const pages = Math.max( 1, Math.ceil( rows.length / JOB_HONOR_ROWS ) );
	const page = Math.min( state.page, pages - 1 );
	const shown = rows.slice( page * JOB_HONOR_ROWS, (page + 1) * JOB_HONOR_ROWS );
	shown.forEach( ( row, index ) => {
		const y = oy + 212 + index * 20;
		text( String( page * JOB_HONOR_ROWS + index + 1 ), [ ox + 28, y, 28, 13 ], WHITE );
		text( row.alias, [ ox + 60, y, 190, 13 ], WHITE );
		text( jobName( row.job ), [ ox + 254, y, 70, 13 ], WHITE );
		text( String( row.points ), [ ox + 328, y, 70, 13 ], WHITE, 2 );
		text(
			tierName( row.tier ) + (row.tonight !== row.tier ? " -> " + tierName( row.tonight ) : ""),
			[ ox + 402, y, 130, 13 ],
			row.tier >= 2 ? GOLD : WHITE
		);
	} );
	if ( feed && !rows.length ) {
		text(
			state.period === "previous" && feed.previousEndMs === 0 ? "No previous period yet." : "No rankings yet.",
			[ ox + 28, oy + 212, JOB_HONOR_WIDTH - 56, 14 ],
			DIM,
			1
		);
	}
	if ( pages > 1 ) {
		button( "job-honor-page:-1", "<", [ ox + 28, oy + JOB_HONOR_HEIGHT - 44, 34, 22 ], false );
		text( "Page " + (page + 1) + "/" + pages, [ ox + 66, oy + JOB_HONOR_HEIGHT - 44, 90, 22 ], DIM, 1, 1 );
		button(
			"job-honor-page:1",
			">",
			[ ox + 160, oy + JOB_HONOR_HEIGHT - 44, 34, 22 ],
			false
		);
	}
	text(
		"Points only from verified job events.",
		[ ox + JOB_HONOR_WIDTH - 328, oy + JOB_HONOR_HEIGHT - 40, 300, 14 ],
		DIM,
		2
	);
	return ops;
}

/*
================
jobHonorPictures

Every picture the window may draw this frame, for the window's
resource admission: the plates and the buttons' skin.
================
*/
export function jobHonorPictures( ops: readonly JobHonorOp[] ): string[] {
	const out = new Set<string>();
	for ( const op of ops ) {
		if ( op.kind === "tile" ) out.add( op.path );
		else if ( op.kind === "button" && op.texture ) out.add( op.texture );
	}
	return [ ...out ];
}
