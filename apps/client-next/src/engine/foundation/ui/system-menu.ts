/*
===========================================================================

system-menu.ts - Escape menu geometry and its experimental entry

Native 5D1920 removes customer support and compacts four controls. The
approved Experimental entry adds one row below Options using the same art.

===========================================================================
*/
import type { AuthoredLayout } from "./authored-layout";
import type { UiRect } from "@/engine/contracts/ui";

export const SYSTEM_MENU_HEIGHT = 246;
export const EXPERIMENTAL_MENU_ID = 15;
const ROW_HEIGHT = 34;

/*
================
systemMenu
================
*/
export function systemMenu( layout: AuthoredLayout, x: number, y: number ) {
	const at = ( a: number, b: number, w: number, h: number ): UiRect => [ x + a, y + b, w, h ];
	const frame = layout.GDR_SYSTEM_FRAME!, tile = layout.GDR_SYSTEM_BGTILE!;
	return {
		frame: at( 0, 0, 214, SYSTEM_MENU_HEIGHT ),
		inner: at( frame.rect[0], frame.rect[1], frame.rect[2], 151 + ROW_HEIGHT ),
		tile: at( tile.rect[0], tile.rect[1], tile.rect[2], 112 + ROW_HEIGHT ),
		buttons: [ 10, EXPERIMENTAL_MENU_ID, 11, 13, 14 ].map( ( id, index ) => {
			const node = Object.values( layout ).find( n => n.id === (id === EXPERIMENTAL_MENU_ID ? 10 : id) );
			if ( !node ) throw Error( "Missing System menu control" );
			return { ...node, id, rect: [ 31, 58 + ROW_HEIGHT * index, ...node.size ] as UiRect };
		} )
	};
}
