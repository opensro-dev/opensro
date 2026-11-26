/*
===========================================================================

gpu-allocations.ts - how much GPU memory the page's allocations hold

Jetsam kills the iPhone page without a trace (#4545), and a bug report
could say nothing about how much GPU memory the page held: no totals
existed. The device owner counts every buffer and texture it creates
here - bytes and counts, live: a destroy returns its bytes - so a
report (and the page-lifetime marker in bug-report/page-lifetime.ts)
reads the page's standing GPU total at any moment.

The counting is exact about what the renderer asked for and silent
about what the driver did with it: a buffer counts its descriptor
size, a texture an estimate from its descriptor (textureAllocationBytes:
its format's footprint over the mip chain). Driver padding, alignment
and the swapchain's own textures sit outside the count; the number is
the renderer's demand, which is what a leak or a budget question asks.

The counters live as long as their owner (the device owner), across
device recreations, like the queue-work totals (gpu-work.ts): a new
device starts empty, so a recreated device's allocations join the same
totals and the destroyed ones of the old device have already left.

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
destroy; drops floor at zero, because a device lost with resources
standing is replaced by a new one whose totals start from these, and
the old owner's late destroys must not drive the count negative.
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

		One buffer destroyed: its bytes leave the standing total.
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

		One texture destroyed: its bytes leave the standing total.
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

// One format's texel block: its width and height in texels and the
// bytes one block occupies. Uncompressed formats are 1x1 blocks of
// their texel size; the compressed families share one block shape.
interface TexelBlock {
	readonly width: number;
	readonly height: number;
	readonly bytes: number;
}

const TEXEL_BLOCKS = {
	"r8unorm": { width: 1, height: 1, bytes: 1 },
	"r8snorm": { width: 1, height: 1, bytes: 1 },
	"r8uint": { width: 1, height: 1, bytes: 1 },
	"r8sint": { width: 1, height: 1, bytes: 1 },
	"r16unorm": { width: 1, height: 1, bytes: 2 },
	"r16snorm": { width: 1, height: 1, bytes: 2 },
	"r16uint": { width: 1, height: 1, bytes: 2 },
	"r16sint": { width: 1, height: 1, bytes: 2 },
	"r16float": { width: 1, height: 1, bytes: 2 },
	"rg8unorm": { width: 1, height: 1, bytes: 2 },
	"rg8snorm": { width: 1, height: 1, bytes: 2 },
	"rg8uint": { width: 1, height: 1, bytes: 2 },
	"rg8sint": { width: 1, height: 1, bytes: 2 },
	"r32uint": { width: 1, height: 1, bytes: 4 },
	"r32sint": { width: 1, height: 1, bytes: 4 },
	"r32float": { width: 1, height: 1, bytes: 4 },
	"rgba8unorm": { width: 1, height: 1, bytes: 4 },
	"rgba8unorm-srgb": { width: 1, height: 1, bytes: 4 },
	"rgba8snorm": { width: 1, height: 1, bytes: 4 },
	"rgba8uint": { width: 1, height: 1, bytes: 4 },
	"rgba8sint": { width: 1, height: 1, bytes: 4 },
	"bgra8unorm": { width: 1, height: 1, bytes: 4 },
	"bgra8unorm-srgb": { width: 1, height: 1, bytes: 4 },
	"rgb10a2unorm": { width: 1, height: 1, bytes: 4 },
	"rgb10a2uint": { width: 1, height: 1, bytes: 4 },
	"rg11b10ufloat": { width: 1, height: 1, bytes: 4 },
	"rgb9e5ufloat": { width: 1, height: 1, bytes: 4 },
	"rg16unorm": { width: 1, height: 1, bytes: 4 },
	"rg16snorm": { width: 1, height: 1, bytes: 4 },
	"rg16uint": { width: 1, height: 1, bytes: 4 },
	"rg16sint": { width: 1, height: 1, bytes: 4 },
	"rg16float": { width: 1, height: 1, bytes: 4 },
	"rg32uint": { width: 1, height: 1, bytes: 8 },
	"rg32sint": { width: 1, height: 1, bytes: 8 },
	"rg32float": { width: 1, height: 1, bytes: 8 },
	"rgba16unorm": { width: 1, height: 1, bytes: 8 },
	"rgba16snorm": { width: 1, height: 1, bytes: 8 },
	"rgba16uint": { width: 1, height: 1, bytes: 8 },
	"rgba16sint": { width: 1, height: 1, bytes: 8 },
	"rgba16float": { width: 1, height: 1, bytes: 8 },
	"rgba32uint": { width: 1, height: 1, bytes: 16 },
	"rgba32sint": { width: 1, height: 1, bytes: 16 },
	"rgba32float": { width: 1, height: 1, bytes: 16 },
	"stencil8": { width: 1, height: 1, bytes: 1 },
	"depth16unorm": { width: 1, height: 1, bytes: 2 },
	"depth24plus": { width: 1, height: 1, bytes: 4 },
	"depth24plus-stencil8": { width: 1, height: 1, bytes: 4 },
	"depth32float": { width: 1, height: 1, bytes: 4 },
	"depth32float-stencil8": { width: 1, height: 1, bytes: 5 },
	"bc1-rgba-unorm": { width: 4, height: 4, bytes: 8 },
	"bc1-rgba-unorm-srgb": { width: 4, height: 4, bytes: 8 },
	"bc2-rgba-unorm": { width: 4, height: 4, bytes: 16 },
	"bc2-rgba-unorm-srgb": { width: 4, height: 4, bytes: 16 },
	"bc3-rgba-unorm": { width: 4, height: 4, bytes: 16 },
	"bc3-rgba-unorm-srgb": { width: 4, height: 4, bytes: 16 },
	"bc4-r-unorm": { width: 4, height: 4, bytes: 8 },
	"bc4-r-snorm": { width: 4, height: 4, bytes: 8 },
	"bc5-rg-unorm": { width: 4, height: 4, bytes: 16 },
	"bc5-rg-snorm": { width: 4, height: 4, bytes: 16 },
	"bc6h-rgb-ufloat": { width: 4, height: 4, bytes: 16 },
	"bc6h-rgb-sfloat": { width: 4, height: 4, bytes: 16 },
	"bc7-rgba-unorm": { width: 4, height: 4, bytes: 16 },
	"bc7-rgba-unorm-srgb": { width: 4, height: 4, bytes: 16 },
	"etc2-rgb8unorm": { width: 4, height: 4, bytes: 8 },
	"etc2-rgb8unorm-srgb": { width: 4, height: 4, bytes: 8 },
	"etc2-rgb8a1unorm": { width: 4, height: 4, bytes: 8 },
	"etc2-rgb8a1unorm-srgb": { width: 4, height: 4, bytes: 8 },
	"etc2-rgba8unorm": { width: 4, height: 4, bytes: 16 },
	"etc2-rgba8unorm-srgb": { width: 4, height: 4, bytes: 16 },
	"eac-r11unorm": { width: 4, height: 4, bytes: 8 },
	"eac-r11snorm": { width: 4, height: 4, bytes: 8 },
	"eac-rg11unorm": { width: 4, height: 4, bytes: 16 },
	"eac-rg11snorm": { width: 4, height: 4, bytes: 16 }
} as const;

/*
================
astcBlock

The ASTC block shape rides in the format's name (astc-6x6-unorm):
its block, or null for every other format.
================
*/
function astcBlock( format: GPUTextureFormat ): TexelBlock | null {
	if ( !format.startsWith( "astc-" ) ) return null;
	const dims = format.slice( 5 ).split( "-", 1 )[0]!.split( "x" );
	const width = Number( dims[0] ), height = Number( dims[1] );
	if ( dims.length !== 2 || !Number.isInteger( width ) || !Number.isInteger( height ) || width < 1 || height < 1 ) {
		return null;
	}
	return { width, height, bytes: 16 };
}

