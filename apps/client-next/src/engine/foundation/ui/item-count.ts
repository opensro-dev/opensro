/*
===========================================================================

item-count.ts - the stack count drawn on an item slot

v1.150 CIFSlotWithHelp::OnCreate 5548C0 and render 568420 -> 564470 draw
counts as 8x8 digit sprites, stepped backwards by five pixels, at the top
left. An item the server never spends (the beta starter kit) shows an
infinity sign instead, drawn in the same pixel style as the digits.

===========================================================================
*/
import type { InventoryItem } from "@/engine/contracts/gameplay";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";

const ROOT = "/assets/images/Media_extracted/interface/item_number/item_number_";
// Each digit sprite is a 4x7 white glyph on a 5x8 black backing.
const DIGIT_STEP = 5;
const DIGIT_SIZE = 8;
const DIGIT_TOP = 2;
const INFINITY_WIDTH = 8;
const INFINITY_HEIGHT = 5;
// Left edge where a two-digit count would start.
const INFINITY_LEFT = 1;

/*
================
countable

4FB260: only expendable, stackable type 3/3 items show a count, and a single
remaining unit is drawn too. A timed COS extension (param bit 2) does not.
================
*/
function countable( item: Partial<Pick<InventoryItem, "quantity" | "typeFlags" | "tooltip">> ): boolean {
	if ( !Number.isInteger( item.quantity ) || item.quantity! < 1 || item.quantity! > 65535 ) return false;
	const type = item.typeFlags;
	if ( type !== undefined && ((type & 2) !== 0 || (type & 0x1c) !== 0xc || (type & 0x60) !== 0x60) ) return false;
	if ( type === undefined && item.quantity === 1 ) return false;
	if ( type !== undefined && (type & 0xff80) === 0x7e80 && ((item.tooltip?.fields.itemParam6_2b0 ?? 0) & 2) ) {
		return false;
	}
	return true;
}

/*
================
infinityQuads

The infinity sign as solid quads: one black backing, then white runs.
================
*/
function infinityQuads( r: UiRect, clip: UiRect ): UiQuad[] {
	// The native "8" glyph turned on its side: 7x4 white pixels on an 8x5
	// backing. Each row lists its white [start, length] runs.
	const rows: readonly (readonly (readonly [number, number])[])[] = [
		[ [ 1, 2 ], [ 4, 2 ] ],
		[ [ 0, 1 ], [ 3, 1 ], [ 6, 1 ] ],
		[ [ 0, 1 ], [ 3, 1 ], [ 6, 1 ] ],
		[ [ 1, 2 ], [ 4, 2 ] ]
	];
	const left = r[0] + INFINITY_LEFT, top = r[1] + DIGIT_TOP;
	const quads: UiQuad[] = [ {
		rect: [ left, top, INFINITY_WIDTH, INFINITY_HEIGHT ],
		clip,
		color: [ 0, 0, 0, 1 ],
		texture: "",
		uv: [ 0, 0, 1, 1 ]
	} ];
	for ( const [row, runs] of rows.entries() ) {
		for ( const [start, length] of runs ) {
			quads.push( {
				rect: [ left + start, top + row, length, 1 ],
				clip,
				color: [ 1, 1, 1, 1 ],
				texture: "",
				uv: [ 0, 0, 1, 1 ]
			} );
		}
	}
	return quads;
}

/*
================
itemCountQuads

The count sprites for a slot, or the infinity sign for an unlimited item.
D3D9's -0.5 vertex correction is implicit in WebGPU pixel coordinates.
================
*/
export function itemCountQuads(
	item: Partial<Pick<InventoryItem, "quantity" | "typeFlags" | "tooltip">> | undefined,
	r: UiRect,
	clip: UiRect,
	unlimited = false
): UiQuad[] {
	if ( !item || !countable( item ) ) return [];
	if ( unlimited ) return infinityQuads( r, clip );
	const digits = String( item.quantity ), quads: UiQuad[] = [];
	for ( let i = digits.length - 1, x = r[0] + digits.length * 4 - 2; i >= 0; i--, x -= DIGIT_STEP ) {
		quads.push( {
			rect: [ x, r[1] + DIGIT_TOP, DIGIT_SIZE, DIGIT_SIZE ],
			clip,
			color: [ 1, 1, 1, 1 ],
			texture: ROOT + digits[i] + ".png",
			uv: [ 0, 0, 1, 1 ]
		} );
	}
	return quads;
}
