/*
===========================================================================

map-chrome.ts - the v1.188 M window's chrome

The later native client draws its world map window from
res_ui/worldmapguide.2dt (the pinned v1.188 archive): a 650 x 422
mframe window whose map panel stands at 5,33 640 x 384, the foot frame
that carries the monster level note and its checkbox, the top-left
corner the side tab hangs from, the four map-panel corner edges, the
globe and the follow pin at the panel's top right, the floor spinner
under them, and the side tab that opens the guide panel at the
window's left. This module places and paints all of it; the map tools
own the state each piece shows (follow mode, filter, floor, guide).

Positions are the layout's own, relative to the window's top left. A
window narrower or shorter than 650 x 422 (the compact phone layout)
keeps the right half's pieces at their distance from its right edge
and the foot at its distance from the bottom.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { GlyphStyle } from "@/engine/foundation/rendering/ui-glyphs";
import { frameParts, frameRing } from "@/engine/foundation/ui/frame-ring";
import { normalTile } from "@/engine/foundation/ui/normal-tile";

const IFACE = "/assets/images/Media_extracted/interface/";
// The worldmap art folder, beside which the build imports the later
// client's map art (import_vsro_worldmap_buttons.py).
export const WMAP = IFACE + "worldmap/";
export const GLOBE = WMAP + "wmap_big_button_world.png";
export const PIN_AUTO = WMAP + "wmap_button_automatic.png";
export const PIN_PASSIVE = WMAP + "wmap_button_passivity.png";
export const CHECK_ON = IFACE + "ifcommon/com_checkbutton_on.png";
export const CHECK_OFF = IFACE + "ifcommon/com_checkbutton_off.png";
const FLOOR_UP = WMAP + "wmap_up_button.png";
const FLOOR_UP_OFF = WMAP + "wmap_up_button_disable.png";
const FLOOR_DOWN = WMAP + "wmap_down_button.png";
const FLOOR_DOWN_OFF = WMAP + "wmap_down_button_disable.png";
const FOOT_FRAME = WMAP + "wmap_wnd_bottom_frame.png";
const CORNER = WMAP + "wmap_wnd_corner_02.png";
const EDGES = [ 1, 2, 3, 4 ].map( n => WMAP + "wmap_window_edge_" + n + ".png" );
const SIDE_TAB = IFACE + "guide/gd_side_tab.png";
// The tab's handle points at the way it moves: left (com_side02) to
// open the guide, right (com_side) to close it, as the Game Guide's.
const SIDE_CLOSED = IFACE + "ifcommon/com_side02_button.png";
const SIDE_OPEN = IFACE + "ifcommon/com_side_button.png";
const GUIDE_FRAME = IFACE + "inventory/int_window_";
const GUIDE_INNER = IFACE + "equipment/equip_window_";
const GUIDE_TILE = IFACE + "ifcommon/bg_tile/com_bg_tile_b.png";
const GUIDE_BLACK = IFACE + "ifcommon/bg_tile/com_bg_tile_d.png";
const GUIDE_TABS = {
	area: { on: IFACE + "guide/gd_area_tab_on.png", off: IFACE + "guide/gd_area_tab_off.png" },
	monster: { on: IFACE + "guide/gd_monster_tab_on.png", off: IFACE + "guide/gd_monster_tab_off.png" }
} as const;
const WHITE = [ 1, 1, 1, 1 ] as const;
const BLACK = [ 0, 0, 0, 1 ] as const;
const FULL_UV: UiRect = [ 0, 0, 1, 1 ];

// The window (CNIFWorldMap id 28) and its map panel.
export const MAP_WINDOW_WIDTH = 650;
export const MAP_WINDOW_HEIGHT = 422;
const PANEL_LEFT = 5, PANEL_TOP = 33, PANEL_RIGHT = 5, PANEL_BOTTOM = 5;
// The foot frame (id 26) at 0,412, 652 x 44: two wider than the window
// and 34 below it, so the window with its foot spans 456.
const FOOT: UiRect = [ 0, 412, 652, 44 ];
export const MAP_WINDOW_EXTENT = FOOT[1] + FOOT[3];
// The level note (id 30) and its checkbox (id 31) on the foot. The note
// stands one row below its layout rect, as map1 shows the text (our
// glyphs centre one higher than the client's in the same rect).
const NOTE: UiRect = [ 23, 429, 583, 10 ];
const CHECK: UiRect = [ 614, 426, 16, 16 ];
// The globe (id 6) and the pin (id 20).
const GLOBE_RECT: UiRect = [ 567, 39, 36, 36 ];
const PIN_RECT: UiRect = [ 605, 39, 36, 36 ];
// The floor spinner (id 4): its black box, up (22), label (27), down (24).
// The layout gives the box 611,89 29x47; map2 shows black only from the
// up arrow's top row to the down arrow's bottom row, 28 wide.
const SPIN_BOX: UiRect = [ 611, 90, 28, 45 ];
const SPIN_UP: UiRect = [ 612, 90, 28, 12 ];
const SPIN_LABEL: UiRect = [ 615, 106, 23, 17 ];
const SPIN_DOWN: UiRect = [ 612, 123, 28, 12 ];
// The corner (id 30, wmap_wnd_corner_02) and the side tab (id 13) with
// its handle (id 15), left of the window. The layout file says -18,1 and
// -12,97; map1 and map2 show the client draws them at -19,0 and -15,96
// (matched to the pixel against the foot frame), so those are the rects.
const CORNER_RECT: UiRect = [ 0, 0, 28, 72 ];
const SIDE_TAB_RECT: UiRect = [ -19, 0, 20, 297 ];
const SIDE_BUTTON_RECT: UiRect = [ -15, 96, 12, 96 ];
const EDGE_SIZE = 4;
// The guide panel (CNIFWorldMapGuide id 11) and its layers, relative to
// the panel: the int_window_ frame, the tile (id 12), the inner
// equip_window_ frame (id 16) and the black tile (id 21) the tree
// stands on. The tabs are v1.696's (worldmapguide_renewal ids 335 and
// 336): v1.188 builds them in code, three to the right of v1.696's place
// (17 and 77), where map1 shows them.
export const GUIDE_WIDTH = 298;
const GUIDE_HEIGHT = 296;
const GUIDE_TILE_RECT: UiRect = [ 16, 16, 270, 264 ];
const GUIDE_INNER_RECT: UiRect = [ 11, 32, 279, 258 ];
const GUIDE_BLACK_RECT: UiRect = [ 22, 43, 257, 235 ];
const GUIDE_AREA_TAB: UiRect = [ 20, 8, 56, 24 ];
const GUIDE_MONSTER_TAB: UiRect = [ 80, 8, 56, 24 ];

/*
================
MapChromeText

Only the font operation the chrome uses.
================
*/
interface MapChromeText {
	quads( value: string, rect: UiRect, clip: UiRect, color: UiQuad["color"], style?: GlyphStyle ): UiQuad[];
}

