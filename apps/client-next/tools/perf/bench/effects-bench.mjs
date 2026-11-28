/*
===========================================================================

effects-bench.mjs - frame rate of many skill effects, without a server

  node tools/perf/bench/effects-bench.mjs [--count N] [--seconds S] [--holders N]
                                          [--match REGEX] [--size WxH]
                                          [--warmup S] [--cpu-rate N]
                                          [--cpu] [--heap] [--trace] [--gpu] [--counts] [--out DIR]

Renders count published effect programs (those whose name matches) at
once through the production renderer in an uncapped browser, every one
looping on a grid in front of the camera, and reports frames a second,
the main thread's frame time, and what the frame drew (particles, ribbon
vertices, draws). No game server or character is involved, so the scene
is the same on every run: the measure for particle presentation work.

--holders N stands N animated bodies on the grid and attaches the effects
to their bones and roots in turn, as a crowd's weapon glows and buffs are
held: the measure for what a held effect costs each frame (its holder's
socket, its transform, its follow) beside the presentation itself.

To compare two trees, run a second dev server from the other tree and
point this at it with SRO_PROBE_CLIENT_NEXT_BASE_URL. --cpu and --heap
capture the measured span as OUT/effects.cpuprofile and .heapprofile
(read them with tools/perf/analyze/profile.mjs); --counts adds WebGPU
calls a frame (bundles recorded, queue writes, dispatches, bind groups).
--trace captures browser and GPU process work in OUT/trace.json.
--gpu records the renderer's bounded GPU pass timings; these instrumented
runs diagnose GPU work and must not be compared to uninstrumented FPS.

===========================================================================
*/
import { launchProbeBrowser } from "../../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../../scripts/lib/probeEndpoints.mjs";
import { parseOptions } from "../core/report.mjs";
import { createCaptures } from "../core/client.mjs";
import { writeFile } from "node:fs/promises";

const USAGE =
	"effects-bench.mjs [--count N] [--seconds S] [--holders N] [--warmup S] [--cpu-rate N] [--match REGEX] [--size WxH] [--cpu] [--heap] [--trace] [--gpu] [--counts] [--screenshot] [--out DIR]";

