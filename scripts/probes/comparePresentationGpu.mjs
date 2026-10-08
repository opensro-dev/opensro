/*
===========================================================================
comparePresentationGpu.mjs - exact GPU comparison of frame presentation across reviewed trees
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
		"Usage: node scripts/probes/comparePresentationGpu.mjs <baseline-root> <candidate-root> <baseline-head>"
	);
}
const roots = [ path.resolve( baselineRoot ), path.resolve( candidateRoot ) ];
const requireClient = createRequire( path.join( roots[1], "apps/client-next/package.json" ) );
const { createServer } = await import( pathToFileURL( requireClient.resolve( "vite" ) ).href );
const output = path.join( roots[1], ".state", "presentation-gpu-comparison" );
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
			const { defaultVideoOptions, changeVideo } = await import(
				"/src/engine/foundation/rendering/video-options.ts"
			);
			const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
			const canvas = document.createElement( "canvas" );
			const copy = document.createElement( "canvas" );
			const context = copy.getContext( "2d", { willReadFrequently: true } );
			if ( !context ) throw Error( "No readback context" );
			const renderer = createRenderer( canvas, createPresentationRandom( 1 ) );
			const pixels = [];
			/*
			================
			identity
			================
			*/
			const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
			const camera = { eye: [ 0, 0, -80 ], target: [ 0, 0, 0 ], originRegion: 257, fov: 1, near: 1, far: 500 };
			try {
				const deadline = performance.now() + 15000;
				while ( renderer.phase() === "starting" && performance.now() < deadline ) {
					await new Promise( requestAnimationFrame );
				}
				if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "Renderer readiness deadline" );
				renderer.setWorld( { id: "presentation-proof", originRegion: 257, groups: [], warnings: [] } );
				renderer.setWorldCamera( camera );
				renderer.setCharacterModel( "body", {
					nodes: [ {
						name: "root",
						parent: -1,
						translation: [ 0, 0, 0 ],
						rotation: [ 0, 0, 0, 1 ],
						scale: [ 1, 1, 1 ]
					} ],
					images: [],
					clips: [],
					primitives: [ {
						name: "triangle",
						node: 0,
						image: -1,
						joints: [ 0 ],
						inverseBind: identity(),
						geometry: {
							positions: Float32Array.of( -30, -25, 0, 30, -25, 0, 0, 30, 0 ),
							normals: Float32Array.of( 0, 0, -1, 0, 0, -1, 0, 0, -1 ),
							joints: new Uint32Array( 12 ),
							weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
							indices: Uint32Array.of( 0, 1, 2 ),
							transform: identity(),
							material: {
								color: [ .6, .8, .4, 1 ],
								alphaCutoff: 0,
								blend: false,
								doubleSided: true,
								unlit: true,
								fogDisabled: true
							}
						}
					} ]
				}, [] );
				renderer.setCharacterActors( [ {
					gid: 1,
					model: "body",
					clip: "",
					time: 0,
					loop: false,
					scale: 1,
					pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 }
				} ] );
				let revision = 0;
				for ( const size of [ 64, 96, 64 ] ) {
					for ( const preview of [ false, true ] ) {
						for ( const bloom of [ false, true ] ) {
							for ( const finish of [ false, true ] ) {
								renderer.setCharacterPreview( preview ? camera : null );
								renderer.videoOptions( changeVideo( defaultVideoOptions(), 11, Number( bloom ) ) );
								renderer.experimentalVideo( {
									postProcessing: finish,
									anisotropicFiltering: false,
									heightFog: false
								} );
								const clip = [ 0, 0, size, size ];
								/*
								================
								quad
								================
								*/
								const quad = ( rect, color, extra = {} ) => ({
									rect,
									color,
									clip,
									uv: [ 0, 0, 1, 1 ],
									texture: "",
									...extra
								});
								renderer.setUi( {
									revision: ++revision,
									width: size,
									height: size,
									quads: [
										quad( [ 0, 0, size / 2, size ], [ .08, .2, .55, 1 ], { layer: "background" } ),
										quad( [ size / 4, size / 3, size / 2, 7 ], [ .8, .1, .7, .75 ], {
											depth: .9999
										} ),
										quad( [ size / 2 - 10, size / 2 - 8, 20, 16 ], [ .9, .4, .15, .5 ] ),
										quad( [ size - 13, 3, 9, 9 ], [ 1, 1, 1, 1 ] ),
										quad( [ size - 11, 5, 5, 5 ], [ 0, 0, 0, 1 ] )
									]
								} );
								for ( let frame = 0; frame < 3; frame++ ) {
									await renderer.frame( { width: size, height: size }, .16 );
								}
								if ( renderer.error() ) throw Error( renderer.error() );
								copy.width = copy.height = size;
								const image = await createImageBitmap( canvas );
								context.drawImage( image, 0, 0 );
								image.close();
								pixels.push( {
									name: `${revision}:${size}:${preview}:${bloom}:${finish}`,
									size,
									preview,
									bloom,
									finish,
									rgba: [ ...context.getImageData( 0, 0, size, size ).data ]
								} );
							}
						}
					}
				}
			} finally {
				renderer.dispose();
			}
			return pixels;
		} );
		assert.deepEqual( errors, [] );
		assert.equal( rows.length, 24 );
		assert.notDeepEqual( rows[0].rgba, rows[1].rgba, "Finish must affect the composed HUD and scene" );
		assert.notDeepEqual( rows[0].rgba, rows[2].rgba, "Native bloom must affect the scene" );
		assert.notDeepEqual(
			rows[0].rgba,
			rows[4].rgba,
			"Preview must cover background art rather than reverse their order"
		);
		for ( let i = 0; i < 8; i++ ) {
			assert.deepEqual( rows[i].rgba, rows[i + 16].rgba, "Resize restoration must preserve every channel" );
		}
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
console.log( "PASS: all presentation captures match baseline at zero tolerance" );
