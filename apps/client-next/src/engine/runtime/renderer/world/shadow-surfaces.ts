/*
===========================================================================

shadow-surfaces.ts - the terrain triangles character shadows land on

Character shadow receivers follow the terrain ranges the frame submits
(character-shadow.ts), by terrain cell. A moving camera changes some
group's selection nearly every frame, but rarely under a character, so this
owner keeps the submitted ranges per cell and stamps each cell with the
revision of its last change. A receiver built at revision R stays valid
while every cell it covers is at or below R.

Rows in a cell keep the renderer's submission order: group draw order,
then range order inside the group.

===========================================================================
*/
import type { TerrainRange, WorldGroup } from "@/engine/contracts/scene";
import type { ShadowTerrainSurface } from "@/engine/foundation/rendering/character-shadow";
import { terrainCellKey } from "@/engine/foundation/rendering/terrain-interaction";

interface SurfaceRow {
	readonly group: WorldGroup;
	readonly order: number;
	readonly range: TerrainRange;
	readonly surface: ShadowTerrainSurface;
}

export type ShadowSurfaces = ReturnType<typeof createShadowSurfaces>;

/*
================
createShadowSurfaces
================
*/
export function createShadowSurfaces() {
	const rows = new Map<number, SurfaceRow[]>();
	const surfaces = new Map<number, readonly ShadowTerrainSurface[]>();
	const revisions = new Map<number, number>();
	// Cells changed this pass; their surface lists are rebuilt once at the end.
	const dirty = new Set<number>();
	let revision = 1, floor = 1;

	/*
	================
	insert

	Keeps the cell sorted by (group order, index start): ranges of one group
	are chosen in index order, so this is the submission order.
	================
	*/
	function insert( key: number, row: SurfaceRow ) {
		let cell = rows.get( key );
		if ( !cell ) {
			cell = [];
			rows.set( key, cell );
		}
		let at = cell.length;
		while (
			at > 0 &&
			(cell[at - 1]!.order > row.order ||
				cell[at - 1]!.order === row.order && cell[at - 1]!.range.indexStart > row.range.indexStart)
		) at--;
		cell.splice( at, 0, row );
		dirty.add( key );
	}

	/*
	================
	remove
	================
	*/
	function remove( key: number, group: WorldGroup, range: TerrainRange ) {
		const cell = rows.get( key );
		if ( !cell ) return;
		const at = cell.findIndex( row => row.group === group && row.range === range );
		if ( at < 0 ) return;
		cell.splice( at, 1 );
		if ( !cell.length ) rows.delete( key );
		dirty.add( key );
	}

	return {
		/*
		================
		reset

		Forgets every row (a new draw order). Every cell reads as changed.
		================
		*/
		reset() {
			rows.clear();
			surfaces.clear();
			revisions.clear();
			dirty.clear();
			floor = ++revision;
		},
		/*
		================
		replace

		A group's chosen ranges went from before to after. Only the ranges in
		one and not the other touch their cells. Both are subsequences of the
		group's ranges, which the decoder emits in index order, so one merge
		by index start finds them without allocating.
		================
		*/
		replace(
			group: WorldGroup,
			order: number,
			before: readonly TerrainRange[] | undefined,
			after: readonly TerrainRange[]
		) {
			const old = before ?? [];
			let i = 0, j = 0;
			while ( i < old.length || j < after.length ) {
				const a = old[i], b = after[j];
				if ( a && b && a === b ) {
					i++;
					j++;
				} else if ( a && (!b || a.indexStart < b.indexStart) ) {
					remove( terrainCellKey( a.cell[0], a.cell[1] ), group, a );
					i++;
				} else {
					insert( terrainCellKey( b!.cell[0], b!.cell[1] ), {
						group,
						order,
						range: b!,
						surface: {
							positions: group.geometry.positions,
							indices: group.geometry.indices,
							start: b!.indexStart,
							count: b!.indexCount
						}
					} );
					j++;
				}
			}
		},
		/*
		================
		commit

		Publishes the cells changed since the last commit under one new
		revision.
		================
		*/
		commit() {
			if ( !dirty.size ) return;
			revision++;
			for ( const key of dirty ) {
				const cell = rows.get( key );
				if ( cell ) surfaces.set( key, cell.map( row => row.surface ) );
				else surfaces.delete( key );
				revisions.set( key, revision );
			}
			dirty.clear();
		},
		/*
		================
		surfaces
		================
		*/
		surfaces: (): ReadonlyMap<number, readonly ShadowTerrainSurface[]> => surfaces,
		/*
		================
		revision

		The current revision: a receiver built now is valid until a cell it
		covers passes it.
		================
		*/
		revision: () => revision,
		/*
		================
		changedSince

		True when a cell in [cx0, cx1] x [cz0, cz1] changed after stamp.
		================
		*/
		changedSince( stamp: number, cx0: number, cz0: number, cx1: number, cz1: number ) {
			if ( floor > stamp ) return true;
			for ( let cz = cz0; cz <= cz1; cz++ ) {
				for ( let cx = cx0; cx <= cx1; cx++ ) {
					if ( (revisions.get( terrainCellKey( cx, cz ) ) ?? 0) > stamp ) return true;
				}
			}
			return false;
		}
	};
}
