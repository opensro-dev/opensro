/*
===========================================================================

cloth-stream.ts - a cloth mesh's solver stream, for a GPU-skinned draw

A cloth mesh is drawn as a skinned mesh: the GPU skins every vertex from
the pose's palette unless the vertex's entry in this stream says the solver
owns its position (STREAM_POSITION) or its normal (STREAM_NORMAL). That
reproduces createClothVertices (cloth-vertices.ts), which skins the pins on
the CPU every frame and rewrites the whole vertex stream:

- a free vertex (pin 0) shows the solver's position, and the normal it had
  when the simulation started;
- a pinned vertex follows the skeleton (AEADE0); on a solver step's frame,
  a pin the step moved shows the solver's position for that frame;
- with the simulation off, every vertex follows the skeleton.

With the simulation on, the CPU runs only when a solver step falls due
(cloth.ts pending, at most 20 Hz): re-anchoring the pins between steps
changes nothing the solver keeps. With it off, the CPU skins only the
vertices the solver integrates (free ones and pin 2, not pin 1) every
frame, because a restart takes their previous frame's anchors, positions
and normals, as createClothVertices' last off frame leaves them. So the
solver's state, its random calls and every value the stream carries are
bit for bit those of createClothVertices.

One result differs from createClothVertices, by design: the GPU skins
the vertices it owns in float32, as it does for the body these pieces
hang from, not with the CPU's double-precision sums.

===========================================================================
*/
import type { Geometry } from "@/engine/contracts/geometry";
import { createCloth, type ClothData } from "./cloth";
import { skinClothVertex } from "./cloth-skin";

// Words a vertex: position x, y, z (float32 bits) and flags, then the
// normal x, y, z and a zero. The skinned shaders read it as a skin entry.
export const CLOTH_STREAM_WORDS = 8;
// The flags word: the solver owns the vertex's position, its normal.
export const STREAM_POSITION = 1;
export const STREAM_NORMAL = 2;

/*
================
ClothStreamWrite

The stream entries an update changed: from vertex first on.
================
*/
export interface ClothStreamWrite {
	readonly first: number;
	readonly entries: Uint32Array;
}

