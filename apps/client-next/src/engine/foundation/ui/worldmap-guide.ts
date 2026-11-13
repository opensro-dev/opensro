/*
===========================================================================

worldmap-guide.ts - the M map guide's catalogue: its trees, links and overlay

The later client's guide (#3387-#3390) reads five tables the importer
(scripts/import_v1696_worldmap_guide.py) publishes as one catalogue:
map pages, the Field tree (area.txt), the Monster tree (monster.txt), each
hunting ground's tiles and the points a Field row links to. This module
decodes it and turns it into plain rows, view targets and quads; the map
tools own which group is open and the map HUD owns the view.

Native rules, read in the v1.696 client (research/notes/worldmap-renewal-
v1696.md): both trees iterate a std::map keyed by the row's order for
the player's race (0xe6e6c0), a sub row hangs under the last main row,
and a monster row reads "Lv<level> <name>" coloured by the level gap
(sub_e6cb90). A picked monster draws its hunting-ground tiles tinted
0x80e9c624 and wmap_monster_icon at its point (sub_e75d50).

Port-only, not native (the owner's choice, 2026-11-13): the instance
list puts a badge on its area row; native never reads it. Every row
stays open whatever the server's level cap (owner, 2026-11-13: "all
should be enabled, playable"), as the native trees show them.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
import { DONWHANG_CAVE_DUNGEON, TOMB_FIRST_DUNGEON } from "@/engine/foundation/ui/world-map";

export const WORLDMAP_GUIDE_CATALOG = "/assets/data/worldmap-guide.json";
const CATALOG_FORMAT = "opensro-worldmap-guide-v1";
const REGION_SIZE = 192;
const POSE_UNITS = 10;
// The guide's world page: map_manager.txt's "field" row, page id 0.
const FIELD_PAGE = "field";
// A sector of -1 marks a page-local point (sub_e75d50).
const PAGE_POINT = -1;
// A Field row's link kinds (area.txt column 9, 0xe6e170).
const LINK_PAGE = 0;
const LINK_LOCALINFO = 1;
const MONSTER_ICON_SIZE = 32;
// sub_e75d50's tint for the picked entry's tiles: half-alpha yellow.
const PICKED_TINT = 0x80e9c624;
// sub_e6cb90's sub row colours by L = monster level, P = player level.
const LEVEL_FAR_ABOVE = 6;
const LEVEL_ABOVE = 3;

export interface GuidePage {
	readonly origin: readonly [number, number];
	readonly tile: readonly [number, number];
	readonly perTile: readonly [number, number];
	readonly id: number;
}

export interface GuideArea {
	readonly name: string;
	readonly page: string;
	readonly main: boolean;
	readonly order: readonly number[];
	readonly linkKind: number;
	readonly link: number;
	readonly instances: readonly string[];
	readonly tier: number;
}

export interface GuideMonster {
	readonly name: string;
	readonly level: number | null;
	readonly page: string;
	readonly main: boolean;
	readonly order: readonly number[];
	readonly sector: readonly [number, number];
	readonly point: readonly [number, number];
	readonly tier: number;
	readonly tiles: readonly (readonly [number, number, string])[];
}

export interface GuideLocal {
	readonly page: number;
	readonly sector: readonly [number, number];
	readonly point: readonly [number, number];
}

export interface WorldMapGuideCatalog {
	readonly imageRoot: string;
	readonly pages: Readonly<Record<string, GuidePage>>;
	readonly areas: readonly GuideArea[];
	readonly monsters: readonly GuideMonster[];
	readonly localinfo: Readonly<Record<string, GuideLocal>>;
}

/*
================
GuideRowColor

A row's text: the gold group, white, the two reds above the player's
level and the blue below it.
================
*/
export type GuideRowColor = "group" | "leaf" | "high" | "far" | "low";

/*
================
GuideRow

One tree row: index is the row's place in the catalogue's list.
================
*/
export interface GuideRow {
	readonly index: number;
	readonly main: boolean;
	readonly label: string;
	readonly color: GuideRowColor;
	readonly badge?: boolean;
}