/*
================
MapChromePaint

One frame's paint target: the window rect (its frame, not the foot),
the screen clip and the output lists.
================
*/
export interface MapChromePaint {
	window: UiRect;
	full: UiRect;
	quads: UiQuad[];
	controls: UiControl[];
	paths: string[];
}

/*
================
place

A layout rect on this window: the right half keeps its distance from
the right edge, the lower half from the bottom.
================
*/
function place( box: UiRect, rel: UiRect ): UiRect {
	const x = rel[0] >= MAP_WINDOW_WIDTH / 2 ?
		box[0] + box[2] - (MAP_WINDOW_WIDTH - rel[0]) :
		box[0] + rel[0];
	const y = rel[1] >= MAP_WINDOW_HEIGHT / 2 ?
		box[1] + box[3] - (MAP_WINDOW_HEIGHT - rel[1]) :
		box[1] + rel[1];
	return [ x, y, rel[2], rel[3] ];
}

/*
================
image
================
*/
function image( paint: MapChromePaint, texture: string, rect: UiRect, clip: UiRect = paint.full ) {
	paint.paths.push( texture );
	paint.quads.push( { texture, rect, uv: FULL_UV, color: WHITE, clip } );
}

/*
================
mapPanel

The map's screen rect inside a window of this size.
================
*/
export function mapPanel( box: UiRect ): UiRect {
	return [
		box[0] + PANEL_LEFT,
		box[1] + PANEL_TOP,
		box[2] - PANEL_LEFT - PANEL_RIGHT,
		box[3] - PANEL_TOP - PANEL_BOTTOM
	];
}

/*
================
windowCorners

The corner the side tab hangs from and the map panel's four edges,
drawn over the frame and the map.
================
*/
export function windowCorners( paint: MapChromePaint ) {
	const panel = mapPanel( paint.window );
	const right = panel[0] + panel[2] - EDGE_SIZE, bottom = panel[1] + panel[3] - EDGE_SIZE;
	const corners: [number, number][] = [ [ panel[0], panel[1] ], [ right, panel[1] ], [ panel[0], bottom ], [
		right,
		bottom
	] ];
	corners.forEach( ( [x, y], index ) => image( paint, EDGES[index]!, [ x, y, EDGE_SIZE, EDGE_SIZE ] ) );
	image( paint, CORNER, place( paint.window, CORNER_RECT ) );
}

