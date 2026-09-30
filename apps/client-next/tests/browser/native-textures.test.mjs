/*
===========================================================================

native-textures.test.mjs - real GPU sampling of native character mip resources

The shared probe launcher owns Chrome. Each device exercises the production
image uploader and shader, including the final sub-block mips. A second device
omits BC support to exercise fallback without a development rendering flag.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

test( "native BC textures and fallback sample every mip without validation errors", { timeout: 60000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createImages } = await import( "/src/engine/runtime/renderer/device/images.ts" );
			const { createPipelines } = await import( "/src/engine/runtime/renderer/device/pipelines.ts" );
			const { decodeNativeTexture } = await import( "/src/engine/foundation/assets/native-texture.ts" );
			const adapter = await navigator.gpu.requestAdapter();
			if ( !adapter ) throw Error( "No GPU adapter" );
			const rows = [], errors = [];
			const BC_FEATURE = "texture-compression-bc";
			const codes = [ 0x31545844, 0x33545844, 0x35545844 ];
			const HEADER_BYTES = 20, ROW_BYTES = 256, TARGET_SIDE = 2;

			/*
			================
			textureBytes

			Independent fixture encoder: opaque red in each authored mip block.
			================
			*/
			function textureBytes( code ) {
				const blockBytes = code === codes[0] ? 8 : 16;
				const bytes = new Uint8Array( HEADER_BYTES + 3 * blockBytes ), view = new DataView( bytes.buffer );
				[ 0x3158544e, 4, 4, code, 3 ].forEach( ( value, index ) => view.setUint32( index * 4, value, true ) );
				for ( let level = 0; level < 3; level++ ) {
					const offset = HEADER_BYTES + level * blockBytes;
					if ( code === codes[1] ) bytes.fill( 255, offset, offset + 8 );
					if ( code === codes[2] ) bytes[offset] = 255;
					const colors = offset + (blockBytes === 8 ? 0 : 8);
					view.setUint16( colors, 0xf800, true );
					view.setUint16( colors + 2, 0xf800, true );
				}
				return bytes;
			}

			for ( const compressed of [ true, false ] ) {
				if ( compressed && !adapter.features.has( BC_FEATURE ) ) continue;
				const selectedAdapter = await navigator.gpu.requestAdapter();
				if ( !selectedAdapter ) throw Error( "No GPU adapter for upload case" );
				const device = await selectedAdapter.requestDevice( {
					requiredFeatures: compressed ? [ BC_FEATURE ] : []
				} );
				device.addEventListener( "uncapturederror", event => errors.push( event.error.message ) );
				const pipelines = createPipelines( device, "rgba8unorm" );
				await pipelines.ready;
				const depth = device.createTexture( {
					size: [ TARGET_SIDE, TARGET_SIDE ],
					format: "depth24plus",
					usage: GPUTextureUsage.RENDER_ATTACHMENT
				} );
				const images = createImages( {
					current: () => device,
					fail: error => errors.push( String( error ) ),
					pipeline: pipelines.image,
					sampler: pipelines.sampler,
					generateMips: () => {
						throw Error( "Native textures must not generate GPU mips" );
					}
				} );
				try {
					for ( const code of codes ) {
						const source = decodeNativeTexture( textureBytes( code ) );
						const draw = images.commands.upload( source );
						const texture = images.texture( draw );
						if ( !texture ) throw Error( "Native upload has no allocation" );
						for ( let level = 0; level < source.levels.length; level++ ) {
							const target = device.createTexture( {
								size: [ TARGET_SIDE, TARGET_SIDE ],
								format: "rgba8unorm",
								usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC
							} );
							const readback = device.createBuffer( {
								size: ROW_BYTES * TARGET_SIDE,
								usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ
							} );
							try {
								const binding = device.createBindGroup( {
									layout: pipelines.image().getBindGroupLayout( 0 ),
									entries: [
										{ binding: 0, resource: pipelines.sampler },
										{
											binding: 1,
											resource: texture.createView( {
												dimension: "2d",
												baseMipLevel: level,
												mipLevelCount: 1
											} )
										}
									]
								} );
								const encoder = device.createCommandEncoder();
								const pass = encoder.beginRenderPass( {
									colorAttachments: [ {
										view: target.createView(),
										loadOp: "clear",
										storeOp: "store",
										clearValue: [ 0, 0, 0, 0 ]
									} ],
									depthStencilAttachment: {
										view: depth.createView(),
										depthClearValue: 1,
										depthLoadOp: "clear",
										depthStoreOp: "store"
									}
								} );
								pass.setPipeline( pipelines.image() );
								pass.setBindGroup( 0, binding );
								pass.draw( 6 );
								pass.end();
								encoder.copyTextureToBuffer( { texture: target }, {
									buffer: readback,
									bytesPerRow: ROW_BYTES
								}, [ TARGET_SIDE, TARGET_SIDE ] );
								device.queue.submit( [ encoder.finish() ] );
								await readback.mapAsync( GPUMapMode.READ );
								rows.push( {
									compressed,
									format: source.format,
									level,
									pixel: [ ...new Uint8Array( readback.getMappedRange() ).slice( 0, 4 ) ]
								} );
								readback.unmap();
							} finally {
								readback.destroy();
								target.destroy();
							}
						}
						images.commands.release( draw );
					}
					await device.queue.onSubmittedWorkDone();
				} finally {
					images.dispose();
					depth.destroy();
					device.destroy();
				}
			}
			return { bcSupported: adapter.features.has( BC_FEATURE ), rows, errors };
		} );
		await mkdir( "../../temp/artifacts/native-textures", { recursive: true } );
		await writeFile( "../../temp/artifacts/native-textures/gpu.json", JSON.stringify( result, null, 2 ) );
		assert.deepEqual( result.errors, [] );
		assert.equal( result.rows.length, result.bcSupported ? 18 : 9 );
		for ( const row of result.rows ) assert.deepEqual( row.pixel, [ 255, 0, 0, 255 ], JSON.stringify( row ) );
	} finally {
		await browser.close();
	}
} );
