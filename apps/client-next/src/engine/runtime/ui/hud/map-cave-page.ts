/*
===========================================================================

map-cave-page.ts - the M window's dungeon page: its painted text and signs

caveMapView (foundation/ui/cave-map.ts) projects a dungeon floor: the page
quads, the floor labels and the gate signs. This module turns one frame of
that projection into the window's images and click hits, and owns the
name label a hovered map sign wears, which the world page's roster signs
share. The map HUD (world-map.ts) owns the window and decides which page
shows.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import type { caveMapView } from "@/engine/foundation/ui/cave-map";

// A dungeon page's gate signs, hovered by index.
export const CAVE_SIGN_PREFIX = "map-cave-sign:";
// A hovered sign's name (57CE80's roster pass): 829F30's text in white
// font 0, its foot 8.0 (BCDC80) above the sign's centre, so it sits on
// the sign's top edge. The gate signs wear the same label above their top.
const SIGN_NAME_FONT = 0;
const SIGN_NAME_RISE = 8;
const WHITE = [ 1, 1, 1, 1 ] as const;

/*
================
CavePageText

The font operations the page paints with; the UI owns the fonts.
================
*/
export interface CavePageText {
	run( value: string, style: number, font: number ): { width: number; };
	extentHeight( font: number ): number;
	quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style?: GlyphStyle ): UiQuad[];
}

export type CaveMapFrame = NonNullable<ReturnType<typeof caveMapView>>;

/*
================
mapSignName

A map sign's name, centred on x with its foot SIGN_NAME_RISE above y,
clipped to the map area like the sign it belongs to.
================
*/
export function mapSignName( text: CavePageText, name: string, x: number, y: number, clip: UiRect ): UiQuad[] {
	const width = text.run( name, 0, SIGN_NAME_FONT ).width,
		height = text.extentHeight( SIGN_NAME_FONT );
	const box: UiRect = [
		Math.trunc( x - Math.trunc( width / 2 ) ),
		Math.trunc( y - SIGN_NAME_RISE - height ),
		width,
		height
	];
	return text.quads( name, box, clip, WHITE, { fontIndex: SIGN_NAME_FONT, hAlign: 0, vAlign: 0 } );
}

/*
================
cavePageImages

The page quads, then its floor labels, then the hovered gate sign's name.
================
*/
export function cavePageImages(
	page: CaveMapFrame,
	text: CavePageText,
	clip: UiRect,
	hover: string | null | undefined
): UiQuad[] {
	return [
		...page.quads,
		...page.labels.flatMap( label => {
			const width = text.run( label.text, 0, label.font ).width,
				height = text.extentHeight( label.font ),
				box: UiRect = [ label.x - Math.floor( width / 2 ), label.y, width, height ];
			return text.quads( label.text, box, clip, label.color, { fontIndex: label.font, hAlign: 0, vAlign: 0 } );
		} ),
		...page.signs.flatMap( ( sign, index ) =>
			hover === CAVE_SIGN_PREFIX + index ?
				mapSignName( text, sign.name, sign.rect[0] + sign.rect[2] / 2, sign.rect[1], clip ) :
				[]
		)
	];
}

/*
================
cavePageHits

Each gate sign is a hit: hovering names it, dragging pans the page.
================
*/
export function cavePageHits( page: CaveMapFrame ): UiControl[] {
	return page.signs.map( ( sign, index ) => ({
		id: CAVE_SIGN_PREFIX + index,
		label: sign.name,
		rect: sign.rect,
		kind: "region",
		draggable: true
	}) );
}