/*
================
GuideView

What the trees are built for: the player's race (picks the order
column), the player's level and the level filter's cap (undefined shows every row), and the open main row.
================
*/
export interface GuideView {
	readonly race: number;
	readonly playerLevel: number | undefined;
	readonly filterCap: number | undefined;
	readonly open: number | null;
}

/*
================
GuideTarget

Where a picked row takes the view: a pose on the world page, a point
on a town page, or a dungeon floor. Null when the port has no page.
================
*/
export type GuideTarget =
	| { readonly kind: "world"; readonly pose: Pose; }
	| { readonly kind: "page"; readonly page: number; readonly point: readonly [number, number]; }
	| { readonly kind: "dungeon"; readonly dungeon: number; readonly floor: number; }
	| null;

/*
================
GuideFrame

The world page's placement on screen (worldMapFrame's fields): its
world bounds, its size in pixels and its top left corner.
================
*/
export interface GuideFrame {
	readonly left: number;
	readonly right: number;
	readonly top: number;
	readonly bottom: number;
	readonly width: number;
	readonly height: number;
	readonly ox: number;
	readonly oy: number;
}

// ============================================================================
// Decoding
// ============================================================================

function record( value: unknown, what: string ): Record<string, unknown> {
	if ( !value || typeof value !== "object" || Array.isArray( value ) ) throw Error( "worldmap guide: bad " + what );
	return value as Record<string, unknown>;
}

function int( value: unknown, what: string ): number {
	if ( typeof value !== "number" || !Number.isInteger( value ) ) throw Error( "worldmap guide: bad " + what );
	return value;
}

function text( value: unknown, what: string ): string {
	if ( typeof value !== "string" ) throw Error( "worldmap guide: bad " + what );
	return value;
}

function pair( value: unknown, what: string ): readonly [number, number] {
	if ( !Array.isArray( value ) || value.length !== 2 ) throw Error( "worldmap guide: bad " + what );
	return [ int( value[0], what ), int( value[1], what ) ];
}

function order( value: unknown ): readonly number[] {
	if ( !Array.isArray( value ) || value.length !== 3 ) throw Error( "worldmap guide: bad order" );
	return value.map( entry => int( entry, "order" ) );
}

/*
================
worldMapGuideCatalog

The published catalogue, refused whole when its shape is wrong: a
half-read tree would hang rows under the wrong group.
================
*/
export function worldMapGuideCatalog( value: unknown ): WorldMapGuideCatalog {
	const root = record( value, "catalogue" );
	if ( root.format !== CATALOG_FORMAT ) throw Error( "worldmap guide: format is not " + CATALOG_FORMAT );
	const pages: Record<string, GuidePage> = {};
	for ( const [name, row] of Object.entries( record( root.pages, "pages" ) ) ) {
		const page = record( row, "page" );
		pages[name] = {
			origin: pair( page.origin, "page origin" ),
			tile: pair( page.tile, "page tile" ),
			perTile: pair( page.perTile, "page perTile" ),
			id: int( page.id, "page id" )
		};
	}
	if ( !Array.isArray( root.areas ) || !Array.isArray( root.monsters ) ) throw Error( "worldmap guide: bad trees" );
	const areas = root.areas.map( ( row ): GuideArea => {
		const area = record( row, "area" );
		if ( !Array.isArray( area.instances ) ) throw Error( "worldmap guide: bad instances" );
		return {
			name: text( area.name, "area name" ),
			page: text( area.page, "area page" ),
			main: area.main === true,
			order: order( area.order ),
			linkKind: int( area.linkKind, "link kind" ),
			link: int( area.link, "link" ),
			instances: area.instances.map( name => text( name, "instance" ) ),
			tier: int( area.tier, "area tier" )
		};
	} );
	const monsters = root.monsters.map( ( row ): GuideMonster => {
		const monster = record( row, "monster" );
		if ( !Array.isArray( monster.tiles ) ) throw Error( "worldmap guide: bad tiles" );
		return {
			name: text( monster.name, "monster name" ),
			level: monster.level === null ? null : int( monster.level, "level" ),
			page: text( monster.page, "monster page" ),
			main: monster.main === true,
			order: order( monster.order ),
			sector: pair( monster.sector, "sector" ),
			point: pair( monster.point, "point" ),
			tier: int( monster.tier, "monster tier" ),
			tiles: monster.tiles.map( ( tile ): readonly [number, number, string] => {
				if ( !Array.isArray( tile ) || tile.length !== 3 ) throw Error( "worldmap guide: bad tile" );
				return [ int( tile[0], "tile" ), int( tile[1], "tile" ), text( tile[2], "tile path" ) ];
			} )
		};
	} );
	const localinfo: Record<string, GuideLocal> = {};
	for ( const [id, row] of Object.entries( record( root.localinfo, "localinfo" ) ) ) {
		const local = record( row, "localinfo row" );
		localinfo[id] = {
			page: int( local.page, "local page" ),
			sector: pair( local.sector, "local sector" ),
			point: pair( local.point, "local point" )
		};
	}
	return { imageRoot: text( root.imageRoot, "image root" ), pages, areas, monsters, localinfo };
}

