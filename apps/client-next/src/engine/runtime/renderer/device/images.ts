/*
===========================================================================

images.ts - GPU image allocation, native mip upload and resource retirement

All world and character images pass through this owner. Compressed adapters
receive the original blocks; other adapters receive temporary decoded mips.
Retained CPU sources belong to the caller and survive device loss.

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
	const textures = new Map<ImageDraw, GPUTexture>(), retire = device.retire ?? destroyNow;
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
					textures.set( draw, texture );
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

		Retiring a stale handle is harmless; the allocation is destroyed once.
		================
		*/
		release( draw: ImageDraw ) {
			const texture = textures.get( draw );
			if ( texture ) retire( texture );
			textures.delete( draw );
		}
	} );
	return {
		commands,
		/*
		================
		texture

		Only live handles can supply textures to other device-owned passes.
		================
		*/
		texture( image: ImageDraw ) {
			const texture = textures.get( image );
			if ( !texture ) throw Error( "Stale image handle" );
			return texture;
		},
		/*
		================
		dispose

		Release every allocation in this device generation, including images
		whose higher-level owners are retaining CPU sources for restoration.
		================
		*/
		dispose() {
			for ( const texture of textures.values() ) texture.destroy();
			textures.clear();
		}
	};
}
