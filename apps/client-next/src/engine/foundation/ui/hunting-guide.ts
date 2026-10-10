/*
===========================================================================

hunting-guide.ts - bounded atlas decoding and native portrait marker layout

Port-only approximate guidance from static outdoor anchors.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { worldMapFrame } from "./world-map";
const MAX_MONSTERS = 4096, MAX_POINTS = 65536, MAX_NAME = 128;
const REGION_SIZE = 192, LOCAL_SCALE = 10, MAX_LOCAL_POSITION = 65536, MAX_HOVER_NAMES = 5;
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
	x: number;
	y: number;
	count: number;
	monsters: Map<number, { monster: HuntingMonster; count: number; }>;
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
	const small = clip[2] < 350, cellSize = small ? 112 : 96;
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
				const key = Math.floor( mx / cellSize ) + ":" + Math.floor( my / cellSize );
				const cell = cells.get( key ) ?? { x: 0, y: 0, count: 0, monsters: new Map() };
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
	// Prioritize beginner groups when a dense viewport cannot fit everything.
	const groups = [ ...cells.entries() ].sort( ( a, b ) =>
		Math.min( ...[ ...a[1].monsters.values() ].map( r => r.monster.level ) ) -
			Math.min( ...[ ...b[1].monsters.values() ].map( r => r.monster.level ) ) || a[0].localeCompare( b[0] )
	);
	const limit = small ? 4 : 18;
	for ( const [key, cell] of groups ) {
		if ( controls.length >= limit ) break;
		const rows = [ ...cell.monsters.values() ].map( entry => entry.monster ).sort( ( a, b ) =>
			a.level - b.level || a.refObjId - b.refObjId
		);
		const representative = [ ...cell.monsters.values() ].sort( ( a, b ) =>
			b.count - a.count || a.monster.level - b.monster.level
		)[0]!.monster;
		const centerX = cell.x / cell.count, centerY = cell.y / cell.count;
		const rect = [ [ 0, 0 ], [ -24, 0 ], [ 24, 0 ], [ 0, -24 ], [ 0, 24 ] ].map( ( [dx, dy] ) =>
			[ Math.round( centerX - 30 + dx! ), Math.round( centerY - 29 + dy! ), 60, 58 ] as UiRect
		).find( r =>
			r[0] >= clip[0] && r[1] >= clip[1] && r[0] + r[2] <= clip[0] + clip[2] &&
			r[1] + r[3] <= clip[1] + clip[3] && !occupied.some( other => overlaps( r, other ) )
		);
		if ( !rect ) continue;
		occupied.push( rect );
		const image = images.get( representative.refObjId ) ?? HUNTING_MONSTER_SIGN;
		paths.add( image );
		paths.add( PORTRAIT_FRAME );
		paths.add( HUNTING_MONSTER_SIGN );
		// The native slot tile has an opaque centre: paint it before the inset portrait.
		quads.push( {
			rect: [ rect[0] + 8, rect[1], 44, 44 ],
			clip,
			texture: PORTRAIT_FRAME,
			uv: [ 0, 0, 1, 1 ],
			color: [ 1, 1, 1, 1 ]
		}, {
			rect: [ rect[0] + 10, rect[1] + 2, 40, 40 ],
			clip,
			texture: image,
			uv: [ 0, 0, 1, 1 ],
			color: [ 1, 1, 1, 1 ]
		}, {
			rect: [ rect[0], rect[1] + 43, 60, 15 ],
			clip,
			texture: "",
			uv: [ 0, 0, 1, 1 ],
			color: [ .07, .055, .035, .86 ]
		} );
		const low = rows[0]!.level, high = rows[rows.length - 1]!.level;
		labels.push( {
			value: `Lv ${low === high ? low : low + "-" + high}`,
			rect: [ rect[0], rect[1] + 43, 60, 15 ],
			color: [ .96, .83, .55, 1 ]
		} );
		if ( rows.length > 1 ) {
			labels.push( {
				value: "+" + (rows.length - 1),
				rect: [ rect[0] + 38, rect[1] + 27, 20, 14 ],
				color: [ 1, .93, .68, 1 ]
			} );
		}
		const names = [
			`${representative.name} - Lv. ${representative.level}`,
			...rows.filter( row => row !== representative ).slice( 0, MAX_HOVER_NAMES - 1 ).map( row =>
				`${row.name} - Lv. ${row.level}`
			)
		];
		if ( rows.length > MAX_HOVER_NAMES ) names.push( `+${rows.length - MAX_HOVER_NAMES} more species` );
		names.push( "Approximate hunting area" );
		controls.push( {
			id: HUNTING_AREA_PREFIX + key,
			kind: "region",
			draggable: true,
			rect,
			label: names.join( "\n" ),
			helpText: names.join( "\n" )
		} );
	}
	return { quads, controls, labels, paths: [ ...paths ] };
}
