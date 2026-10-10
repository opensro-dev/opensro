/*
===========================================================================

hunting-guide.ts - fixed world-space hunting sections from static anchors

Port-only approximate guidance from static outdoor anchors.

===========================================================================
*/
import type { UiControl, UiQuad, UiRect } from "@/engine/contracts/ui";
import type { worldMapFrame } from "./world-map";
const MAX_MONSTERS = 4096, MAX_POINTS = 65536, MAX_NAME = 128;
const REGION_SIZE = 192, LOCAL_SCALE = 10, MAX_LOCAL_POSITION = 65536;
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
	readonly sections: readonly HuntingSection[];
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
	return { monsters, regions, sections: buildHuntingSections( regions ) };
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
export const HUNTING_PORTRAIT_FRAME = "/assets/images/Media_extracted/interface/quick_slot/qsl_hriz01_slot_tile.png";

/*
================
HuntingSection

A fixed world-space partition of the public static anchors. These outlines
are approximate guide areas, not native region borders or spawn radii.
================
*/
export interface HuntingSection {
	readonly id: string;
	readonly polygon: readonly HuntingVertex[];
	readonly monsters: readonly HuntingMonster[];
	readonly color: readonly [number, number, number];
}

/*
================
HuntingVertex
================
*/
export type HuntingVertex = readonly [number, number];

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
	readonly areas: readonly { section: HuntingSection; rect: UiRect; }[];
}

// World units: two-region seeds, three-region spacing, half-region padding.
const SEED_SIZE = 384, SECTION_SPACING = 576, MAX_SECTION_SEEDS = 512;
const OUTLINE_PADDING = 96, FILL_STRIP = 2, DOT_SPACING = 7, DOT_SIZE = 3;

/*
================
HuntingSeed
================
*/
interface HuntingSeed {
	id: string;
	x: number;
	z: number;
	count: number;
}

/*
================
convexHuntingHull

Use the anchors' footprint for the outside edge rather than painting an
arbitrary rectangular atlas grid across empty terrain.
================
*/
function convexHuntingHull( points: readonly HuntingVertex[] ): HuntingVertex[] {
	const sorted = [ ...points ].sort( ( a, b ) => a[0] - b[0] || a[1] - b[1] );
	const lower: HuntingVertex[] = [], upper: HuntingVertex[] = [];
	/*
	================
	append
	================
	*/
	function append( chain: HuntingVertex[], point: HuntingVertex ) {
		while ( chain.length > 1 ) {
			const a = chain[chain.length - 2]!, b = chain[chain.length - 1]!;
			if ( (b[0] - a[0]) * (point[1] - a[1]) - (b[1] - a[1]) * (point[0] - a[0]) > 0 ) break;
			chain.pop();
		}
		chain.push( point );
	}
	for ( const point of sorted ) append( lower, point );
	for ( const point of sorted.reverse() ) append( upper, point );
	return [ ...lower.slice( 0, -1 ), ...upper.slice( 0, -1 ) ];
}

/*
================
clipHuntingPolygon

Clip a convex polygon against ax + by <= distance. Shared bisectors give
neighbouring sections one boundary and one unambiguous hover owner.
================
*/
function clipHuntingPolygon( polygon: readonly HuntingVertex[], a: number, b: number, distance: number ) {
	const result: HuntingVertex[] = [];
	for ( let i = 0; i < polygon.length; i++ ) {
		const start = polygon[i]!, end = polygon[(i + 1) % polygon.length]!;
		const from = start[0] * a + start[1] * b - distance, to = end[0] * a + end[1] * b - distance;
		if ( from <= 0 ) result.push( start );
		if ( (from <= 0) === (to <= 0) ) continue;
		const ratio = from / (from - to);
		result.push( [ start[0] + (end[0] - start[0]) * ratio, start[1] + (end[1] - start[1]) * ratio ] );
	}
	return result;
}

