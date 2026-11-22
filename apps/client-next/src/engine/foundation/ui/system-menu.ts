/*
===========================================================================

system-menu.ts - Escape menu geometry and its experimental entry

Native 5D1920 removes customer support and compacts four controls. The
approved Experimental entry adds one row below Options using the same art.
Port-only entries (the Event Calendar's, #3532; the Unique Status
window's, #3531) add their rows below that while their Experimental rows
are on, in the same art; with none on, the menu is exactly the
native-plus-Experimental one.

===========================================================================
*/
import type { AuthoredLayout } from "./authored-layout";
import type { UiRect } from "@/engine/contracts/ui";

export const SYSTEM_MENU_HEIGHT = 246;
export const EXPERIMENTAL_MENU_ID = 15;
export const EVENT_CALENDAR_MENU_ID = 16;
export const UNIQUE_STATUS_MENU_ID = 17;
const ROW_HEIGHT = 34;

/*
================
systemMenuHeight

The frame's height for the rows shown: SYSTEM_MENU_HEIGHT carries the
Experimental row; each extra row adds one more.
================
*/
export function systemMenuHeight( extras: number ): number {
	return SYSTEM_MENU_HEIGHT + extras * ROW_HEIGHT;
}

/*
================
systemMenu

extras lists the menu ids of the extra rows, inserted after the
Experimental row in list order; each borrows the Option row's art, as
the Experimental row does. The native row ids are 10, 11, 13 and 14;
anything else in the list is an extra.
================
*/
export function systemMenu( layout: AuthoredLayout, x: number, y: number, extras: readonly number[] ) {
	const at = ( a: number, b: number, w: number, h: number ): UiRect => [ x + a, y + b, w, h ];
	const frame = layout.GDR_SYSTEM_FRAME!, tile = layout.GDR_SYSTEM_BGTILE!;
	const added = (1 + extras.length) * ROW_HEIGHT;
	const ids = [ 10, EXPERIMENTAL_MENU_ID, ...extras, 11, 13, 14 ];
	return {
		frame: at( 0, 0, 214, systemMenuHeight( extras.length ) ),
		inner: at( frame.rect[0], frame.rect[1], frame.rect[2], 151 + added ),
		tile: at( tile.rect[0], tile.rect[1], tile.rect[2], 112 + added ),
		buttons: ids.map( ( id, index ) => {
			const native = id === 10 || id === 11 || id === 13 || id === 14;
			const node = Object.values( layout ).find( n => n.id === (native ? id : 10) );
			if ( !node ) throw Error( "Missing System menu control" );
			return { ...node, id, rect: [ 31, 58 + ROW_HEIGHT * index, ...node.size ] as UiRect };
		} )
	};
}