/*
================
textureAllocationBytes

One texture descriptor's estimated footprint: every mip level at the
format's block size, the layer count held across levels for an array
and the depth halved with the level for a 3-D texture. An unknown
format estimates as one byte a texel; the total stays the right order
of magnitude whatever a future format does.
================
*/
export function textureAllocationBytes( descriptor: GPUTextureDescriptor ): number {
	const block: TexelBlock = astcBlock( descriptor.format ) ??
		TEXEL_BLOCKS[descriptor.format as keyof typeof TEXEL_BLOCKS] ?? { width: 1, height: 1, bytes: 1 };
	const size = descriptor.size;
	const extent = Array.isArray( size ) ?
		{ width: size[0] ?? 1, height: size[1] ?? 1, depthOrArrayLayers: size[2] ?? 1 } :
		{
			width: (size as GPUExtent3DDict).width ?? 1,
			height: (size as GPUExtent3DDict).height ?? 1,
			depthOrArrayLayers: (size as GPUExtent3DDict).depthOrArrayLayers ?? 1
		};
	const levels = Math.max( 1, descriptor.mipLevelCount ?? 1 );
	const is3D = (descriptor.dimension ?? "2d") === "3d";
	let bytes = 0;
	for ( let level = 0; level < levels; level++ ) {
		const width = Math.max( 1, extent.width >> level );
		const height = Math.max( 1, extent.height >> level );
		const depth = is3D ? Math.max( 1, extent.depthOrArrayLayers >> level ) : extent.depthOrArrayLayers;
		bytes += Math.ceil( width / block.width ) * Math.ceil( height / block.height ) * block.bytes * depth;
	}
	return bytes;
}

