/*
===========================================================================

world-map.ts - the M window: town pages, the overview tiles and markers

Projects world positions onto the authored map pages (worldmap_mapinfo),
decodes the map's icons and labels, and builds the window's quads in
CIFWorldMap's paint order. Pure layout; the UI owns the window state.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { minimapRotation } from "./hud-readouts";
const WORLD_MAP_ROOT = "/assets/images/Media_extracted/interface/worldmap/map/";
// The overview page is 28 x 8 tiles of 128 pixels; tile names count four
// regions per tile from region 66 x 113.
const WORLD_TILE_COLUMNS = 28;
const WORLD_TILE_ROWS = 8;
const WORLD_TILE_SIZE = 128;

/*
================
worldMapTilePath

The overview tile at column x, row y.
================
*/
function worldMapTilePath( x: number, y: number ): string {
	return WORLD_MAP_ROOT + `map_world_${66 + x * 4}x${113 - y * 4}.png`;
}

/*
================
worldMapPagePath

A town page's single picture.
================
*/
function worldMapPagePath( image: string ): string {
	return WORLD_MAP_ROOT + "map_" + image + ".png";
}

/*
================
worldMapImagePaths

Every image the map window can draw: all overview tiles, every town page
and every icon. The presentation culls to the view, so a drag reaches
tiles a single build never asked for; the window warm-up keeps these
decoded so a tile is never black while it loads. Native CIFWorldMap
creates every tile when the window is built.
================
*/
export function worldMapImagePaths( icons: readonly MapIcon[] = [] ): string[] {
	const paths: string[] = [];
	for ( let x = 0; x < WORLD_TILE_COLUMNS; x++ ) {
		for ( let y = 0; y < WORLD_TILE_ROWS; y++ ) paths.push( worldMapTilePath( x, y ) );
	}
	for ( const page of worldMapPages() ) paths.push( worldMapPagePath( page.image ) );
	for ( const icon of icons ) paths.push( icon.path );
	return paths;
}

