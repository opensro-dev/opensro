/*
===========================================================================

draw.ts - the two leaf primitives every frame painter shares

A rectangle quad and an image that names its path for loading and draws
once resident. They read only the frame's arrays and resources, so both
frame.ts and its other internals (item-slot.ts) draw through them. An
internal of frame.ts, which re-exports it.

===========================================================================
*/
import type { UiRect, UiQuad } from "@/engine/contracts/ui";
import type { UiFrame } from "./frame-contract";

/*
================
rect
================
*/
export function rect(
	frame: UiFrame,
	{ r, color, textureId = "", uv = [ 0, 0, 1, 1 ], clip = frame.full }: {
		r: UiRect;
		color: UiQuad["color"];
		textureId?: string;
		uv?: UiRect;
		clip?: UiRect;
	}
) {
	const quads = frame.quads;
	if ( r[2] > 0 && r[3] > 0 ) quads.push( { rect: r, color, texture: textureId, uv, clip } );
}

/*
================
image
================
*/
export function image(
	frame: UiFrame,
	{ r, path, color = frame.white, uv = [ 0, 0, 1, 1 ], clip = frame.full }: {
		r: UiRect;
		path: string;
		color?: UiQuad["color"];
		uv?: UiRect;
		clip?: UiRect;
	}
) {
	const paths = frame.paths, resources = frame.resources;
	paths.push( path );
	if ( resources.has( path ) ) rect( frame, { r: r, color: color, textureId: path, uv: uv, clip: clip } );
}
