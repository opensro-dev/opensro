/*
===========================================================================

hunting-guide.ts - bounded atlas decoding and native portrait marker layout

Port-only approximate guidance from static outdoor anchors.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { worldMapFrame } from "./world-map";
const MAX_MONSTERS = 4096, MAX_POINTS = 65536, MAX_NAME = 128;
const REGION_SIZE = 192, LOCAL_SCALE = 10, MAX_LOCAL_POSITION = 65536;
const ANCHOR_CELL_SIZE = 64, GROUP_SPACING = 132, SMALL_GROUP_SPACING = 144;
const MAX_GROUPS = 12, MAX_SMALL_GROUPS = 4, MARKER_WIDTH = 52, MARKER_HEIGHT = 48;
const PORTRAIT_SIZE = 34, LABEL_HEIGHT = 14, PLACEMENT_STEP = 16, PLACEMENT_REACH = 48;
export const HUNTING_AREA_PREFIX = "map-hunting-area:";
/*
================
HuntingMonster
================
*/
export interface HuntingMonster {
	readonly refObjId: number;
	readonly name: string;
	readonly nameKey: string;
	readonly level: number;
}
/*
================
HuntingPoint
================
*/
interface HuntingPoint {
	readonly monster: HuntingMonster;
	readonly x: number;
	readonly z: number;
}
/*
================
HuntingCatalogue
================
*/
export interface HuntingCatalogue {
	readonly monsters: readonly HuntingMonster[];
	readonly regions: ReadonlyMap<number, readonly HuntingPoint[]>;
}
/*
================
decodeHuntingGuide

Bound total locations as well as species; reject dungeon/nonfinite positions.
================
*/
export function decodeHuntingGuide( value: unknown ): HuntingCatalogue {
	const data = value as { format?: unknown; version?: unknown; rows?: unknown; };
	if (
		!data || data.format !== "sro-hunting-guide" || data.version !== 1 ||
		!Array.isArray( data.rows ) || data.rows.length > MAX_MONSTERS
	) throw Error( "Invalid hunting guide" );
	const monsters: HuntingMonster[] = [], regions = new Map<number, HuntingPoint[]>(), ids = new Set<number>();
	let count = 0;
	for ( const value of data.rows ) {
		const row = value as HuntingMonster & { points: { regionId: number; x: number; z: number; }[]; };
		if (
			!row || !Number.isInteger( row.refObjId ) || row.refObjId <= 0 || ids.has( row.refObjId ) ||
			typeof row.name !== "string" || !row.name.trim() || row.name.length > MAX_NAME ||
			typeof row.nameKey !== "string" || row.nameKey.length > MAX_NAME ||
			!Number.isInteger( row.level ) || row.level < 1 || row.level > 255 || !Array.isArray( row.points ) ||
			(count += row.points.length) > MAX_POINTS
		) throw Error( "Invalid hunting guide row" );
		const monster: HuntingMonster = {
			refObjId: row.refObjId,
			name: row.name,
			nameKey: row.nameKey,
			level: row.level
		};
		ids.add( row.refObjId );
		monsters.push( monster );
		for ( const p of row.points ) {
			if (
				!p || !Number.isInteger( p.regionId ) || p.regionId < 0 || p.regionId >= 0x8000 ||
				!Number.isFinite( p.x ) || !Number.isFinite( p.z ) ||
				Math.abs( p.x ) > MAX_LOCAL_POSITION || Math.abs( p.z ) > MAX_LOCAL_POSITION
			) {
				throw Error( "Invalid hunting guide location" );
			}
			const x = (p.regionId & 255) * REGION_SIZE + p.x / LOCAL_SCALE,
				z = (p.regionId >>> 8) * REGION_SIZE + p.z / LOCAL_SCALE,
				rx = Math.floor( x / REGION_SIZE ),
				rz = Math.floor( z / REGION_SIZE );
			if ( rx < 0 || rx > 255 || rz < 0 || rz > 127 ) throw Error( "Invalid hunting guide region" );
			const key = rz * 256 + rx, points = regions.get( key ) ?? [];
			points.push( { monster, x, z } );
			regions.set( key, points );
		}
	}
	return { monsters, regions };
}

