/*
===========================================================================

attendance-view.ts - the Attendance Calendar window as draw operations

Port-only, not native (#3255). Turns the window's state
(hud/attendance-hud.ts) into a flat list of draw operations; the
renderer (ui.ts) paints them with its ordinary primitives and owns
nothing else of the window. The Calendar tab is the month's grid (the
checked days marked, today highlighted, END on the last day) over the
Check Attendance button; the Rewards tab lists the Continuity and
Accumulation tiers with a Receive button each. All times are the feed's
server time; the day turns at 04:00 UTC.

===========================================================================
*/

import type { AttendanceFeed } from "@/engine/contracts/attendance";
import type { UiRect } from "@/engine/contracts/ui";
import { dayChecked, tierClaimed, tierEarned } from "@/engine/foundation/gameplay/attendance";

type Color = readonly [number, number, number, number];

export const ATTENDANCE_WIDTH = 560;
export const ATTENDANCE_HEIGHT = 480;
const ROW_SKIN = "/assets/images/Media_extracted/interface/option/opt_key02.png";
const GRID_COLUMNS = 7;
const CELL_W = 70;
const CELL_H = 44;

const WHITE: Color = [ 1, 1, 1, 1 ];
const GOLD: Color = [ 1, .85, .5, 1 ];
const DIM: Color = [ .72, .72, .72, 1 ];
const CHECKED_WASH: Color = [ .25, .75, .35, .4 ];
const TODAY_WASH: Color = [ 1, .85, .5, .35 ];

/*
================
AttendanceOp

One draw operation, in screen pixels. "tile" repeats its picture over
rect; "rect" is a flat wash; "button" is a hit area whose pixels are its
own skinned rect (ui.ts admits the texture and draws it while the
resource lasts). Same shapes as the Job Honor window's, with disabled
buttons.
================
*/
export type AttendanceOp =
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
		readonly disabled: boolean;
	};

/*
================
AttendanceViewState

The parts of the state owner's state() this window draws. Declared here,
not imported from the hud: the two are siblings under ui.ts, and the
renderer hands the state across.
================
*/
export interface AttendanceViewState {
	readonly feed: AttendanceFeed | null;
	readonly tab: "calendar" | "rewards";
	readonly selectedDay: number;
	readonly serverNowMs: number;
}

/*
================
AttendanceViewInput

Everything the window reads, in one struct: the screen origin, the option
tile's texture ("" draws no plates) and the window's state.
================
*/
export interface AttendanceViewInput {
	readonly origin: readonly [number, number];
	readonly tile: string;
	readonly state: AttendanceViewState;
}

/*
================
attendanceMonthText

The month's heading: YYYYMM as YYYY-MM.
================
*/
export function attendanceMonthText( monthKey: number ): string {
	return Math.floor( monthKey / 100 ) + "-" + String( monthKey % 100 ).padStart( 2, "0" );
}

/*
================
attendanceCountdownText

Time to the next day turn as h:mm, by the clock the state runs.
================
*/
export function attendanceCountdownText( feed: AttendanceFeed, serverNowMs: number ): string {
	const minutes = Math.max( 0, Math.floor( (feed.nextResetMs - serverNowMs) / 60_000 ) );
	return Math.floor( minutes / 60 ) + ":" + String( minutes % 60 ).padStart( 2, "0" );
}