/*
================
countDestroy

Wraps one resource's destroy so the first call returns its bytes to
the totals, and returns false when the object refuses the wrap (a
frozen host object): the caller then leaves the resource uncounted,
so a destroy that breaks never takes the renderer's resource with it.
================
*/
function countDestroy( resource: GPUBuffer | GPUTexture, drop: () => void ): boolean {
	const destroy = resource.destroy.bind( resource );
	let dropped = false;
	try {
		resource.destroy = () => {
			if ( !dropped ) {
				dropped = true;
				drop();
			}
			destroy();
		};
	} catch {
		return false;
	}
	return true;
}

/*
================
instrumentGpuAllocations

Counts one device's allocations into the totals: createBuffer and
createTexture note their bytes, and each resource's wrapped destroy
returns them. WebGPU hands the device object to every allocation
site in the renderer, so wrapping its two creation methods here is
the one funnel every allocation passes. A device (or a resource)
that refuses to be wrapped stays unwrapped and the totals simply
miss it; a diagnostics counter must never take the renderer down.
================
*/
export function instrumentGpuAllocations( device: GPUDevice, allocations: GpuAllocations ): void {
	const createBuffer = device.createBuffer.bind( device );
	const createTexture = device.createTexture.bind( device );
	try {
		device.createBuffer = ( descriptor: GPUBufferDescriptor ): GPUBuffer => {
			const buffer = createBuffer( descriptor );
			const bytes = descriptor.size;
			if ( countDestroy( buffer, () => allocations.dropBuffer( bytes ) ) ) {
				allocations.noteBuffer( bytes );
			}
			return buffer;
		};
		device.createTexture = ( descriptor: GPUTextureDescriptor ): GPUTexture => {
			const texture = createTexture( descriptor );
			const bytes = textureAllocationBytes( descriptor );
			if ( countDestroy( texture, () => allocations.dropTexture( bytes ) ) ) {
				allocations.noteTexture( bytes );
			}
			return texture;
		};
	} catch {
		// The unpatched device stays as it was; creation still works.
		try {
			device.createBuffer = createBuffer;
			device.createTexture = createTexture;
		} catch {
			// Nothing more can be restored; the methods are unchanged
			// where the assignment itself was refused.
		}
	}
}