/*
================
decodeHuntingPortraits

The optional, locally generated art index cannot supply arbitrary URLs.
================
*/
export function decodeHuntingPortraits( value: unknown ): ReadonlyMap<number, string> {
	const data = value as { format?: unknown; version?: unknown; rows?: unknown; };
	if (
		!data || data.format !== "sro-hunting-portraits" || data.version !== 1 || !Array.isArray( data.rows ) ||
		data.rows.length > 4096
	) {
		throw Error( "Invalid hunting portraits" );
	}
	const images = new Map<number, string>();
	for ( const row of data.rows ) {
		if (
			!Array.isArray( row ) || !Number.isInteger( row[0] ) || row[0] <= 0 || images.has( row[0] ) ||
			typeof row[1] !== "string" || !/^\/assets\/npc\/hunting-portraits\/[a-f0-9]{64}\.png$/.test( row[1] )
		) {
			throw Error( "Invalid hunting portrait reference" );
		}
		images.set( row[0], row[1] );
	}
	return images;
}

export const HUNTING_PORTRAITS = "/assets/npc/hunting-portraits/manifest.json";
export const HUNTING_MONSTER_SIGN = "/assets/images/Media_extracted/interface/minimap/mm_sign_monster.png";
const PORTRAIT_FRAME = "/assets/images/Media_extracted/interface/quick_slot/qsl_hriz01_slot_tile.png";

/*
================
HuntingProjection
================
*/
export interface HuntingProjection {
	readonly quads: readonly UiQuad[];
	readonly controls: readonly UiControl[];
	readonly paths: readonly string[];
	readonly labels: readonly { value: string; rect: UiRect; color: UiQuad["color"]; }[];
}

/*
================
HuntingCell
================
*/
interface HuntingCell {
	key: string;
	x: number;
	y: number;
	count: number;
	monsters: Map<number, { monster: HuntingMonster; count: number; }>;
}

/*
================
mergeHuntingCells

Keep every species when nearby areas share a marker, including groups that
cannot fit between native landmarks. Coordinates remain weighted anchors.
================
*/
function mergeHuntingCells( target: HuntingCell, source: HuntingCell ) {
	target.x += source.x;
	target.y += source.y;
	target.count += source.count;
	for ( const [id, entry] of source.monsters ) {
		const previous = target.monsters.get( id );
		target.monsters.set( id, { monster: entry.monster, count: entry.count + (previous?.count ?? 0) } );
	}
}

/*
================
huntingCellDistance
================
*/
function huntingCellDistance( a: HuntingCell, b: HuntingCell ) {
	return Math.hypot( a.x / a.count - b.x / b.count, a.y / a.count - b.y / b.count );
}

/*
================
groupHuntingCells

Merge the closest neighbours instead of displaying one repeated portrait in
every grid cell. The small seed grid only bounds work; it is never painted.
================
*/
function groupHuntingCells( cells: HuntingCell[], small: boolean ) {
	const spacing = small ? SMALL_GROUP_SPACING : GROUP_SPACING, limit = small ? MAX_SMALL_GROUPS : MAX_GROUPS;
	while ( cells.length > 1 ) {
		let first = 0, second = 1, nearest = Infinity;
		for ( let a = 0; a < cells.length; a++ ) {
			for ( let b = a + 1; b < cells.length; b++ ) {
				const distance = huntingCellDistance( cells[a]!, cells[b]! );
				if ( distance >= nearest ) continue;
				first = a;
				second = b;
				nearest = distance;
			}
		}
		if ( nearest >= spacing && cells.length <= limit ) break;
		mergeHuntingCells( cells[first]!, cells[second]! );
		cells.splice( second, 1 );
	}
	return cells;
}