/*
================
admitEffects

Page side: creates the renderer and admits the effects, leaving the scene
in globalThis.__effectsBench for measureEffects. It runs in the page, so
it closes over nothing.
================
*/
async function admitEffects( { count, match, holders, width, height, counts, gpu } ) {
	const GRID_SPACING = 40;
	// A holder: a binary tree of turning bones under one skinned triangle,
	// so every socket an effect hangs on is a moving chain of several bones.
	const HOLDER = "bench/holder", HOLDER_BONES = 48, HOLDER_BONE_LENGTH = 3;
	// One held effect in this many stands at its holder's root, as a buff does.
	const ROOT_SHARE = 4;
	const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
	const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
	const { createCharacterResources } = await import( "/src/engine/runtime/characters/resources/resources.ts" );
	const canvas = document.createElement( "canvas" );
	canvas.width = width;
	canvas.height = height;
	document.body.append( canvas );
	const renderer = createRenderer( canvas, undefined, undefined, { gpuTiming: gpu } ), assets = createAssets();
	const resources = createCharacterResources( assets, renderer, location.origin );
	const { EFFECT_PROGRAM_INDEX, effectProgramModel } = await import(
		"/src/engine/foundation/assets/effect-programs.ts"
	);
	const index = await (await fetch( EFFECT_PROGRAM_INDEX )).json();
	const pattern = new RegExp( match );
	const paths = index.effects.filter( name => pattern.test( name ) ).slice( 0, count ).map( effectProgramModel );
	// The renderer's frame probe (frame-probes.ts): counts, and "@stage" ms
	// since the owner's previous mark, as client.mjs records them.
	const tally = {}, marks = { render: 0, character: 0 };
	const add = ( key, value ) => {
		tally[key] = (tally[key] ?? 0) + value;
	};
	const mark = ( owner, stage ) => {
		const now = performance.now();
		add( "@" + stage, now - marks[owner] );
		marks[owner] = now;
	};
	if ( counts ) {
		const wrap = ( prototype, name, key ) => {
			const original = prototype[name];
			prototype[name] = function( ...args ) {
				add( key, 1 );
				if ( name === "writeBuffer" ) {
					const data = args[2], elementBytes = data.BYTES_PER_ELEMENT ?? 1;
					const bytes = args[4] === undefined ?
						data.byteLength - (args[3] ?? 0) * elementBytes :
						args[4] * elementBytes;
					add( "uploaded bytes", bytes );
				}
				if ( name === "dispatchWorkgroups" ) {
					add( "compute workgroups", args[0] * (args[1] ?? 1) * (args[2] ?? 1) );
				}
				if ( name === "copyBufferToBuffer" ) add( "copied bytes", args[4] );
				return original.apply( this, args );
			};
		};
		wrap( GPUDevice.prototype, "createRenderBundleEncoder", "bundles recorded" );
		wrap( GPUQueue.prototype, "writeBuffer", "writeBuffer" );
		wrap( GPUComputePassEncoder.prototype, "dispatchWorkgroups", "dispatches" );
		wrap( GPUComputePassEncoder.prototype, "setBindGroup", "compute bind groups" );
		wrap( GPUCommandEncoder.prototype, "copyBufferToBuffer", "buffer copies" );
		wrap( GPURenderPassEncoder.prototype, "executeBundles", "executeBundles" );
	}
	const probe = {
		characterCount: ( name, value = 1 ) => add( name, value ),
		characterBegin: () => marks.character = performance.now(),
		characterMark: stage => mark( "character", stage ),
		renderBegin: () => marks.render = performance.now(),
		renderMark: stage => mark( "render", stage ),
		detailBegin() {},
		detailEnd() {},
		sampleDetails: () => false,
		worldBegin() {},
		worldMark() {}
	};
	try {
		const limit = performance.now() + 60000;
		while ( renderer.phase() === "starting" && performance.now() < limit ) {
			await new Promise( requestAnimationFrame );
		}
		renderer.setWorld( { id: "effects-bench", originRegion: 257, groups: [], warnings: [] } );
		if ( holders ) {
			const identity = [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ];
			const nodes = Array.from( { length: HOLDER_BONES }, ( _, i ) => ({
				name: "b" + i,
				parent: i ? Math.floor( (i - 1) / 2 ) : -1,
				translation: [ i % 2 ? HOLDER_BONE_LENGTH : -HOLDER_BONE_LENGTH, i ? HOLDER_BONE_LENGTH : 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ 1, 1, 1 ]
			}) );
			renderer.setCharacterModel( HOLDER, {
				nodes,
				images: [],
				clips: [ {
					name: "idle",
					duration: 1,
					channels: nodes.map( ( _, node ) => ({
						node,
						path: "rotation",
						interpolation: "LINEAR",
						times: Float32Array.of( 0, .5, 1 ),
						values: Float32Array.of( 0, 0, 0, 1, 0, 0, .0998, .995, 0, 0, 0, 1 )
					}) )
				} ],
				primitives: [ {
					name: "body",
					node: 0,
					joints: nodes.map( ( _, i ) => i ),
					inverseBind: Float32Array.from( nodes.flatMap( () => identity ) ),
					image: -1,
					geometry: {
						positions: Float32Array.of( 0, 0, 0, 2, 0, 0, 0, 6, 0 ),
						indices: Uint32Array.of( 0, 1, 2 ),
						joints: Uint32Array.of( 0, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0 ),
						weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
						transform: Float32Array.from( identity ),
						material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true }
					}
				} ]
			}, [] );
		}
		const side = Math.ceil( Math.sqrt( holders || paths.length ) );
		renderer.setWorldCamera( {
			eye: [ 0, side * GRID_SPACING * .6, -side * GRID_SPACING * 1.1 ],
			target: [ 0, 0, 0 ],
			originRegion: 257,
			fov: 1,
			near: 1,
			far: 5000
		} );
		while ( performance.now() < limit ) {
			resources.begin( 0 );
			resources.poll();
			if ( resources.error() ) throw Error( resources.error() );
			if ( paths.every( path => resources.ready( path ) ) ) break;
			await new Promise( requestAnimationFrame );
		}
		const ready = paths.filter( path => resources.ready( path ) );
		const place = i => ({
			regionId: 257,
			x: (i % side - (side - 1) / 2) * GRID_SPACING,
			y: 0,
			z: (Math.floor( i / side ) - (side - 1) / 2) * GRID_SPACING,
			yaw: 0
		});
		const bodies = time =>
			Array.from( { length: holders }, ( _, h ) => ({
				gid: 1 + h,
				model: HOLDER,
				pose: place( h ),
				clip: "idle",
				time: time + h * .21,
				loop: true,
				scale: 1
			}) );
		// Effect i of a held scene hangs on holder i % holders: on a bone, or
		// at the root for one in ROOT_SHARE.
		const held = i =>
			holders ?
				{
					attachment: {
						gid: 1 + i % holders,
						bone: "b" + (1 + i % (HOLDER_BONES - 1)),
						offset: [ 0, 0, 0 ],
						basis: "native",
						...(i % ROOT_SHARE ? {} : { root: true })
					}
				} :
				{};
		const actors = ( time ) => [
			...bodies( time ),
			...ready.map( ( model, i ) => ({
				gid: -1 - i,
				model,
				pose: place( holders ? i % holders : i ),
				clip: "effect",
				time: time + i * .137,
				loop: true,
				scale: 1,
				...held( i )
			}) )
		];
		globalThis.__effectsBench = { renderer, resources, assets, actors, probe, tally, ready, width, height };
	} catch ( error ) {
		resources.dispose();
		assets.dispose();
		renderer.dispose();
		throw error;
	}
}

