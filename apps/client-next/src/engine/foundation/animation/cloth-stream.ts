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
(cloth.ts pending, at most 20 Hz), and re-anchors only the pins the step
reads (solverPins): re-anchoring the pins between steps, or a pin no step
reads, changes nothing the solver keeps. With it off, a restart takes the
anchors and normals of the vertices the solver integrates (free ones and
pin 2, not pin 1) from the last off frame, as createClothVertices' last off
frame leaves them. The CPU skins them on the frame the simulation stops;
on every later off frame the renderer only keeps its pose's inputs, and
the restart rebuilds that last off frame's palette from them and skins
them then (ClothPalettes). So the solver's state, its random calls and
every value the stream carries are bit for bit those of
createClothVertices.

The stream holds one entry (a slot) for each vertex the solver can move,
in index order (clothStreamSlots), not one a vertex: every other vertex is
skinned by the GPU for its whole life. The device keeps a static map from
vertex to slot next to the stream (device/geometry.ts), so a step uploads
the few dozen entries the solver can change, not the span of the mesh
between them.

One result differs from createClothVertices, by design: the GPU skins
the vertices it owns in float32, as it does for the body these pieces
hang from, not with the CPU's double-precision sums.

===========================================================================
*/
import type { Geometry } from "@/engine/contracts/geometry";
import { createCloth, type ClothData } from "./cloth";
import { skinClothVertex } from "./cloth-skin";

// Words a slot: position x, y, z (float32 bits) and flags, then the
// normal x, y, z and a zero. The skinned shaders read it as a skin entry.
export const CLOTH_STREAM_WORDS = 8;
// The flags word: the solver owns the vertex's position, its normal.
export const STREAM_POSITION = 1;
export const STREAM_NORMAL = 2;
// A vertex's slot when the stream holds none for it: the GPU skins it.
export const CLOTH_NO_SLOT = 0xffffffff;

/*
================
ClothStreamWrite

The stream entries an update changed: from slot first on.
================
*/
export interface ClothStreamWrite {
	readonly first: number;
	readonly entries: Uint32Array;
}

/*
================
ClothPalettes

The pose palettes a stream's update may ask for, all on the CPU. current
is this frame's. On an off frame, keep stores what rebuilds this frame's
palette later, without building it (false when it cannot); kept rebuilds
the palette of the frame of the last keep that returned true. entries
(clothStreamJoints) names the only palette entries the stream reads from
the result, so only they need be built; current without it is the whole
palette.
================
*/
export interface ClothPalettes {
	current( entries?: Int32Array ): Float32Array;
	keep(): boolean;
	kept( entries: Int32Array ): Float32Array;
}

/*
================
solverPins

The pins a solver step reads (cloth.ts): a pin that integrates (pin 2),
and every endpoint of a constraint. A pin 1 on no constraint is never moved
and never read; re-anchoring it changes nothing the solver keeps, and the
GPU skins it from the palette. The shipped character cloth reads 3,547 of
its 74,430 pins.
================
*/
export function solverPins( data: ClothData ): Int32Array {
	const constrained = new Uint8Array( data.pins.length );
	for ( const constraint of data.constraints ) constrained[constraint[0]] = constrained[constraint[1]] = 1;
	const pins: number[] = [];
	for ( let i = 0; i < data.pins.length; i++ ) {
		if ( data.pins[i] !== 0 && (data.pins[i] !== 1 || constrained[i]) ) pins.push( i );
	}
	return Int32Array.from( pins );
}

/*
================
clothStreamSlots

The vertices a cloth stream holds, one slot each, in index order: the free
ones and the pins a step reads. Only they can leave their anchor after an
advance; a pin no step reads is re-anchored to an anchor that never
changes, so it never leaves it (cloth.ts advance), never takes a position
or a normal from the stream, and the GPU skins it for its whole life. The
slots depend on the mesh's cloth data alone, so the stream and the
device's slot map (device/geometry.ts) agree for every wearer.
================
*/
export function clothStreamSlots( data: ClothData ): Int32Array {
	const read = solverPins( data ),
		slots = new Int32Array( read.length + data.pins.filter( pin => pin === 0 ).length );
	let k = 0;
	for ( let i = 0, r = 0; i < data.pins.length; i++ ) {
		if ( data.pins[i] === 0 ) slots[k++] = i;
		else if ( read[r] === i ) {
			slots[k++] = i;
			r++;
		}
	}
	return slots;
}

