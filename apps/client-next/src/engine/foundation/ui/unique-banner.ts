/*
===========================================================================

unique-banner.ts - the warning and notice banners' plate geometry

Centres the measured caption and frames it with the notice chrome. The
plate normally hugs the caption; a BannerPlateStyle (port-only, #1976)
grows it and sets its opacity at full show. The chrome is always native.

===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";

/*
================
BannerPlateStyle

Port-only, not native. Empty is the native plate.
================
*/
export interface BannerPlateStyle {
	readonly padX?: number;
	readonly padY?: number;
	readonly plateAlpha?: number;
}

/*
================
uniqueBannerQuads

CIFNotify warning sibling: fixed Y 130, measured and centered text,
untextured blue decorator followed by the outside stretch chrome.
================
*/
export function uniqueBannerQuads(
	value: string,
	alpha: number,
	width: number,
	height: number,
	textWidth: number,
	textHeight: number,
	paths: readonly string[],
	size: ( p: string ) => readonly [number, number] | undefined,
	y = 130,
	rgb: readonly [number, number, number] = [ 0, 52, 92 ],
	style: BannerPlateStyle = {}
): UiQuad[] {
	const { padX = 0, padY = 0, plateAlpha } = style;
	if ( alpha <= 0 || !value ) return [];
	// 68E841..68E8E2 pushes bottom/top edge, right/left edge2 (right-to-left
	// arguments). 6FA940 stores its first resource as the left edge at +38C.
	// edge2 is the 40px side fade; edge is the 4px-wide top/bottom strip.
	const [corner, horizontal, vertical] = paths;
	if ( !corner || !horizontal || !vertical ) return [];
	const c = size( corner ), e = size( vertical ), h = size( horizontal );
	if ( !c || !e || !h ) return [];
	const x = (width >> 1) - (Math.ceil( textWidth ) >> 1),
		w = Math.ceil( textWidth ),
		t = textHeight,
		clip: UiRect = [ 0, 0, width, height ];
	const a = Math.round( alpha * 255 ),
		plate: UiRect = [ x - padX, y - padY, w + 2 * padX, t + 2 * padY ],
		quads: UiQuad[] = [ {
			rect: plate,
			texture: "",
			uv: [ 0, 0, 1, 1 ],
			clip,
			color: [
				rgb[0] / 255,
				rgb[1] / 255,
				rgb[2] / 255,
				(plateAlpha === undefined ? 128 - ((255 - a) >> 1) : Math.round( plateAlpha * a )) / 255
			]
		} ];
	const add = ( texture: string, rect: UiRect, uv: UiRect ) =>
		quads.push( { texture, rect, uv, clip, color: [ 1, 1, 1, alpha ] } );
	const [px, py, pw, pt] = plate;
	add( corner, [ px - c[0]!, py - c[1]!, c[0]!, c[1]! ], [ 0, 0, 1, 1 ] );
	add( corner, [ px + pw, py - c[1]!, c[0]!, c[1]! ], [ 1, 0, -1, 1 ] );
	add( corner, [ px + pw, py + pt, c[0]!, c[1]! ], [ 1, 1, -1, -1 ] );
	add( corner, [ px - c[0]!, py + pt, c[0]!, c[1]! ], [ 0, 1, 1, -1 ] );
	add( vertical, [ px - e[0]!, py, e[0]!, pt ], [ 0, 0, 1, 1 ] );
	add( vertical, [ px + pw, py, e[0]!, pt ], [ 1, 0, -1, 1 ] );
	add( horizontal, [ px, py - h[1]!, pw, h[1]! ], [ 0, 0, 1, 1 ] );
	add( horizontal, [ px, py + pt, pw, h[1]! ], [ 0, 1, 1, -1 ] );
	return quads;
}
