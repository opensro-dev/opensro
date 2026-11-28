/*
===========================================================================

device-draw.ts - the geometry draw handle

The handle geometry.ts gives every consumer: read-only outside, updated in
place by the geometry owner. Kept apart from that owner so the handle's
shape (one class, monomorphic count reads) is the whole of this file.

===========================================================================
*/
import type { GeometryDraw } from "@/engine/runtime/renderer/internal/gpu-contract";

/*
================
DeviceDraw

A draw handle. Consumers only read it: assigning a count throws. Its
counts, binding and capacity change in place, through select and rebind,
which only the geometry owner calls. Every handle is one class, one shape, so the
per-frame reads of its counts are monomorphic.
================
*/
export class DeviceDraw implements GeometryDraw {
	readonly deferredParticle: boolean | undefined;
	readonly blended: boolean;
	readonly vertices: GPUBuffer;
	readonly indices: GPUBuffer;
	readonly firstIndex: number;
	readonly baseVertex: number;
	readonly count: number;
	#pipeline: GPURenderPipeline;
	#binding: GPUBindGroup;
	#instanceCapacity: number;
	#indexCount: number;
	#instanceCount: number;
	#indirect: GPUBuffer | null = null;
	#indirectOffset = 0;

	constructor(
		fixed: Pick<
			GeometryDraw,
			| "deferredParticle"
			| "blended"
			| "pipeline"
			| "vertices"
			| "indices"
			| "firstIndex"
			| "baseVertex"
			| "count"
		>,
		binding: GPUBindGroup,
		instanceCapacity: number,
		instanceCount: number
	) {
		this.deferredParticle = fixed.deferredParticle;
		this.blended = fixed.blended ?? false;
		this.#pipeline = fixed.pipeline;
		this.vertices = fixed.vertices;
		this.indices = fixed.indices;
		this.firstIndex = fixed.firstIndex ?? 0;
		this.baseVertex = fixed.baseVertex ?? 0;
		this.count = fixed.count;
		this.#binding = binding;
		this.#instanceCapacity = instanceCapacity;
		this.#indexCount = fixed.count;
		this.#instanceCount = instanceCount;
		Object.freeze( this );
	}

	get pipeline() {
		return this.#pipeline;
	}

	get binding() {
		return this.#binding;
	}

	get instanceCapacity() {
		return this.#instanceCapacity;
	}

	get indexCount() {
		return this.#indexCount;
	}

	get instanceCount() {
		return this.#instanceCount;
	}

	get indirect() {
		return this.#indirect;
	}

	get indirectOffset() {
		return this.#indirectOffset;
	}

	// The watched draws (the static world runs, object-cull.ts) and a count
	// bumped by each change of a watched draw's binding, pipeline or
	// arguments buffer (not its counts): a recording of watched indirect
	// draws stays valid while it holds (frame.ts recordPersistent). A
	// character, particle or UI draw changing never touches it.
	static #watched = new WeakSet<DeviceDraw>();
	static #revision = 0;

	static get revision() {
		return DeviceDraw.#revision;
	}

	/*
	================
	watch / unwatch
	================
	*/
	static watch( draw: DeviceDraw ) {
		DeviceDraw.#watched.add( draw );
		DeviceDraw.#changed( draw );
	}

	static unwatch( draw: DeviceDraw ) {
		if ( DeviceDraw.#watched.delete( draw ) ) DeviceDraw.#revision++;
	}

	static #changed( draw: DeviceDraw ) {
		if ( DeviceDraw.#watched.has( draw ) ) DeviceDraw.#revision++;
	}

	/*
	================
	indirect

	The arguments a GPU-culled draw submits (index-cull.ts), or null to
	submit its counts again. Only the geometry owner calls it.
	================
	*/
	static drawIndirect( draw: DeviceDraw, buffer: GPUBuffer | null, offset = 0 ) {
		draw.#indirect = buffer;
		draw.#indirectOffset = offset;
		DeviceDraw.#changed( draw );
	}

	/*
	================
	select

	The index and instance counts the draw submits.
	================
	*/
	static select( draw: DeviceDraw, indexCount: number, instanceCount: number ) {
		draw.#indexCount = indexCount;
		draw.#instanceCount = instanceCount;
	}

	/*
	================
	rebind

	A new binding, and with grown instance storage its capacity.
	================
	*/
	static rebind( draw: DeviceDraw, binding: GPUBindGroup, instanceCapacity = draw.#instanceCapacity ) {
		draw.#binding = binding;
		draw.#instanceCapacity = instanceCapacity;
		DeviceDraw.#changed( draw );
	}

	/*
	================
	repipeline

	A new pipeline and binding together: the HDR stage swaps every scene
	draw to its rgba16float sibling, and a new pipeline means a new implicit
	layout, so the binding travels with it.
	================
	*/
	static repipeline( draw: DeviceDraw, pipeline: GPURenderPipeline, binding: GPUBindGroup ) {
		draw.#pipeline = pipeline;
		draw.#binding = binding;
		DeviceDraw.#changed( draw );
	}
}
