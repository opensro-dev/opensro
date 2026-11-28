/*
===========================================================================

index-cull.ts - GPU-driven culling of terrain layer ranges

Port-only, not native (owner 2026-11-28: "dragging camera should have no
cost"). The native client culls each terrain range's sphere against the
view on the CPU every frame (A2D1B0) and draws the survivors. Here the
CPU hands each culled draw its candidate ranges once (when the eye
crosses a cell or a member joins), and a compute pass tests every range
against the live frustum and the water mirror's, packs the survivors'
indices into the draw's own index buffer and counts them into the draw's
indirect arguments. The draw submits drawIndexedIndirect, so a turning
camera changes no CPU state: no re-cull, no index upload, no bundle
re-record. The same ranges survive as on the CPU (a sphere outside a
plane, grown by CULL_EPSILON for float32).

Order: the atomic counter packs survivors in any order, which changes no
pixel. A layer is all opaque or all blended (its key holds the blend
flag). Opaque terrain writes depth with less-equal, so order is moot. A
blended overlay tests less-equal and writes no depth: where two of its
cells overlap on screen, the nearer cell's opaque base is a separate draw
that either precedes the whole layer (the farther fragment fails the test)
or follows it (and overwrites both). Ranges of one layer are distinct
cells; neighbours meet only along stitched shared edges.

Why compaction: WebGPU core has no multi-draw-indirect, so persistent
indices drawn range by range would be one indirect draw a cell (hundreds a
frame). Compaction keeps one draw a layer, copies only the survivors'
indices, and only on a frame whose view or table changed. The water
mirror's ranges are culled into the same set (the union, as the CPU cull
chose), so the main pass may draw a mirror-only range the rasterizer
clips; the experimental sun cascade draws the camera's set, as before.

One owner per device generation. A culled draw holds:
- a slot of the shared arguments buffer (5 words: indexCount, instanceCount,
  firstIndex, baseVertex, firstInstance), written once at adopt but for
  indexCount, which the reset dispatch zeroes before every cull;
- a source range of every member's indices, already offset by its vertex
  slot (written by the owner of the layer as members join);
- a range table (RANGE_WORDS a range), replaced when the candidates change.

The pass runs only when the view or a table changed since the last one:
a still camera dispatches nothing and the arguments keep their counts.

===========================================================================
*/
import { createBufferSlabs, type BufferSlice } from "./buffer-slabs";
import { DeviceDraw } from "./device-draw";
import type { Retire } from "./retirement";
import type { GpuTimingFrame } from "../internal/gpu-contract";

// One draw's indirect arguments (drawIndexedIndirect's five words).
const ARG_WORDS = 5;
const ARG_BYTES = ARG_WORDS * 4;
const MIN_SLOTS = 64;
// A range: centre xyz and radius (float), source start, index count, the
// draw's slot and a pad (uint).
export const RANGE_WORDS = 8;
const RANGE_BYTES = RANGE_WORDS * 4;
export const RANGE_SLOT_WORD = 6;
const MIN_RANGES = 16;
// Twelve planes (the view's six, the mirror's six) and a flags vec4.
const VIEW_FLOATS = 13 * 4;
const PLANES = 6;
// Threads per range: each copies every WORKGROUP-th index of its range.
const WORKGROUP = 64;
// World units every tested sphere grows by: float32 planes and positions
// round where the CPU test ran in float64, and a superset changes no pixel.
const CULL_EPSILON = 1;

