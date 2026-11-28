import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
test(
	"rain and snow produce GPU pixels through retained weather geometry and reset cleanly",
	{ timeout: 90000 },
	async () => {
		const { browser, page } = await launchProbeBrowser();
		try {
			await page.goto( CLIENT_NEXT_BASE_URL );
			const result = await page.evaluate( async () => {
				const { createDevice } = await import( "/src/engine/runtime/renderer/device/device.ts" );
				const { createFrame } = await import( "/src/engine/runtime/renderer/frame/frame.ts" );
				const { createWeather } = await import( "/src/engine/runtime/renderer/weather/weather.ts" );
				const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
				const device = createDevice(), canvas = document.createElement( "canvas" );
				canvas.width = 512;
				canvas.height = 384;
				document.body.append( canvas );
				try {
					for ( let n = 0; n < 300 && device.phase() === "starting"; n++ ) {
						await new Promise( requestAnimationFrame );
					}
					if ( device.phase() !== "running" ) throw Error( device.error() ?? "Device startup timeout" );
					const ctx = canvas.getContext( "webgpu" );
					device.surfaceCommands().configure( ctx, device.format() );
					const depth = device.surfaceCommands().createDepth( 512, 384 ),
						frame = createFrame( device.commands() ),
						images = new Map();
					for ( const name of [ "rain1", "snow1", "snow2" ] ) {
						const path = "/assets/images/Map_extracted/weather/" + name + ".png";
						const response = await fetch( path );
						if ( !response.ok ) throw Error( "Missing weather texture " + path );
						const bitmap = await createImageBitmap( await response.blob(), {
							premultiplyAlpha: "none",
							colorSpaceConversion: "none"
						} );
						images.set( path, device.images().upload( bitmap ) );
						bitmap.close();
					}
					const camera = { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], near: 1, far: 2000, fov: 1 },
						matrix = Float32Array.from( [ .005, 0, 0, 0, 0, .005, 0, 0, 0, 0, .001, 0, 0, 0, .5, 1 ] );
					device.worldView( matrix, new Float32Array( 80 ) );
					async function capture( draws ) {
						frame.draw( ctx.getCurrentTexture().createView(), undefined, undefined, depth.view, draws );
						const bitmap = await createImageBitmap( canvas ), copy = document.createElement( "canvas" );
						copy.width = 512;
						copy.height = 384;
						const c = copy.getContext( "2d" );
						c.drawImage( bitmap, 0, 0 );
						bitmap.close();
						return c.getImageData( 0, 0, 512, 384 ).data;
					}
					const background = await capture( [] ), rows = [];
					for ( const mode of [ 2, 3 ] ) {
						const weather = createWeather( createPresentationRandom( 77 ) );
						weather.set( { mode, amount: 80 } );
						let draws = [];
						for ( let i = 0; i <= 120; i++ ) {
							weather.update( camera, i / 60, 0, true );
							draws = weather.prepare( device.geometry(), images, camera, matrix );
							if ( i < 120 ) {
								frame.draw(
									ctx.getCurrentTexture().createView(),
									undefined,
									undefined,
									depth.view,
									draws
								);
							}
						}
						const pixels = await capture( draws );
						let changed = 0;
						for ( let i = 0; i < pixels.length; i += 4 ) {
							if (
								pixels[i] !== background[i] || pixels[i + 1] !== background[i + 1] ||
								pixels[i + 2] !== background[i + 2]
							) changed++;
						}
						rows.push( { mode, changed, draws: draws.length, ...weather.stats() } );
						if ( weather.stats().refused ) throw Error( "A submitted frame refused its weather step" );
						weather.set( null );
						weather.update( camera, 3, 0, true );
						const empty = weather.prepare( device.geometry(), images, camera, matrix );
						if ( empty.length ) throw Error( "Weather survived reset" );
						weather.dispose( device.geometry() );
					}
					return { rows, error: device.error() };
				} finally {
					device.dispose();
					canvas.remove();
				}
			} );
			await mkdir( "temp/artifacts/live-weather", { recursive: true } );
			await writeFile( "temp/artifacts/live-weather/result.json", JSON.stringify( result, null, 2 ) );
			console.log( JSON.stringify( result ) );
			assert.equal( result.error, null );
			assert.ok(
				result.rows.every( r => r.changed > 0 && r.draws > 0 && r.draws <= 2 && r.windows.some( w => w > 0 ) )
			);
		} finally {
			await browser.close();
		}
	}
);

