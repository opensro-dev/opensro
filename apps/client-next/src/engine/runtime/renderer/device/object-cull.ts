/*
===========================================================================

object-cull.ts - GPU-driven visibility of world object draws

Port-only, not native (#4692). The native client tests each object's
bound against the view every frame on the CPU (8AA9C5..8AA9D8, A2D410),
and the port did the same per world group, then rebuilt the frame's
draw list from the survivors: a turning camera changed the list, and
every changed run of draws re-recorded its render bundle.

Here an opaque object draw holds a persistent slot: its indirect
arguments (written once, and again only when its counts change) and a
record of its bounding sphere and instance count. One dispatch a frame
whose view changed tests every record against the view's planes and the
water mirror's (the union, as the CPU test chose) and writes each draw's
instance count, or zero. The world keeps the draw in its list while it
is resident (world.ts), so the list, and the bundles recording it, stay
the same however the camera turns. The same draws survive as on the CPU:
a sphere outside a plane, grown by CULL_EPSILON for float32.

One owner per device generation, beside the terrain index cull
(index-cull.ts), which shares its plane layout.

===========================================================================
*/
import { DeviceDraw } from "./device-draw";
import { writePlanes } from "./index-cull";
import type { Retire } from "./retirement";
import type { GpuTimingFrame } from "../internal/gpu-contract";

const ARG_WORDS = 5;
const ARG_BYTES = ARG_WORDS * 4;
// A record: centre xyz, radius (float), instance count, live flag, pad.
const RECORD_WORDS = 8;
const MIN_SLOTS = 256;
const PLANES = 6;
// Twelve planes (the view's six, the mirror's six) and a flags vec4.
const VIEW_FLOATS = 13 * 4;
const WORKGROUP = 64;
const CULL_EPSILON = 1;

const shader = /* wgsl */ `
struct View { planes: array<vec4f, 12>, flags: vec4u }
struct Record { sphere: vec4f, instances: u32, live: u32, pad0: u32, pad1: u32 }
@group(0) @binding(0) var<uniform> view: View;
@group(0) @binding(1) var<storage, read_write> args: array<u32>;
@group(0) @binding(2) var<storage, read> records: array<Record>;

fn inside(first: u32, sphere: vec4f) -> bool {
	for (var i = 0u; i < ${PLANES}u; i++) {
		let plane = view.planes[first + i];
		if (dot(plane.xyz, sphere.xyz) + plane.w < -(sphere.w + ${CULL_EPSILON.toFixed( 1 )})) { return false; }
	}
	return true;
}

@compute @workgroup_size(${WORKGROUP})
fn main(@builtin(global_invocation_id) id: vec3u) {
	if (id.x >= view.flags.y) { return; }
	let record = records[id.x];
	if (record.live == 0u) { return; }
	var visible = inside(0u, record.sphere);
	if (!visible && view.flags.x != 0u) { visible = inside(${PLANES}u, record.sphere); }
	args[id.x * ${ARG_WORDS}u + 1u] = select(0u, record.instances, visible);
}
`;