const shader = /* wgsl */ `
struct View { planes: array<vec4f, 12>, flags: vec4u }
struct Range { sphere: vec4f, start: u32, count: u32, slot: u32, pad: u32 }
struct Args { indexCount: atomic<u32>, instanceCount: u32, firstIndex: u32, baseVertex: u32, firstInstance: u32 }
@group(0) @binding(0) var<uniform> view: View;
@group(0) @binding(1) var<storage, read_write> args: array<Args>;
@group(0) @binding(2) var<storage, read> ranges: array<Range>;
@group(0) @binding(3) var<storage, read> source: array<u32>;
@group(0) @binding(4) var<storage, read_write> output: array<u32>;
var<workgroup> keptAt: u32;
var<workgroup> kept: u32;

fn inside(first: u32, sphere: vec4f) -> bool {
	for (var i = 0u; i < ${PLANES}u; i++) {
		let plane = view.planes[first + i];
		if (dot(plane.xyz, sphere.xyz) + plane.w < -(sphere.w + ${CULL_EPSILON.toFixed( 1 )})) { return false; }
	}
	return true;
}

@compute @workgroup_size(${WORKGROUP})
fn reset(@builtin(global_invocation_id) id: vec3u) {
	if (id.x < view.flags.y) { atomicStore(&args[id.x].indexCount, 0u); }
}

@compute @workgroup_size(${WORKGROUP})
fn main(@builtin(workgroup_id) group: vec3u, @builtin(local_invocation_index) lane: u32) {
	let range = ranges[group.x];
	if (lane == 0u) {
		var visible = inside(0u, range.sphere);
		if (!visible && view.flags.x != 0u) { visible = inside(${PLANES}u, range.sphere); }
		kept = select(0u, 1u, visible);
		if (visible) { keptAt = atomicAdd(&args[range.slot].indexCount, range.count); }
	}
	workgroupBarrier();
	if (kept == 0u) { return; }
	let at = args[range.slot].firstIndex + keptAt;
	for (var i = lane; i < range.count; i += ${WORKGROUP}u) { output[at + i] = source[range.start + i]; }
}
`;

/*
================
Culled

One culled draw: its slot, its source and range slices, and the bind
group naming them (null until the next pass builds it).
================
*/
type Culled = {
	draw: DeviceDraw;
	slot: number;
	capacity: number;
	source: BufferSlice;
	ranges: BufferSlice | null;
	rangeCapacity: number;
	rangeCount: number;
	binding: GPUBindGroup | null;
};

/*
================
writePlanes

Normalised planes of a prepareViewFrustum array (a, b, c, d, length each)
into view from float first.
================
*/
function writePlanes( view: Float32Array, first: number, planes: Float64Array ) {
	for ( let plane = 0; plane < PLANES; plane++ ) {
		const at = plane * 5, length = planes[at + 4]! || 1;
		for ( let k = 0; k < 4; k++ ) view[first + plane * 4 + k] = planes[at + k]! / length;
	}
}