/*
================
measureEffects

Page side: loops the admitted effects for seconds and returns the frame
statistics, then releases the scene.
================
*/
async function measureEffects( { seconds, warmup } ) {
	const { renderer, resources, assets, actors, probe, tally, ready, width, height } = globalThis.__effectsBench;
	try {
		const frames = [], main = [], cpu = [], readback = [];
		const admitted = performance.now();
		while ( performance.now() - admitted < warmup * 1000 ) {
			await new Promise( requestAnimationFrame );
			const time = (performance.now() - admitted) / 1000;
			renderer.setCharacterActors( actors( time ) );
			await renderer.frame( { width, height }, time );
			if ( renderer.error() ) throw Error( renderer.error() );
		}
		const started = performance.now();
		let last = started;
		for ( const key in tally ) delete tally[key];
		let measured = 0;
		while ( performance.now() - started < seconds * 1000 ) {
			await new Promise( requestAnimationFrame );
			const now = performance.now(), time = (now - admitted) / 1000;
			frames.push( now - last );
			last = now;
			renderer.setCharacterActors( actors( time ) );
			await renderer.frame( { width, height }, time, undefined, probe );
			const elapsed = performance.now() - now, waiting = renderer.readbackWaitMs?.() ?? 0;
			main.push( elapsed );
			readback.push( waiting );
			cpu.push( Math.max( 0, elapsed - waiting ) );
			measured++;
			if ( renderer.error() ) throw Error( renderer.error() );
		}
		const sorted = frames.slice( 2 ).sort( ( a, b ) => a - b ),
			mainSorted = main.slice( 2 ).sort( ( a, b ) => a - b ),
			mean = list => list.reduce( ( a, b ) => a + b, 0 ) / Math.max( 1, list.length );
		const counts = {};
		for ( const key in tally ) counts[key] = Number( (tally[key] / measured).toFixed( key[0] === "@" ? 2 : 1 ) );
		return {
			effects: ready.length,
			fps: 1000 / mean( sorted ),
			p50: sorted[Math.floor( sorted.length * .5 )],
			p95: sorted[Math.floor( sorted.length * .95 )],
			p99: sorted[Math.floor( sorted.length * .99 )],
			max: sorted.at( -1 ),
			frames: sorted.length,
			framesOver25ms: sorted.filter( value => value > 25 ).length,
			framesOver50ms: sorted.filter( value => value > 50 ).length,
			main: mean( main.slice( 2 ) ),
			mainP50: mainSorted[Math.floor( mainSorted.length * .5 )],
			mainP99: mainSorted[Math.floor( mainSorted.length * .99 )],
			mainMax: mainSorted.at( -1 ),
			mainOver25ms: mainSorted.filter( value => value > 25 ).length,
			mainOver50ms: mainSorted.filter( value => value > 50 ).length,
			cpuMain: mean( cpu.slice( 2 ) ),
			readback: mean( readback.slice( 2 ) ),
			draws: renderer.characterStats().draws,
			gpuTiming: renderer.gpuTiming(),
			counts
		};
	} finally {
		resources.dispose();
		assets.dispose();
		renderer.dispose();
	}
}