/*
================
huntingMarkerRect

Prefer the area's centre, then the nearest clear space. Edge groups remain
inside the map and native names, landmarks and movement controls stay clear.
================
*/
function huntingMarkerRect( cell: HuntingCell, clip: UiRect, occupied: readonly UiRect[] ): UiRect | undefined {
	const candidates: { rect: UiRect; distance: number; }[] = [];
	for ( let dy = -PLACEMENT_REACH; dy <= PLACEMENT_REACH; dy += PLACEMENT_STEP ) {
		for ( let dx = -PLACEMENT_REACH; dx <= PLACEMENT_REACH; dx += PLACEMENT_STEP ) {
			const x = Math.round( cell.x / cell.count - MARKER_WIDTH / 2 + dx ),
				y = Math.round( cell.y / cell.count - MARKER_HEIGHT / 2 + dy );
			const rect: UiRect = [
				Math.max( clip[0], Math.min( clip[0] + clip[2] - MARKER_WIDTH, x ) ),
				Math.max( clip[1], Math.min( clip[1] + clip[3] - MARKER_HEIGHT, y ) ),
				MARKER_WIDTH,
				MARKER_HEIGHT
			];
			if ( occupied.some( other => overlaps( rect, other ) ) ) continue;
			candidates.push( { rect, distance: Math.hypot( dx, dy ) } );
		}
	}
	return candidates.sort( ( a, b ) => a.distance - b.distance )[0]?.rect;
}

/*
================
overlaps
================
*/
function overlaps( a: UiRect, b: UiRect, gap = 4 ) {
	return a[0] < b[0] + b[2] + gap && a[0] + a[2] + gap > b[0] && a[1] < b[1] + b[3] + gap && a[1] + a[3] + gap > b[1];
}

