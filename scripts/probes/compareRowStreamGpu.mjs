/*
===========================================================================
compareRowStreamGpu.mjs - exact GPU comparison of two reviewed git trees
No application source interception: only the empty fixture document is supplied.
===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { launchProbeBrowser } from "../lib/probeBrowser.mjs";
const [baselineRoot, candidateRoot, baselineHead] = process.argv.slice( 2 );
if ( !baselineRoot || !candidateRoot || !baselineHead ) {
	throw Error(
		"Usage: node scripts/probes/compareRowStreamGpu.mjs <baseline-root> <candidate-root> <baseline-head>"
	);
}
const roots = [ path.resolve( baselineRoot ), path.resolve( candidateRoot ) ];
const requireClient = createRequire( path.join( roots[1], "apps/client-next/package.json" ) );
const { createServer } = await import( pathToFileURL( requireClient.resolve( "vite" ) ).href );
const output = path.join( roots[1], ".state", "row-stream-gpu-comparison" );
await mkdir( output, { recursive: true } );
const captures = [];
for ( const root of roots ) {
	const head = execFileSync( "git", [ "rev-parse", "HEAD" ], { cwd: root, encoding: "utf8" } ).trim();
	if ( root === roots[0] ) assert.equal( head, baselineHead );
	const server = await createServer( {
		root: root + "/apps/client-next",
		configFile: root + "/apps/client-next/vite.config.mjs",
		server: { host: "127.0.0.1", port: 0, strictPort: false }
	} );
	let browser;
	try {
		await server.listen();
		const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
		const launch = await launchProbeBrowser( { viewport: { width: 256, height: 256 } } );
		browser = launch.browser;
		const page = launch.page;
		const errors = [];
		page.on( "pageerror", e => errors.push( e.message ) );
		await page.route(
			origin + "/",
			route => route.fulfill( { contentType: "text/html", body: "<!doctype html><html><body></body></html>" } )
		);
		await page.goto( origin );
		const rows = await page.evaluate( async () => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const { createUiResources } = await import( "/src/engine/runtime/renderer/device/ui.ts" );
			const { NATIVE_CHARACTER_LIGHTING } = await import( "/src/engine/foundation/rendering/video-options.ts" );
			if ( NATIVE_CHARACTER_LIGHTING ) throw Error( "Fixture expects current shipping lighting setting" );
			const pixels = [], size = 128;
			const canvas = document.createElement( "canvas" ), copy = document.createElement( "canvas" );
			copy.width = copy.height = size;
			const context = copy.getContext( "2d", { willReadFrequently: true } );
			if ( !context ) throw Error( "No readback context" );
			const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
			const renderer = createRenderer( canvas, createPresentationRandom( 1 ) );
			/*
			================
			identity
			================
			*/
			const I = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
			const model = {
				nodes: [ {
					name: "root",
					parent: -1,
					translation: [ 0, 0, 0 ],
					rotation: [ 0, 0, 0, 1 ],
					scale: [ 1, 1, 1 ]
				} ],
				images: [],
				clips: [],
				primitives: [ -1, 1 ].map( ( side, index ) => ({
					name: "piece" + index,
					node: 0,
					image: -1,
					joints: [ 0 ],
					inverseBind: I(),
					geometry: {
						positions: Float32Array.of( side * 8 - 7, -12, 0, side * 8 + 7, -12, 0, side * 8, 12, 0 ),
						normals: Float32Array.of( 0, 0, -1, 0, 0, -1, 0, 0, -1 ),
						joints: new Uint32Array( 12 ),
						weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
						indices: Uint32Array.of( 0, 1, 2 ),
						transform: I(),
						material: {
							color: index ? [ .2, .8, .3, 1 ] : [ .8, .2, .3, 1 ],
							ambient: [ .2, .2, .2 ],
							objectLight: 1,
							alphaCutoff: 0,
							blend: false,
							doubleSided: true,
							unlit: false,
							fogDisabled: true
						}
					}
				}) )
			};
			/*
			================
			actor
			================
			*/
			const actor = ( gid, x, opacity = 1, lit = false ) => ({
				gid,
				model: "body",
				clip: "",
				time: 0,
				loop: false,
				scale: 1,
				opacity,
				pose: { regionId: 257, x, y: 0, z: 0, yaw: 0 },
				...(lit ?
					{
						pointLight: {
							pose: { regionId: 257, x: x + 1, y: 2, z: -20, yaw: 0 },
							ambient: [ .1, .3, .6 ],
							diffuse: [ .2, .1, .4 ],
							attenuation: .2,
							range: 1000
						}
					} :
					{})
			});
			/*
			================
			snapshot
			================
			*/
			async function snapshot( name ) {
				const image = await createImageBitmap( canvas );
				context.drawImage( image, 0, 0 );
				image.close();
				pixels.push( { name, rgba: [ ...context.getImageData( 0, 0, size, size ).data ] } );
			}
			/*
			================
			capture
			================
			*/
			async function capture( name, actors ) {
				renderer.setCharacterActors( actors );
				for ( let i = 0; i < 3; i++ ) await renderer.frame( { width: size, height: size }, 0 );
				if ( renderer.error() ) throw Error( renderer.error() );
				await snapshot( name );
			}
			try {
				const deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "Renderer readiness deadline" );
				renderer.setWorld( { id: "streams", originRegion: 257, groups: [], warnings: [] } );
				renderer.setWorldCamera( {
					eye: [ 0, 0, -100 ],
					target: [ 0, 0, 0 ],
					originRegion: 257,
					fov: 1,
					near: 1,
					far: 500
				} );
				renderer.setCharacterModel( "body", model, [] );
				await capture( "empty", [] );
				await capture( "opaque-two", [ actor( 1, -23 ), actor( 2, 23 ) ] );
				await capture( "half-two", [ actor( 1, -23, .5, true ), actor( 2, 23, .5 ) ] );
				await capture( "grow-four", [
					actor( 1, -36, .2, true ),
					actor( 2, -12, .4 ),
					actor( 3, 12, .6, true ),
					actor( 4, 36, .8 )
				] );
				await capture( "reorder-four", [
					actor( 4, 36, .8 ),
					actor( 3, 12, .6 ),
					actor( 2, -12, .4, true ),
					actor( 1, -36, .2 )
				] );
				await capture( "shrink-one-lit", [ actor( 3, 0, .5, true ) ] );
				await capture( "expire-one-light", [ actor( 3, 0, .5 ) ] );
				await capture( "restore-opaque", [ actor( 1, -23 ), actor( 2, 23 ) ] );
				const { defaultVideoOptions, changeVideo } = await import(
					"/src/engine/foundation/rendering/video-options.ts"
				);
				const cloth = {
					mobility: [ 1, 1, 0, 0 ],
					pins: [ 0, 0, 1, 1 ],
					constraints: [ [ 2, 1, 2 ], [ 3, 0, 2 ], [ 0, 1, 2 ] ],
					order: [ 0, 1, 2 ],
					force: [ 1, 0, 0 ],
					gravity: 20,
					gravityMobility: 0,
					windMobility: 0,
					damping: .9,
					windPeriod: 1
				};
				renderer.setCharacterModel( "cloth", {
					nodes: model.nodes,
					clips: [],
					images: [],
					primitives: [ {
						name: "cloth",
						node: 0,
						joints: [ 0 ],
						inverseBind: I(),
						image: -1,
						cloth,
						geometry: {
							positions: Float32Array.of( -1, 0, 3, 1, 0, 3, 1, 2, 3, -1, 2, 3 ),
							normals: new Float32Array( 12 ).fill( 1 ),
							uvs: new Float32Array( 8 ),
							indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
							transform: I(),
							joints: new Uint32Array( 16 ),
							weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
							material: {
								color: [ 1, 0, 0, 1 ],
								alphaCutoff: 0,
								blend: false,
								doubleSided: true,
								unlit: true,
								fogDisabled: true
							}
						}
					} ]
				}, [] );
				renderer.setWorldCamera( {
					eye: [ 0, 1, -5 ],
					target: [ 0, 1, 3 ],
					originRegion: 257,
					near: .1,
					far: 100,
					fov: 1
				} );
				renderer.setCharacterActors( [ { ...actor( 10, 0, .5 ), model: "cloth" } ] );
				let clothFrame = 0;
				for (
					const [name, enabled] of [ [ "cloth-static", false ], [ "cloth-moving", true ], [
						"cloth-restored",
						false
					] ]
				) {
					renderer.videoOptions( changeVideo( defaultVideoOptions(), 12, Number( enabled ) ) );
					for ( let i = 0; i < 40; i++, clothFrame++ ) {
						await renderer.frame( { width: size, height: size }, clothFrame * .05 );
						if ( renderer.error() ) throw Error( renderer.error() );
					}
					await snapshot( name );
				}
			} finally {
				renderer.dispose();
			}
			const adapter = await navigator.gpu.requestAdapter();
			if ( !adapter ) throw Error( "No GPU adapter" );
			const device = await adapter.requestDevice();
			const gpuErrors = [];
			device.addEventListener( "uncapturederror", e => gpuErrors.push( e.error.message ) );
			const ui = createUiResources( device, "rgba8unorm" );
			await ui.ready;
			const target = device.createTexture( {
				size: [ 64, 64 ],
				format: "rgba8unorm",
				usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC
			} );
			const depth = device.createTexture( {
				size: [ 64, 64 ],
				format: "depth24plus",
				usage: GPUTextureUsage.RENDER_ATTACHMENT
			} );
			const buffer = device.createBuffer( {
				size: 256 * 64,
				usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ
			} );
			try {
				let revision = 0;
				for (
					const [name, width, height, color, remove] of [
						[ "portrait-first", 32, 32, [ 1, 0, 0, 1 ], false ],
						[ "portrait-repeat", 32, 32, [ 0, 1, 0, 1 ], false ],
						[ "portrait-resize", 16, 24, [ 0, 0, 1, 1 ], false ],
						[ "portrait-restore", 32, 32, [ 1, 0, 0, 1 ], false ],
						[ "portrait-recreate", 32, 32, [ 0, 1, 0, 1 ], true ]
					]
				) {
					if ( remove ) ui.texture( "__fixture", null );
					const view = ui.portraitTarget( "__fixture", width, height );
					const encoder = device.createCommandEncoder();
					const fill = encoder.beginRenderPass( {
						colorAttachments: [ { view, loadOp: "clear", storeOp: "store", clearValue: color } ]
					} );
					fill.end();
					const draws = ui.prepare( {
						revision: ++revision,
						width: 64,
						height: 64,
						quads: [ {
							texture: "__fixture",
							rect: [ 8, 8, 48, 48 ],
							uv: [ 0, 0, 1, 1 ],
							color: [ 1, 1, 1, 1 ],
							clip: [ 0, 0, 64, 64 ]
						} ]
					} );
					const pass = encoder.beginRenderPass( {
						colorAttachments: [ {
							view: target.createView(),
							loadOp: "clear",
							storeOp: "store",
							clearValue: [ 0, 0, 0, 1 ]
						} ],
						depthStencilAttachment: {
							view: depth.createView(),
							depthClearValue: 1,
							depthLoadOp: "clear",
							depthStoreOp: "store"
						}
					} );
					for ( const draw of draws ) {
						pass.setPipeline( draw.pipeline );
						pass.setBindGroup( 0, draw.binding );
						pass.draw( 6, draw.count, 0, draw.first );
					}
					pass.end();
					encoder.copyTextureToBuffer( { texture: target }, { buffer, bytesPerRow: 256 }, [ 64, 64 ] );
					device.queue.submit( [ encoder.finish() ] );
					await buffer.mapAsync( GPUMapMode.READ );
					pixels.push( { name, rgba: [ ...new Uint8Array( buffer.getMappedRange() ) ] } );
					buffer.unmap();
				}
				await device.queue.onSubmittedWorkDone();
				if ( gpuErrors.length ) throw Error( JSON.stringify( gpuErrors ) );
			} finally {
				buffer.destroy();
				depth.destroy();
				target.destroy();
				ui.dispose();
				device.destroy();
			}
			return pixels;
		} );
		assert.deepEqual( errors, [] );
		const byName = Object.fromEntries( rows.map( r => [ r.name, r.rgba ] ) );
		assert.notDeepEqual( byName["opaque-two"], byName.empty, "Actors must be visible" );
		assert.notDeepEqual( byName["half-two"], byName["opaque-two"], "Opacity must affect visible output" );
		assert.deepEqual( byName["restore-opaque"], byName["opaque-two"], "Opaque actors restore exactly" );
		assert.deepEqual(
			byName["shrink-one-lit"],
			byName["expire-one-light"],
			"Shipping shader disables actor hit lighting"
		);
		assert.notDeepEqual( byName["cloth-static"], byName["cloth-moving"], "Cloth must visibly animate" );
		assert.deepEqual(
			byName["cloth-static"],
			byName["cloth-restored"],
			"Disabling dynamics restores original geometry"
		);
		assert.notDeepEqual(
			byName["portrait-first"],
			byName["portrait-repeat"],
			"Cached target must show newly rendered contents"
		);
		assert.deepEqual( byName["portrait-first"], byName["portrait-restore"] );
		assert.deepEqual( byName["portrait-repeat"], byName["portrait-recreate"] );
		await writeFile( `${output}/${head}.json`, JSON.stringify( { head, rows } ) );
		captures.push( { head, rows } );
		console.log( `Captured ${rows.length} cases from ${head}` );
	} finally {
		await browser?.close();
		await server.close();
	}
}
assert.deepEqual( captures[1].rows, captures[0].rows, "Every GPU channel must match the baseline exactly" );
await writeFile(
	`${output}/result.json`,
	JSON.stringify(
		{
			baseline: captures[0].head,
			candidate: captures[1].head,
			cases: captures[0].rows.map( r => r.name ),
			changedChannels: 0
		},
		null,
		2
	)
);
console.log( "PASS: all production GPU captures match baseline at zero tolerance" );
