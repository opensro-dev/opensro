/*
===========================================================================

reverse-map-buttons.ts - the reverse return points' buttons on the M map

While the map holds a reverse return scroll (reverse-return-hud.ts), each
published point is a button. The later client draws them with its own
art (sub_e57d50, issue #3391): interface/worldmap/map/xy_returnicon with
_focus and _press faces. This module paints one frame of them from the
points the world map already projected; the map HUD owns the window.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import { REVERSE_MAP_MARKER } from "@/engine/foundation/ui/world-map";

const RETURN_ICON = "/assets/images/Media_extracted/interface/worldmap/map/xy_returnicon";
const RETURN_ICON_SIZE = 32;
// The normal face is a 64 x 32 strip whose left half the button draws
// (its quad's u runs 0 to 0.5, sub_e57d50); hover and press are 32 x 32.
const RETURN_ICON_STRIP: UiRect = [ 0, 0, .5, 1 ];
const FULL_UV: UiRect = [ 0, 0, 1, 1 ];
const WHITE = [ 1, 1, 1, 1 ] as const;
export const REVERSE_MAP_POINT = "reverse-map-point:";

/*
================
ReverseMapButtons

One frame's input: the projected points, the map clip, the hovered id
and whether an image is loaded.
================
*/
export interface ReverseMapButtons {
	points: readonly { point: { id: number; name: string; }; rect: UiRect; }[];
	clip: UiRect;
	hover: string | null;
	has( path: string ): boolean;
}

/*
================
reverseMapButtons

The buttons, centred on their points (inference, as every map sign
here: the placement code was not read). A build without the later
client's art keeps the port's old sign at the point's own rect.
================
*/
export function reverseMapButtons( input: ReverseMapButtons ): {
	images: UiQuad[];
	hits: UiControl[];
	paths: string[];
} {
	const images: UiQuad[] = [], hits: UiControl[] = [];
	const paths = [ RETURN_ICON + ".png", RETURN_ICON + "_focus.png" ];
	const art = input.has( RETURN_ICON + ".png" );
	for ( const { point, rect: at } of input.points ) {
		const id = REVERSE_MAP_POINT + point.id;
		if ( !art ) {
			images.push( { rect: at, texture: REVERSE_MAP_MARKER, uv: FULL_UV, color: WHITE, clip: input.clip } );
			hits.push( { id, label: point.name, rect: at, kind: "button" } );
			continue;
		}
		const rect: UiRect = [
			Math.round( at[0] + at[2] / 2 - RETURN_ICON_SIZE / 2 ),
			Math.round( at[1] + at[3] / 2 - RETURN_ICON_SIZE / 2 ),
			RETURN_ICON_SIZE,
			RETURN_ICON_SIZE
		];
		const focus = input.hover === id && input.has( RETURN_ICON + "_focus.png" );
		images.push( {
			rect,
			texture: RETURN_ICON + (focus ? "_focus.png" : ".png"),
			uv: focus ? FULL_UV : RETURN_ICON_STRIP,
			color: WHITE,
			clip: input.clip
		} );
		hits.push( { id, label: point.name, rect, kind: "button" } );
	}
	return { images, hits, paths };
}