const options = parseOptions(
	process.argv.slice( 2 ),
	{
		count: 36,
		holders: 0,
		seconds: 5,
		warmup: 5,
		match: "^skill/",
		size: "1600x900",
		cpu: false,
		heap: false,
		trace: false,
		gpu: false,
		counts: false,
		screenshot: false,
		cpuRate: 1,
		out: "temp/artifacts/effects-bench"
	},
	USAGE
);
const [width, height] = options.size.split( "x" ).map( Number );
if (
	!Number.isFinite( options.cpuRate ) || options.cpuRate < 1 ||
	!Number.isFinite( options.warmup ) || options.warmup < 0 ||
	!Number.isFinite( options.seconds ) || options.seconds <= 0
) throw Error( "Invalid effects benchmark timing" );
if ( !Number.isInteger( options.holders ) || options.holders < 0 ) throw Error( "Invalid holder count" );
process.env.SRO_PROBE_UNLOCK_FPS = "1";
const { browser, page } = await launchProbeBrowser();
try {
	// A static document of the dev server's origin: the page imports the
	// renderer from it, and no client boots beside the measured one.
	await page.goto( CLIENT_NEXT_BASE_URL + "/assets/skillfx/manifest.json" );
	await page.evaluate( admitEffects, {
		count: options.count,
		match: options.match,
		holders: options.holders,
		width,
		height,
		counts: options.counts,
		gpu: options.gpu
	} );
	if ( options.cpuRate !== 1 ) {
		const cdp = await page.context().newCDPSession( page );
		await cdp.send( "Emulation.setCPUThrottlingRate", { rate: options.cpuRate } );
	}
	const captures = await createCaptures( page, {
		dir: options.out,
		cpu: options.cpu,
		heap: options.heap,
		trace: options.trace ? `${options.out}/trace.json` : null
	} );
	await captures.start();
	console.log( `[effects] ${options.warmup}s warmup, ${options.seconds}s measurement, CPU rate ${options.cpuRate}` );
	const measured = page.evaluate( measureEffects, { seconds: options.seconds, warmup: options.warmup } );
	// The measured scene, halfway through: measureEffects disposes the
	// renderer when it returns, so the capture runs beside it.
	if ( options.screenshot ) {
		await new Promise( resolve => setTimeout( resolve, (options.warmup + options.seconds / 2) * 1000 ) );
		await page.locator( "canvas" ).screenshot( { path: `${options.out}/scene.png` } );
	}
	const result = await measured;
	await captures.stop( "effects" );
	await captures.finish();
	await writeFile(
		`${options.out}/results.json`,
		JSON.stringify(
			{
				...result,
				cpuRate: options.cpuRate,
				warmup: options.warmup,
				seconds: options.seconds
			},
			null,
			2
		)
	);
	console.log(
		`${CLIENT_NEXT_BASE_URL}  ${result.effects} effects  ${result.fps.toFixed( 0 )} fps  frame p50 ${
			result.p50.toFixed( 2 )
		} p99 ${result.p99.toFixed( 2 )} ms  main ${result.main.toFixed( 2 )} ms/f  draws ${result.draws}  | ${
			Object.entries( result.counts ).map( ( [key, value] ) => `${key} ${value}` ).join( ", " )
		}`
	);
} finally {
	await browser.close();
}
