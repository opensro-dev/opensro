/*
===========================================================================

event-calendar-view.ts - the Event Calendar window as draw operations

Port-only, not native (#3532). Turns the calendar's feed state
(hud/event-calendar-hud.ts) and the server clock into a flat list of
draw operations; the renderer (ui.ts) paints them with its ordinary
primitives and owns nothing else of the window. The plates are the
native option window's tile (GDR_OPTION_BGTILE, a CIFNormalTile) under
seven day columns, today first and washed gold, with the selected
event's detail under the grid. All times are the feed's server time.

===========================================================================
*/

import type { UiRect } from "@/engine/contracts/ui";
import {
	clockText,
	dateText,
	dayHeading,
	eventStatusText,
	spanText,
	type CalendarDay,
	type CalendarEvent
} from "@/engine/foundation/ui/event-calendar";

type Color = readonly [number, number, number, number];

export const EVENT_CALENDAR_WIDTH = 780;
export const EVENT_CALENDAR_HEIGHT = 470;
// One day column, and the most event rows it lists before "+N more".
const COLUMN_WIDTH = 102;
const COLUMN_GAP = 6;
const MAX_LISTED = 6;
const EVENT_SKIN = "/assets/images/Media_extracted/interface/option/opt_key02.png";

const WHITE: Color = [ 1, 1, 1, 1 ];
const GOLD: Color = [ 1, .85, .5, 1 ];
const DIM: Color = [ .72, .72, .72, 1 ];
const GREEN: Color = [ .55, 1, .55, 1 ];
const TODAY_TINT: Color = [ 1, .85, .5, .08 ];

/*
================
EventCalendarOp

One draw operation, in screen pixels. "tile" repeats its picture over
rect; "rect" is a flat wash; "button" is a hit area whose pixels are
its own skinned rect (ui.ts admits the texture and draws it while the
resource lasts).
================
*/
export type EventCalendarOp =
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
EventCalendarViewState

The parts of the feed owner's state() this window draws. Declared
here, not imported from the hud: the two are siblings under ui.ts,
and the renderer hands the state across.
================
*/
export interface EventCalendarViewState {
	readonly status: "idle" | "loading" | "ready" | "failed";
	readonly week: readonly CalendarDay[];
	readonly selected: CalendarEvent | null;
}

/*
================
EventCalendarViewInput

Everything the window reads, in one struct: the screen origin, the
option tile's texture ("" draws no plates), the feed state and the
server clock at this frame.
================
*/
export interface EventCalendarViewInput {
	readonly origin: readonly [number, number];
	readonly tile: string;
	readonly state: EventCalendarViewState;
	readonly serverNowMs: number;
}