// ============================================================================
// Trees
// ============================================================================

/*
================
guideRace

The order column for a character's country byte: 1 (European) reads
the second column, every other value the first (0xe6e6c0 keys the map
by the race byte; the third column repeats the first in every row).
================
*/
export function guideRace( country: number | undefined ): number {
	return country === 1 ? 1 : 0;
}

/*
================
guideLevelColor

sub_e6cb90's colour for a monster row: red more than six levels above
the player, light red four to six above, white within three, blue
below. White while the player's level is unknown.
================
*/
export function guideLevelColor( level: number | null, player: number | undefined ): GuideRowColor {
	if ( level === null || player === undefined ) return "leaf";
	if ( level > player + LEVEL_FAR_ABOVE ) return "far";
	if ( level > player + LEVEL_ABOVE ) return "high";
	if ( level >= player ) return "leaf";
	return "low";
}

/*
================
guideGroups

The rows in the race's order, each sub row under the main row before
it (0xe6cb90 parents a sub slot on the last main slot). Rows before the
first main row have no group and are dropped, as natively they have no
parent slot. The sort is stable, so equal keys keep the file's order
where a std::map would keep only the first; no shipped key repeats.
================
*/
export function guideGroups(
	rows: readonly { readonly main: boolean; readonly order: readonly number[]; }[],
	race: number
): { main: number; subs: number[]; }[] {
	const column = race === 1 ? 1 : 0;
	const sorted = rows.map( ( _, index ) => index ).sort( ( a, b ) =>
		rows[a]!.order[column]! - rows[b]!.order[column]!
	);
	const groups: { main: number; subs: number[]; }[] = [];
	for ( const index of sorted ) {
		if ( rows[index]!.main ) groups.push( { main: index, subs: [] } );
		else groups[groups.length - 1]?.subs.push( index );
	}
	return groups;
}

/*
================
guideMonsterRows

The Monster tab: every main row, and the open one's monsters as
"Lv<level> <name>". The level filter hides monsters above its cap.
================
*/
export function guideMonsterRows( catalog: WorldMapGuideCatalog, view: GuideView ): GuideRow[] {
	const out: GuideRow[] = [];
	for ( const group of guideGroups( catalog.monsters, view.race ) ) {
		const main = catalog.monsters[group.main]!;
		out.push( { index: group.main, main: true, label: main.name, color: "group" } );
		if ( group.main !== view.open ) continue;
		for ( const index of group.subs ) {
			const monster = catalog.monsters[index]!;
			if ( view.filterCap !== undefined && monster.level !== null && monster.level > view.filterCap ) continue;
			out.push( {
				index,
				main: false,
				label: "Lv" + (monster.level ?? "?") + " " + monster.name,
				color: guideLevelColor( monster.level, view.playerLevel )
			} );
		}
	}
	return out;
}

