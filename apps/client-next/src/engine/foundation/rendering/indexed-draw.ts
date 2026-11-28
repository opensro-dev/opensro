/*
===========================================================================

indexed-draw.ts - the one way a pass or bundle issues a geometry draw

Every pass that may draw a world GeometryDraw (the frame's lists and
bundles, the water mirror, the sun cascade) binds its streams and issues
it here, so a GPU-culled draw (index-cull.ts: its counts in an indirect
buffer) draws the same everywhere a direct one does. Character shadows
draw only character parts, which are never culled.

===========================================================================
*/
// The fields of a GeometryDraw (gpu-contract.ts) that issuing it reads;
// Buffer is the backend's buffer (GPUBuffer), kept generic so this shared
// module names no backend type.
export interface IndexedDrawArgs<Buffer> {
	readonly indirect: Buffer | null;
	readonly indirectOffset: number;
	readonly indexCount: number;
	readonly instanceCount: number;
	readonly firstIndex?: number;
	readonly baseVertex?: number;
}

// The two draw calls of a render pass or bundle encoder.
export interface IndexedEncoder<Buffer> {
	drawIndexed(
		indexCount: number,
		instanceCount: number,
		firstIndex: number,
		baseVertex: number,
		firstInstance: number
	): void;
	drawIndexedIndirect( indirect: Buffer, offset: number ): void;
}

/*
================
issueIndexedDraw

Draws draw with the encoder's bound pipeline, binding and streams: its
indirect arguments when it has them, else its own counts.
================
*/
export function issueIndexedDraw<Buffer>( encoder: IndexedEncoder<Buffer>, draw: IndexedDrawArgs<Buffer> ) {
	if ( draw.indirect ) encoder.drawIndexedIndirect( draw.indirect, draw.indirectOffset );
	else encoder.drawIndexed( draw.indexCount, draw.instanceCount, draw.firstIndex ?? 0, draw.baseVertex ?? 0, 0 );
}
