/*
===========================================================================

system-menu.ts - Escape menu geometry and its experimental entry

Native 5D1920 removes customer support and compacts four controls. The
approved Experimental entry adds one row below Options using the same art.
Port-only, not native (#3532): while the Event Calendar's Experimental row
is on, its entry adds one more row below that, in the same art; off, the
menu is exactly the native-plus-Experimental one.

===========================================================================
*/
import type { AuthoredLayout } from "./authored-layout";
import type { UiRect } from "@/engine/contracts/ui";

export const SYSTEM_MENU_HEIGHT = 246;
export const EXPERIMENTAL_MENU_ID = 15;
export const EVENT_CALENDAR_MENU_ID = 16;
const ROW_HEIGHT = 34;

/*
================
systemMenuHeight

The frame's height for the rows shown: SYSTEM_MENU_HEIGHT carries the
Experimental row; the calendar row adds one more.
================
*/
export function systemMenuHeight( eventCalendar: boolean ): number {
	return SYSTEM_MENU_HEIGHT + (eventCalendar ? ROW_HEIGHT : 0);
}

/*
================
systemMenu
================
*/
export function systemMenu( layout: AuthoredLayout, x: number, y: number, eventCalendar: boolean ) {
	const at = ( a: number, b: number, w: number, h: number ): UiRect => [ x + a, y + b, w, h ];
	const frame = layout.GDR_SYSTEM_FRAME!, tile = layout.GDR_SYSTEM_BGTILE!;
	const added = (1 + (eventCalendar ? 1 : 0)) * ROW_HEIGHT;
	const ids = eventCalendar ?
		[ 10, EXPERIMENTAL_MENU_ID, EVENT_CALENDAR_MENU_ID, 11, 13, 14 ] :
		[ 10, EXPERIMENTAL_MENU_ID, 11, 13, 14 ];
	return {
		frame: at( 0, 0, 214, systemMenuHeight( eventCalendar ) ),
		inner: at( frame.rect[0], frame.rect[1], frame.rect[2], 151 + added ),
		tile: at( tile.rect[0], tile.rect[1], tile.rect[2], 112 + added ),
		buttons: ids.map( ( id, index ) => {
			const node = Object.values( layout ).find( n =>
				n.id === (id === EXPERIMENTAL_MENU_ID || id === EVENT_CALENDAR_MENU_ID ? 10 : id)
			);
			if ( !node ) throw Error( "Missing System menu control" );
			return { ...node, id, rect: [ 31, 58 + ROW_HEIGHT * index, ...node.size ] as UiRect };
		} )
	};
}
