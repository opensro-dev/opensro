/*
===========================================================================

caravan-hire-window.ts - the AI caravan hire window and lead badge, drawn

Port-only, not native (SRO_AI_CARAVANS). v1.150 has no such window, so it
is built from the client's own chrome: the framed window, the message
tile (com_bg_tile_b), the native buttons and com_checkbutton. The list is
the server's (0x3FCF, foundation/gameplay/caravan-hire.ts): available
agents in green, the player's own renewable ones in gold, the rest in red
with the reason. caravan-hire-hud.ts owns the filter, the page and the
hire being confirmed; this file lays them out and owns nothing.

The window is modal: ui.ts clears the frame's controls and blocks the
whole screen before it draws.

===========================================================================
*/

import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import type { CaravanHire } from "@/engine/foundation/gameplay/caravan-hire";
import { MESSAGE_TILE } from "@/engine/foundation/ui/party-proposal";
import { normalTile } from "@/engine/foundation/ui/normal-tile";
import type { UiAuthoredFrame } from "../frame/frame-contract";
import type { CaravanHireHud, CaravanHirePick } from "./caravan-hire-hud";

const WIDTH = 540;
const HEIGHT = 470;
const ROW_HEIGHT = 22;
const ROWS_PER_PAGE = 12;
const PICK_WIDTH = 340;
const PICK_HEIGHT = 210;
const CHECK = "/assets/images/Media_extracted/interface/ifcommon/com_checkbutton_";
const GREEN: UiQuad["color"] = [ 0.47, 0.92, 0.47, 1 ];
const RED: UiQuad["color"] = [ 0.95, 0.42, 0.38, 1 ];
const MUTED: UiQuad["color"] = [ 0.82, 0.77, 0.66, 1 ];
const GOLD: UiQuad["color"] = [ 1, 0.84, 0.5, 1 ];
const RULE: UiQuad["color"] = [ 0.46, 0.4, 0.28, 0.8 ];
const STRIPE: UiQuad["color"] = [ 1, 1, 1, 0.04 ];
const SHADE: UiQuad["color"] = [ 0, 0, 0, 0.45 ];
const CENTRED = { hAlign: 1 } as const;
const ROW_TEXT = { vAlign: 1 } as const;

/*
================
CaravanHirePaint

The frame's primitives, handed in by ui.ts, their owner.
================
*/
export type CaravanHireMark = readonly [number, number, number, number];
export interface CaravanHirePaint {
	beginWindow( frame: UiAuthoredFrame ): CaravanHireMark;
	endWindow( frame: UiAuthoredFrame, mark: CaravanHireMark, owner: string ): void;
	windowBox(
		frame: UiAuthoredFrame,
		spec: { title: string; x: number; y: number; width: number; height: number; }
	): void;
	closeButton( frame: UiAuthoredFrame, x: number, y: number, id: "caravan-hire-close" ): void;
	button(
		frame: UiAuthoredFrame,
		spec: { id: string; value: string; x: number; y: number; width: number; disabled?: boolean; }
	): void;
	rect( frame: UiAuthoredFrame, spec: { r: UiRect; color: UiQuad["color"]; } ): void;
	image( frame: UiAuthoredFrame, spec: { r: UiRect; path: string; } ): void;
	// One line of text from (x, y), clipped to the screen.
	label( frame: UiAuthoredFrame, spec: { value: string; x: number; y: number; color?: UiQuad["color"]; } ): void;
}

/*
================
caravanLeadCaption

What the party board's lead badge says: its label, and the tooltip
naming what a click does. self is the hirer's own badge.
================
*/
export function caravanLeadCaption( leads: boolean, self: boolean, display: string ) {
	const who = self ? "You" : display, verb = self ? "lead" : "leads";
	if ( leads ) {
		return {
			label: `${who} ${verb} the caravan`,
			help: `${who} ${verb} the caravan. Click to ${self ? "let your first Trader lead" : "lead it yourself"}.`
		};
	}
	if ( self ) {
		return {
			label: "Lead the caravan yourself",
			help: "Lead the caravan yourself: your hired Traders follow you."
		};
	}
	return {
		label: `Let ${display} lead the caravan`,
		help: `Let ${display} lead the caravan: your other Traders follow ${display}.`
	};
}