/*
================
buildHuntingSections

Port-only inference: cluster the complete static catalogue once in world
coordinates, partition it by nearest seed, then bound each section to its
padded anchor hull. Neither pan, viewport, labels nor hover participates.
Every anchor is assigned to exactly one section; every local species is kept.
================
*/
function buildHuntingSections( regions: ReadonlyMap<number, readonly HuntingPoint[]> ): HuntingSection[] {
	const points = [ ...regions.values() ].flat(), bins = new Map<string, HuntingSeed>();
	const colors = [
		[ .48, .72, .32 ],
		[ .32, .56, .88 ],
		[ .88, .64, .25 ],
		[ .85, .38, .33 ],
		[ .68, .45, .82 ],
		[ .32, .72, .66 ]
	] as const;
	// Bound clustering work for a valid but unusually dispersed catalogue.
	let seedSize = SEED_SIZE;
	while ( true ) {
		bins.clear();
		for ( const point of points ) {
			const id = Math.floor( point.x / seedSize ) + ":" + Math.floor( point.z / seedSize );
			const bin = bins.get( id ) ?? { id, x: 0, z: 0, count: 0 };
			bin.x += point.x;
			bin.z += point.z;
			bin.count++;
			bins.set( id, bin );
		}
		if ( bins.size <= MAX_SECTION_SEEDS ) break;
		seedSize *= 2;
	}
	const seeds = [ ...bins.values() ].sort( ( a, b ) => a.id.localeCompare( b.id ) );
	while ( seeds.length > 1 ) {
		let first = 0, second = 1, nearest = SECTION_SPACING;
		for ( let a = 0; a < seeds.length; a++ ) {
			for ( let b = a + 1; b < seeds.length; b++ ) {
				const left = seeds[a]!, right = seeds[b]!;
				const distance = Math.hypot(
					left.x / left.count - right.x / right.count,
					left.z / left.count - right.z / right.count
				);
				if ( distance >= nearest ) continue;
				first = a;
				second = b;
				nearest = distance;
			}
		}
		if ( nearest === SECTION_SPACING ) break;
		seeds[first]!.x += seeds[second]!.x;
		seeds[first]!.z += seeds[second]!.z;
		seeds[first]!.count += seeds[second]!.count;
		seeds.splice( second, 1 );
	}
	for ( const seed of seeds ) {
		seed.x /= seed.count;
		seed.z /= seed.count;
	}
	const members = seeds.map( () => [] as HuntingPoint[] );
	for ( const point of points ) {
		let nearest = Infinity, chosen = 0;
		for ( let i = 0; i < seeds.length; i++ ) {
			const seed = seeds[i]!, distance = (point.x - seed.x) ** 2 + (point.z - seed.z) ** 2;
			if ( distance >= nearest ) continue;
			nearest = distance;
			chosen = i;
		}
		members[chosen]!.push( point );
	}
	return seeds.flatMap( ( seed, index ) => {
		const anchors = members[index]!;
		if ( !anchors.length ) return [];
		const hull = convexHuntingHull( anchors.map( point => [ point.x, point.z ] ) );
		const outline = hull.length ? hull : [ [ anchors[0]!.x, anchors[0]!.z ] as HuntingVertex ];
		let polygon = convexHuntingHull( outline.flatMap( ( [x, z] ) => [
			[ x - OUTLINE_PADDING, z - OUTLINE_PADDING ] as HuntingVertex,
			[ x + OUTLINE_PADDING, z - OUTLINE_PADDING ] as HuntingVertex,
			[ x + OUTLINE_PADDING, z + OUTLINE_PADDING ] as HuntingVertex,
			[ x - OUTLINE_PADDING, z + OUTLINE_PADDING ] as HuntingVertex
		] ) );
		for ( const other of seeds ) {
			if ( other === seed ) continue;
			polygon = clipHuntingPolygon(
				polygon,
				other.x - seed.x,
				other.z - seed.z,
				(other.x ** 2 + other.z ** 2 - seed.x ** 2 - seed.z ** 2) / 2
			);
		}
		const monsters = [ ...new Map( anchors.map( point => [ point.monster.refObjId, point.monster ] ) ).values() ]
			.sort( ( a, b ) => a.level - b.level || a.refObjId - b.refObjId );
		return [ { id: seed.id, polygon, monsters, color: colors[index % colors.length]! } ];
	} );
}

