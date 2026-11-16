/*
===========================================================================

glow-row-states.test.mjs - a shared glow batch draws what one-row batches draw

On a real WebGPU device: four glow wearers without cloth share one batch,
whose shader reads each row's glow colour and UV from its row state
(binding 16). Each wearer drawn alone is a one-row batch, which keeps the
glow in its material uniform as before. Inside each wearer's footprint the
two pictures must be equal to the byte. No game server: the real renderer
draws in the page with a preview camera.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

const WIDTH = 320;
const HEIGHT = 160;
const ACTORS = 4;

/*
================
footprint

The bounding box of the pixels that differ from the image's corner (the
cleared background), one pixel wider on each side; undefined when none do.
================
*/
function footprint( rgba ) {
	let left = WIDTH, right = -1, top = HEIGHT, bottom = -1;
	for ( let y = 0; y < HEIGHT; y++ ) {
		for ( let x = 0; x < WIDTH; x++ ) {
			const at = (y * WIDTH + x) * 4;
			if ( [ 0, 1, 2 ].every( c => rgba[at + c] === rgba[c] ) ) continue;
			left = Math.min( left, x );
			right = Math.max( right, x );
			top = Math.min( top, y );
			bottom = Math.max( bottom, y );
		}
	}
	if ( right < 0 ) return undefined;
	return {
		left: Math.max( 0, left - 1 ),
		right: Math.min( WIDTH - 1, right + 1 ),
		top: Math.max( 0, top - 1 ),
		bottom: Math.min( HEIGHT - 1, bottom + 1 )
	};
}

/*
================
inside
================
*/
function inside( box, x, y ) {
	return x >= box.left && x <= box.right && y >= box.top && y <= box.bottom;
}

/*
================
regionDifference

The pixels inside box (or, with outside, outside every box) where the two
images differ in any channel, and the mean RGB of the first image there.
================
*/
function regionDifference( a, b, boxes, test ) {
	let changed = 0, count = 0;
	const sum = [ 0, 0, 0 ];
	for ( let y = 0; y < HEIGHT; y++ ) {
		for ( let x = 0; x < WIDTH; x++ ) {
			if ( !test( boxes, x, y ) ) continue;
			const at = (y * WIDTH + x) * 4;
			count++;
			for ( let c = 0; c < 3; c++ ) sum[c] += a[at + c];
			if ( [ 0, 1, 2, 3 ].some( c => a[at + c] !== b[at + c] ) ) changed++;
		}
	}
	return { changed, count, mean: sum.map( v => v / Math.max( 1, count ) ) };
}

