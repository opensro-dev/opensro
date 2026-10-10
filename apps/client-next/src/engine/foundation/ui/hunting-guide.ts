/*
===========================================================================

hunting-guide.ts - bounded public atlas decoding and approximate map areas

Port-only, not native. Cells are visual guidance from static anchors, not
spawn-radius, roaming, density or availability claims. Existing map art and
marker passes remain the owners of the map's native presentation.

===========================================================================
*/

import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { worldMapFrame } from "./world-map";

const MAX_MONSTERS = 4096;
const MAX_POINTS = 65536;
const MAX_NAME = 128;
const REGION_SIZE = 192;
const LOCAL_SCALE = 10;
const MAX_LOCAL_POSITION = 65536;
const CELL_SIZE = 32;
const CELL_INSET = 4;
const MAX_HOVER_NAMES = 5;

export const HUNTING_AREA_PREFIX = "map-hunting-area:";
export const HUNTING_GUIDE_BAR_HEIGHT = 92;
/*
================
huntingLevelColors

Fresh immutable presentation data; pure shared modules own no array state.
================
*/
export function huntingLevelColors() {
	return [
		{ label: "1–20", max: 20, color: [ .24, .88, .48, 1 ] },
		{ label: "21–40", max: 40, color: [ .98, .78, .28, 1 ] },
		{ label: "41–60", max: 60, color: [ .32, .68, 1, 1 ] },
		{ label: "61+", max: 255, color: [ .98, .42, .58, 1 ] }
	] as const;
}

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

Region index includes every anchor for a species, unlike the NPC lookup.
================
*/
export interface HuntingCatalogue {
	readonly monsters: readonly HuntingMonster[];
	readonly regions: ReadonlyMap<number, readonly HuntingPoint[]>;
}

/*
================
HuntingFilter
================
*/
export interface HuntingFilter {
	readonly search: string;
	readonly min: number;
	readonly max: number;
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
HuntingProjection
================
*/
export interface HuntingProjection {
	readonly quads: readonly UiQuad[];
	readonly controls: readonly UiControl[];
	readonly matches: number;
}

/*
================
HuntingCell
================
*/
interface HuntingCell {
	x: number;
	y: number;
	monsters: Map<number, HuntingMonster>;
}

/*
================
projectHuntingGuide

Query visible regions, then aggregate into page-aligned 32px cells. No
persistent labels crowd the art; hover shows the species in each cell.
================
*/
export function projectHuntingGuide(
	catalogue: HuntingCatalogue,
	filter: HuntingFilter,
	f: ReturnType<typeof worldMapFrame>,
	clip: UiRect
): HuntingProjection {
	const search = filter.search.trim().toLowerCase();
	const admitted = new Set(
		catalogue.monsters.filter( row =>
			row.level >= filter.min && row.level <= filter.max && row.name.toLowerCase().includes( search )
		).map( row => row.refObjId )
	);
	const cells = new Map<string, HuntingCell>();
	const scaleX = f.width / (f.right - f.left), scaleY = f.height / (f.top - f.bottom);
	// Include a cell margin so an anchor just outside the clip still contributes
	// to its partially visible area. Index normalized positions across regions.
	const rx0 = Math.max( 0, Math.floor( (f.left + (clip[0] - f.ox - CELL_SIZE) / scaleX) / REGION_SIZE ) ),
		rx1 = Math.min( 255, Math.floor( (f.left + (clip[0] + clip[2] - f.ox + CELL_SIZE) / scaleX) / REGION_SIZE ) ),
		rz0 = Math.max( 0, Math.floor( (f.top - (clip[1] + clip[3] - f.oy + CELL_SIZE) / scaleY) / REGION_SIZE ) ),
		rz1 = Math.min( 127, Math.floor( (f.top - (clip[1] - f.oy - CELL_SIZE) / scaleY) / REGION_SIZE ) );
	for ( let rz = rz0; rz <= rz1; rz++ ) {
		for ( let rx = rx0; rx <= rx1; rx++ ) {
			for ( const point of catalogue.regions.get( rz * 256 + rx ) ?? [] ) {
				if ( !admitted.has( point.monster.refObjId ) ) continue;
				const mx = (point.x - f.left) * scaleX, my = (f.top - point.z) * scaleY;
				if ( mx < 0 || my < 0 || mx >= f.width || my >= f.height ) continue;
				const gx = Math.floor( mx / CELL_SIZE ),
					gy = Math.floor( my / CELL_SIZE ),
					x = f.ox + gx * CELL_SIZE,
					y = f.oy + gy * CELL_SIZE;
				if (
					x + CELL_SIZE <= clip[0] || y + CELL_SIZE <= clip[1] ||
					x >= clip[0] + clip[2] || y >= clip[1] + clip[3]
				) continue;
				const key = gx + ":" + gy, cell = cells.get( key ) ?? { x, y, monsters: new Map() };
				cell.monsters.set( point.monster.refObjId, point.monster );
				cells.set( key, cell );
			}
		}
	}
	const quads: UiQuad[] = [], controls: UiControl[] = [], bands = huntingLevelColors();
	for ( const [key, cell] of cells ) {
		const rows = [ ...cell.monsters.values() ].sort( ( a, b ) =>
				a.level - b.level || a.name.localeCompare( b.name )
			),
			color = bands.find( band => rows[0]!.level <= band.max )!.color;
		const rect: UiRect = [
			cell.x + CELL_INSET,
			cell.y + CELL_INSET,
			CELL_SIZE - 2 * CELL_INSET,
			CELL_SIZE - 2 * CELL_INSET
		];
		quads.push( { rect, clip, texture: "", uv: [ 0, 0, 1, 1 ], color: [ color[0], color[1], color[2], .22 ] } );
		quads.push( {
			rect: [ rect[0], rect[1], rect[2], 1 ],
			clip,
			texture: "",
			uv: [ 0, 0, 1, 1 ],
			color: [ color[0], color[1], color[2], .75 ]
		} );
		const left = Math.max( rect[0], clip[0] ),
			top = Math.max( rect[1], clip[1] ),
			right = Math.min( rect[0] + rect[2], clip[0] + clip[2] ),
			bottom = Math.min( rect[1] + rect[3], clip[1] + clip[3] );
		if ( right <= left || bottom <= top ) continue;
		const names = rows.slice( 0, MAX_HOVER_NAMES ).map( row => `${row.name} · Lv. ${row.level}` );
		if ( rows.length > MAX_HOVER_NAMES ) names.push( `+${rows.length - MAX_HOVER_NAMES} more species` );
		names.push( "Approximate hunting area" );
		controls.push( {
			id: HUNTING_AREA_PREFIX + key,
			kind: "region",
			draggable: true,
			rect: [ left, top, right - left, bottom - top ],
			label: names.join( "\n" ),
			helpText: names.join( "\n" )
		} );
	}
	return { quads, controls, matches: admitted.size };
}
