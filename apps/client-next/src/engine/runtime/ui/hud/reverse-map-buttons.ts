/*
===========================================================================

reverse-map-buttons.ts - the reverse return points' buttons on the M map

While the map holds a reverse return scroll (reverse-return-hud.ts), each
published map point is a button. The later client draws them with its own
art (sub_e57d50, issue #3391): interface/worldmap/map/xy_returnicon with
_focus and _press faces. This module paints one frame of them from the
points the world map already projected; the map HUD owns the window.

A dungeon group's flag opens a small menu of the group's floors, the
v1.188 client's own popup (sro_client.exe: the button-up handler
0x6971c8..0x6973bd fills it and moves it to the icon's bottom-left corner,
4 px down; 0x631d60 adds one 155 x 22 interface/worldmap/map/map_return
button per floor, stacked from y 1, labelled with the floor's zone name).
Which group's menu is open belongs to the reverse return HUD.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import { REVERSE_MAP_FLOOR } from "@/engine/foundation/gameplay/reverse-return-map";
import { REVERSE_MAP_MARKER } from "@/engine/foundation/ui/world-map";

const RETURN_ICON = "/assets/images/Media_extracted/interface/worldmap/map/xy_returnicon";
const RETURN_ICON_SIZE = 32;
// The normal face is a 64 x 32 strip whose left half the button draws
// (its quad's u runs 0 to 0.5, sub_e57d50); hover and press are 32 x 32.
const RETURN_ICON_STRIP: UiRect = [ 0, 0, .5, 1 ];
const FULL_UV: UiRect = [ 0, 0, 1, 1 ];
const WHITE = [ 1, 1, 1, 1 ] as const;
const REVERSE_MAP_POINT = "reverse-map-point:";
// The floor menu (v1.188 0x631d60 and 0x6973a5): each entry is a 155 x 22
// map_return button; the first sits 1 px into the popup, which the map
// moves to the flag icon's bottom-left corner plus 4 px down.
const MENU_ENTRY = "/assets/images/Media_extracted/interface/worldmap/map/map_return";
const MENU_ENTRY_WIDTH = 155;
const MENU_ENTRY_HEIGHT = 22;
const MENU_FIRST_ENTRY_Y = 1;
const MENU_DROP = 4;
// Without the later client's art the entry is a dark plate (port-only).
const MENU_PLATE = [ .08, .07, .05, .85 ] as const;
const MENU_PLATE_FOCUS = [ .22, .18, .1, .9 ] as const;

/*
================
ReverseMapButtons

One frame's input: the projected map points (never a floor), the open
menu's floors (the flag's group, in table order) or null, the map clip,
the hovered id and whether an image is loaded.
================
*/
export interface ReverseMapButtons {
	points: readonly {
		point: { id: number; name: string; group?: number; };
		rect: UiRect;
	}[];
	menu?: { group: number; floors: readonly { id: number; name: string; }[]; } | null;
	clip: UiRect;
	hover: string | null;
	has( path: string ): boolean;
}

/*
================
reverseMapButtons

The buttons, centred on their points (inference, as every map sign
here: the placement code was not read). A build without the later
client's art keeps the port's old sign at the point's own rect. labels
are the open menu's floor names, which the map writes over the entries.
================
*/
export function reverseMapButtons( input: ReverseMapButtons ): {
	images: UiQuad[];
	hits: UiControl[];
	labels: { value: string; rect: UiRect; }[];
	paths: string[];
} {
	const images: UiQuad[] = [], hits: UiControl[] = [], labels: { value: string; rect: UiRect; }[] = [];
	const paths = [ RETURN_ICON + ".png", RETURN_ICON + "_focus.png", MENU_ENTRY + ".png", MENU_ENTRY + "_focus.png" ];
	const art = input.has( RETURN_ICON + ".png" );
	let menuAnchor: UiRect | null = null;
	for ( const { point, rect: placed } of input.points ) {
		const id = REVERSE_MAP_POINT + point.id;
		const rect: UiRect = art ?
			[
				Math.round( placed[0] + placed[2] / 2 - RETURN_ICON_SIZE / 2 ),
				Math.round( placed[1] + placed[3] / 2 - RETURN_ICON_SIZE / 2 ),
				RETURN_ICON_SIZE,
				RETURN_ICON_SIZE
			] :
			placed;
		if ( input.menu && point.group === input.menu.group ) menuAnchor = rect;
		if ( !art ) {
			images.push( { rect, texture: REVERSE_MAP_MARKER, uv: FULL_UV, color: WHITE, clip: input.clip } );
			hits.push( { id, label: point.name, rect, kind: "button" } );
			continue;
		}
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
	if ( input.menu && menuAnchor ) {
		const anchor: UiRect = menuAnchor;
		const entryArt = input.has( MENU_ENTRY + ".png" );
		input.menu.floors.forEach( ( floor, index ) => {
			const id = REVERSE_MAP_FLOOR + floor.id;
			const rect: UiRect = [
				anchor[0],
				anchor[1] + anchor[3] + MENU_DROP + MENU_FIRST_ENTRY_Y + index * MENU_ENTRY_HEIGHT,
				MENU_ENTRY_WIDTH,
				MENU_ENTRY_HEIGHT
			];
			const focus = input.hover === id;
			images.push(
				entryArt ?
					{
						rect,
						texture: MENU_ENTRY + (focus && input.has( MENU_ENTRY + "_focus.png" ) ? "_focus.png" : ".png"),
						uv: FULL_UV,
						color: WHITE,
						clip: input.clip
					} :
					{ rect, texture: "", uv: FULL_UV, color: focus ? MENU_PLATE_FOCUS : MENU_PLATE, clip: input.clip }
			);
			hits.push( { id, label: floor.name, rect, kind: "button" } );
			labels.push( { value: floor.name, rect } );
		} );
	}
	return { images, hits, labels, paths };
}
