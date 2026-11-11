/*
===========================================================================

cave-map.ts - the M window's dungeon pages

The later client's Dungeonmap pages (import_vsro_dungeon_maps.py): one image
per floor, its room names and gate signs at their authored positions.
Pure layout; the map HUD owns the window state. A floor without a page keeps
the world map.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { REGION_SIZE } from "@/engine/foundation/world/world-space";
import { planeKindOf } from "@/engine/foundation/world/plane";
import { minimapRotation } from "./hud-readouts";
import { MAP_LOCAL_MARKER } from "./world-map";

const CATALOG_FORMAT = "sro-later-dungeon-map";
const CATALOG_VERSION = 1;
// The offset in a dungeon tile's name, as the minimap's (minimapTiles).
const CAVE_TILE_ORIGIN = 128;
const CAVE_MARKER_SIZE = 16;
const COLOR_RANGE = 255;

export interface CaveMapLabel {
	readonly text: string;
	readonly x: number;
	readonly y: number;
	readonly font: number;
	readonly color: readonly [number, number, number];
}

export interface CaveMapSign {
	readonly name: string;
	readonly x: number;
	readonly y: number;
	readonly width: number;
	readonly height: number;
}

export interface CaveMapPage {
	readonly regionId: number;
	readonly floorIndex: number;
	readonly layer: number;
	readonly floorLabel: string;
	readonly columns: number;
	readonly rows: number;
	readonly width: number;
	readonly height: number;
	readonly left: number;
	readonly top: number;
	readonly image: string;
	readonly labels: readonly CaveMapLabel[];
	readonly signs: readonly CaveMapSign[];
}

export interface CaveMapCatalog {
	readonly imageRoot: string;
	readonly signIcon: string;
	readonly maps: readonly CaveMapPage[];
}

/*
================
caveMapCatalog

The published catalogue, refused whole when its shape is wrong.
================
*/
export function caveMapCatalog( value: unknown ): CaveMapCatalog {
	const data = value as Partial<CaveMapCatalog> & { format?: unknown; version?: unknown; };
	if (
		data?.format !== CATALOG_FORMAT || data.version !== CATALOG_VERSION || !Array.isArray( data.maps ) ||
		typeof data.imageRoot !== "string" || typeof data.signIcon !== "string"
	) throw Error( "Invalid dungeon map catalog" );
	for ( const map of data.maps ) {
		if (
			!Number.isInteger( map.regionId ) || !Number.isInteger( map.floorIndex ) ||
			!Number.isInteger( map.layer ) || !(map.columns > 0) ||
			!(map.rows > 0) || !(map.width > 0) || !(map.height > 0) || typeof map.image !== "string" ||
			!Array.isArray( map.labels ) || !Array.isArray( map.signs )
		) throw Error( "Invalid dungeon map page" );
	}
	return data as CaveMapCatalog;
}

/*
================
caveMapPage

The page of a floor, or undefined when the client has none.
================
*/
export function caveMapPage( catalog: CaveMapCatalog | undefined, regionId: number, floor: number | undefined ) {
	return catalog?.maps.find( map => map.regionId === regionId && map.floorIndex === floor );
}

/*
================
caveDungeonFloors

The pages of the dungeon a region belongs to, in floor order (issue
#3167's floor selector): Donwhang's cave is one region whose pages
differ by floorIndex; Ch'in Tomb spreads its floors over regions
2..7 of the dungeon mask, ordered by their authored layer. A region
in no known dungeon floors with its own pages alone.
================
*/
export function caveDungeonFloors(
	catalog: CaveMapCatalog | undefined,
	regionId: number
): readonly CaveMapPage[] {
	if ( !catalog ) return [];
	const dungeon = regionId & 0x7fff;
	if ( planeKindOf( regionId ) === "cave" && dungeon === 1 ) {
		return catalog.maps
			.filter( map => map.regionId === regionId )
			.sort( ( a, b ) => a.floorIndex - b.floorIndex );
	}
	if ( planeKindOf( regionId ) === "cave" && dungeon >= 2 && dungeon <= 7 ) {
		return catalog.maps
			.filter( map => (map.regionId & 0x7fff) >= 2 && (map.regionId & 0x7fff) <= 7 )
			.sort( ( a, b ) => a.layer - b.layer );
	}
	return catalog.maps
		.filter( map => map.regionId === regionId )
		.sort( ( a, b ) => a.floorIndex - b.floorIndex );
}

/*
================
caveDungeonName

The dungeon a page belongs to, the name both dungeon doors carry on
the world map: region 1 of the mask is Donwhang's cave and 2..7 are
the Ch'in Tomb. Null for a page in no known dungeon.
================
*/
export function caveDungeonName( regionId: number ): string | null {
	const dungeon = regionId & 0x7fff;
	if ( dungeon === 1 ) return "Donwhang Cave";
	if ( dungeon >= 2 && dungeon <= 7 ) return "Ch'in Tomb";
	return null;
}