/*
================
followPin

The follow pin (id 20). v1.188 has no captioned AUTO_MOVE button: the
pin is the one follow switch, wmap_button_automatic while the map
follows the character, wmap_button_passivity while it stands where it
was put. The press lands on the map HUD's own follow state.
================
*/
export function followPin( paint: MapChromePaint, followed: boolean ) {
	const rect = place( paint.window, PIN_RECT );
	image( paint, followed ? PIN_AUTO : PIN_PASSIVE, rect );
	paint.paths.push( PIN_AUTO, PIN_PASSIVE );
	paint.controls.push( {
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
globeButton

The big globe back to the world (id 6), beside the pin. map1 and map2
show it on a dungeon page and not on the world page: it stands on any
page but the world.
================
*/
export function globeButton( paint: MapChromePaint ) {
	const rect = place( paint.window, GLOBE_RECT );
	image( paint, GLOBE, rect );
	paint.controls.push( {
		id: "map-globe",
		kind: "button",
		label: "World map",
		helpText: "Back to the world map",
		rect
	} );
}

/*
================
floorSpinner

The floor spinner (id 4): a black box with the up arrow, the shown
floor's label and the down arrow. The up arrow takes the next floor
down; on the first floor the client greys the down arrow, and the
ends hold.
================
*/
export function floorSpinner(
	paint: MapChromePaint,
	text: MapChromeText,
	spin: { label: string; upOff: boolean; downOff: boolean; }
) {
	const box = place( paint.window, SPIN_BOX ),
		up = place( paint.window, SPIN_UP ),
		label = place( paint.window, SPIN_LABEL ),
		down = place( paint.window, SPIN_DOWN );
	paint.paths.push( FLOOR_UP, FLOOR_UP_OFF, FLOOR_DOWN, FLOOR_DOWN_OFF );
	paint.quads.push( { texture: "", rect: box, uv: FULL_UV, color: BLACK, clip: paint.full } );
	image( paint, spin.upOff ? FLOOR_UP_OFF : FLOOR_UP, up );
	paint.controls.push( {
		id: "map-floor-up",
		kind: "button",
		label: "Next floor",
		helpText: "Show the next floor down",
		rect: up,
		disabled: spin.upOff
	} );
	paint.quads.push( ...text.quads( spin.label, label, paint.full, WHITE, { hAlign: 1, vAlign: 1 } ) );
	image( paint, spin.downOff ? FLOOR_DOWN_OFF : FLOOR_DOWN, down );
	paint.controls.push( {
		id: "map-floor-down",
		kind: "button",
		label: "Previous floor",
		helpText: "Show the floor before",
		rect: down,
		disabled: spin.downOff
	} );
}

/*
================
mapFoot

The foot frame (id 26) with the monster level note (id 30) right
against its checkbox (id 31), on the world and a dungeon page alike.
The note is the client's UIIT_STT_WORLDMAP_LEVEL_MONSTER_LIST text.
================
*/
export function mapFoot(
	paint: MapChromePaint,
	text: MapChromeText,
	foot: { note: string | undefined; checked: boolean; }
) {
	const extra = paint.window[2] - MAP_WINDOW_WIDTH;
	const frame = place( paint.window, FOOT ), note = place( paint.window, NOTE ), check = place( paint.window, CHECK );
	const footRect: UiRect = [ frame[0], frame[1], frame[2] + extra, frame[3] ];
	// The foot reaches below the window frame: its surface keeps clicks
	// off the world as the window's own does.
	paint.controls.push( { id: "map-foot", label: "Map foot", kind: "region", rect: footRect } );
	image( paint, FOOT_FRAME, footRect );
	if ( foot.note ) {
		paint.quads.push(
			// The note never leaves its box: the small window's foot is
			// narrower than the text, which then loses its left end.
			...text.quads(
				foot.note,
				[ note[0], note[1], note[2] + extra, note[3] ],
				[
					note[0],
					footRect[1],
					Math.max( 0, note[2] + extra ),
					footRect[3]
				],
				WHITE,
				{
					hAlign: 2,
					vAlign: 1
				}
			)
		);
	}
	paint.paths.push( CHECK_ON, CHECK_OFF );
	image( paint, foot.checked ? CHECK_ON : CHECK_OFF, check );
	paint.controls.push( {
		id: "map-level-filter",
		kind: "button",
		label: "Monster level filter",
		helpText: foot.note,
		rect: check,
		selected: foot.checked
	} );
}

/*
================
mapLeftReach

How far left of the window its chrome reaches: the side tab, and the
guide panel beside it while open. The map HUD keeps the window that far
in from the screen's left edge.
================
*/
export function mapLeftReach( guideOpen: boolean ): number {
	return (guideOpen ? GUIDE_WIDTH : 0) - SIDE_TAB_RECT[0];
}

/*
================
guideLeft

The guide panel's left edge: beside the window, or at the screen's
left edge when the screen is too narrow for both (port-only: the
v1.188 client's 800-wide minimum always fits them side by side).
================
*/
export function guideLeft( paint: { window: UiRect; full: UiRect; } ): number {
	return Math.max( paint.full[0], paint.window[0] - GUIDE_WIDTH );
}

/*
================
sideTab

The side tab (id 13) and its handle (id 15): at the window's left edge
while the guide is shut, at the guide panel's left edge while it is
open, as map2 and map1 show them.
================
*/
export function sideTab( paint: MapChromePaint, open: boolean ) {
	const shift = open ? guideLeft( paint ) - paint.window[0] : 0;
	const tab = place( paint.window, SIDE_TAB_RECT ), handle = place( paint.window, SIDE_BUTTON_RECT );
	image( paint, SIDE_TAB, [ tab[0] + shift, tab[1], tab[2], tab[3] ] );
	const handleRect: UiRect = [ handle[0] + shift, handle[1], handle[2], handle[3] ];
	image( paint, open ? SIDE_OPEN : SIDE_CLOSED, handleRect );
	paint.paths.push( SIDE_OPEN, SIDE_CLOSED );
	paint.controls.push( {
		id: "map-side-tab",
		kind: "button",
		label: open ? "Close the guide" : "Open the guide",
		helpText: (open ? "Close" : "Open") + " the field and monster guide",
		rect: handleRect,
		selected: open
	} );
}

/*
================
guidePanel

The guide panel left of the window (CNIFWorldMapGuide): its frame,
tiles, inner frame and black tile, the Field and Monster tabs, and a
region control that keeps clicks off the world. Returns the black
tile's rect, where the tree stands.
================
*/
export function guidePanel(
	paint: MapChromePaint,
	guide: { tab: "area" | "monster"; size: ( path: string ) => readonly [number, number] | undefined; }
): UiRect {
	const left = guideLeft( paint ), top = paint.window[1];
	const at = ( rel: UiRect ): UiRect => [ left + rel[0], top + rel[1], rel[2], rel[3] ];
	const rect: UiRect = [ left, top, GUIDE_WIDTH, GUIDE_HEIGHT ];
	const ring = ( prefix: string, r: UiRect ) => {
		const parts = frameParts().map( part => prefix + part + ".png" );
		paint.paths.push( ...parts );
		paint.quads.push( ...frameRing( r, prefix, parts.map( part => guide.size( part ) ), paint.full ) );
	};
	const tile = ( texture: string, r: UiRect ) => {
		paint.paths.push( texture );
		paint.quads.push( ...normalTile( r, texture, guide.size( texture ), paint.full ) );
	};
	paint.controls.push( { id: "map-guide", label: "Guide", kind: "region", rect } );
	tile( GUIDE_TILE, at( GUIDE_TILE_RECT ) );
	ring( GUIDE_FRAME, rect );
	ring( GUIDE_INNER, at( GUIDE_INNER_RECT ) );
	tile( GUIDE_BLACK, at( GUIDE_BLACK_RECT ) );
	for (
		const [tab, rel, label] of [ [ "area", GUIDE_AREA_TAB, "Field" ], [
			"monster",
			GUIDE_MONSTER_TAB,
			"Monster"
		] ] as const
	) {
		const r = at( rel );
		paint.paths.push( GUIDE_TABS[tab].on, GUIDE_TABS[tab].off );
		image( paint, guide.tab === tab ? GUIDE_TABS[tab].on : GUIDE_TABS[tab].off, r );
		paint.controls.push( {
			id: "map-guide-tab:" + tab,
			kind: "button",
			label,
			helpText: "The " + label.toLowerCase() + " guide",
			rect: r,
			selected: guide.tab === tab
		} );
	}
	return at( GUIDE_BLACK_RECT );
}
