/*
===========================================================================

images.ts - GPU image allocation, native mip upload and resource retirement

All world and character images pass through this owner. Compressed adapters
receive the original blocks; other adapters receive temporary decoded mips.
Retained CPU sources belong to the caller and survive device loss.

An image has one owner, which releases it, and any number of device draws
that bind it (ImageLeases). The texture is retired when the owner has
released it and no draw binds it any more, so an owner may drop an image
while a resident draw still names it: a draw's bind group can be rebuilt
at any time (texture settings, water reflection) and must find its texture.

===========================================================================
*/
import type { ImageCommands, ImageDraw } from "@/engine/runtime/renderer/internal/gpu-contract";
import type { WorldTexture } from "@/engine/contracts/texture";
import {
	decodeNativeTextureLevel,
	nativeTextureBlockBytes,
	validateNativeTexture
} from "@/engine/foundation/assets/native-texture";
import { destroyNow, type Retire } from "./retirement";

const BLOCK_SIDE = 4;
const CHANNELS = 4;

/*
================
ImageLeases

What device draws need from this owner: the texture of a live image, and a
counted claim held for as long as a draw binds it.
================
*/
export interface ImageLeases {
	readonly texture: ( image: ImageDraw ) => GPUTexture;
	readonly acquire: ( image: ImageDraw ) => void;
	readonly drop: ( image: ImageDraw ) => void;
}

/*
================
ImageEntry

released is the owner's release; uses counts the draws binding the image.
================
*/
interface ImageEntry {
	readonly texture: GPUTexture;
	uses: number;
	released: boolean;
}

/*
================
ImageDevice

The device owner grants these capabilities for one device generation.
The mip generator is used only for bitmaps, never authored native levels.
================
*/
interface ImageDevice {
	// A frame may still sample a released texture: the device decides when it dies.
	readonly retire?: Retire;
	readonly current: () => GPUDevice;
	readonly fail: ( error: unknown ) => void;
	readonly pipeline: () => GPURenderPipeline;
	readonly sampler: GPUSampler;
	readonly generateMips: ( texture: GPUTexture, levels: number, layers: number ) => void;
}

