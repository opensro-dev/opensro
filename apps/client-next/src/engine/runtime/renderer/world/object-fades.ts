/*
===========================================================================

object-fades.ts - the fade of every placed object, by placement id

SWorld advances each resident object's fade every frame (the rule is
object-visibility.ts advanceObjectFade). The material pieces of one
building share one fade, keyed by placement id, and a placement keeps its
fade across outdoor scenes so a crossing does not fade retained buildings
in again.

Rows are typed columns indexed by a row number the instance walk resolves
once per scene, so the per-frame walk over every resident placement reads
numbers instead of chasing one object per placement.

===========================================================================
*/
import { advanceObjectFade, type ObjectFade } from "@/engine/foundation/rendering/object-visibility";

const INITIAL_ROWS = 256;

export type ObjectFades = ReturnType<typeof createObjectFades>;

/*
================
createObjectFades
================
*/
export function createObjectFades() {
	let ids = new Map<string, number>(), rows = 0;
	// state 0..3, alpha (float32 values), the frame last advanced or stamped,
	// the walk frame that last visited the row, and the published alpha
	// (0..255) the draw takes.
	let state = new Uint8Array( INITIAL_ROWS ),
		alpha = new Float32Array( INITIAL_ROWS ),
		lastFrame = new Uint32Array( INITIAL_ROWS ),
		visited = new Float64Array( INITIAL_ROWS ).fill( -1 ),
		published = new Uint8Array( INITIAL_ROWS );
	// Rows visited by the last walk, in visit order: a stationary frame
	// stamps exactly these (world.ts retainedFadeFrame).
	let active = new Int32Array( INITIAL_ROWS ), activeCount = 0;
	const scratch: ObjectFade = { state: 0, alpha: 0, lastFrame: 0 };

	/*
	================
	grow
	================
	*/
	function grow( capacity: number ) {
		const next = <T extends Uint8Array | Float32Array | Uint32Array | Float64Array>( old: T, make: () => T ) => {
			const array = make();
			array.set( old.subarray( 0, rows ) as never );
			return array;
		};
		state = next( state, () => new Uint8Array( capacity ) );
		alpha = next( alpha, () => new Float32Array( capacity ) );
		lastFrame = next( lastFrame, () => new Uint32Array( capacity ) );
		visited = next( visited, () => new Float64Array( capacity ).fill( -1 ) );
		published = next( published, () => new Uint8Array( capacity ) );
	}

	return {
		/*
		================
		row

		The row of a placement, created out and invisible.
		================
		*/
		row( id: string ): number {
			let row = ids.get( id );
			if ( row !== undefined ) return row;
			if ( rows === state.length ) grow( rows * 2 );
			row = rows++;
			state[row] = 0;
			alpha[row] = 0;
			lastFrame[row] = 0;
			visited[row] = -1;
			published[row] = 0;
			ids.set( id, row );
			return row;
		},
		/*
		================
		find
		================
		*/
		find: ( id: string ): number | undefined => ids.get( id ),
		/*
		================
		visit

		Marks the row visited by the walk of frame, once.
		================
		*/
		visit( row: number, frame: number ) {
			if ( visited[row] === frame ) return;
			visited[row] = frame;
			if ( activeCount === active.length ) {
				const next = new Int32Array( active.length * 2 );
				next.set( active );
				active = next;
			}
			active[activeCount++] = row;
		},
		/*
		================
		advance

		advanceObjectFade on the row, then its published alpha, as native
		8B98FF..8B9932 converts alpha after the fade update.
		================
		*/
		advance( row: number, distance: number, radius: number, range: number, dt: number, frame: number ) {
			scratch.state = state[row] as ObjectFade["state"];
			scratch.alpha = alpha[row]!;
			scratch.lastFrame = lastFrame[row]!;
			advanceObjectFade( scratch, distance, radius, range, dt, frame, scratch );
			state[row] = scratch.state;
			alpha[row] = scratch.alpha;
			lastFrame[row] = scratch.lastFrame;
			published[row] = Math.max( 0, Math.min( 255, Math.trunc( scratch.alpha ) ) );
		},
		/*
		================
		stamp

		A steady row's frame stamp without an advance (state and alpha keep).
		================
		*/
		stamp( row: number, frame: number ) {
			lastFrame[row] = frame;
			published[row] = Math.max( 0, Math.min( 255, Math.trunc( alpha[row]! ) ) );
		},
		state: ( row: number ) => state[row]!,
		lastFrame: ( row: number ) => lastFrame[row]!,
		visited: ( row: number ) => visited[row]!,
		published: ( row: number ) => published[row]!,
		/*
		================
		stampActive

		Gives every row the last walk visited the frame stamp of frame.
		================
		*/
		stampActive( frame: number ) {
			for ( let i = 0; i < activeCount; i++ ) lastFrame[active[i]!] = frame;
		},
		/*
		================
		clearActive
		================
		*/
		clearActive() {
			activeCount = 0;
		},
		/*
		================
		keepOnly

		Forgets the placements not in placed and compacts the rows. Row
		numbers change, so the instance walk resolves them again.
		================
		*/
		keepOnly( placed: ReadonlySet<string> ) {
			const remap = new Int32Array( rows ).fill( -1 ), names: string[] = [];
			for ( const [id, row] of ids ) {
				if ( !placed.has( id ) ) continue;
				remap[row] = 0;
				names[row] = id;
			}
			// Kept rows move down in row order, so each copy reads a row not yet
			// overwritten.
			const kept = new Map<string, number>();
			let next = 0;
			for ( let from = 0; from < rows; from++ ) {
				if ( remap[from]! < 0 ) continue;
				const to = next++;
				remap[from] = to;
				kept.set( names[from]!, to );
				state[to] = state[from]!;
				alpha[to] = alpha[from]!;
				lastFrame[to] = lastFrame[from]!;
				visited[to] = visited[from]!;
				published[to] = published[from]!;
			}
			let at = 0;
			for ( let i = 0; i < activeCount; i++ ) {
				const row = remap[active[i]!]!;
				if ( row >= 0 ) active[at++] = row;
			}
			activeCount = at;
			ids = kept;
			rows = next;
		},
		/*
		================
		clear
		================
		*/
		clear() {
			ids = new Map();
			rows = 0;
			activeCount = 0;
		},
		size: () => ids.size
	};
}