/*
================
caption

One line of text, centred unless asked otherwise (only the list rows
are not), clipped to the screen.
================
*/
function caption(
	frame: UiAuthoredFrame,
	value: string,
	r: UiRect,
	color: UiQuad["color"],
	options: { readonly hAlign?: number; readonly vAlign?: number; } = CENTRED
) {
	frame.quads.push( ...frame.text.quads( value, r, frame.full, color, options ) );
}

/*
================
messageTile
================
*/
function messageTile( frame: UiAuthoredFrame, r: UiRect ) {
	frame.quads.push( ...normalTile( r, MESSAGE_TILE, frame.resources.size( MESSAGE_TILE ), frame.full ) );
}

/*
================
drawCaravanHire
================
*/
export function drawCaravanHire(
	frame: UiAuthoredFrame,
	paint: CaravanHirePaint,
	view: { readonly hire: CaravanHire; readonly hud: CaravanHireHud; }
) {
	const { hire, hud } = view;
	const onlyAvailable = hud.onlyAvailable();
	const rows = onlyAvailable ? hire.rows.filter( r => r.available ) : hire.rows;
	const pages = Math.max( 1, Math.ceil( rows.length / ROWS_PER_PAGE ) );
	const page = hud.page( pages );
	const px = Math.floor( (frame.w - WIDTH) / 2 ), py = Math.floor( (frame.h - HEIGHT) / 2 );
	const admission = paint.beginWindow( frame );
	paint.windowBox( frame, { title: hire.title, x: px, y: py, width: WIDTH, height: HEIGHT } );
	paint.closeButton( frame, px + WIDTH - 26, py + 6, "caravan-hire-close" );
	frame.paths.push( MESSAGE_TILE, ...frame.chrome.parts.map( part => frame.chrome.frame + part + ".png" ) );
	messageTile( frame, [ px + 16, py + 40, WIDTH - 32, HEIGHT - 56 ] );
	const available = hire.rows.filter( r => r.available ).length;
	paint.label( frame, {
		value: `${available} of ${hire.rows.length} available - hire fee ${hire.fee} gold`,
		x: px + 28,
		y: py + 50,
		color: GOLD
	} );
	if ( hire.stars.length > 0 ) {
		paint.label( frame, {
			value: "Traders also carry goods you buy for them.",
			x: px + 28,
			y: py + 68,
			color: MUTED
		} );
	}
	paint.image( frame, {
		r: [ px + WIDTH - 196, py + 50, 16, 16 ],
		path: CHECK + (onlyAvailable ? "on" : "off") + ".png"
	} );
	paint.label( frame, { value: "Show only available", x: px + WIDTH - 174, y: py + 50 } );
	frame.controls.push( {
		id: "caravan-hire-filter",
		label: "Show only available",
		kind: "button",
		rect: [ px + WIDTH - 200, py + 46, 176, 22 ],
		selected: onlyAvailable
	} );
	const top = py + 92;
	paint.rect( frame, { r: [ px + 28, top - 4, WIDTH - 56, 1 ], color: RULE } );
	paint.label( frame, { value: "Name", x: px + 34, y: top, color: MUTED } );
	paint.label( frame, { value: "Status", x: px + 330, y: top, color: MUTED } );
	paint.rect( frame, { r: [ px + 28, top + 18, WIDTH - 56, 1 ], color: RULE } );
	rows.slice( page * ROWS_PER_PAGE, (page + 1) * ROWS_PER_PAGE ).forEach( ( row, i ) => {
		const y = top + 24 + i * ROW_HEIGHT, color = row.available ? GREEN : row.extend ? GOLD : RED;
		if ( i % 2 === 0 ) paint.rect( frame, { r: [ px + 28, y - 2, WIDTH - 56, ROW_HEIGHT ], color: STRIPE } );
		const who = row.role === "Thief" ? `${row.display} (Thief)` : `${row.display} (${row.role}, ${row.town})`;
		caption( frame, who, [ px + 34, y, 290, ROW_HEIGHT - 4 ], color, ROW_TEXT );
		caption( frame, row.status, [ px + 330, y, WIDTH - 366, ROW_HEIGHT - 4 ], color, ROW_TEXT );
		if ( row.available || row.extend ) {
			frame.controls.push( {
				id: "caravan-hire-row:" + row.name,
				label: row.extend ? `Extend ${row.display}'s hire` : `Hire ${row.display}`,
				kind: "button",
				rect: [ px + 28, y - 2, WIDTH - 56, ROW_HEIGHT ]
			} );
		}
	} );
	if ( rows.length === 0 ) {
		caption(
			frame,
			onlyAvailable ? "Nobody is free right now." : "Nobody works here.",
			[ px + 28, top + 40, WIDTH - 56, 20 ],
			MUTED
		);
	}
	const bottom = py + HEIGHT - 92;
	paint.button( frame, {
		id: "caravan-hire-prev",
		value: "Previous",
		x: px + 40,
		y: bottom,
		width: 100,
		disabled: page === 0
	} );
	caption( frame, `Page ${page + 1} / ${pages}`, [ px + 150, bottom + 5, 120, 20 ], frame.white );
	paint.button( frame, {
		id: "caravan-hire-next",
		value: "Next page",
		x: px + 280,
		y: bottom,
		width: 100,
		disabled: page >= pages - 1
	} );
	paint.button( frame, { id: "caravan-hire-refresh", value: "Refresh", x: px + 400, y: bottom, width: 100 } );
	caption(
		frame,
		hire.message || "Click a green name to hire.",
		[ px + 24, bottom + 34, WIDTH - 48, 36 ],
		hire.message ? GOLD : MUTED
	);
	const pick = hud.pick();
	if ( pick ) drawPick( frame, paint, { hire, pick, px, py } );
	paint.endWindow( frame, admission, "caravan-hire" );
}