/*
================
createImages

Own each GPU allocation until release or shutdown. Upload failures destroy
the partially initialized allocation before propagating to the caller.
================
*/
export function createImages( device: ImageDevice ) {
	const textures = new Map<ImageDraw, ImageEntry>(), retire = device.retire ?? destroyNow;
	/*
	================
	retireUnused

	The last of the owner's release and the binding draws' drops retires.
	================
	*/
	function retireUnused( draw: ImageDraw, entry: ImageEntry ) {
		if ( !entry.released || entry.uses > 0 ) return;
		retire( entry.texture );
		textures.delete( draw );
	}
	/*
	================
	texture

	Only live handles can supply textures to other device-owned passes. A
	released image a draw still binds is live.
	================
	*/
	function texture( image: ImageDraw ) {
		const entry = textures.get( image );
		if ( !entry ) throw Error( "Stale image handle" );
		return entry.texture;
	}
	const leases: ImageLeases = Object.freeze( {
		texture,
		/*
		================
		acquire

		A draw may bind only an image its owner still holds.
		================
		*/
		acquire( image: ImageDraw ) {
			const entry = textures.get( image );
			if ( !entry || entry.released ) throw Error( "Stale image handle" );
			entry.uses++;
		},
		/*
		================
		drop
		================
		*/
		drop( image: ImageDraw ) {
			const entry = textures.get( image );
			if ( !entry || entry.uses === 0 ) throw Error( "Unbalanced image lease" );
			entry.uses--;
			retireUnused( image, entry );
		}
	} );
	const commands: ImageCommands = Object.freeze( {
		/*
		================
		upload

		Validate every layer before allocation, including native mip extents.
		Small native mips copy a complete block into the physical padded extent.
		================
		*/
		upload( image: WorldTexture, frames: readonly WorldTexture[] = [ image ], mipmaps = true ): ImageDraw {
			const gpu = device.current();
			const native = "kind" in image ? image : null;
			if (
				!frames.length || frames.some( frame => frame.width !== image.width || frame.height !== image.height )
			) {
				throw Error( "Animation texture dimensions differ" );
			}
			for ( const source of frames ) {
				if ( "kind" in source ) {
					if ( !native || source.format !== native.format || source.levels.length !== native.levels.length ) {
						throw Error( "Native texture levels differ" );
					}
					validateNativeTexture( source );
				} else if ( native ) {
					throw Error( "Mixed native and bitmap texture array" );
				}
			}
			const blockBytes = native ? nativeTextureBlockBytes( native.format ) : 0;
			// WebGPU requires block-aligned base dimensions. Tiny authored textures
			// therefore use the same fallback as adapters without BC support.
			const decompress = !!blockBytes && (!gpu.features.has( "texture-compression-bc" ) ||
				image.width % BLOCK_SIDE !== 0 || image.height % BLOCK_SIDE !== 0);
			const levels = native ? native.levels.length : mipmaps ?
				1 + Math.floor( Math.log2( Math.max( image.width, image.height ) ) ) :
				1;
			gpu.pushErrorScope( "validation" );
			try {
				const texture = gpu.createTexture( {
					mipLevelCount: levels,
					size: [ image.width, image.height, frames.length ],
					format: decompress ? "rgba8unorm" : native?.format ?? "rgba8unorm",
					usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST |
						(native ? 0 : GPUTextureUsage.RENDER_ATTACHMENT)
				} );
				try {
					for ( let layer = 0; layer < frames.length; layer++ ) {
						const source = frames[layer]!;
						if ( "kind" in source ) {
							for ( let level = 0; level < levels; level++ ) {
								const width = Math.max( 1, image.width >> level ),
									height = Math.max( 1, image.height >> level );
								const compressed = !!blockBytes && !decompress;
								const data = decompress ?
									decodeNativeTextureLevel( source, level ) :
									source.levels[level]!;
								gpu.queue.writeTexture(
									{ texture, mipLevel: level, origin: [ 0, 0, layer ] },
									data as Uint8Array<ArrayBuffer>,
									{
										bytesPerRow: compressed ?
											Math.ceil( width / BLOCK_SIDE ) * blockBytes :
											width * CHANNELS,
										rowsPerImage: compressed ? Math.ceil( height / BLOCK_SIDE ) : height
									},
									[
										compressed ? Math.ceil( width / BLOCK_SIDE ) * BLOCK_SIDE : width,
										compressed ? Math.ceil( height / BLOCK_SIDE ) * BLOCK_SIDE : height
									]
								);
							}
						} else {
							gpu.queue.copyExternalImageToTexture(
								{ source },
								{ texture, origin: [ 0, 0, layer ] },
								[ image.width, image.height ]
							);
						}
					}
					if ( !native && levels > 1 ) device.generateMips( texture, levels, frames.length );
					const binding = gpu.createBindGroup( {
						layout: device.pipeline().getBindGroupLayout( 0 ),
						entries: [
							{ binding: 0, resource: device.sampler },
							{ binding: 1, resource: texture.createView( { dimension: "2d", arrayLayerCount: 1 } ) }
						]
					} );
					const draw = Object.freeze( { pipeline: device.pipeline(), binding } );
					textures.set( draw, { texture, uses: 0, released: false } );
					return draw;
				} catch ( error ) {
					texture.destroy();
					throw error;
				}
			} finally {
				gpu.popErrorScope().then( error => {
					if ( error ) device.fail( error.message );
				} ).catch( device.fail );
			}
		},
		/*
		================
		release

		The owner's release. Releasing a stale or already released handle is
		harmless; the allocation is destroyed once, after its last binding draw.
		================
		*/
		release( draw: ImageDraw ) {
			const entry = textures.get( draw );
			if ( !entry || entry.released ) return;
			entry.released = true;
			retireUnused( draw, entry );
		}
	} );
	return {
		commands,
		texture,
		leases,
		/*
		================
		dispose

		Release every allocation in this device generation, including images
		whose higher-level owners are retaining CPU sources for restoration.
		================
		*/
		dispose() {
			for ( const entry of textures.values() ) entry.texture.destroy();
			textures.clear();
		}
	};
}