test( "a shared glow batch's row states draw each wearer as its one-row batch does", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async ( { width, height, actors } ) => {
			const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
			const I = [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ];
			// A 16x16 image: the glow pattern varies along both axes, so a UV
			// scroll moves it; the albedo is a flat dark grey.
			const image = glow =>
				createImageBitmap(
					new ImageData(
						Uint8ClampedArray.from( { length: 16 * 16 * 4 }, ( _, i ) => {
							const p = i >> 2, c = i & 3, x = p % 16, y = p >> 4;
							if ( c === 3 ) return 255;
							if ( !glow ) return 40;
							return [ x * 16, y * 16, 255 - x * 8 - y * 8 ][c];
						} ),
						16,
						16
					)
				);
			const geometry = {
				positions: Float32Array.of( -2, 0, 0, 2, 0, 0, 2, 4, 0, -2, 4, 0 ),
				indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
				uvs: Float32Array.of( 0, 1, 1, 1, 1, 0, 0, 0 ),
				transform: Float32Array.from( I ),
				joints: new Uint32Array( 16 ),
				weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
				material: { color: [ 1, 1, 1, 1 ], unlit: true, doubleSided: true, blend: false, alphaCutoff: 0 }
			};
			const model = {
				nodes: [ {
					name: "root",
					parent: -1,
					translation: [ 0, 0, 0 ],
					rotation: [ 0, 0, 0, 1 ],
					scale: [ 1, 1, 1 ]
				} ],
				images: [ { width: 16, height: 16 }, { width: 16, height: 16 } ],
				clips: [ { name: "idle", duration: 1, channels: [] } ],
				primitives: [ {
					name: "weapon",
					node: 0,
					image: 1,
					joints: [ 0 ],
					inverseBind: Float32Array.from( I ),
					geometry,
					equipmentGlow: {
						threshold: 1,
						image: 0,
						uv: [ .02, -.013 ],
						color1: [ 1, .2, 0 ],
						color2: [ 0, .4, 1 ],
						period: 400,
						gain: 1,
						alphaTest: false
					}
				} ]
			};
			// Renderer 0 draws every wearer; renderer i draws wearer i - 1 alone.
			// Each wearer's glow clock sees the same steps in both: the same
			// seconds and the same level of detail, frame by frame.
			const canvases = Array.from( { length: actors + 1 }, () => {
				const canvas = document.createElement( "canvas" );
				canvas.width = width;
				canvas.height = height;
				return canvas;
			} );
			// Observe the WebGPU calls (the browser's API, not the client's
			// source): which device compiles the row-state variant, which bind
			// groups carry binding 16, and which pipelines each renderer sets.
			// Variants compile at first use, inside a renderer's frame: each call
			// is that renderer's (active). Outside a frame none may happen.
			const variantModules = new WeakSet(), variantPipelines = new WeakSet();
			const variants = canvases.map( () => ({ modules: 0, bindGroups: 0, plainSets: 0, variantSets: 0 }) );
			const outside = { modules: 0, bindGroups: 0 };
			let active = -1;
			const owner = () => active >= 0 ? variants[active] : outside;
			const createShaderModule = GPUDevice.prototype.createShaderModule;
			GPUDevice.prototype.createShaderModule = function( d ) {
				const module = createShaderModule.call( this, d );
				if ( d.code.includes( "@binding(16) var<storage,read> rowStates" ) ) {
					variantModules.add( module );
					owner().modules++;
				}
				return module;
			};
			const createRenderPipeline = GPUDevice.prototype.createRenderPipeline;
			GPUDevice.prototype.createRenderPipeline = function( d ) {
				const pipeline = createRenderPipeline.call( this, d );
				if ( variantModules.has( d.vertex.module ) ) variantPipelines.add( pipeline );
				return pipeline;
			};
			const createBindGroup = GPUDevice.prototype.createBindGroup;
			GPUDevice.prototype.createBindGroup = function( d ) {
				if ( [ ...d.entries ].some( e => e.binding === 16 ) ) owner().bindGroups++;
				return createBindGroup.call( this, d );
			};
			for ( const encoder of [ GPURenderBundleEncoder, GPURenderPassEncoder ] ) {
				const setPipeline = encoder.prototype.setPipeline;
				encoder.prototype.setPipeline = function( pipeline ) {
					if ( active >= 0 ) {
						variants[active][variantPipelines.has( pipeline ) ? "variantSets" : "plainSets"]++;
					}
					return setPipeline.call( this, pipeline );
				};
			}
			const renderers = canvases.map( c => createRenderer( c ) );
			const out = document.createElement( "canvas" );
			out.width = width;
			out.height = height;
			const ctx = out.getContext( "2d", { willReadFrequently: true } );
			if ( !ctx ) throw Error( "No 2D context for the readback" );
			try {
				const start = performance.now();
				while ( renderers.some( r => r.phase() === "starting" ) ) {
					if ( performance.now() - start > 15000 ) throw Error( "GPU startup timeout" );
					await new Promise( requestAnimationFrame );
				}
				for ( const r of renderers ) {
					r.setCharacterModel( "m", model, [ await image( true ), await image( false ) ] );
					r.setCharacterPreview( { eye: [ 0, 2, -24 ], target: [ 0, 2, 0 ], near: 1, far: 1000, fov: .8 } );
				}
				// Wearer j's glow runs from frame 3j on: its colour phase and UV
				// scroll differ from every other wearer's by the last frame,
				// where all four draw their glow.
				const FRAMES = 16;
				const wearer = ( j, frame ) => ({
					gid: 10 + j,
					model: "m",
					clip: "idle",
					time: 0,
					loop: true,
					scale: 1,
					pose: { regionId: 0, x: (j - 1.5) * 10, y: 0, z: 0, yaw: 0 },
					animationLod: { fraction: frame >= 3 * j ? 0 : .75, crowded: false }
				});
				const pixels = [], stats = [];
				for ( let frame = 0; frame < FRAMES; frame++ ) {
					for ( let r = 0; r < renderers.length; r++ ) {
						const list = r === 0 ?
							Array.from( { length: actors }, ( _, j ) => wearer( j, frame ) ) :
							[ wearer( r - 1, frame ) ];
						renderers[r].setCharacterActors( list );
						active = r;
						renderers[r].frame( { width, height }, frame / 30 );
						active = -1;
						if ( frame !== FRAMES - 1 ) continue;
						// The canvas is opaque (device.ts alphaMode), so the 2D copy's
						// RGBA8 bytes are the presented bytes, unpremultiplied as-is.
						const bitmap = await createImageBitmap( canvases[r] );
						ctx.clearRect( 0, 0, width, height );
						ctx.drawImage( bitmap, 0, 0 );
						bitmap.close();
						pixels.push( Array.from( ctx.getImageData( 0, 0, width, height ).data ) );
						const s = renderers[r].characterStats();
						stats.push( { frameGroups: s.frameGroups, visibleActors: s.visibleActors, draws: s.draws } );
					}
				}
				return { pixels, stats, variants, outside, errors: renderers.map( r => r.error() ) };
			} finally {
				renderers.forEach( r => r.dispose() );
			}
		}, { width: WIDTH, height: HEIGHT, actors: ACTORS } );
		assert.deepEqual( result.errors, Array( ACTORS + 1 ).fill( null ) );
		// Frame A: one batch of four rows, one draw. Each B: one row.
		assert.deepEqual( result.stats[0], { frameGroups: 1, visibleActors: ACTORS, draws: 1 } );
		for ( let i = 1; i <= ACTORS; i++ ) {
			assert.deepEqual( result.stats[i], { frameGroups: 1, visibleActors: 1, draws: 1 } );
		}
		// The shared renderer compiles and draws the row-state variant; every
		// lone renderer only ever has the plain shader, and no bind group of
		// theirs carries binding 16.
		const [shared, ...lone] = result.variants;
		assert.deepEqual( result.outside, { modules: 0, bindGroups: 0 } );
		assert.equal( shared.modules, 1, JSON.stringify( result.variants ) );
		assert.ok( shared.bindGroups > 0 && shared.variantSets > 0, JSON.stringify( shared ) );
		for ( const one of lone ) {
			assert.equal( one.modules + one.bindGroups + one.variantSets, 0, JSON.stringify( one ) );
			assert.ok( one.plainSets > 0, JSON.stringify( one ) );
		}
		const [all, ...alone] = result.pixels;
		assert.ok( all.every( ( v, i ) => i % 4 !== 3 || v === 255 ), "opaque readback" );
		const boxes = alone.map( one => {
			const box = footprint( one );
			assert.ok( box, "every wearer is on screen" );
			return box;
		} );
		for ( let i = 0; i < ACTORS; i++ ) {
			for ( let j = i + 1; j < ACTORS; j++ ) {
				const [a, b] = [ boxes[i], boxes[j] ];
				assert.ok(
					a.right < b.left || b.right < a.left || a.bottom < b.top || b.bottom < a.top,
					`footprints ${i} and ${j} overlap`
				);
			}
		}
		const rows = alone.map( ( one, i ) => regionDifference( all, one, boxes[i], inside ) );
		const rest = regionDifference( all, alone[0], boxes, ( list, x, y ) => !list.some( b => inside( b, x, y ) ) );
		await mkdir( "temp/artifacts/glow-row-states", { recursive: true } );
		await writeFile(
			"temp/artifacts/glow-row-states/result.json",
			JSON.stringify( { boxes, rows, rest, stats: result.stats, variants: result.variants }, null, 2 )
		);
		for ( const [i, row] of rows.entries() ) {
			assert.ok( row.count > 100, `wearer ${i} covers pixels: ${JSON.stringify( row )}` );
			assert.equal( row.changed, 0, `wearer ${i}: ${JSON.stringify( row )}` );
		}
		assert.equal( rest.changed, 0, `outside the wearers: ${JSON.stringify( rest )}` );
		// The rows differ, so a row reading another row's state changes pixels.
		for ( let i = 0; i < ACTORS; i++ ) {
			for ( let j = i + 1; j < ACTORS; j++ ) {
				assert.notDeepEqual( rows[i].mean, rows[j].mean, `wearers ${i} and ${j} look alike` );
			}
		}
	} finally {
		await browser.close();
	}
} );
