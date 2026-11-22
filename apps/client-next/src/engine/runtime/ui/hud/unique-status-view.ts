/*
===========================================================================

unique-status-view.ts - the Unique Status window as draw operations

Port-only, not native (#3531). This module turns the window's state -
the worker's feed (contracts/unique-status.ts), the open tab, and the
notify draft - into a flat list of draw operations; the renderer
(ui.ts) paints them with its ordinary primitives between beginWindow
and endWindow and owns nothing else of the window. The window is the
"Unique Status" panel; every other action id is one ui.ts already
dispatches (unique-status-tab:/-mark:/-deaths/-browser/-save/-scroll).

Row text is shaped here from the server's clock, run forward by the
caller: the Died column reads "HH:MM (x min ago)" beside the killer,
and the footer reminds the player the times are server time. Nothing
here knows a respawn time, and none is drawn.

===========================================================================
*/
import type { UiRect } from "@/engine/contracts/ui";
import type { UniqueStatusFeed } from "@/engine/contracts/unique-status";
import {
	uniqueStatusAgo,
	uniqueStatusClock,
	uniqueStatusRows,
	type UniqueNotifySettings,
	type UniqueStatusTab
} from "@/engine/foundation/gameplay/unique-status";

type Color = readonly [number, number, number, number];

const WHITE: Color = [ 1, 1, 1, 1 ];
const HEADER: Color = [ 180 / 255, 180 / 255, 180 / 255, 1 ];
const ALIVE: Color = [ .62, 1, .62, 1 ];
const CHECK_ON = "/assets/images/Media_extracted/interface/ifcommon/com_checkbutton_on.png";
const CHECK_OFF = "/assets/images/Media_extracted/interface/ifcommon/com_checkbutton_off.png";

export const UNIQUE_STATUS_WIDTH = 560;
export const UNIQUE_STATUS_PAGE_ROWS = 12;

const ROW_PITCH = 20;
const TABS_TOP = 40;
const HEAD_TOP = 78;
const LIST_TOP = 98;
const LIST_HEIGHT = UNIQUE_STATUS_PAGE_ROWS * ROW_PITCH;
const TOGGLES_TOP = LIST_TOP + LIST_HEIGHT + 12;

export const UNIQUE_STATUS_HEIGHT = TOGGLES_TOP + 56;

// [left, width, hAlign] of the name, level, status, died and killer columns.
const COLUMNS = [
	[ 42, 176, 0 ],
	[ 222, 44, 2 ],
	[ 272, 62, 0 ],
	[ 340, 112, 0 ],
	[ 458, 92, 0 ]
] as const;

const TABS = [
	[ "all", "All" ],
	[ "alive", "Alive" ],
	[ "dead", "Dead" ],
	[ "notify", "Notify list" ]
] as const;

/*
================
UniqueStatusOp

One draw operation, in screen pixels. "box" is the window frame with
its title and close button; "button", "hit" and "scrollbar" are hit
areas as well as pixels (a "hit" paints nothing itself: its checkbox
or label is a separate op).
================
*/
export type UniqueStatusOp =
	| { readonly kind: "box"; readonly title: string; readonly rect: UiRect; }
	| {
		readonly kind: "button";
		readonly id: string;
		readonly label: string;
		readonly rect: UiRect;
		readonly disabled: boolean;
		readonly selected: boolean;
	}
	| { readonly kind: "image"; readonly path: string; readonly rect: UiRect; }
	| {
		readonly kind: "text";
		readonly value: string;
		readonly rect: UiRect;
		readonly color: Color;
		readonly hAlign: number;
		readonly vAlign: number;
	}
	| { readonly kind: "rect"; readonly rect: UiRect; readonly color: Color; }
	| {
		readonly kind: "hit";
		readonly id: string;
		readonly label: string;
		readonly rect: UiRect;
		readonly selected: boolean;
	}
	| {
		readonly kind: "scrollbar";
		readonly id: string;
		readonly rect: UiRect;
		readonly total: number;
		readonly page: number;
		readonly value: number;
	};

/*
================
UniqueStatusViewInput

What the window draws this frame: where it sits, the feed (null while
the shard has sent none), the tab and first row, the notify draft, and
the server's clock run forward to now.
================
*/
export interface UniqueStatusViewInput {
	readonly origin: UiRect;
	readonly feed: UniqueStatusFeed | null;
	readonly tab: UniqueStatusTab;
	readonly firstRow: number;
	// The notify draft being edited (the saved list's shape).
	readonly draft: UniqueNotifySettings;
	readonly dirty: boolean;
	readonly hover: string | null;
	readonly serverNowMs: number;
}

/*
================
UniqueStatusView

The operations, and the list's geometry the renderer's wheel and
thumb routing scrolls by.
================
*/
export interface UniqueStatusView {
	readonly ops: readonly UniqueStatusOp[];
	readonly list: { readonly rect: UiRect; readonly total: number; readonly page: number; };
}

