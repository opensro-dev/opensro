/*
===========================================================================

gpu-allocations.ts - how much GPU memory the page's allocations hold

Jetsam kills the iPhone page without a trace (#4545), and a bug report
could say nothing about how much GPU memory the page held: no totals
existed. The device owner counts every buffer and texture it creates
into these totals - bytes and counts, live: a destroy returns its
bytes - so a report (and the page-lifetime marker in
bug-report/page-lifetime.ts) reads the page's standing GPU total at
any moment. The counting itself lives beside the device (device/
gpu-allocation-meter.ts); this module is the renderer-neutral count:
plain figures any reader may name.

The counters live as long as their owner (the device owner), across
device recreations, like the queue-work totals (gpu-work.ts): a new
device starts empty, and device recovery destroys the old device's
resources through the same counted releases before the replacement's
allocations join, so the standing total never carries a dead device's
bytes. What no destroy ever reaches - a resource the renderer simply
let go of - leaves through the garbage collector instead (device/
gpu-allocation-meter.ts registers each counted resource for
finalization), so the standing total can fall as well as rise.

===========================================================================
*/

/*
================
GpuAllocationTotals

The live allocation totals, as plain values: how many buffers and
textures stand, and the bytes they asked for.
================
*/
export interface GpuAllocationTotals {
	readonly buffers: number;
	readonly bufferBytes: number;
	readonly textures: number;
	readonly textureBytes: number;
}

export type GpuAllocations = ReturnType<typeof createGpuAllocations>;

/*
================
createGpuAllocations

The totals one device owner keeps. note* on creation, drop* on
release (a destroy, or the collector's finalization of a resource
nothing destroyed); drops floor at zero as a guard, though every
counted resource releases exactly once by construction.
================
*/
export function createGpuAllocations() {
	let buffers = 0, bufferBytes = 0, textures = 0, textureBytes = 0;
	return {
		/*
		================
		noteBuffer

		One buffer created with the descriptor's byte size.
		================
		*/
		noteBuffer( bytes: number ): void {
			buffers++;
			bufferBytes += bytes;
		},
		/*
		================
		dropBuffer

		One buffer released: its bytes leave the standing total.
		================
		*/
		dropBuffer( bytes: number ): void {
			buffers = Math.max( 0, buffers - 1 );
			bufferBytes = Math.max( 0, bufferBytes - bytes );
		},
		/*
		================
		noteTexture

		One texture created with its estimated byte footprint.
		================
		*/
		noteTexture( bytes: number ): void {
			textures++;
			textureBytes += bytes;
		},
		/*
		================
		dropTexture

		One texture released: its bytes leave the standing total.
		================
		*/
		dropTexture( bytes: number ): void {
			textures = Math.max( 0, textures - 1 );
			textureBytes = Math.max( 0, textureBytes - bytes );
		},
		/*
		================
		read

		The totals so far, as plain values.
		================
		*/
		read(): GpuAllocationTotals {
			return { buffers, bufferBytes, textures, textureBytes };
		}
	};
}
