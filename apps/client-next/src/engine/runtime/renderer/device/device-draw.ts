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
	readonly water: boolean | undefined;
	readonly softDepth: boolean | undefined;
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

	constructor(
		fixed: Pick<
			GeometryDraw,
			| "deferredParticle"
			| "softDepth"
			| "water"
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
		this.water = fixed.water;
		this.softDepth = fixed.softDepth;
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
	}
}