/*
================
drawPick

The confirm over the list. Every hire asks first; a Trader's also picks
its goods' stars, and the hirer pays the fee plus those goods.
================
*/
function drawPick(
	frame: UiAuthoredFrame,
	paint: CaravanHirePaint,
	at: {
		hire: CaravanHire;
		pick: Readonly<CaravanHirePick>;
		px: number;
		py: number;
	}
) {
	const { hire, pick, px, py } = at;
	const trader = pick.stars > 0, choice = trader ? hire.stars[pick.stars - 1] : undefined;
	const sw = PICK_WIDTH, sx = px + Math.floor( (WIDTH - sw) / 2 ), sy = py + 120;
	paint.rect( frame, { r: [ px, py, WIDTH, HEIGHT ], color: SHADE } );
	paint.windowBox( frame, {
		title: `${pick.extend ? "Extend hire" : "Confirm hire"}: ${pick.display}`,
		x: sx,
		y: sy,
		width: sw,
		height: PICK_HEIGHT
	} );
	messageTile( frame, [ sx + 14, sy + 38, sw - 28, PICK_HEIGHT - 52 ] );
	if ( trader ) {
		caption( frame, "How many stars of goods will this Trader carry?", [ sx + 20, sy + 46, sw - 40, 18 ], MUTED );
		paint.button( frame, {
			id: "caravan-hire-star-less",
			value: "-",
			x: sx + 70,
			y: sy + 74,
			width: 40,
			disabled: pick.stars <= 1
		} );
		caption( frame, `${pick.stars} ${pick.stars === 1 ? "star" : "stars"}`, [ sx + 115, sy + 78, 110, 20 ], GOLD );
		paint.button( frame, {
			id: "caravan-hire-star-more",
			value: "+",
			x: sx + 230,
			y: sy + 74,
			width: 40,
			disabled: pick.stars >= hire.stars.length
		} );
		if ( choice ) {
			caption( frame, `Goods: ${choice.goods} gold`, [ sx + 20, sy + 112, sw - 40, 18 ], frame.white );
			caption(
				frame,
				`Pay ${choice.total} gold in total (fee included)?`,
				[ sx + 20, sy + 132, sw - 40, 18 ],
				GOLD
			);
		}
	} else {
		caption(
			frame,
			pick.extend ?
				`Extend ${pick.display}'s hire by one hour?` :
				`Hire ${pick.display} (${pick.role}) for one trade run?`,
			[ sx + 20, sy + 66, sw - 40, 18 ],
			frame.white
		);
		caption( frame, `Pay ${hire.fee} gold?`, [ sx + 20, sy + 98, sw - 40, 18 ], GOLD );
	}
	paint.button( frame, {
		id: "caravan-hire-star-ok",
		value: pick.extend ? "Extend" : "Hire",
		x: sx + 40,
		y: sy + 166,
		width: 110
	} );
	paint.button( frame, {
		id: "caravan-hire-star-cancel",
		value: "Cancel",
		x: sx + sw - 150,
		y: sy + 166,
		width: 110
	} );
}