/*
================
cavePageCaption

The later client's dungeon page title (issue #3238):
"Underground Level <n> of <dungeon>", its level the page's authored
layer. The title names the tomb "Tomb of Qin-Shi" where the doors
and zones say "Ch'in Tomb" (the reference page head). Null when the
page's dungeon is unknown.
================
*/
export function cavePageCaption( page: CaveMapPage ): string | null {
	const dungeon = page.regionId & 0x7fff;
	const name = dungeon === 1 ? "Donwhang Cave" : dungeon >= 2 && dungeon <= 7 ? "Tomb of Qin-Shi" : null;
	return name ? "Underground Level " + page.layer + " of " + name : null;
}

/*
================
cavePageCenter

The world position a page's centre shows, for viewing a dungeon the
player is not standing in (issue #3167): the inverse of caveMapView's
projection at the page's middle tile.
================
*/
export function cavePageCenter( page: CaveMapPage ): Pose {
	return {
		regionId: page.regionId,
		x: (page.left - CAVE_TILE_ORIGIN + page.columns / 2) * REGION_SIZE,
		y: 0,
		z: (page.top + 1 - CAVE_TILE_ORIGIN - page.rows / 2) * REGION_SIZE,
		angle: 0
	};
}

/*
================
caveMapImage

A page's one published image, so a closed window can demand it.
================
*/
export function caveMapImage( catalog: CaveMapCatalog, page: CaveMapPage ) {
	return catalog.imageRoot + page.image + ".png";
}

/*
================
axisOffset

Where a page axis starts on screen: the point centred, clamped so the view
stays on the page, or the whole page centred when it is smaller than the view.
================
*/
function axisOffset( view: number, size: number, point: number, pan: number ) {
	if ( size <= view ) return (view - size) / 2;
	return Math.min( 0, Math.max( view - size, view / 2 - point + pan ) );
}

export interface CaveMapInput {
	readonly pose: Pose;
	readonly center: Pose;
	readonly page: CaveMapPage;
	readonly catalog: CaveMapCatalog;
	readonly clip: UiRect;
	readonly pan: readonly [number, number];
}

/*
================
caveMapView

The page image, gate signs and the player's sign in paint order, its room
names at screen positions, and the pan after clamping.
================
*/
export function caveMapView( input: CaveMapInput ) {
	const { pose, center, page, catalog, clip, pan } = input;
	const unitX = page.width / page.columns, unitY = page.height / page.rows;
	const mapX = ( x: number ) => (x / REGION_SIZE + CAVE_TILE_ORIGIN - page.left) * unitX,
		mapY = ( z: number ) => (page.top + 1 - CAVE_TILE_ORIGIN - z / REGION_SIZE) * unitY;
	const offsetX = axisOffset( clip[2], page.width, mapX( center.x ), pan[0] ),
		offsetY = axisOffset( clip[3], page.height, mapY( center.z ), pan[1] );
	const left = clip[0] + offsetX, top = clip[1] + offsetY;
	const quads: UiQuad[] = [];
	const white = [ 1, 1, 1, 1 ] as const;
	quads.push( {
		texture: caveMapImage( catalog, page ),
		rect: [ left, top, page.width, page.height ],
		uv: [ 0, 0, 1, 1 ],
		color: white,
		clip
	} );
	const signs = page.signs.map( sign => ({
		name: sign.name,
		rect: [ left + sign.x, top + sign.y, sign.width, sign.height ] as UiRect
	}) );
	for ( const sign of signs ) {
		quads.push( {
			texture: catalog.imageRoot + catalog.signIcon + ".png",
			rect: sign.rect,
			uv: [ 0, 0, 1, 1 ],
			color: white,
			clip
		} );
	}
	quads.push( {
		texture: MAP_LOCAL_MARKER,
		rect: [
			left + mapX( pose.x ) - CAVE_MARKER_SIZE / 2,
			top + mapY( pose.z ) - CAVE_MARKER_SIZE / 2,
			CAVE_MARKER_SIZE,
			CAVE_MARKER_SIZE
		],
		uv: [ 0, 0, 1, 1 ],
		color: white,
		clip,
		rotation: minimapRotation( pose.angle )
	} );
	const labels = page.labels.map( label => ({
		text: label.text,
		x: left + label.x,
		y: top + label.y,
		font: label.font,
		color: [
			label.color[0] / COLOR_RANGE,
			label.color[1] / COLOR_RANGE,
			label.color[2] / COLOR_RANGE,
			1
		] as const
	}) );
	// The pan a clamped view really holds, so reversing a drag never unwinds
	// an overshoot first (579920 on the world map).
	const clampedPan: [number, number] = [
		page.width <= clip[2] ? 0 : offsetX - (clip[2] / 2 - mapX( center.x )),
		page.height <= clip[3] ? 0 : offsetY - (clip[3] / 2 - mapY( center.z ))
	];
	return { quads, labels, signs, pan: clampedPan };
}