/*
================
guideAreaRows

The Field tab: Town, Field and Dungeon, and the open one's places. A
place with instance worlds carries the badge (port-only).
================
*/
export function guideAreaRows( catalog: WorldMapGuideCatalog, view: GuideView ): GuideRow[] {
	const out: GuideRow[] = [];
	for ( const group of guideGroups( catalog.areas, view.race ) ) {
		const main = catalog.areas[group.main]!;
		out.push( { index: group.main, main: true, label: main.name, color: "group" } );
		if ( group.main !== view.open ) continue;
		for ( const index of group.subs ) {
			const area = catalog.areas[index]!;
			out.push( {
				index,
				main: false,
				label: area.name,
				color: "leaf",
				badge: area.instances.length > 0
			} );
		}
	}
	return out;
}

// ============================================================================
// Targets
// ============================================================================

/*
================
worldPose

A world position (x east, z north, in world units) as a pose.
================
*/
function worldPose( x: number, z: number ): Pose {
	const rx = Math.floor( x / REGION_SIZE ), rz = Math.floor( z / REGION_SIZE );
	return {
		regionId: (rz << 8) | rx,
		x: (x - rx * REGION_SIZE) * POSE_UNITS,
		y: 0,
		z: (z - rz * REGION_SIZE) * POSE_UNITS,
		angle: 0
	};
}

/*
================
tileWorld

Where a sector's tile starts on the world page, in world units:
sub_e75d50 places it at ((sx - ox) / perTile) * tile pixels, integer
division, so a sector inside a tile snaps to the tile's corner; y
counts down from the origin's top row. Also the world units per pixel.
================
*/
function tileWorld( page: GuidePage, sx: number, sy: number ): { x: number; z: number; scale: number; } {
	const [ox, oy] = page.origin, [cx, cy] = page.perTile;
	const tx = Math.trunc( (sx - ox) / cx ) * cx, ty = Math.trunc( (oy - sy) / cy ) * cy;
	return {
		x: (ox + tx) * REGION_SIZE,
		z: (oy + 1 - ty) * REGION_SIZE,
		scale: (cx * REGION_SIZE) / page.tile[0]
	};
}

/*
================
pointPose

A sector and a pixel point within its tile, on the world page.
================
*/
function pointPose( page: GuidePage, sector: readonly [number, number], point: readonly [number, number] ): Pose {
	const at = tileWorld( page, sector[0], sector[1] );
	return worldPose( at.x + point[0] * at.scale, at.z - point[1] * at.scale );
}

/*
================
dungeonTarget

A later-client dungeon page the port has floors for: "jinsi3" is the
tomb's third floor. Null for the pages it lacks (temple, jupiter...).
================
*/
function dungeonTarget( name: string ): GuideTarget {
	// The page name's stem and the dungeon it is; the trailing number is
	// the floor. A table built per call: shared module state may only be
	// a literal, and the dungeon ids are world-map's.
	const pages = [ [ "dunhuang", DONWHANG_CAVE_DUNGEON ], [ "jinsi", TOMB_FIRST_DUNGEON ] ] as const;
	for ( const [stem, dungeon] of pages ) {
		const floor = Number( name.slice( stem.length ) );
		if ( name.startsWith( stem ) && Number.isInteger( floor ) && floor >= 1 ) {
			return { kind: "dungeon", dungeon, floor: floor - 1 };
		}
	}
	return null;
}

/*
================
guideMonsterTarget

A picked monster: its icon's point on the world page, or its dungeon
floor. A page point (sector -1) off the world page has no port page.
================
*/
export function guideMonsterTarget( catalog: WorldMapGuideCatalog, index: number ): GuideTarget {
	const monster = catalog.monsters[index];
	if ( !monster || monster.main ) return null;
	if ( monster.page !== FIELD_PAGE ) return dungeonTarget( monster.page );
	const page = catalog.pages[FIELD_PAGE];
	if ( !page || monster.sector[0] === PAGE_POINT ) return null;
	return { kind: "world", pose: pointPose( page, monster.sector, monster.point ) };
}