/*
================
huntingPolygonBounds
================
*/
function huntingPolygonBounds( polygon: readonly HuntingVertex[] ): UiRect {
	const x = Math.min( ...polygon.map( p => p[0] ) ), y = Math.min( ...polygon.map( p => p[1] ) );
	return [ x, y, Math.max( ...polygon.map( p => p[0] ) ) - x, Math.max( ...polygon.map( p => p[1] ) ) - y ];
}

/*
================
projectHuntingGuide

Translate fixed section geometry with the map. Clip only the visible paint
and hover polygon: identities, members and boundaries never repack on pan.
================
*/
export function projectHuntingGuide(
	catalogue: HuntingCatalogue,
	f: ReturnType<typeof worldMapFrame>,
	clip: UiRect,
	hover: string | null = null
): HuntingProjection {
	// Authored local pages are towns: this outdoor guide must never imply spawns there.
	if ( f.page ) return { quads: [], controls: [], areas: [], paths: [], labels: [] };
	const quads: UiQuad[] = [], controls: UiControl[] = [], areas: { section: HuntingSection; rect: UiRect; }[] = [];
	const scaleX = f.width / (f.right - f.left), scaleY = f.height / (f.top - f.bottom);
	for ( const section of catalogue.sections ) {
		const polygon: HuntingVertex[] = section.polygon.map( (
			[x, z]
		) => [ f.ox + (x - f.left) * scaleX, f.oy + (f.top - z) * scaleY ] );
		const bounds = huntingPolygonBounds( polygon );
		if (
			bounds[0] >= clip[0] + clip[2] || bounds[1] >= clip[1] + clip[3] || bounds[0] + bounds[2] <= clip[0] ||
			bounds[1] + bounds[3] <= clip[1]
		) continue;
		let visible = clipHuntingPolygon( polygon, -1, 0, -clip[0] );
		visible = clipHuntingPolygon( visible, 1, 0, clip[0] + clip[2] );
		visible = clipHuntingPolygon( visible, 0, -1, -clip[1] );
		visible = clipHuntingPolygon( visible, 0, 1, clip[1] + clip[3] );
		if ( visible.length < 3 ) continue;
		const id = HUNTING_AREA_PREFIX + section.id, active = hover === id;
		const color: UiQuad["color"] = [ ...section.color, active ? .24 : .11 ];
		// Scanline quads stay on the section's own origin; panning does not snap
		// its fill to a screen grid. The DOM uses the exact same polygon for hits.
		const first = Math.max( 0, Math.floor( (clip[1] - bounds[1]) / FILL_STRIP ) );
		for (
			let row = first;
			bounds[1] + row * FILL_STRIP < Math.min( bounds[1] + bounds[3], clip[1] + clip[3] );
			row++
		) {
			const y = bounds[1] + row * FILL_STRIP, middle = y + FILL_STRIP / 2, crossings: number[] = [];
			for ( let i = 0; i < polygon.length; i++ ) {
				const a = polygon[i]!, b = polygon[(i + 1) % polygon.length]!;
				if ( (a[1] <= middle) === (b[1] <= middle) ) continue;
				crossings.push( a[0] + (b[0] - a[0]) * (middle - a[1]) / (b[1] - a[1]) );
			}
			if ( crossings.length < 2 ) continue;
			const left = Math.min( ...crossings ), right = Math.max( ...crossings );
			quads.push( { rect: [ left, y, right - left, FILL_STRIP ], clip, texture: "", uv: [ 0, 0, 1, 1 ], color } );
		}
		for ( let i = 0; i < polygon.length; i++ ) {
			const a = polygon[i]!,
				b = polygon[(i + 1) % polygon.length]!,
				length = Math.hypot( b[0] - a[0], b[1] - a[1] );
			for ( let step = 0; step < length; step += DOT_SPACING ) {
				const x = a[0] + (b[0] - a[0]) * step / length, y = a[1] + (b[1] - a[1]) * step / length;
				if (
					x < clip[0] - DOT_SIZE || x > clip[0] + clip[2] || y < clip[1] - DOT_SIZE || y > clip[1] + clip[3]
				) continue;
				quads.push( {
					rect: [ x - DOT_SIZE / 2, y - DOT_SIZE / 2, DOT_SIZE, DOT_SIZE ],
					clip,
					texture: "",
					uv: [ 0, 0, 1, 1 ],
					color: [ ...section.color, active ? 1 : .85 ]
				} );
			}
		}
		const rect = huntingPolygonBounds( visible );
		const names = [
			`Hunting area - ${section.monsters.length} species`,
			...section.monsters.map( row => `${row.name} - Lv. ${row.level}` ),
			"Approximate static locations"
		];
		controls.push( { id, kind: "region", draggable: true, rect, hitPolygon: visible, label: names.join( "\n" ) } );
		areas.push( { section, rect } );
	}
	return { quads, controls, areas, paths: [], labels: [] };
}