/*
================
attendanceView

The whole window's content, back to front: the heading, the two tabs and
the active tab's body.
================
*/
export function attendanceView( input: AttendanceViewInput ): AttendanceOp[] {
	const ops: AttendanceOp[] = [];
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

	One skinned choice: a tab, Check Attendance, a day or Receive.
	================
	*/
	function button( id: string, label: string, rect: UiRect, selected: boolean, disabled = false, hidden = false ) {
		ops.push( {
			kind: "button",
			id,
			label,
			rect,
			texture: ROW_SKIN,
			color: disabled ? DIM : selected ? GOLD : WHITE,
			selected,
			disabled
		} );
		if ( !hidden ) text( label, rect, disabled ? DIM : selected ? GOLD : WHITE, 1, 1 );
	}

	if ( input.tile ) {
		ops.push( { kind: "tile", path: input.tile, rect: [ ox + 16, oy + 44, ATTENDANCE_WIDTH - 32, 56 ] } );
	}
	text( "Attendance Calendar", [ ox + 28, oy + 52, 300, 14 ], GOLD );
	if ( feed ) {
		text(
			attendanceMonthText( feed.monthKey ) + " - day " + feed.today + "/" + feed.daysInMonth,
			[ ox + 28, oy + 72, 300, 13 ],
			WHITE
		);
		text(
			"Day turns 04:00 UTC (" + attendanceCountdownText( feed, state.serverNowMs ) + ")",
			[ ox + ATTENDANCE_WIDTH - 328, oy + 72, 300, 13 ],
			DIM,
			2
		);
	}

	button( "attendance-tab:calendar", "Calendar", [ ox + 28, oy + 108, 96, 22 ], state.tab === "calendar" );
	button( "attendance-tab:rewards", "Rewards", [ ox + 128, oy + 108, 96, 22 ], state.tab === "rewards" );

	if ( !feed ) {
		text( "Waiting for the attendance calendar...", [ ox + 28, oy + 190, ATTENDANCE_WIDTH - 56, 14 ], WHITE, 1 );
		return ops;
	}

	if ( state.tab === "calendar" ) {
		const gridX = ox + 28 + Math.floor( (ATTENDANCE_WIDTH - 56 - GRID_COLUMNS * CELL_W) / 2 );
		const gridY = oy + 140;
		for ( let day = 1; day <= feed.daysInMonth; day++ ) {
			const col = (day - 1) % GRID_COLUMNS, row = Math.floor( (day - 1) / GRID_COLUMNS );
			const rect: UiRect = [ gridX + col * CELL_W, gridY + row * (CELL_H + 4), CELL_W - 4, CELL_H ];
			const checked = dayChecked( feed.checked, day );
			button( "attendance-day:" + day, String( day ), rect, state.selectedDay === day, false, true );
			if ( checked ) ops.push( { kind: "rect", rect, color: CHECKED_WASH } );
			if ( day === feed.today ) ops.push( { kind: "rect", rect, color: TODAY_WASH } );
			text( String( day ), [ rect[0] + 6, rect[1] + 4, 28, 13 ], day === feed.today ? GOLD : WHITE );
			if ( checked ) text( "Checked", [ rect[0], rect[1] + 24, rect[2], 13 ], WHITE, 1 );
			else if ( day === feed.daysInMonth ) text( "END", [ rect[0], rect[1] + 24, rect[2], 13 ], GOLD, 1 );
			if ( checked && day === feed.daysInMonth ) text( "END", [ rect[0] + 30, rect[1] + 4, 34, 13 ], GOLD, 2 );
		}
		const todayChecked = dayChecked( feed.checked, feed.today );
		if ( state.selectedDay >= 1 && state.selectedDay <= feed.daysInMonth ) {
			const selected = state.selectedDay;
			const status = dayChecked( feed.checked, selected ) ?
				"checked" :
				selected > feed.today ?
				"not yet" :
				"not checked";
			text( "Day " + selected + ": " + status, [ ox + 28, oy + ATTENDANCE_HEIGHT - 76, 300, 13 ], WHITE );
		}
		button(
			"attendance-check-in",
			"Check Attendance",
			[ ox + ATTENDANCE_WIDTH - 188, oy + ATTENDANCE_HEIGHT - 80, 160, 26 ],
			false,
			todayChecked
		);
		text(
			todayChecked ? "Today is checked." : "Today is not checked yet.",
			[ ox + 28, oy + ATTENDANCE_HEIGHT - 52, 300, 13 ],
			todayChecked ? DIM : GOLD
		);
	} else {
		const columns = [
			{
				id: "continuity" as const,
				title: "Continuity",
				tiers: feed.continuityTiers,
				claimed: feed.continuityClaimed
			},
			{
				id: "accumulation" as const,
				title: "Accumulation",
				tiers: feed.accumulationTiers,
				claimed: feed.accumulationClaimed
			}
		];
		columns.forEach( ( column, index ) => {
			const x = ox + 28 + index * 252;
			text( column.title, [ x, oy + 140, 236, 14 ], GOLD );
			column.tiers.forEach( ( tier, i ) => {
				const y = oy + 164 + i * 50;
				const received = tierClaimed( column.claimed, i );
				ops.push( { kind: "rect", rect: [ x, y, 236, 44 ], color: [ 0, 0, 0, .25 ] } );
				text( tier.days + " days", [ x + 8, y + 4, 90, 13 ], WHITE );
				text( tier.reward, [ x + 8, y + 24, 140, 13 ], DIM );
				button(
					"attendance-claim:" + column.id + ":" + i,
					received ? "Received" : "Receive",
					[ x + 150, y + 10, 78, 24 ],
					false,
					received || !tierEarned( feed, column.id, i )
				);
			} );
		} );
	}
	return ops;
}

/*
================
attendancePictures

Every picture the window may draw this frame, for the window's resource
admission: the plates and the buttons' skin.
================
*/
export function attendancePictures( ops: readonly AttendanceOp[] ): string[] {
	const out = new Set<string>();
	for ( const op of ops ) {
		if ( op.kind === "tile" ) out.add( op.path );
		else if ( op.kind === "button" && op.texture ) out.add( op.texture );
	}
	return [ ...out ];
}