/*
================
guideAreaTarget

A picked place (0xe6e170): kind 1 links a worldmap_localinfo row, a
point on the world page or on a town page; kind 0 names a map page,
which the port has only for the two dungeons' floors.
================
*/
export function guideAreaTarget( catalog: WorldMapGuideCatalog, index: number ): GuideTarget {
	const area = catalog.areas[index];
	if ( !area || area.main ) return null;
	if ( area.linkKind === LINK_PAGE ) return dungeonTarget( area.page );
	if ( area.linkKind !== LINK_LOCALINFO ) return null;
	const local = catalog.localinfo[String( area.link )];
	if ( !local ) return null;
	if ( local.sector[0] === PAGE_POINT ) return { kind: "page", page: local.page, point: local.point };
	const page = catalog.pages[FIELD_PAGE];
	return page ? { kind: "world", pose: pointPose( page, local.sector, local.point ) } : null;
}

// ============================================================================
// Overlay
// ============================================================================

function tint( argb: number ): UiQuad["color"] {
	return [ ((argb >>> 16) & 255) / 255, ((argb >>> 8) & 255) / 255, (argb & 255) / 255, (argb >>> 24) / 255 ];
}

/*
================
guideOverlay

The picked monster on the world page (sub_e75d50): every hunting-ground
tile, a white mask the importer cut from the page's colour-keyed tile,
tinted half-alpha yellow; then wmap_monster_icon at its point.
Returns the quads, the icon's rect (the row's hover names it) and the
images to demand: the caller draws a quad only once its image is in.
================
*/
export function guideOverlay(
	catalog: WorldMapGuideCatalog,
	index: number,
	frame: GuideFrame,
	clip: UiRect
): { quads: UiQuad[]; icon: UiRect | null; paths: string[]; } {
	const quads: UiQuad[] = [];
	const paths = () => quads.map( quad => quad.texture );
	const monster = catalog.monsters[index], page = catalog.pages[FIELD_PAGE];
	if ( !monster || monster.main || monster.page !== FIELD_PAGE || !page ) return { quads, icon: null, paths: [] };
	const sx = frame.width / (frame.right - frame.left), sy = frame.height / (frame.top - frame.bottom);
	const screen = ( x: number, z: number ) => [ frame.ox + (x - frame.left) * sx, frame.oy + (frame.top - z) * sy ];
	const color = tint( PICKED_TINT );
	for ( const [tx, ty, path] of monster.tiles ) {
		const at = tileWorld( page, tx, ty );
		const [x, y] = screen( at.x, at.z );
		const w = page.tile[0] * at.scale * sx, h = page.tile[1] * at.scale * sy;
		// Edges snap to whole pixels from the unrounded corners, so
		// neighbouring tiles share an edge: a fractional rect overlaps
		// or gaps its neighbour and the half-alpha tint shows the seam.
		const left = Math.round( x! ), top = Math.round( y! );
		quads.push( {
			texture: catalog.imageRoot + path + ".png",
			rect: [ left, top, Math.round( x! + w ) - left, Math.round( y! + h ) - top ],
			uv: [ 0, 0, 1, 1 ],
			color,
			clip
		} );
	}
	if ( monster.sector[0] === PAGE_POINT ) return { quads, icon: null, paths: paths() };
	const pose = pointPose( page, monster.sector, monster.point );
	const [x, y] = screen(
		(pose.regionId & 255) * REGION_SIZE + pose.x / POSE_UNITS,
		(pose.regionId >>> 8) * REGION_SIZE + pose.z / POSE_UNITS
	);
	// The icon's corner stands on the point: sub_e75d50 adds it with
	// anchor flags 1, and sub_f42820 shifts only with 0x10 (centre) or
	// 4 / 8 (right / bottom) set.
	const icon: UiRect = [ Math.round( x! ), Math.round( y! ), MONSTER_ICON_SIZE, MONSTER_ICON_SIZE ];
	quads.push( {
		texture: catalog.imageRoot + "worldmap/wmap_monster_icon.png",
		rect: icon,
		uv: [ 0, 0, 1, 1 ],
		color: [ 1, 1, 1, 1 ],
		clip
	} );
	return { quads, icon, paths: paths() };
}