const DETAIL_MARGIN = 12, DETAIL_COLUMN = 204, DETAIL_ROW = 38, DETAIL_HEADER = 42, DETAIL_FOOTER = 28;

/*
================
huntingGuideDetails

Only the hovered section demands portraits. Columns keep the complete local
species list on screen; its world-space membership never depends on this card.
================
*/
export function huntingGuideDetails(
	projection: HuntingProjection,
	images: ReadonlyMap<number, string>,
	hover: string | null,
	viewport: UiRect
) {
	const area = projection.areas.find( row => HUNTING_AREA_PREFIX + row.section.id === hover );
	if ( !area ) return null;
	const monsters = area.section.monsters;
	const capacity = Math.max(
		1,
		Math.floor( (viewport[3] - DETAIL_MARGIN * 2 - DETAIL_HEADER - DETAIL_FOOTER) / DETAIL_ROW )
	);
	const columns = Math.ceil( monsters.length / capacity ), rows = Math.ceil( monsters.length / columns );
	const width = columns * DETAIL_COLUMN + DETAIL_MARGIN * 2,
		height = DETAIL_HEADER + rows * DETAIL_ROW + DETAIL_FOOTER;
	const right = area.rect[0] + area.rect[2] + DETAIL_MARGIN;
	const left = right + width <= viewport[2] - DETAIL_MARGIN ? right : area.rect[0] - width - DETAIL_MARGIN;
	const x = Math.max( DETAIL_MARGIN, Math.min( viewport[2] - width - DETAIL_MARGIN, left ) );
	const y = Math.max( DETAIL_MARGIN, Math.min( viewport[3] - height - DETAIL_MARGIN, area.rect[1] ) );
	const low = monsters[0]!.level, high = monsters[monsters.length - 1]!.level;
	const entries = monsters.map( ( monster, index ) => ({
		monster,
		image: images.get( monster.refObjId ) ?? HUNTING_MONSTER_SIGN,
		rect: [
			x + DETAIL_MARGIN + Math.floor( index / rows ) * DETAIL_COLUMN,
			y + DETAIL_HEADER + index % rows * DETAIL_ROW,
			DETAIL_COLUMN,
			DETAIL_ROW
		] as UiRect
	}) );
	return {
		rect: [ x, y, width, height ] as UiRect,
		title: `Hunting area - Lv. ${low === high ? low : low + "-" + high}`,
		caption: `${monsters.length} species - Approximate area`,
		rows: entries,
		paths: [ HUNTING_PORTRAIT_FRAME, HUNTING_MONSTER_SIGN, ...new Set( entries.map( row => row.image ) ) ]
	};
}
