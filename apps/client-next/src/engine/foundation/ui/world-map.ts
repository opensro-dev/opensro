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
import { featuresAt } from "@/engine/foundation/world/plane";
import { REGION_SIZE, sectorX, sectorY } from "@/engine/foundation/world/world-space";
import { minimapRotation } from "./hud-readouts";
const WORLD_MAP_ROOT = "/assets/images/Media_extracted/interface/worldmap/map/";
// The overview page is tiles of 128 pixels; tile names count four regions per
// tile from the layout's top-left region (66 x 113 and 28 x 8 tiles natively;
// worldmap_mapinfo.txt row 0 widens it for a client that ships more map).
// The local player's map sign, drawn last (57ce80).
export const MAP_LOCAL_MARKER = "/assets/images/Media_extracted/interface/minimap/mm_sign_character.png";
const WORLD_TILE_SIZE = 128;
const WORLD_TILE_REGIONS = 4;

/*
================
worldMapTilePath

The overview tile at column x, row y.
================
*/
function worldMapTilePath( x: number, y: number, overview: WorldOverview ): string {
	return WORLD_MAP_ROOT +
		`map_world_${overview.left + x * WORLD_TILE_REGIONS}x${overview.top - y * WORLD_TILE_REGIONS}.png`;
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
export function worldMapImagePaths(
	icons: readonly MapIcon[] = [],
	layout: WorldMapLayout = nativeLayout()
): string[] {
	const paths: string[] = [];
	for ( let x = 0; x < layout.overview.columns; x++ ) {
		for ( let y = 0; y < layout.overview.rows; y++ ) paths.push( worldMapTilePath( x, y, layout.overview ) );
	}
	for ( const page of worldMapPages( layout ) ) paths.push( worldMapPagePath( page.image ) );
	for ( const icon of icons ) paths.push( icon.path );
	return paths;
}

/*
================
WorldOverview

The overview page's region box and its size in tiles. Natively it is regions
66..177 across and 113..82 down (28 x 8 tiles).
================
*/
interface WorldOverview {
	readonly left: number;
	readonly top: number;
	readonly columns: number;
	readonly rows: number;
}
function nativeOverview(): WorldOverview {
	return { left: 66, top: 113, columns: 28, rows: 8 };
}
// Town pages beyond the five the native client knows, from the published
// worldmap_mapinfo rows. Ids 1000 and up are the fortress and arena pages,
// which this window does not draw.
const EXTRA_PAGE_FIRST = 6;
const EXTRA_PAGE_LAST = 999;

/*
================
WorldMapLayout

What the published worldmap_mapinfo rows add to the native map: the overview
box and the extra town pages. Plain data the HUD resources decode once and
the window passes down; this module keeps no state of its own.
================
*/
export interface WorldMapLayout {
	readonly overview: WorldOverview;
	readonly extra: readonly TownPage[];
}

/*
================
nativeLayout

The layout of a build that publishes no extra map: the native overview and
the five native towns. A function, not a constant, because this module keeps
no shared state (the ownership gate allows scalar constants only).
================
*/
export function nativeLayout(): WorldMapLayout {
	return { overview: nativeOverview(), extra: [] };
}

interface TownPage {
	readonly id: number;
	readonly name: string;
	readonly image: string;
	readonly size: readonly [number, number];
	readonly texture: readonly [number, number];
	readonly picture: readonly [number, number];
	readonly bounds: readonly [number, number, number, number, number, number, number, number];
}

/*
================
decodeWorldMapLayout

Reads the published worldmap_mapinfo rows: the overview's region box (row 0)
and every extra town page (a kind 1 row with id 6..999). Rows that do not
describe a whole-tile overview or a usable page are ignored, so a build with
no extra map keeps the native layout.
================
*/
export function decodeWorldMapLayout( raw: unknown ): WorldMapLayout {
	const rows = (raw as { rows?: unknown; } | null)?.rows;
	let overview = nativeOverview();
	if ( !Array.isArray( rows ) ) return nativeLayout();
	const pages: TownPage[] = [];
	for ( const row of rows ) {
		if ( !Array.isArray( row ) || row.length < 19 ) continue;
		const id = Number( row[0] ), kind = Number( row[1] );
		const box = [ 10, 11, 12, 13 ].map( i => Number( row[i] ) );
		if ( !box.every( Number.isInteger ) ) continue;
		const [left, top, right, bottom] = box as [number, number, number, number];
		if ( id === 0 && kind === 0 ) {
			const columns = (right - left + 1) / WORLD_TILE_REGIONS, tileRows = (top - bottom + 1) / WORLD_TILE_REGIONS;
			if ( Number.isInteger( columns ) && Number.isInteger( tileRows ) && columns > 0 && tileRows > 0 ) {
				overview = { left, top, columns, rows: tileRows };
			}
		} else if ( kind === 1 && id >= EXTRA_PAGE_FIRST && id <= EXTRA_PAGE_LAST ) {
			const picture = String( row[3] ).replaceAll( "\\", "/" ).split( "/" ).pop() ?? "";
			const sizes = [ 4, 5, 6, 7, 8, 9, 14, 15, 16, 17 ].map( i => Number( row[i] ) );
			if ( !/^map_[a-z0-9_]+\.ddj$/.test( picture ) || !sizes.every( Number.isFinite ) ) continue;
			const [textureX, textureY, pictureX, pictureY, drawX, drawY, ...offsets] = sizes as number[];
			pages.push( {
				id,
				name: String( row[2] ),
				image: picture.slice( "map_".length, -".ddj".length ),
				size: [ drawX!, drawY! ],
				texture: [ textureX!, textureY! ],
				picture: [ pictureX!, pictureY! ],
				bounds: [ left, top, right, bottom, ...offsets ] as unknown as TownPage["bounds"]
			} );
		}
	}
	return { overview, extra: pages };
}

/*
================
worldMapPages

The shipped worldmap_mapinfo.txt fields; 5758D0 probes towns 1..5 in order.
Pages published beyond those five (decodeWorldMapLayout) follow them.
================
*/
export function worldMapPages( layout: WorldMapLayout = nativeLayout() ) {
	return [ ...worldMapTownPages(), ...layout.extra ];
}

/*
================
worldMapTownPages
================
*/
function worldMapTownPages() {
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
export function worldMapPageAt( p: Pose, layout: WorldMapLayout = nativeLayout() ): number {
	if ( !featuresAt( p.regionId ).worldMap ) return 0;
	const rx = sectorX( p.regionId ),
		rz = sectorY( p.regionId );
	for ( const page of worldMapPages( layout ) ) {
		const [l, t, r, b, lx, ly, rxoff, ry] = page.bounds;
		if ( rx < l || rx > r || rz < b || rz > t ) continue;
		const x = (rx - l) * REGION_SIZE + p.x,
			y = (rz - b) * REGION_SIZE + p.z;
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
export function decodeMapIcons( raw: unknown, layout: WorldMapLayout = nativeLayout() ): readonly MapIcon[] {
	const rows = (raw as { rows?: unknown; }).rows;
	if ( !Array.isArray( rows ) ) throw Error( "Invalid world map records" );
	return rows.flatMap( ( row ) => {
		if ( !Array.isArray( row ) || row.length !== 20 ) {
			throw Error( "Invalid world map row" );
		}
		if ( Number( row[1] ) !== 2 ) return [];
		const page = Number( row[7] ),
			x = Number( row[10] ) + (page === 0 ? (Number( row[8] ) - layout.overview.left) * 32 : 0),
			y = Number( row[11] ) + (page === 0 ? (layout.overview.top - Number( row[9] )) * 32 : 0),
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
	names: Readonly<Record<string, string>>,
	layout: WorldMapLayout = nativeLayout()
): readonly MapLabel[] {
	const rows = (raw as { rows?: unknown; }).rows;
	if ( !Array.isArray( rows ) ) throw Error( "Invalid world map records" );
	return rows.flatMap( ( row ) => {
		if ( !Array.isArray( row ) || row.length !== 20 ) {
			throw Error( "Invalid world map row" );
		}
		if ( Number( row[1] ) !== 1 ) return [];
		const page = Number( row[7] ),
			x = Number( row[10] ) + (page === 0 ? (Number( row[8] ) - layout.overview.left) * 32 : 0),
			y = Number( row[11] ) + (page === 0 ? (layout.overview.top - Number( row[9] )) * 32 : 0),
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
the icon hit rectangles, the label anchors and the clamped pan. markerRows
names the source row of each marker quad (markers.length for the local
player), so a caller can paint after one marker without re-projecting.
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
	markers: readonly MapMarker[] = [],
	layout: WorldMapLayout = nativeLayout()
) {
	const hits: { icon: MapIcon; rect: UiRect; }[] = [];
	// 57fe60 paints the page, then 57bbb0 traverses the 6200 list twice:
	// kind-2 icons first, kind-1 labels second, regardless of record-id order.
	// The caller inserts glyph quads after overlay and before the marker passes;
	// 57ce80 puts the local player last.
	const background: UiQuad[] = [],
		overlay: UiQuad[] = [],
		markerQuads: UiQuad[] = [],
		markerRows: number[] = [],
		white = [ 1, 1, 1, 1 ] as const;
	const frame = worldMapFrame( pageId, clip, pan, center, layout );
	const { page, width, height, cx, cy, px, py, ox, oy } = frame;
	/*
	================
	sprite
	================
	*/
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
			return true;
		}
		return false;
	}
	/*
	================
	marker

	57b1c0/57b550/57ce80 reuse the local-player projection verbatim for every
	other marker, so one helper owns it.
	================
	*/
	function marker(
		row: number,
		regionId: number,
		x: number,
		z: number,
		texture: string,
		rotation: number
	) {
		const rect = worldMapMarkerRect( frame, { regionId, x, z } );
		if ( rect && sprite( markerQuads, texture, rect, [ 0, 0, 1, 1 ], rotation ) ) markerRows.push( row );
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
		for ( let x = 0; x < layout.overview.columns; x++ ) {
			for ( let y = 0; y < layout.overview.rows; y++ ) {
				sprite( background, worldMapTilePath( x, y, layout.overview ), [
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
	markers.forEach( ( row, index ) => marker( index, row.regionId, row.x, row.z, row.path, row.rotation ) );
	marker(
		markers.length,
		p.regionId,
		p.x,
		p.z,
		MAP_LOCAL_MARKER,
		minimapRotation( p.angle )
	);
	return {
		background,
		overlay,
		markers: markerQuads,
		markerRows,
		hits,
		labels: labels
			.filter( ( label ) => label.page === pageId )
			.map( ( label ) => ({ label, x: ox + label.x, y: oy + label.y, clip }) ),
		pan: [ px - cx, py - cy ] as [number, number]
	};
}
/*
================
worldMapFrame

Where a page lies in the view: its size and world bounds, and the
origin the view's centre and pan put it at. 579920 moves the map by each
drag delta after clipping it against the four view edges, so the stored
position never leaves the page; pan is that clamped position.
================
*/
export function worldMapFrame(
	pageId: number,
	clip: UiRect,
	pan: readonly [number, number],
	center: Pose,
	layout: WorldMapLayout = nativeLayout()
) {
	const overview = layout.overview;
	const page = worldMapPages( layout ).find( ( row ) => row.id === pageId );
	const width = page?.size[0] ?? overview.columns * WORLD_TILE_SIZE,
		height = page?.size[1] ?? overview.rows * WORLD_TILE_SIZE;
	const b = page?.bounds,
		left = b ? b[0] * 192 + b[4] : overview.left * 192,
		bottom = b ? b[3] * 192 + b[7] : (overview.top + 1 - overview.rows * WORLD_TILE_REGIONS) * 192,
		top = b ? b[1] * 192 + b[5] : (overview.top + 1) * 192,
		right = b ? b[2] * 192 + b[6] : (overview.left + overview.columns * WORLD_TILE_REGIONS) * 192;
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
	return { page, width, height, left, bottom, top, right, cx, cy, px, py, ox: clip[0] + px, oy: clip[1] + py };
}

/*
================
worldMapPoint

The inverse of the marker projection. The
outdoor region and region-local x/z under a view point, or null off-page.
Reuses worldMapFrame so the pick always agrees with the painted page.
================
*/
export function worldMapPoint(
	pageId: number,
	clip: UiRect,
	pan: readonly [number, number],
	center: Pose,
	sx: number,
	sy: number,
	layout: WorldMapLayout = nativeLayout()
): { regionId: number; x: number; z: number; } | null {
	const f = worldMapFrame( pageId, clip, pan, center, layout );
	const mx = sx - f.ox,
		my = sy - f.oy;
	if ( mx < 0 || my < 0 || mx >= f.width || my >= f.height ) return null;
	const u = f.left + (mx / f.width) * (f.right - f.left),
		v = f.top - (my / f.height) * (f.top - f.bottom);
	const rx = Math.floor( u / 192 ),
		rz = Math.floor( v / 192 );
	if ( rx < 0 || rx > 255 || rz < 0 || rz > 127 ) return null;
	return { regionId: (rz << 8) | rx, x: (u - rx * 192) * 10, z: (v - rz * 192) * 10 };
}

/*
================
inClip

The sprite test of worldMapPresentation: rect overlaps clip.
================
*/
function inClip( x: number, y: number, w: number, h: number, clip: UiRect ): boolean {
	return x + w > clip[0] && x < clip[0] + clip[2] && y + h > clip[1] && y < clip[1] + clip[3];
}

/*
================
worldMapDemand

The page images and icons worldMapPresentation would draw for the same
view, added to out, without building its quads, labels or hits: a closed
map demands what opening it shows, at a fraction of the cost. Markers add
their own images (the caller knows them).
================
*/
export function worldMapDemand(
	pageId: number,
	clip: UiRect,
	pan: readonly [number, number],
	center: Pose,
	icons: readonly MapIcon[],
	out: string[],
	layout: WorldMapLayout = nativeLayout()
) {
	const { page, width, height, ox, oy } = worldMapFrame( pageId, clip, pan, center, layout );
	if ( page ) {
		if ( inClip( ox, oy, width, height, clip ) ) out.push( worldMapPagePath( page.image ) );
	} else {
		for ( let x = 0; x < layout.overview.columns; x++ ) {
			for ( let y = 0; y < layout.overview.rows; y++ ) {
				const tx = ox + x * WORLD_TILE_SIZE, ty = oy + y * WORLD_TILE_SIZE;
				if ( inClip( tx, ty, WORLD_TILE_SIZE, WORLD_TILE_SIZE, clip ) ) {
					out.push( worldMapTilePath( x, y, layout.overview ) );
				}
			}
		}
	}
	for ( const icon of icons ) {
		if ( icon.page === pageId && inClip( ox + icon.x, oy + icon.y, icon.width, icon.height, clip ) ) {
			out.push( icon.path );
		}
	}
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
	center: Pose = p,
	layout: WorldMapLayout = nativeLayout()
): readonly UiQuad[] {
	// Paint order without the text layer; callers that render labels splice
	// their glyph quads between background and overlay instead.
	const presentation = worldMapPresentation( p, pageId, clip, pan, center, [], [], [], layout );
	return [ ...presentation.background, ...presentation.overlay, ...presentation.markers ];
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

// MAP_MARKER_SIZE is a marker sprite's side in screen pixels.
const MAP_MARKER_SIZE = 16;
// MAP_REGION_UNITS is a region's side in map units (1920 local / 10).
const MAP_REGION_UNITS = 192;
// REVERSE_MAP_MARKER marks a reverse return map point (port-only): the
// shipped hunting-point sign.
export const REVERSE_MAP_MARKER = "/assets/images/Media_extracted/interface/worldmap/wmap_sign_huntingpoint.png";

/*
================
worldMapMarkerRect

One projection owns the native markers and the reverse return map points
(after GrazKe, #336). Null for a dungeon region.
================
*/
function worldMapMarkerRect(
	frame: ReturnType<typeof worldMapFrame>,
	point: { readonly regionId: number; readonly x: number; readonly z: number; }
): UiRect | null {
	if ( !featuresAt( point.regionId ).worldMap ) return null;
	const { left, right, top, bottom, width, height, ox, oy } = frame;
	const x = ((sectorX( point.regionId ) * MAP_REGION_UNITS + point.x / 10 - left) / (right - left)) * width;
	const y = ((top - (sectorY( point.regionId ) * MAP_REGION_UNITS + point.z / 10)) / (top - bottom)) * height;
	return [ ox + x - MAP_MARKER_SIZE / 2, oy + y - MAP_MARKER_SIZE / 2, MAP_MARKER_SIZE, MAP_MARKER_SIZE ];
}

/*
================
worldMapPointMarkers

Port-only: the reverse return map points inside the map's clip, with the
rect each one is painted and clicked at.
================
*/
export function worldMapPointMarkers<
	Point extends { readonly regionId: number; readonly x: number; readonly z: number; }
>(
	view: {
		readonly page: number;
		readonly clip: UiRect;
		readonly pan: readonly [number, number];
		readonly center: Pose;
		readonly points: readonly Point[];
		readonly layout?: WorldMapLayout;
	}
): { readonly point: Point; readonly rect: UiRect; }[] {
	const frame = worldMapFrame( view.page, view.clip, view.pan, view.center, view.layout );
	return view.points.flatMap( point => {
		const rect = worldMapMarkerRect( frame, point );
		return rect && mapLabelVisible( rect, view.clip ) ? [ { point, rect } ] : [];
	} );
}