/*
================
projectHuntingGuide

Group nearby static anchors at the displayed scale. One representative per
group and bounded spacing leave native landmarks legible; the range and hover
describe all species in that group. Counts are never presented as live mobs.
================
*/
export function projectHuntingGuide(
	catalogue: HuntingCatalogue,
	images: ReadonlyMap<number, string>,
	f: ReturnType<typeof worldMapFrame>,
	clip: UiRect,
	reserved: readonly UiRect[] = []
): HuntingProjection {
	const small = clip[2] < 350;
	const cells = new Map<string, HuntingCell>();
	const scaleX = f.width / (f.right - f.left), scaleY = f.height / (f.top - f.bottom);
	const rx0 = Math.max( 0, Math.floor( (f.left + (clip[0] - f.ox) / scaleX) / REGION_SIZE ) ),
		rx1 = Math.min( 255, Math.floor( (f.left + (clip[0] + clip[2] - f.ox) / scaleX) / REGION_SIZE ) ),
		rz0 = Math.max( 0, Math.floor( (f.top - (clip[1] + clip[3] - f.oy) / scaleY) / REGION_SIZE ) ),
		rz1 = Math.min( 127, Math.floor( (f.top - (clip[1] - f.oy) / scaleY) / REGION_SIZE ) );
	for ( let rz = rz0; rz <= rz1; rz++ ) {
		for ( let rx = rx0; rx <= rx1; rx++ ) {
			for ( const point of catalogue.regions.get( rz * 256 + rx ) ?? [] ) {
				const mx = (point.x - f.left) * scaleX, my = (f.top - point.z) * scaleY, x = f.ox + mx, y = f.oy + my;
				if (
					mx < 0 || my < 0 || mx >= f.width || my >= f.height || x < clip[0] || y < clip[1] ||
					x >= clip[0] + clip[2] || y >= clip[1] + clip[3]
				) continue;
				const key = Math.floor( mx / ANCHOR_CELL_SIZE ) + ":" + Math.floor( my / ANCHOR_CELL_SIZE );
				const cell = cells.get( key ) ?? { key, x: 0, y: 0, count: 0, monsters: new Map() };
				cell.x += x;
				cell.y += y;
				cell.count++;
				const entry = cell.monsters.get( point.monster.refObjId ) ?? { monster: point.monster, count: 0 };
				entry.count++;
				cell.monsters.set( point.monster.refObjId, entry );
				cells.set( key, cell );
			}
		}
	}
	const quads: UiQuad[] = [], controls: UiControl[] = [], paths = new Set<string>();
	const labels: { value: string; rect: UiRect; color: UiQuad["color"]; }[] = [];
	const occupied = [ ...reserved ];
	const groups = groupHuntingCells( [ ...cells.values() ].sort( ( a, b ) => a.key.localeCompare( b.key ) ), small );
	const placed: { cell: HuntingCell; rect: UiRect; }[] = [], unplaced: HuntingCell[] = [];
	for ( const cell of groups ) {
		const rect = huntingMarkerRect( cell, clip, occupied );
		if ( !rect ) {
			unplaced.push( cell );
			continue;
		}
		occupied.push( rect );
		placed.push( { cell, rect } );
	}
	for ( const cell of unplaced ) {
		const nearest = [ ...placed ].sort( ( a, b ) =>
			huntingCellDistance( a.cell, cell ) - huntingCellDistance( b.cell, cell )
		)[0];
		if ( nearest ) {
			mergeHuntingCells( nearest.cell, cell );
		}
	}
	const portraits = new Map<number, number>();
	for ( const { cell, rect } of placed ) {
		const rows = [ ...cell.monsters.values() ].map( entry => entry.monster ).sort( ( a, b ) =>
			a.level - b.level || a.refObjId - b.refObjId
		);
		const representative = [ ...cell.monsters.values() ].sort( ( a, b ) =>
			b.count / (1 + (portraits.get( b.monster.refObjId ) ?? 0)) -
				a.count / (1 + (portraits.get( a.monster.refObjId ) ?? 0)) || a.monster.level - b.monster.level
		)[0]!.monster;
		portraits.set( representative.refObjId, (portraits.get( representative.refObjId ) ?? 0) + 1 );
		const image = images.get( representative.refObjId ) ?? HUNTING_MONSTER_SIGN;
		paths.add( image );
		paths.add( PORTRAIT_FRAME );
		paths.add( HUNTING_MONSTER_SIGN );
		// The native slot tile has an opaque centre: paint it before the inset portrait.
		quads.push( {
			rect: [ rect[0] + 9, rect[1], PORTRAIT_SIZE, PORTRAIT_SIZE ],
			clip,
			texture: PORTRAIT_FRAME,
			uv: [ 0, 0, 1, 1 ],
			color: [ 1, 1, 1, 1 ]
		}, {
			rect: [ rect[0] + 11, rect[1] + 2, PORTRAIT_SIZE - 4, PORTRAIT_SIZE - 4 ],
			clip,
			texture: image,
			uv: [ 0, 0, 1, 1 ],
			color: [ 1, 1, 1, 1 ]
		}, {
			rect: [ rect[0], rect[1] + PORTRAIT_SIZE, MARKER_WIDTH, LABEL_HEIGHT ],
			clip,
			texture: "",
			uv: [ 0, 0, 1, 1 ],
			color: [ .07, .055, .035, .72 ]
		} );
		const low = rows[0]!.level, high = rows[rows.length - 1]!.level;
		labels.push( {
			value: `Lv ${low === high ? low : low + "-" + high}`,
			rect: [ rect[0], rect[1] + PORTRAIT_SIZE, MARKER_WIDTH, LABEL_HEIGHT ],
			color: [ .96, .83, .55, 1 ]
		} );
		if ( rows.length > 1 ) {
			const badge: UiRect = [ rect[0] + 35, rect[1] + 21, 17, 13 ];
			quads.push( { rect: badge, clip, texture: "", uv: [ 0, 0, 1, 1 ], color: [ .07, .055, .035, .9 ] } );
			labels.push( {
				value: String( rows.length ),
				rect: badge,
				color: [ 1, .93, .68, 1 ]
			} );
		}
		const names = [
			`Hunting area - ${rows.length} species`,
			...rows.map( row =>
				`${row.name} - Lv. ${row.level}`
			),
			"Approximate locations"
		];
		controls.push( {
			id: HUNTING_AREA_PREFIX + cell.key,
			kind: "region",
			draggable: true,
			rect,
			label: names.join( "\n" ),
			helpText: names.join( "\n" )
		} );
	}
	return { quads, controls, labels, paths: [ ...paths ] };
}