/*
================
clothStreamJoints

The palette entries skinning the slots' vertices reads (cloth-skin.ts reads
a joint only where its weight is not zero), ascending. Every update anchors
slots alone but the first, which anchors every vertex; so no other entry of
a palette asked for these is ever read. One to seven on the 204 shipped
player cloth pieces, of 43 to 45 joints (2026-11-16).
================
*/
export function clothStreamJoints( mesh: Geometry, slots: Int32Array ): Int32Array {
	if ( !mesh.joints || !mesh.weights ) throw Error( "A cloth stream needs a skinned mesh" );
	const read = new Set<number>();
	for ( const i of slots ) {
		for ( let k = 0; k < 4; k++ ) if ( mesh.weights[i * 4 + k] ) read.add( mesh.joints[i * 4 + k]! );
	}
	return Int32Array.from( read ).sort();
}

/*
================
validClothStreamSlots

Whether slots can name a stream's vertices on a mesh of vertices: strictly
increasing indices into it, as clothStreamSlots gives them.
================
*/
export function validClothStreamSlots( slots: unknown, vertices: number ): slots is Int32Array {
	if ( !(slots instanceof Int32Array) ) return false;
	for ( let k = 0; k < slots.length; k++ ) {
		if ( slots[k]! < (k ? slots[k - 1]! + 1 : 0) || slots[k]! >= vertices ) return false;
	}
	return true;
}