test( "thunder blends the scene through the production device/frame owners", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createDevice } = await import( "/src/engine/runtime/renderer/device/device.ts" );
			const { createFrame } = await import( "/src/engine/runtime/renderer/frame/frame.ts" );
			const device = createDevice(), canvas = document.createElement( "canvas" );
			canvas.width = 32;
			canvas.height = 24;
			document.body.append( canvas );
			try {
				for ( let n = 0; n < 300 && device.phase() === "starting"; n++ ) {
					await new Promise( requestAnimationFrame );
				}
				if ( device.phase() !== "running" ) throw Error( device.error() ?? "Device startup timeout" );
				const context = canvas.getContext( "webgpu" );
				device.surfaceCommands().configure( context, device.format() );
				const frame = createFrame( device.commands() ),
					depth = device.surfaceCommands().createDepth( 32, 24 ),
					samples = [];
				for (
					const color of [ [ 1, 1, 1, 0 ], [ 156 / 255, 156 / 255, 156 / 255, 128 / 255 ], [ 0, 0, 0, 1 ] ]
				) {
					frame.draw(
						context.getCurrentTexture().createView(),
						undefined,
						undefined,
						depth.view,
						[],
						[],
						[],
						undefined,
						device.thunder( color )
					);
					const bitmap = await createImageBitmap( canvas ), copy = document.createElement( "canvas" );
					copy.width = 32;
					copy.height = 24;
					const ctx = copy.getContext( "2d" );
					ctx.drawImage( bitmap, 0, 0 );
					bitmap.close();
					samples.push( [ ...ctx.getImageData( 16, 12, 1, 1 ).data ] );
				}
				return { samples, error: device.error() };
			} finally {
				device.dispose();
				canvas.remove();
			}
		} );
		assert.equal( result.error, null );
		assert.deepEqual( result.samples[2], [ 0, 0, 0, 255 ] );
		const expected = result.samples[0].slice( 0, 3 ).map( v =>
			Math.min( 255, Math.round( v * 156 / 255 + 156 * 128 / 255 ) )
		);
		assert.ok( expected.every( ( v, i ) => Math.abs( v - result.samples[1][i] ) <= 1 ) );
		await writeFile(
			"temp/artifacts/live-weather/thunder.json",
			JSON.stringify(
				{ ...result, scope: "GPU blend equation sanity check; not strict D3D9 or whole-scene acceptance" },
				null,
				2
			)
		);
	} finally {
		await browser.close();
	}
} );