/*
================
createClothStream

One mesh instance's solver and stream; each actor keeps its own.
================
*/
export function createClothStream(
	primitive: { geometry: Geometry; cloth?: ClothData; },
	random: () => number
) {
	const mesh = primitive.geometry, data = primitive.cloth!, count = mesh.positions.length / 3;
	if ( !mesh.joints || !mesh.weights ) throw Error( "A cloth stream needs a skinned mesh" );
	const simulation = createCloth( data, mesh.positions );
	const anchors = new Float32Array( mesh.positions.length );
	const words = new Uint32Array( count * CLOTH_STREAM_WORDS ), floats = new Float32Array( words.buffer );
	// One skinned vertex, [x, y, z, nx, ny, nz] (cloth-skin.ts).
	const skin = new Float64Array( 6 );
	// The pins the last step moved: they show the solver's position on that
	// frame only, as createClothVertices re-anchors them on the next.
	const moved: number[] = [];
	let lastTime: number | undefined, lastEnabled = false;
	// The entries this update changed: [low, high).
	let low = 0, high = 0;

	/*
	================
	touch
	================
	*/
	function touch( vertex: number ) {
		if ( low === high ) {
			low = vertex;
			high = vertex + 1;
		} else if ( vertex < low ) low = vertex;
		else if ( vertex >= high ) high = vertex + 1;
	}

	/*
	================
	written

	The changed entries, or null when nothing changed.
	================
	*/
	function written(): ClothStreamWrite | null {
		return low === high ?
			null :
			{ first: low, entries: words.subarray( low * CLOTH_STREAM_WORDS, high * CLOTH_STREAM_WORDS ) };
	}

	/*
	================
	anchor

	Vertex i's skinned position into anchors and its skinned normal into
	the stream's normal slot (read only where STREAM_NORMAL is set).
	================
	*/
	function anchor( i: number, palette: Float32Array ) {
		skinClothVertex( mesh, palette, i, skin );
		const at = i * 3, entry = i * CLOTH_STREAM_WORDS;
		anchors[at] = skin[0]!;
		anchors[at + 1] = skin[1]!;
		anchors[at + 2] = skin[2]!;
		floats[entry + 4] = skin[3]!;
		floats[entry + 5] = skin[4]!;
		floats[entry + 6] = skin[5]!;
	}

	/*
	================
	publish

	The solver's positions after an advance. A free vertex always shows
	them; a pin shows them only when the step moved it off its anchor.
	started rewrites every pin's flags, for the frame the simulation starts.
	================
	*/
	function publish( positions: Float32Array, started: boolean ) {
		for ( let i = 0; i < count; i++ ) {
			const at = i * 3, entry = i * CLOTH_STREAM_WORDS;
			let owned = data.pins[i] === 0;
			if ( !owned ) {
				owned = !Object.is( positions[at], anchors[at] ) || !Object.is( positions[at + 1], anchors[at + 1] ) ||
					!Object.is( positions[at + 2], anchors[at + 2] );
				if ( owned ) moved.push( i );
				else if ( started ) {
					words[entry + 3] = 0;
					touch( i );
				}
			}
			if ( !owned ) continue;
			floats[entry] = positions[at]!;
			floats[entry + 1] = positions[at + 1]!;
			floats[entry + 2] = positions[at + 2]!;
			words[entry + 3] = data.pins[i] === 0 ? STREAM_POSITION | STREAM_NORMAL : STREAM_POSITION;
			touch( i );
		}
	}

	return {
		/*
		================
		update

		The frame's changes to the stream, for palette at seconds. enabled is
		the option and detail gate createClothVertices takes; motion feeds the
		wind. Returns null on most frames: no step due, nothing to write.
		================
		*/
		update(
			palette: Float32Array,
			seconds: number,
			enabled: boolean,
			motion: { direction: readonly number[]; speed: number; }
		): ClothStreamWrite | null {
			low = high = 0;
			for ( const i of moved ) {
				words[i * CLOTH_STREAM_WORDS + 3] = 0;
				touch( i );
			}
			moved.length = 0;
			// Measured from the last update that advanced the solver: frames in
			// between sum into one delta, as cloth-vertices.ts hold does.
			const deltaMs = lastTime === undefined ? 0 : Math.trunc( seconds * 1000 ) - Math.trunc( lastTime * 1000 );
			const input = { anchors, deltaMs, enabled, direction: motion.direction, speed: motion.speed, random };
			if ( !enabled ) {
				// Off: the GPU skins every vertex. The CPU keeps this frame's
				// anchor and normal of each vertex the solver integrates: a
				// restart starts them there (pin 1 is re-anchored first).
				if ( lastTime === undefined || lastEnabled ) {
					for ( let i = 0; i < count; i++ ) {
						words[i * CLOTH_STREAM_WORDS + 3] = 0;
						touch( i );
					}
				}
				for ( let i = 0; i < count; i++ ) {
					if ( data.pins[i] !== 1 ) anchor( i, palette );
				}
				lastTime = seconds;
				lastEnabled = false;
				return written();
			}
			if ( lastTime === undefined ) {
				// The first frame: every vertex's anchor and normal now.
				for ( let i = 0; i < count; i++ ) anchor( i, palette );
				publish( simulation.advance( input ), true );
			} else if ( !lastEnabled ) {
				// The simulation starts again: the solver resets to the last off
				// frame's anchors, then the pins re-anchor to this frame's.
				simulation.advance( { ...input, deltaMs: 0, enabled: false } );
				for ( let i = 0; i < count; i++ ) {
					if ( data.pins[i] !== 0 ) anchor( i, palette );
				}
				publish( simulation.advance( input ), true );
			} else if ( simulation.pending( deltaMs ) ) {
				// A step falls due: re-anchor the pins to this frame's skeleton.
				for ( let i = 0; i < count; i++ ) {
					if ( data.pins[i] !== 0 ) anchor( i, palette );
				}
				publish( simulation.advance( input ), false );
			} else {
				return written();
			}
			lastTime = seconds;
			lastEnabled = true;
			return written();
		}
	};
}
