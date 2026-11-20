/*
===========================================================================

combo-box.ts - the CIFComboBox box and its open list

The closed box (CIFComboBox_OnCreate 51DC80: black field, com_blacksquare_
ring, the down arrow, an inset static) and the list it opens
(CIFComboBox_OpenList 51EA20). Callers draw the quads and own the state:
which combo is open and its scroll position.

===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { stretchRing } from "./stretch-ring";

/*
================
comboBoxChrome

CIFComboBox_OnCreate 51DC80: the black field, its ring, the right arrow
and the inset static. The text static is a new child, so the parent's
ClientRect does not carry into it.
================
*/
export function comboBoxChrome(
	r: UiRect,
	size: ( path: string ) => readonly [number, number] | undefined,
	clip: UiRect,
	state: 0 | 1 | 2 = 0
) {
	const root = "/assets/images/Media_extracted/interface/ifcommon/",
		list: UiRect = [ r[0], r[1], r[2] - 20, r[3] ],
		ring = stretchRing( list, root + "com_blacksquare_", size, clip );
	const arrows = [ "", "_focus", "_press" ].map( s => root + "com_qst_downarrow_button" + s + ".png" ),
		quads: UiQuad[] = [
			{ rect: list, clip, texture: "", uv: [ 0, 0, 1, 1 ], color: [ 0, 0, 0, 1 ] },
			...ring.quads
		];
	const arrowSize = size( arrows[state]! );
	if ( arrowSize ) {
		quads.push( {
			rect: [
				r[0] + r[2] - 20 + Math.floor( (20 - arrowSize[0]) / 2 ),
				r[1] + Math.floor( (r[3] - arrowSize[1]) / 2 ),
				arrowSize[0],
				arrowSize[1]
			],
			clip,
			texture: arrows[state]!,
			uv: [ 0, 0, 1, 1 ],
			color: [ 1, 1, 1, 1 ]
		} );
	}
	return { quads, paths: [ ...ring.paths, ...arrows ], textRect: [ r[0] + 5, r[1], r[2] - 30, r[3] ] as UiRect };
}

/*
================
comboListLayout

The open list of a CIFComboBox (CIFComboBox_OpenList 51EA20): a popup
under the box (its top + 20), as wide as the box and as tall as the
visible row count times the list's row pitch plus 8, whatever the item
count. The list inside (536E20) starts at (4, 4) with a pitch of the
font height + 4; when the items outnumber the visible rows (536FB0) a
16 pixel scroll bar shows on the right and the list narrows by 20
(537090). first is the top item shown, clamped to the scroll range.
================
*/
export function comboListLayout( r: UiRect, count: number, visibleRows: number, fontHeight: number, first: number ) {
	const LIST_INSET = 4, PITCH_EXTRA = 4, COMBO_HEIGHT = 20, SCROLL_WIDTH = 16, SCROLL_RESERVE = 20, ARROW = 16;
	const pitch = fontHeight + PITCH_EXTRA,
		popup: UiRect = [ r[0], r[1] + COMBO_HEIGHT, r[2], pitch * visibleRows + 2 * LIST_INSET ],
		scrolls = count > visibleRows,
		range = Math.max( 0, count - visibleRows ),
		top = Math.max( 0, Math.min( range, Math.trunc( first ) ) ),
		width = popup[2] - 2 * LIST_INSET - (scrolls ? SCROLL_RESERVE : 0),
		rows: { index: number; rect: UiRect; }[] = [];
	for ( let index = top; index < Math.min( count, top + visibleRows ); index++ ) {
		rows.push( {
			index,
			rect: [ popup[0] + LIST_INSET, popup[1] + LIST_INSET + (index - top) * pitch, width, pitch ]
		} );
	}
	const track: UiRect | undefined = scrolls ?
		[
			popup[0] + popup[2] - LIST_INSET - SCROLL_WIDTH,
			popup[1] + LIST_INSET + ARROW,
			SCROLL_WIDTH,
			popup[3] - 2 * LIST_INSET - 3 * ARROW
		] :
		undefined;
	return { popup, rows, track, first: top, range };
}