/*
================
worldMapPages

The shipped worldmap_mapinfo.txt fields; 5758D0 probes towns 1..5 in order.
================
*/
export function worldMapPages() {
	return [
		{
			id: 1,
			name: "JANGAN",
			image: "jangan",
			size: [ 1024, 768 ],
			texture: [ 1024, 1024 ],
			bounds: [ 166, 99, 170, 96, 96, 96, 96, 96 ]
		},
		{
			id: 2,
			name: "DONHWANG",
			image: "donhwang",
			size: [ 640, 640 ],
			texture: [ 512, 512 ],
			picture: [ 512, 512 ],
			bounds: [ 152, 104, 154, 101, 0, 48, 96, 144 ]
		},
		{
			id: 3,
			name: "KHOTAN",
			image: "khotan",
			size: [ 640, 1047 ],
			texture: [ 512, 1024 ],
			picture: [ 512, 838 ],
			bounds: [ 134, 95, 136, 91, 48, 96, 192, 0 ]
		},
		{
			id: 4,
			name: "SAMARKAND",
			image: "samarkand",
			size: [ 640, 534 ],
			texture: [ 512, 512 ],
			picture: [ 512, 427 ],
			bounds: [ 106, 108, 109, 105, 96, 48, 96, 144 ]
		},
		{
			id: 5,
			name: "CONSTANTINOPLE",
			image: "constantinople",
			size: [ 890, 1024 ],
			texture: [ 1024, 1024 ],
			picture: [ 890, 1024 ],
			bounds: [ 77, 108, 81, 102, 0, 48, 192, 96 ]
		}
	] as const;
}
/*
================
worldMapPageAt

The town page whose region box holds the pose, or 0 for the overview.
================
*/
export function worldMapPageAt( p: Pose ): number {
	if ( p.regionId & 0x8000 ) return 0;
	const rx = p.regionId & 255,
		rz = p.regionId >>> 8;
	for ( const page of worldMapPages() ) {
		const [l, t, r, b, lx, ly, rxoff, ry] = page.bounds;
		if ( rx < l || rx > r || rz < b || rz > t ) continue;
		const x = (rx - l) * 1920 + p.x,
			y = (rz - b) * 1920 + p.z;
		if (
			x >= lx * 10 &&
			x < ((r - l) * 192 + rxoff) * 10 &&
			y >= ly * 10 &&
			y < ((t - b) * 192 + ry) * 10
		) {
			return page.id;
		}
	}
	return 0;
}
export interface MapLabel {
	readonly page: number;
	readonly x: number;
	readonly y: number;
	readonly text: string;
	readonly font: number;
	readonly color: UiQuad["color"];
}
export interface MapIcon {
	readonly id: number;
	readonly page: number;
	readonly destination: number;
	readonly x: number;
	readonly y: number;
	readonly width: number;
	readonly height: number;
	readonly path: string;
}
// A world-space marker for the roster/quest/hunting passes 57b1c0, 57b550 and
// 57ce80. Those read a live world position and project it through the page box
// exactly as the local-player marker does, so the caller supplies the position
// and the sprite, never a pixel offset. Every one is a 16x16 quad centred on
// the projected point (native corner offsets -8.0/+8.0).
export interface MapMarker {
	readonly regionId: number;
	readonly x: number;
	readonly z: number;
	readonly rotation: number;
	readonly path: string;
}
/*
================
decodeMapIcons

Validate the published map icon records.
================
*/
export function decodeMapIcons( raw: unknown ): readonly MapIcon[] {
	const rows = (raw as { rows?: unknown; }).rows;
	if ( !Array.isArray( rows ) ) throw Error( "Invalid world map records" );
	return rows.flatMap( ( row ) => {
		if ( !Array.isArray( row ) || row.length !== 20 ) {
			throw Error( "Invalid world map row" );
		}
		if ( Number( row[1] ) !== 2 ) return [];
		const page = Number( row[7] ),
			x = Number( row[10] ) + (page === 0 ? (Number( row[8] ) - 66) * 32 : 0),
			y = Number( row[11] ) + (page === 0 ? (113 - Number( row[9] )) * 32 : 0),
			width = Number( row[12] ),
			height = Number( row[13] ),
			destination = Number( row[6] ),
			id = Number( row[0] );
		if (
			![ id, page, x, y, width, height, destination ].every( Number.isFinite ) ||
			width <= 0 ||
			height <= 0 ||
			typeof row[2] !== "string"
		) {
			throw Error( "Invalid map icon" );
		}
		const path = row[2].replaceAll( "\\", "/" ).toLowerCase();
		if (
			!/^(interface\/worldmap|icon)\//.test( path ) ||
			path.includes( ".." ) ||
			!path.endsWith( ".ddj" )
		) {
			throw Error( "Invalid map icon path" );
		}
		return [
			{
				id,
				page,
				x,
				y,
				width,
				height,
				destination,
				path: "/assets/images/Media_extracted/" + path.replace( /\.ddj$/, ".png" )
			}
		];
	} );
}
/*
================
decodeMapLabels

807CC0 / 57E9A0: local coordinates are authored pixels; world labels
add 32 pixels per region. Text is centered by its measured glyph extent.
================
*/
export function decodeMapLabels(
	raw: unknown,
	names: Readonly<Record<string, string>>
): readonly MapLabel[] {
	const rows = (raw as { rows?: unknown; }).rows;
	if ( !Array.isArray( rows ) ) throw Error( "Invalid world map records" );
	return rows.flatMap( ( row ) => {
		if ( !Array.isArray( row ) || row.length !== 20 ) {
			throw Error( "Invalid world map row" );
		}
		if ( Number( row[1] ) !== 1 ) return [];
		const page = Number( row[7] ),
			x = Number( row[10] ) + (page === 0 ? (Number( row[8] ) - 66) * 32 : 0),
			y = Number( row[11] ) + (page === 0 ? (113 - Number( row[9] )) * 32 : 0),
			font = Number( row[13] );
		if ( ![ page, x, y, font ].every( Number.isFinite ) ) {
			throw Error( "Invalid map coordinates" );
		}
		const name = names[String( row[2] )];
		if ( name === undefined ) throw Error( "Missing map label " + row[2] );
		return [
			{
				page,
				x,
				y,
				font,
				text: Number( row[6] ) === 1 ?
					"<-" + name :
					Number( row[6] ) === 2 ?
					name + "->" :
					name,
				color: [
					Number( row[14] ) / 255,
					Number( row[15] ) / 255,
					Number( row[16] ) / 255,
					1
				] as const
			}
		];
	} );
}
/*
================
worldMapPresentation

The window's layers for a page at a pan: background, overlay, markers,
the icon hit rectangles, the label anchors and the clamped pan.
================
*/
export function worldMapPresentation(
	p: Pose,
	pageId: number,
	clip: UiRect,
	pan: readonly [number, number],
	center: Pose = p,
	labels: readonly MapLabel[] = [],
	icons: readonly MapIcon[] = [],
	markers: readonly MapMarker[] = []
) {
	const hits: { icon: MapIcon; rect: UiRect; }[] = [];
	// 57fe60 paints the page, then 57bbb0 traverses the 6200 list twice:
	// kind-2 icons first, kind-1 labels second, regardless of record-id order.
	// The caller inserts glyph quads after overlay and before the marker passes;
	// 57ce80 puts the local player last.
	const page = worldMapPages().find( ( row ) => row.id === pageId ),
		background: UiQuad[] = [],
		overlay: UiQuad[] = [],
		markerQuads: UiQuad[] = [],
		white = [ 1, 1, 1, 1 ] as const;
	const width = page?.size[0] ?? 3584,
		height = page?.size[1] ?? 1024;
	const b = page?.bounds,
		left = b ? b[0] * 192 + b[4] : 66 * 192,
		bottom = b ? b[3] * 192 + b[7] : 82 * 192,
		top = b ? b[1] * 192 + b[5] : 114 * 192,
		right = b ? b[2] * 192 + b[6] : 178 * 192;
	// 579920 moves the map by each drag delta after clipping it against the four
	// view edges, so the stored position never leaves the page. The returned pan
	// is that clamped position; callers keep it instead of an unbounded sum.
	const cx = clip[2] / 2 -
			(((center.regionId & 255) * 192 + center.x / 10 - left) /
					(right - left)) *
				width,
		cy = clip[3] / 2 -
			((top - ((center.regionId >>> 8) * 192 + center.z / 10)) /
					(top - bottom)) *
				height;
	const px = Math.min( 0, Math.max( clip[2] - width, cx + pan[0] ) ),
		py = Math.min( 0, Math.max( clip[3] - height, cy + pan[1] ) );
	const ox = clip[0] + px,
		oy = clip[1] + py;
	function sprite(
		layer: UiQuad[],
		texture: string,
		rect: UiRect,
		uv: UiRect = [ 0, 0, 1, 1 ],
		rotation = 0
	) {
		if (
			rect[0] + rect[2] > clip[0] &&
			rect[0] < clip[0] + clip[2] &&
			rect[1] + rect[3] > clip[1] &&
			rect[1] < clip[1] + clip[3]
		) {
			layer.push( { texture, rect, uv, color: white, clip, rotation } );
		}
	}
	// 57b1c0/57b550/57ce80 reuse the local-player projection verbatim for every
	// other marker, so one helper owns it.
	function marker(
		regionId: number,
		x: number,
		z: number,
		texture: string,
		rotation: number
	) {
		if ( regionId & 0x8000 ) return;
		const mx = (((regionId & 255) * 192 + x / 10 - left) / (right - left)) * width,
			my = ((top - ((regionId >>> 8) * 192 + z / 10)) / (top - bottom)) * height;
		sprite(
			markerQuads,
			texture,
			[ ox + mx - 8, oy + my - 8, 16, 16 ],
			[ 0, 0, 1, 1 ],
			rotation
		);
	}
	if ( page ) {
		const picture = "picture" in page ? page.picture : page.size;
		sprite(
			background,
			worldMapPagePath( page.image ),
			[ ox, oy, width, height ],
			[ 0, 0, picture[0] / page.texture[0], picture[1] / page.texture[1] ]
		);
	} else {
		for ( let x = 0; x < WORLD_TILE_COLUMNS; x++ ) {
			for ( let y = 0; y < WORLD_TILE_ROWS; y++ ) {
				sprite( background, worldMapTilePath( x, y ), [
					ox + x * WORLD_TILE_SIZE,
					oy + y * WORLD_TILE_SIZE,
					WORLD_TILE_SIZE,
					WORLD_TILE_SIZE
				] );
			}
		}
	}
	for ( const icon of icons ) {
		if ( icon.page === pageId ) {
			const r: UiRect = [ ox + icon.x, oy + icon.y, icon.width, icon.height ];
			sprite( overlay, icon.path, r );
			const x = Math.max( r[0], clip[0] ),
				y = Math.max( r[1], clip[1] ),
				right = Math.min( r[0] + r[2], clip[0] + clip[2] ),
				bottom = Math.min( r[1] + r[3], clip[1] + clip[3] );
			if ( right > x && bottom > y ) {
				hits.push( { icon, rect: [ x, y, right - x, bottom - y ] } );
			}
		}
	}
	// Caller-supplied passes first, then the local player, matching 57fe60's
	// order (quest NPCs, hunting points, apprenticeship, party) and 57ce80's
	// decision to draw mm_sign_character last.
	for ( const row of markers ) {
		marker( row.regionId, row.x, row.z, row.path, row.rotation );
	}
	marker(
		p.regionId,
		p.x,
		p.z,
		"/assets/images/Media_extracted/interface/minimap/mm_sign_character.png",
		minimapRotation( p.angle )
	);
	return {
		background,
		overlay,
		markers: markerQuads,
		// Paint order without the text layer; callers that render labels splice
		// their glyph quads between `background` and `overlay` instead.
		quads: [ ...background, ...overlay, ...markerQuads ],
		hits,
		labels: labels
			.filter( ( label ) => label.page === pageId )
			.map( ( label ) => ({ label, x: ox + label.x, y: oy + label.y, clip }) ),
		pan: [ px - cx, py - cy ] as [number, number]
	};
}
/*
================
worldMapQuads

The paint-ordered quads without the label text layer.
================
*/
export function worldMapQuads(
	p: Pose,
	pageId: number,
	clip: UiRect,
	pan: readonly [number, number],
	center: Pose = p
): readonly UiQuad[] {
	return worldMapPresentation( p, pageId, clip, pan, center ).quads;
}

// Glyph ink may reach past a label's measured box (bearings, descent).
const MAP_LABEL_INK_MARGIN = 16;

/*
================
mapLabelVisible

Whether a label's box, widened by the ink margin, reaches the window. A
label wholly outside draws nothing and cannot overlap a visible label's
ink, so it is not laid out: a page has hundreds of labels, and the map
follows the player, so every one moves each frame.
================
*/
export function mapLabelVisible( box: UiRect, clip: UiRect ): boolean {
	return !(
		box[0] + box[2] + MAP_LABEL_INK_MARGIN < clip[0] ||
		box[0] - MAP_LABEL_INK_MARGIN > clip[0] + clip[2] ||
		box[1] + box[3] + MAP_LABEL_INK_MARGIN < clip[1] ||
		box[1] - MAP_LABEL_INK_MARGIN > clip[1] + clip[3]
	);
}