/*
================
createClothStream

One mesh instance's solver and stream; each actor keeps its own. slots is
the mesh's clothStreamSlots and joints its clothStreamJoints, which a
caller may share between the streams of every wearer of the mesh.
================
*/
export function createClothStream(
	primitive: { geometry: Geometry; cloth?: ClothData; },
	random: () => number,
	slots = clothStreamSlots( primitive.cloth! ),
	joints = clothStreamJoints( primitive.geometry, slots )
) {
	const mesh = primitive.geometry, data = primitive.cloth!, count = mesh.positions.length / 3;
	if ( !mesh.joints || !mesh.weights ) throw Error( "A cloth stream needs a skinned mesh" );
	const simulation = createCloth( data, mesh.positions );
	const anchors = new Float32Array( mesh.positions.length );
	// The pins a step reads (solverPins): the slots that are not free.
	const read = slots.filter( i => data.pins[i] !== 0 ), size = slots.length;
	// Each vertex's slot, or CLOTH_NO_SLOT.
	const slotOf = new Uint32Array( count ).fill( CLOTH_NO_SLOT );
	for ( let k = 0; k < size; k++ ) slotOf[slots[k]!] = k;
	const words = new Uint32Array( size * CLOTH_STREAM_WORDS ), floats = new Float32Array( words.buffer );
	// One skinned vertex, [x, y, z, nx, ny, nz] (cloth-skin.ts).
	const skin = new Float64Array( 6 );
	// The slots of the pins the last step moved: they show the solver's
	// position on that frame only, as createClothVertices re-anchors them on
	// the next.
	const moved: number[] = [];
	let lastTime: number | undefined, lastEnabled = false;
	// Off: the anchors and normals of the vertices the solver integrates are
	// not the last off frame's; palettes.kept rebuilds that frame's palette.
	let stale = false;
	// The slots this update changed: [low, high).
	let low = 0, high = 0;

	/*
	================
	touch
	================
	*/
	function touch( slot: number ) {
		if ( low === high ) {
			low = slot;
			high = slot + 1;
		} else if ( slot < low ) low = slot;
		else if ( slot >= high ) high = slot + 1;
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

	Vertex i's skinned position into anchors and, for a free vertex, its
	skinned normal into its stream entry. The shaders read a normal only
	where STREAM_NORMAL is set, which only a free vertex's entry carries
	(publish), so a pin's normal is never written.
	================
	*/
	function anchor( i: number, palette: Float32Array ) {
		skinClothVertex( mesh, palette, i, skin );
		const at = i * 3;
		anchors[at] = skin[0]!;
		anchors[at + 1] = skin[1]!;
		anchors[at + 2] = skin[2]!;
		if ( data.pins[i] !== 0 ) return;
		const entry = slotOf[i]! * CLOTH_STREAM_WORDS;
		floats[entry + 4] = skin[3]!;
		floats[entry + 5] = skin[4]!;
		floats[entry + 6] = skin[5]!;
	}

	/*
	================
	anchorIntegrated

	The anchors and normals of the vertices a step integrates (free ones and
	pin 2, all in the slots): what a restart starts them from.
	================
	*/
	function anchorIntegrated( palette: Float32Array ) {
		for ( let k = 0; k < size; k++ ) {
			if ( data.pins[slots[k]!] !== 1 ) anchor( slots[k]!, palette );
		}
		stale = false;
	}

	/*
	================
	publish

	The solver's positions after an advance, for every slot. A free vertex
	always shows them; a pin shows them only when the step moved it off its
	anchor. started also clears the flags of every pin left on its anchor,
	for the frame the simulation starts.
	================
	*/
	function publish( positions: Float32Array, started: boolean ) {
		for ( let k = 0; k < size; k++ ) {
			const i = slots[k]!, at = i * 3, entry = k * CLOTH_STREAM_WORDS;
			let owned = data.pins[i] === 0;
			if ( !owned ) {
				owned = !Object.is( positions[at], anchors[at] ) || !Object.is( positions[at + 1], anchors[at + 1] ) ||
					!Object.is( positions[at + 2], anchors[at + 2] );
				if ( owned ) moved.push( k );
				else if ( started ) {
					words[entry + 3] = 0;
					touch( k );
				}
			}
			if ( !owned ) continue;
			floats[entry] = positions[at]!;
			floats[entry + 1] = positions[at + 1]!;
			floats[entry + 2] = positions[at + 2]!;
			words[entry + 3] = data.pins[i] === 0 ? STREAM_POSITION | STREAM_NORMAL : STREAM_POSITION;
			touch( k );
		}
	}

	return {
		// The vertex each slot holds (clothStreamSlots): the device's slot
		// map for this stream's draw is built from it.
		slots,
		/*
		================
		update

		The frame's changes to the stream, for the pose at seconds. enabled is
		the option and detail gate createClothVertices takes; motion feeds the
		wind. Returns null on most frames: no step due, nothing to write.
		palettes gives the pose's palette on the CPU (ClothPalettes), asked
		only for the solver's steps, the frame the simulation starts or stops,
		and an off frame whose palette cannot be kept: a skeleton the GPU
		samples is built on the CPU for nothing else, and after the first
		frame only for the entries the slots read (joints). An off frame
		after the first only keeps its pose's inputs; a restart rebuilds the
		last off frame's palette from them (kept) and anchors from it what
		every off frame used to anchor, so the solver restarts exactly where
		createClothVertices does.
		================
		*/
		update(
			palettes: ClothPalettes,
			seconds: number,
			enabled: boolean,
			motion: { direction: readonly number[]; speed: number; }
		): ClothStreamWrite | null {
			low = high = 0;
			for ( const k of moved ) {
				words[k * CLOTH_STREAM_WORDS + 3] = 0;
				touch( k );
			}
			moved.length = 0;
			// Measured from the last update that advanced the solver: frames in
			// between sum into one delta, as cloth-vertices.ts hold does.
			const deltaMs = lastTime === undefined ? 0 : Math.trunc( seconds * 1000 ) - Math.trunc( lastTime * 1000 );
			const input = {
				anchors,
				deltaMs,
				enabled,
				direction: motion.direction,
				speed: motion.speed,
				random,
				pins: read
			};
			if ( !enabled ) {
				// Off: the GPU skins every vertex. A restart starts the vertices
				// the solver integrates from the last off frame's anchors and
				// normals (pin 1 is re-anchored first). The first off frame
				// anchors them now, for the write that clears every flag; a later
				// one only keeps what rebuilds its palette, nothing being drawn
				// or written from them until the restart.
				const stopping = lastTime === undefined || lastEnabled;
				if ( stopping ) {
					for ( let k = 0; k < size; k++ ) {
						words[k * CLOTH_STREAM_WORDS + 3] = 0;
						touch( k );
					}
				}
				if ( !stopping && palettes.keep() ) stale = true;
				else anchorIntegrated( palettes.current( joints ) );
				lastTime = seconds;
				lastEnabled = false;
				return written();
			}
			if ( lastTime === undefined ) {
				// The first frame: every vertex's anchor and normal now, from
				// the whole palette.
				const palette = palettes.current();
				for ( let i = 0; i < count; i++ ) anchor( i, palette );
				publish( simulation.advance( input ), true );
			} else if ( !lastEnabled ) {
				// The simulation starts again: the solver resets to the last off
				// frame's anchors, then the pins re-anchor to this frame's.
				if ( stale ) anchorIntegrated( palettes.kept( joints ) );
				simulation.advance( { ...input, deltaMs: 0, enabled: false } );
				const palette = palettes.current( joints );
				for ( const i of read ) anchor( i, palette );
				publish( simulation.advance( input ), true );
			} else if ( simulation.pending( deltaMs ) ) {
				// A step falls due: re-anchor the pins it reads to this frame's skeleton.
				const palette = palettes.current( joints );
				for ( const i of read ) anchor( i, palette );
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
