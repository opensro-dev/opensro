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
import type { GeometryDraw } from "@/engine/runtime/renderer/internal/gpu-contract";

type IndexedEncoder = Pick<GPURenderPassEncoder, "drawIndexed" | "drawIndexedIndirect">;

/*
================
issueIndexedDraw

Draws draw with the encoder's bound pipeline, binding and streams: its
indirect arguments when it has them, else its own counts.
================
*/
export function issueIndexedDraw( encoder: IndexedEncoder, draw: GeometryDraw ) {
	if ( draw.indirect ) encoder.drawIndexedIndirect( draw.indirect, draw.indirectOffset );
	else encoder.drawIndexed( draw.indexCount, draw.instanceCount, draw.firstIndex ?? 0, draw.baseVertex ?? 0, 0 );
}