/*
================
eventCalendarView

The whole window's content, back to front: the grid plate, the seven
columns (or the loading/failed line), the detail plate and its texts,
then the footer.
================
*/
export function eventCalendarView( input: EventCalendarViewInput ): EventCalendarOp[] {
	const ops: EventCalendarOp[] = [];
	const [ox, oy] = input.origin;
	const state = input.state;

	/*
	================
	text

	One line of the window's text, aligned as asked.
	================
	*/
	function text( value: string, rect: UiRect, color: Color, hAlign = 0, vAlign = 0 ) {
		ops.push( { kind: "text", value, rect, color, hAlign, vAlign } );
	}

	if ( input.tile ) {
		ops.push( { kind: "tile", path: input.tile, rect: [ ox + 16, oy + 44, EVENT_CALENDAR_WIDTH - 32, 268 ] } );
	}
	if ( state.status === "ready" ) {
		const left = ox + 22, top = oy + 52;
		state.week.forEach( ( day, dayIndex ) => {
			const x = left + dayIndex * (COLUMN_WIDTH + COLUMN_GAP),
				heading = dayHeading( day.startMs, dayIndex === 0 );
			if ( dayIndex === 0 ) {
				ops.push( { kind: "rect", rect: [ x - 3, top - 4, COLUMN_WIDTH + 6, 252 ], color: TODAY_TINT } );
			}
			text( heading.title, [ x, top, COLUMN_WIDTH, 14 ], dayIndex === 0 ? GOLD : WHITE, 1 );
			text( heading.date, [ x, top + 14, COLUMN_WIDTH, 14 ], DIM, 1 );
			day.events.slice( 0, MAX_LISTED ).forEach( ( event, index ) => {
				const r: UiRect = [ x, top + 36 + index * 36, COLUMN_WIDTH, 32 ],
					chosen = state.selected === event;
				ops.push( {
					kind: "button",
					id: "event-calendar-event:" + dayIndex + ":" + index,
					label: event.title,
					rect: r,
					texture: EVENT_SKIN,
					color: chosen ? GOLD : WHITE,
					selected: chosen
				} );
				text(
					clockText( event.startsAtMs ),
					[ r[0] + 5, r[1] + 2, COLUMN_WIDTH - 10, 12 ],
					event.running ? GREEN : WHITE
				);
				text( event.title, [ r[0] + 5, r[1] + 15, COLUMN_WIDTH - 10, 12 ], WHITE );
			} );
			if ( day.events.length > MAX_LISTED ) {
				text(
					"+" + (day.events.length - MAX_LISTED) + " more",
					[ x, top + 36 + MAX_LISTED * 36, COLUMN_WIDTH, 12 ],
					DIM,
					1
				);
			}
			if ( !day.events.length ) {
				text( "No events", [ x, top + 36, COLUMN_WIDTH, 12 ], DIM, 1 );
			}
		} );
	} else {
		text(
			state.status === "failed" ? "The schedule could not be loaded." : "Loading the schedule...",
			[ ox + 22, oy + 60, EVENT_CALENDAR_WIDTH - 44, 16 ],
			WHITE,
			1
		);
	}
	if ( input.tile ) {
		ops.push( { kind: "tile", path: input.tile, rect: [ ox + 16, oy + 322, EVENT_CALENDAR_WIDTH - 32, 96 ] } );
	}
	const chosen = state.selected;
	if ( chosen ) {
		text( chosen.title, [ ox + 28, oy + 332, EVENT_CALENDAR_WIDTH - 56, 16 ], GOLD );
		text( dateText( chosen.startsAtMs ) + "  " + spanText( chosen ), [
			ox + 28,
			oy + 354,
			EVENT_CALENDAR_WIDTH - 56,
			14
		], WHITE );
		text(
			eventStatusText( chosen, input.serverNowMs ),
			[ ox + 28, oy + 374, EVENT_CALENDAR_WIDTH - 56, 14 ],
			chosen.running ? GREEN : WHITE
		);
	} else if ( state.status === "ready" ) {
		text( "No events this week.", [ ox + 28, oy + 332, EVENT_CALENDAR_WIDTH - 56, 16 ], DIM );
	}
	if ( state.week.length ) {
		text(
			dateText( state.week[0]!.startMs ) + " - " + dateText( state.week[6]!.startMs ),
			[ ox + 28, oy + EVENT_CALENDAR_HEIGHT - 40, 300, 14 ],
			DIM
		);
	}
	text(
		"All times are server time.",
		[ ox + EVENT_CALENDAR_WIDTH - 328, oy + EVENT_CALENDAR_HEIGHT - 40, 300, 14 ],
		DIM,
		2
	);
	return ops;
}

/*
================
eventCalendarPictures

Every picture the window may draw this frame, for the window's
resource admission: the plates and the event rows' skin.
================
*/
export function eventCalendarPictures( ops: readonly EventCalendarOp[] ): string[] {
	const out = new Set<string>();
	for ( const op of ops ) {
		if ( op.kind === "tile" ) out.add( op.path );
		else if ( op.kind === "button" && op.texture ) out.add( op.texture );
	}
	return [ ...out ];
}