// #4692: the resident GPU pool (weather-particles.ts) steps the same drops
// the CPU model of it does (tests/helpers/weather-reference.mjs), which the
// node tests use for drop-level behaviour: positions, ages, frame ticks,
// drift flags, the visible list in draw order and the indirect count.
test( "the GPU weather pool steps drops exactly as the reference model", { timeout: 120000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createWeatherParticles } = await import(
				"/src/engine/runtime/renderer/device/weather-particles.ts"
			);
			const { createWeather } = await import( "/src/engine/runtime/renderer/weather/weather.ts" );
			const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
			const { createWeatherReference } = await import( "/tests/helpers/weather-reference.mjs" );
			const adapter = await navigator.gpu.requestAdapter(), gpu = await adapter.requestDevice();
			const rows = [];
			for ( const mode of [ 2, 3 ] ) {
				const particles = createWeatherParticles( gpu, resource => resource.destroy?.() );
				await particles.ready;
				const reference = createWeatherReference();
				const commands = {
					...reference.commands,
					weatherSpawn( region, slot, records ) {
						reference.commands.weatherSpawn( region, slot, records );
						particles.spawn( region, slot, records );
					},
					weatherStep( step ) {
						reference.commands.weatherStep( step );
						return particles.step( step );
					}
				};
				const weather = createWeather( createPresentationRandom( 91 ) );
				weather.set( { mode, amount: 255 } );
				for ( let i = 0; i <= 900; i++ ) {
					// A turning camera: drops cross the view's edges, so visibility,
					// drift and the list order all change from frame to frame.
					const angle = i / 120, c = Math.cos( angle ), s = Math.sin( angle );
					const matrix = Float32Array.from( [
						.02 * c,
						0,
						.001 * s,
						0,
						0,
						.01,
						0,
						0,
						-.02 * s,
						0,
						.001 * c,
						0,
						0,
						-1,
						.5,
						1
					] );
					const camera = { eye: [ 0, 0, 0 ], target: [ s, 0, c ], near: 1, far: 2000, fov: 1 };
					weather.update( camera, i / 90, 0, true );
					weather.prepare( commands, new Map(), camera, matrix );
					const encoder = gpu.createCommandEncoder();
					particles.encode( encoder );
					gpu.queue.submit( [ encoder.finish() ] );
				}
				const { pool, args, offsets } = particles.buffers();
				const read = gpu.createBuffer( {
						size: pool.size,
						usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST
					} ),
					readArgs = gpu.createBuffer( {
						size: args.size,
						usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST
					} );
				const encoder = gpu.createCommandEncoder();
				encoder.copyBufferToBuffer( pool, 0, read, 0, pool.size );
				encoder.copyBufferToBuffer( args, 0, readArgs, 0, args.size );
				gpu.queue.submit( [ encoder.finish() ] );
				await read.mapAsync( GPUMapMode.READ );
				await readArgs.mapAsync( GPUMapMode.READ );
				const words = new Uint32Array( read.getMappedRange().slice( 0 ) ),
					floats = new Float32Array( words.buffer ),
					argWords = new Uint32Array( readArgs.getMappedRange().slice( 0 ) );
				for ( const [region, capacity] of [ 8192, 16384, 8192 ].entries() ) {
					const base = offsets[region] + 32, model = reference.regions[region];
					let alive = 0, flagMismatch = 0, maxError = 0, ageMismatch = 0;
					for ( let slot = 0; slot < capacity; slot++ ) {
						const at = slot * 8, gpuFlags = words[base + at + 6] & 7, refFlags = model.words[at + 6] & 7;
						if ( gpuFlags !== refFlags ) {
							flagMismatch++;
							continue;
						}
						if ( !(refFlags & 1) ) continue;
						alive++;
						for ( let k = 0; k < 3; k++ ) {
							maxError = Math.max( maxError, Math.abs( floats[base + at + k] - model.floats[at + k] ) );
						}
						if (
							words[base + at + 4] !== model.words[at + 4] || words[base + at + 5] !== model.words[at + 5]
						) ageMismatch++;
					}
					const listAt = base + capacity * 8,
						visible = reference.visible( region ),
						count = argWords[region * 5] / 3;
					let order = 0;
					for ( let k = 0; k < Math.min( count, visible.length ); k++ ) {
						if ( words[listAt + k] === visible[k] ) order++;
					}
					rows.push( {
						mode,
						region,
						alive,
						flagMismatch,
						ageMismatch,
						maxError,
						count,
						expected: visible.length,
						order
					} );
				}
				read.unmap();
				readArgs.unmap();
				particles.dispose();
			}
			gpu.destroy();
			return rows;
		} );
		console.log( JSON.stringify( result ) );
		const active = result.filter( r => r.alive > 0 );
		assert.ok( active.length >= 2, "no region simulated" );
		for ( const row of result ) {
			assert.equal( row.ageMismatch, 0, JSON.stringify( row ) );
			// float32 rounding of the view test may differ at a frustum edge.
			assert.ok( row.flagMismatch <= Math.max( 2, row.alive * .002 ), JSON.stringify( row ) );
			assert.ok( row.maxError < .05, JSON.stringify( row ) );
			assert.ok(
				Math.abs( row.count - row.expected ) <= Math.max( 2, row.expected * .002 ),
				JSON.stringify( row )
			);
			assert.ok( row.order >= Math.min( row.count, row.expected ) * .99, JSON.stringify( row ) );
		}
	} finally {
		await browser.close();
	}
} );