/*
================
createObjectCull
================
*/
export function createObjectCull( device: GPUDevice, retire: Retire ) {
	let failure: string | null = null, disposed = false, pipeline: GPUComputePipeline | undefined;
	const slots = new Map<DeviceDraw, number>(), draws: (DeviceDraw | null)[] = [], freeSlots: number[] = [];
	let capacity = 0, top = 0;
	let args: GPUBuffer | null = null, records: GPUBuffer | null = null, viewBuffer: GPUBuffer | null = null;
	let binding: GPUBindGroup | null = null;
	let argWords = new Uint32Array( 0 ), recordWords = new Uint32Array( 0 ), recordFloats = new Float32Array( 0 );
	const view = new Float32Array( VIEW_FLOATS ), viewWords = new Uint32Array( view.buffer );
	const lastWords = new Uint32Array( PLANES * 8 + 1 );
	let due = false, viewSet = false, dispatches = 0, writes = 0;
	const ready = Promise.resolve()
		.then( () =>
			device.createComputePipelineAsync( {
				label: "object-cull",
				layout: "auto",
				compute: {
					module: device.createShaderModule( { label: "object-cull", code: shader } ),
					entryPoint: "main"
				}
			} )
		)
		.then( created => {
			if ( !disposed ) pipeline = created;
		} )
		.catch( error => {
			if ( !disposed ) failure = String( error );
		} );

	/*
	================
	grow

	Larger arguments and record buffers holding every slot; every culled
	draw re-points at the new arguments (its bundles re-record once).
	================
	*/
	function grow( need: number ) {
		let next = Math.max( MIN_SLOTS, capacity );
		while ( next < need ) next *= 2;
		const nextArgs = new Uint32Array( next * ARG_WORDS ), nextRecords = new Uint32Array( next * RECORD_WORDS );
		nextArgs.set( argWords );
		nextRecords.set( recordWords );
		if ( args ) retire( args );
		if ( records ) retire( records );
		args = device.createBuffer( {
			label: "object-cull-arguments",
			size: next * ARG_BYTES,
			// COPY_SRC: the WebGPU parity test reads the verdicts back (buffers).
			usage: GPUBufferUsage.INDIRECT | GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC
		} );
		records = device.createBuffer( {
			label: "object-cull-records",
			size: next * RECORD_WORDS * 4,
			usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
		} );
		device.queue.writeBuffer( args, 0, nextArgs.buffer, 0, nextArgs.byteLength );
		device.queue.writeBuffer( records, 0, nextRecords.buffer, 0, nextRecords.byteLength );
		argWords = nextArgs;
		recordWords = nextRecords;
		recordFloats = new Float32Array( recordWords.buffer );
		capacity = next;
		binding = null;
		for ( const [draw, slot] of slots ) DeviceDraw.drawIndirect( draw, args, slot * ARG_BYTES );
		due = true;
	}

	/*
	================
	writeSlot

	A slot's arguments from its draw's counts (the instance count stays
	the GPU's until the next pass) and its record's instance count.
	================
	*/
	function writeSlot( slot: number, draw: DeviceDraw ) {
		const at = slot * ARG_WORDS;
		argWords[at] = draw.indexCount;
		argWords[at + 2] = draw.firstIndex;
		argWords[at + 3] = draw.baseVertex;
		argWords[at + 4] = 0;
		device.queue.writeBuffer( args!, at * 4, argWords.buffer, at * 4, ARG_BYTES );
		const record = slot * RECORD_WORDS;
		recordWords[record + 4] = draw.instanceCount;
		recordWords[record + 5] = 1;
		device.queue.writeBuffer( records!, record * 4, recordWords.buffer, record * 4, RECORD_WORDS * 4 );
		writes++;
		due = true;
	}

	return {
		ready,
		/*
		================
		adopt

		Makes draw GPU-culled against sphere (centre xyz, radius). It draws
		nothing until the next pass decides.
		================
		*/
		adopt( draw: DeviceDraw, sphere: readonly number[] ) {
			if ( disposed ) throw Error( "Disposed object cull" );
			if ( slots.has( draw ) ) throw Error( "Draw is already culled" );
			if ( sphere.length !== 4 || !sphere.every( Number.isFinite ) ) throw Error( "Invalid cull sphere" );
			const slot = freeSlots.pop() ?? top++;
			if ( slot >= capacity ) grow( slot + 1 );
			slots.set( draw, slot );
			// A recording of the draw lives while its state holds (DeviceDraw.watch).
			DeviceDraw.watch( draw );
			draws[slot] = draw;
			recordFloats.set( sphere, slot * RECORD_WORDS );
			argWords[slot * ARG_WORDS + 1] = 0;
			DeviceDraw.drawIndirect( draw, args!, slot * ARG_BYTES );
			writeSlot( slot, draw );
		},
		/*
		================
		refresh

		A culled draw's counts changed (geometry.ts updateInstances,
		updateIndices): its arguments and record follow.
		================
		*/
		refresh( draw: DeviceDraw ) {
			const slot = slots.get( draw );
			if ( slot !== undefined ) writeSlot( slot, draw );
		},
		culled( draw: DeviceDraw ) {
			return slots.has( draw );
		},
		/*
		================
		view

		The frame's frustum and the water mirror's (or null); an unchanged
		view leaves the pass undue.
		================
		*/
		view( main: Float64Array, mirror: Float64Array | null ) {
			writePlanes( view, 0, main );
			if ( mirror ) writePlanes( view, PLANES * 4, mirror );
			else view.fill( 0, PLANES * 4, PLANES * 8 );
			viewWords[PLANES * 8] = mirror ? 1 : 0;
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

		One dispatch over every slot, when the view or a slot changed.
		================
		*/
		encode( encoder: GPUCommandEncoder, timing?: GpuTimingFrame ) {
			if ( disposed || !due || !viewSet || !slots.size ) return;
			if ( failure ) throw Error( `Object cull pipeline failed: ${failure}` );
			if ( !pipeline ) throw Error( "Object cull pipeline is not ready" );
			viewBuffer ??= device.createBuffer( {
				label: "object-cull-view",
				size: VIEW_FLOATS * 4,
				usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
			} );
			viewWords[PLANES * 8 + 1] = top;
			device.queue.writeBuffer( viewBuffer, 0, view.buffer, 0, view.byteLength );
			binding ??= device.createBindGroup( {
				label: "object-cull",
				layout: pipeline.getBindGroupLayout( 0 ),
				entries: [
					{ binding: 0, resource: { buffer: viewBuffer } },
					{ binding: 1, resource: { buffer: args! } },
					{ binding: 2, resource: { buffer: records! } }
				]
			} );
			const pass = encoder.beginComputePass( {
				label: "object-cull",
				timestampWrites: timing?.pass( "object-cull" )
			} );
			pass.setPipeline( pipeline );
			pass.setBindGroup( 0, binding );
			pass.dispatchWorkgroups( Math.ceil( top / WORKGROUP ) );
			pass.end();
			dispatches++;
			due = false;
		},
		/*
		================
		forget

		A released draw gives back its slot; its record stops counting.
		================
		*/
		forget( draw: DeviceDraw ) {
			const slot = slots.get( draw );
			if ( slot === undefined ) return;
			slots.delete( draw );
			DeviceDraw.unwatch( draw );
			draws[slot] = null;
			recordWords[slot * RECORD_WORDS + 5] = 0;
			device.queue.writeBuffer(
				records!,
				(slot * RECORD_WORDS + 5) * 4,
				recordWords.buffer,
				(slot * RECORD_WORDS + 5) * 4,
				4
			);
			freeSlots.push( slot );
		},
		stats: () => ({ draws: slots.size, slots: capacity, dispatches, writes, failure }),
		// The arguments buffer and a draw's slot, for the parity test only.
		buffers: () => ({ args, slotOf: ( draw: DeviceDraw ) => slots.get( draw ) }),
		dispose() {
			disposed = true;
			slots.clear();
			for ( const buffer of [ args, records, viewBuffer ] ) if ( buffer ) retire( buffer );
			args = records = viewBuffer = null;
			binding = null;
		}
	};
}