/*
================
createIndexCull
================
*/
export function createIndexCull( device: GPUDevice, retire: Retire ) {
	let failure: string | null = null, disposed = false;
	let cullPipeline: GPUComputePipeline | undefined, resetPipeline: GPUComputePipeline | undefined;
	const culled = new Map<DeviceDraw, Culled>(), freeSlots: number[] = [];
	const storage = createBufferSlabs( device, "index-cull", GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST, retire );
	let slotCapacity = 0, slotTop = 0, args: GPUBuffer | null = null, argWords = new Uint32Array( 0 );
	let viewBuffer: GPUBuffer | null = null, resetBinding: GPUBindGroup | null = null;
	const view = new Float32Array( VIEW_FLOATS ), viewWords = new Uint32Array( view.buffer );
	// The planes and mirror flag of the last view, to tell a changed one.
	const lastWords = new Uint32Array( PLANES * 8 + 1 );
	// The pass is due when the view or any table changed since the last one.
	let due = false, viewSet = false, dispatches = 0;
	const ready = Promise.resolve()
		.then( () => {
			const module = device.createShaderModule( { label: "index-cull", code: shader } );
			return Promise.all( [
				device.createComputePipelineAsync( {
					label: "index-cull",
					layout: "auto",
					compute: { module, entryPoint: "main" }
				} ),
				device.createComputePipelineAsync( {
					label: "index-cull-reset",
					layout: "auto",
					compute: { module, entryPoint: "reset" }
				} )
			] );
		} )
		.then( ( [cull, reset] ) => {
			if ( disposed ) return;
			cullPipeline = cull;
			resetPipeline = reset;
		} )
		.catch( error => {
			if ( !disposed ) failure = String( error );
		} );

	/*
	================
	growSlots

	A larger arguments buffer holding every slot's words; every culled
	draw re-points at it, so its bundles re-record (frame.ts compares
	indirect) and its bind group is rebuilt.
	================
	*/
	function growSlots( need: number ) {
		let capacity = Math.max( MIN_SLOTS, slotCapacity );
		while ( capacity < need ) capacity *= 2;
		const words = new Uint32Array( capacity * ARG_WORDS );
		words.set( argWords );
		const buffer = device.createBuffer( {
			label: "index-cull-arguments",
			size: capacity * ARG_BYTES,
			usage: GPUBufferUsage.INDIRECT | GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
		} );
		device.queue.writeBuffer( buffer, 0, words.buffer, 0, words.byteLength );
		if ( args ) retire( args );
		args = buffer;
		argWords = words;
		slotCapacity = capacity;
		resetBinding = null;
		for ( const entry of culled.values() ) {
			DeviceDraw.drawIndirect( entry.draw, buffer, entry.slot * ARG_BYTES );
			entry.binding = null;
		}
		due = true;
	}

	/*
	================
	writeSlot
	================
	*/
	function writeSlot( entry: Culled ) {
		const at = entry.slot * ARG_WORDS;
		argWords[at] = 0;
		argWords[at + 1] = 1;
		argWords[at + 2] = entry.draw.firstIndex;
		argWords[at + 3] = entry.draw.baseVertex;
		argWords[at + 4] = 0;
		device.queue.writeBuffer( args!, at * 4, argWords.buffer, at * 4, ARG_BYTES );
	}

	return {
		ready,
		/*
		================
		adopt

		Makes draw GPU-culled with room for capacity source indices. Its
		counts come from its slot from now on; until a pass culls it, it
		draws nothing.
		================
		*/
		adopt( draw: DeviceDraw, capacity: number ) {
			if ( disposed ) throw Error( "Disposed index cull" );
			if ( culled.has( draw ) ) throw Error( "Draw is already culled" );
			if ( !Number.isInteger( capacity ) || capacity <= 0 || capacity > draw.count ) {
				throw Error( "Invalid cull capacity" );
			}
			const slot = freeSlots.pop() ?? slotTop++;
			if ( slot >= slotCapacity ) growSlots( slot + 1 );
			const entry: Culled = {
				draw,
				slot,
				capacity,
				source: storage.allocate( capacity * 4 ),
				ranges: null,
				rangeCapacity: 0,
				rangeCount: 0,
				binding: null
			};
			culled.set( draw, entry );
			writeSlot( entry );
			DeviceDraw.drawIndirect( draw, args, slot * ARG_BYTES );
			due = true;
		},
		/*
		================
		source

		Writes indices (already offset by their member's vertex slot) at
		source index at.
		================
		*/
		source( draw: DeviceDraw, at: number, indices: Uint32Array ) {
			const entry = culled.get( draw );
			if ( !entry ) throw Error( "Draw is not culled" );
			if ( !Number.isInteger( at ) || at < 0 || at + indices.length > entry.capacity ) {
				throw Error( "Invalid cull source write" );
			}
			if ( !indices.length ) return;
			const binding = entry.source.binding;
			device.queue.writeBuffer(
				binding.buffer,
				binding.offset + at * 4,
				indices.buffer as ArrayBuffer,
				indices.byteOffset,
				indices.byteLength
			);
			due = true;
		},
		/*
		================
		ranges

		The draw's candidate ranges, RANGE_WORDS each (centre and radius as
		float bits, source start, index count); the slot word is filled here.
		Every range must lie inside the draw's source and the counts must
		fit its index buffer.
		================
		*/
		ranges( draw: DeviceDraw, table: Uint32Array, count: number ) {
			const entry = culled.get( draw );
			if ( !entry ) throw Error( "Draw is not culled" );
			if ( !Number.isInteger( count ) || count < 0 || count * RANGE_WORDS > table.length ) {
				throw Error( "Invalid cull ranges" );
			}
			let total = 0;
			for ( let i = 0; i < count; i++ ) {
				const start = table[i * RANGE_WORDS + 4]!, length = table[i * RANGE_WORDS + 5]!;
				if ( start + length > entry.capacity ) throw Error( "Invalid cull range" );
				total += length;
				table[i * RANGE_WORDS + RANGE_SLOT_WORD] = entry.slot;
			}
			if ( total > draw.count ) throw Error( "Cull ranges overflow the draw" );
			if ( count > entry.rangeCapacity ) {
				let capacity = Math.max( MIN_RANGES, entry.rangeCapacity );
				while ( capacity < count ) capacity *= 2;
				if ( entry.ranges ) retire( entry.ranges );
				entry.ranges = storage.allocate( capacity * RANGE_BYTES );
				entry.rangeCapacity = capacity;
				entry.binding = null;
			}
			entry.rangeCount = count;
			if ( count ) {
				const binding = entry.ranges!.binding;
				device.queue.writeBuffer(
					binding.buffer,
					binding.offset,
					table.buffer as ArrayBuffer,
					table.byteOffset,
					count * RANGE_BYTES
				);
			}
			due = true;
		},
		/*
		================
		view

		The frame's frustum (prepareViewFrustum planes) and the water mirror's,
		or null. An unchanged view leaves the pass undue.
		================
		*/
		view( main: Float64Array, mirror: Float64Array | null ) {
			writePlanes( view, 0, main );
			if ( mirror ) writePlanes( view, PLANES * 4, mirror );
			else view.fill( 0, PLANES * 4, PLANES * 8 );
			viewWords[PLANES * 8] = mirror ? 1 : 0;
			// Compared as words: the mirror flag is an integer in a float slot.
			for ( let i = 0; i < lastWords.length; i++ ) {
				if ( viewWords[i] !== lastWords[i] ) {
					due = true;
					lastWords[i] = viewWords[i]!;
				}
			}
			if ( !viewSet ) due = true;
			viewSet = true;
		},
		/*
		================
		encode

		The cull pass, when it is due: one reset over every slot, then one
		dispatch a culled draw with candidates. Once per frame at most: a
		second call in the same frame finds nothing due.
		================
		*/
		encode( encoder: GPUCommandEncoder, timing?: GpuTimingFrame ) {
			if ( disposed || !due || !viewSet || !culled.size ) return;
			if ( failure ) throw Error( `Index cull pipeline failed: ${failure}` );
			if ( !cullPipeline || !resetPipeline ) throw Error( "Index cull pipeline is not ready" );
			viewBuffer ??= device.createBuffer( {
				label: "index-cull-view",
				size: VIEW_FLOATS * 4,
				usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
			} );
			viewWords[PLANES * 8 + 1] = slotTop;
			device.queue.writeBuffer( viewBuffer, 0, view.buffer, 0, view.byteLength );
			resetBinding ??= device.createBindGroup( {
				label: "index-cull-reset",
				layout: resetPipeline.getBindGroupLayout( 0 ),
				entries: [
					{ binding: 0, resource: { buffer: viewBuffer } },
					{ binding: 1, resource: { buffer: args! } }
				]
			} );
			const pass = encoder.beginComputePass( {
				label: "index-cull",
				timestampWrites: timing?.pass( "index-cull" )
			} );
			pass.setPipeline( resetPipeline );
			pass.setBindGroup( 0, resetBinding );
			pass.dispatchWorkgroups( Math.ceil( slotTop / WORKGROUP ) );
			pass.setPipeline( cullPipeline );
			for ( const entry of culled.values() ) {
				if ( !entry.rangeCount ) continue;
				entry.binding ??= device.createBindGroup( {
					label: "index-cull",
					layout: cullPipeline.getBindGroupLayout( 0 ),
					entries: [
						{ binding: 0, resource: { buffer: viewBuffer } },
						{ binding: 1, resource: { buffer: args! } },
						{ binding: 2, resource: entry.ranges!.binding },
						{ binding: 3, resource: entry.source.binding },
						{ binding: 4, resource: { buffer: entry.draw.indices } }
					]
				} );
				pass.setBindGroup( 0, entry.binding );
				pass.dispatchWorkgroups( entry.rangeCount );
				dispatches++;
			}
			pass.end();
			due = false;
		},
		/*
		================
		forget

		A released draw gives back its slot and slices.
		================
		*/
		forget( draw: DeviceDraw ) {
			const entry = culled.get( draw );
			if ( !entry ) return;
			culled.delete( draw );
			retire( entry.source );
			if ( entry.ranges ) retire( entry.ranges );
			freeSlots.push( entry.slot );
		},
		stats: () => ({ draws: culled.size, slots: slotCapacity, dispatches, failure }),
		/*
		================
		dispose
		================
		*/
		dispose() {
			disposed = true;
			culled.clear();
			storage.dispose();
			args?.destroy();
			viewBuffer?.destroy();
			args = viewBuffer = null;
		}
	};
}