/*
================
uniqueStatusView

The whole window as operations, tabs first and the Save last. The
first row is clamped here so the list never scrolls past its end.
================
*/
export function uniqueStatusView( input: UniqueStatusViewInput ): UniqueStatusView {
	const px = input.origin[0], py = input.origin[1];
	const rows = input.feed ? uniqueStatusRows( input.feed.rows, input.tab, input.draft.refs ) : [];
	const firstRow = Math.min( input.firstRow, Math.max( 0, rows.length - UNIQUE_STATUS_PAGE_ROWS ) );
	const ops: UniqueStatusOp[] = [];
	const cells = ( values: readonly string[], top: number, color: Color ) => {
		values.forEach( ( value, i ) => {
			const [left, width, hAlign] = COLUMNS[i]!;
			ops.push( {
				kind: "text",
				value,
				rect: [ px + left, top, width, ROW_PITCH ],
				color,
				hAlign,
				vAlign: 1
			} );
		} );
	};

	ops.push( {
		kind: "box",
		title: "Unique Status",
		rect: [ px, py, UNIQUE_STATUS_WIDTH, UNIQUE_STATUS_HEIGHT ]
	} );
	TABS.forEach( ( [tab, label], i ) => {
		ops.push( {
			kind: "button",
			id: "unique-status-tab:" + tab,
			label,
			rect: [ px + 14 + i * 108, py + TABS_TOP, 100, 24 ],
			disabled: false,
			selected: input.tab === tab
		} );
	} );
	cells( [ "Unique", "Lv", "Status", "Died", "Killed by" ], py + HEAD_TOP, HEADER );
	ops.push( {
		kind: "rect",
		rect: [ px + 10, py + LIST_TOP - 3, UNIQUE_STATUS_WIDTH - 20, 1 ],
		color: HEADER
	} );

	rows.slice( firstRow, firstRow + UNIQUE_STATUS_PAGE_ROWS ).forEach( ( row, i ) => {
		const top = py + LIST_TOP + i * ROW_PITCH,
			markId = "unique-status-mark:" + row.refObjId,
			marked = input.draft.refs.includes( row.refObjId );
		if ( input.hover === markId ) {
			ops.push( {
				kind: "rect",
				rect: [ px + 10, top, UNIQUE_STATUS_WIDTH - 20, ROW_PITCH ],
				color: [ 1, 1, 1, .07 ]
			} );
		}
		ops.push( {
			kind: "image",
			path: marked ? CHECK_ON : CHECK_OFF,
			rect: [ px + 16, top + 2, 16, 16 ]
		} );
		ops.push( {
			kind: "hit",
			id: markId,
			label: row.name,
			rect: [ px + 10, top, 218, ROW_PITCH ],
			selected: marked
		} );
		cells(
			[
				row.name,
				String( row.level ),
				row.alive ? "Alive" : "Dead",
				!row.alive && row.diedAtMs > 0 ?
					`${uniqueStatusClock( row.diedAtMs )} (${uniqueStatusAgo( row.diedAtMs, input.serverNowMs )})` :
					"-",
				row.killer || "-"
			],
			top,
			row.alive ? ALIVE : WHITE
		);
	} );
	if ( !input.feed ) {
		ops.push( {
			kind: "text",
			value: "No unique states yet - the shard's feed may be off.",
			rect: [ px + 14, py + LIST_TOP + 40, UNIQUE_STATUS_WIDTH - 28, 20 ],
			color: HEADER,
			hAlign: 1,
			vAlign: 1
		} );
	} else if ( rows.length === 0 ) {
		ops.push( {
			kind: "text",
			value: "Nothing on this tab.",
			rect: [ px + 14, py + LIST_TOP + 40, UNIQUE_STATUS_WIDTH - 28, 20 ],
			color: HEADER,
			hAlign: 1,
			vAlign: 1
		} );
	}

	if ( rows.length > UNIQUE_STATUS_PAGE_ROWS ) {
		ops.push( {
			kind: "scrollbar",
			id: "unique-status-scroll",
			rect: [ px + UNIQUE_STATUS_WIDTH - 30, py + LIST_TOP + 16, 16, LIST_HEIGHT - 32 ],
			total: rows.length,
			page: UNIQUE_STATUS_PAGE_ROWS,
			// The bar counts down from the top row the list shows last:
			// chatScrollbar's value names rows below the thumb.
			value: rows.length - UNIQUE_STATUS_PAGE_ROWS - firstRow
		} );
	}

	const toggles = [
		[ "unique-status-deaths", "Also when it dies", input.draft.deathsToo ],
		[ "unique-status-browser", "Browser notifications", input.draft.browser ]
	] as const;
	toggles.forEach( ( [id, label, on], i ) => {
		const top = py + TOGGLES_TOP + i * 22;
		ops.push( { kind: "image", path: on ? CHECK_ON : CHECK_OFF, rect: [ px + 16, top + 2, 16, 16 ] } );
		ops.push( {
			kind: "text",
			value: label,
			rect: [ px + 40, top, 240, ROW_PITCH ],
			color: WHITE,
			hAlign: 0,
			vAlign: 1
		} );
		ops.push( { kind: "hit", id, label, rect: [ px + 10, top, 280, 22 ], selected: on } );
	} );
	ops.push( {
		kind: "text",
		value: "Times are server time.",
		rect: [ px + 14, py + UNIQUE_STATUS_HEIGHT - 36, 240, 16 ],
		color: HEADER,
		hAlign: 0,
		vAlign: 1
	} );
	ops.push( {
		kind: "button",
		id: "unique-status-save",
		label: "Save",
		rect: [ px + UNIQUE_STATUS_WIDTH - 134, py + UNIQUE_STATUS_HEIGHT - 42, 120, 24 ],
		disabled: !input.dirty,
		selected: false
	} );

	return {
		ops,
		list: {
			rect: [ px + 10, py + LIST_TOP, UNIQUE_STATUS_WIDTH - 20, LIST_HEIGHT ],
			total: rows.length,
			page: UNIQUE_STATUS_PAGE_ROWS
		}
	};
}
