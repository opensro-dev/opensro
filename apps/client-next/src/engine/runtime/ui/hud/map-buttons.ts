/*
===========================================================================

map-buttons.ts - the renewal M window's button row and its location sign

The later client's world map (v1.696 worldmapguide_renewal.2dt) stands
three big buttons in one row at the render panel's top right: the
location button, the globe back to the world and the follow pin, with
no captioned AUTO_MOVE button. This module paints that row and frames
the location sign's animated strip; the map tools own the state each
button shows (the follow mode, the marked point) and call in here.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";

// The worldmap art folder, beside which the build imports the later
// client's map art (import_vsro_worldmap_buttons.py).
export const WMAP = "/assets/images/Media_extracted/interface/worldmap/";
const WHITE = [ 1, 1, 1, 1 ] as const;
export const GLOBE = WMAP + "wmap_big_button_world.png";
export const PIN_AUTO = WMAP + "wmap_button_automatic.png";
export const PIN_PASSIVE = WMAP + "wmap_button_passivity.png";
export const LOCATION = WMAP + "wmap_button_location.png";
// The location sign: one 12-frame strip (uv is u, v, width, height), a
// frame per 100 ms tick (v1.696 E73870 steps u by 1/12 on the timer
// E73CE0 starts at 0x64).
export const LOCATION_SIGN = WMAP + "wmap_sign_location.png";
const LOCATION_SIGN_FRAMES = 12;
const LOCATION_SIGN_FRAME_MS = 100;
const LOCATION_SIGN_SIZE = 32;
// The renewal window's button row (v1.696 worldmapguide_renewal.2dt):
// location 516, globe 555 and pin 594 at y 47, each 36 square, in a
// render panel at 5,33 640 wide - so measured from the panel's right
// edge and top, offsets the map's clip keeps on any window width.
const BUTTON_SIZE = 36;
const BUTTON_TOP = 14;
const PIN_RIGHT = 51;
const GLOBE_RIGHT = 90;
const LOCATION_RIGHT = 129;

/*
================
LocationSignMarker

The location sign as a map marker: the marked point, the strip and
this tick's frame of it, drawn at the native 32 square.
================
*/
export interface LocationSignMarker {
	regionId: number;
	x: number;
	z: number;
	path: string;
	uv: UiRect;
	size: number;
}

/*
================
locationSignFrame

The sign strip's frame at a clock reading in milliseconds.
================
*/
export function locationSignFrame( nowMs: number ): number {
	return Math.floor( nowMs / LOCATION_SIGN_FRAME_MS ) % LOCATION_SIGN_FRAMES;
}

/*
================
locationSignMarker
================
*/
export function locationSignMarker(
	point: { regionId: number; x: number; z: number; },
	frame: number
): LocationSignMarker {
	return {
		regionId: point.regionId,
		x: point.x,
		z: point.z,
		path: LOCATION_SIGN,
		uv: [ frame / LOCATION_SIGN_FRAMES, 0, 1 / LOCATION_SIGN_FRAMES, 1 ],
		size: LOCATION_SIGN_SIZE
	};
}

/*
================
followPin

The follow pin at the end of the renewal button row, on the world and
on a dungeon page alike (issue #3210). The renewal window has no
captioned AUTO_MOVE button: the pin is the one follow switch (v1.696
E74590 picks its face and its AUTO_MOVE / MANUAL_MOVE tooltip from the
map's follow setting), wmap_button_automatic while the map follows the
character and wmap_button_passivity while it stands where it was put.
The press lands on the map HUD's own follow state.
================
*/
export function followPin( clip: UiRect, followed: boolean, quads: UiQuad[], controls: UiControl[] ) {
	const rect = rowButton( clip, PIN_RIGHT );
	quads.push( {
		texture: followed ? PIN_AUTO : PIN_PASSIVE,
		rect,
		uv: [ 0, 0, 1, 1 ],
		color: WHITE,
		clip
	} );
	controls.push( {
		id: "map-pin",
		kind: "button",
		label: followed ? "Auto move" : "Manual move",
		helpText: followed ?
			"The map follows you; press for manual move" :
			"The map stands where you put it; press for auto move",
		rect,
		selected: followed
	} );
}

/*
================
rowButton

One button of the renewal row, measured from the clip's right edge.
================
*/
function rowButton( clip: UiRect, right: number ): UiRect {
	return [ clip[0] + clip[2] - right, clip[1] + BUTTON_TOP, BUTTON_SIZE, BUTTON_SIZE ];
}

/*
================
globeButton

The big globe back to the world, beside the pin. v1.696 E743B0 shows
it while the map stands on a page other than the world one (the flag
6B7900 sets compares the shown region with the player's); the port
reads that as any town or dungeon page (an inference: the second flag
it tests is not named yet).
================
*/
export function globeButton( clip: UiRect, quads: UiQuad[], controls: UiControl[] ) {
	const rect = rowButton( clip, GLOBE_RIGHT );
	quads.push( { texture: GLOBE, rect, uv: [ 0, 0, 1, 1 ], color: WHITE, clip } );
	controls.push( {
		id: "map-globe",
		kind: "button",
		label: "World map",
		helpText: "Back to the world map",
		rect
	} );
}

/*
================
locationButton

The renewal row's first button (v1.696 id 19, "show position"): marks
the player's point with the location sign; a second press clears it.
================
*/
export function locationButton( clip: UiRect, located: boolean, quads: UiQuad[], controls: UiControl[] ) {
	const rect = rowButton( clip, LOCATION_RIGHT );
	quads.push( { texture: LOCATION, rect, uv: [ 0, 0, 1, 1 ], color: WHITE, clip } );
	controls.push( {
		id: "map-locate",
		kind: "button",
		label: "My location",
		helpText: located ? "Clear the location sign" : "Mark where you stand",
		rect,
		selected: located
	} );
}
