/*
===========================================================================

effects-bench.mjs - frame rate of many skill effects, without a server

  node tools/perf/bench/effects-bench.mjs [--count N] [--seconds S]
                                          [--match REGEX] [--size WxH]
                                          [--cpu] [--heap] [--out DIR]

Renders count published effect programs (those whose name matches) at
once through the production renderer in an uncapped browser, every one
looping on a grid in front of the camera, and reports frames a second,
the main thread's frame time, and what the frame drew (particles, ribbon
vertices, draws). No game server or character is involved, so the scene
is the same on every run: the measure for particle presentation work.

To compare two trees, run a second dev server from the other tree and
point this at it with SRO_PROBE_CLIENT_NEXT_BASE_URL. --cpu and --heap
capture the measured span as OUT/effects.cpuprofile and .heapprofile
(read them with tools/perf/analyze/profile.mjs).

===========================================================================
*/
import { launchProbeBrowser } from "../../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../../scripts/lib/probeEndpoints.mjs";
import { parseOptions } from "../core/report.mjs";
import { createCaptures } from "../core/client.mjs";

const USAGE = "effects-bench.mjs [--count N] [--seconds S] [--match REGEX] [--size WxH] [--cpu] [--heap] [--out DIR]";

/*
================
admitEffects

Page side: creates the renderer and admits the effects, leaving the scene
in globalThis.__effectsBench for measureEffects. It runs in the page, so
it closes over nothing.
================
*/
async function admitEffects( { count, match, width, height } ) {
	const GRID_SPACING = 40;
	const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
	const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
	const { createCharacterResources } = await import( "/src/engine/runtime/characters/resources/resources.ts" );
	const canvas = document.createElement( "canvas" );
	canvas.width = width;
	canvas.height = height;
	document.body.append( canvas );
	const renderer = createRenderer( canvas ), assets = createAssets();
	const resources = createCharacterResources( assets, renderer, location.origin );
	const catalog = await (await fetch( "/assets/effects/programs.json" )).json();
	const pattern = new RegExp( match );
	const paths = Object.keys( catalog.effects ).filter( name => pattern.test( name ) ).slice( 0, count ).map( name =>
		"/assets/effects/programs.json#" + encodeURIComponent( name )
	);
	const tally = {};
	const probe = {
		characterCount( name, value = 1 ) {
			tally[name] = (tally[name] ?? 0) + value;
		},
		characterBegin() {},
		characterMark() {},
		renderBegin() {},
		renderMark() {},
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
		const side = Math.ceil( Math.sqrt( paths.length ) );
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
		const actors = ( time ) =>
			ready.map( ( model, i ) => ({
				gid: -1 - i,
				model,
				pose: {
					regionId: 257,
					x: (i % side - (side - 1) / 2) * GRID_SPACING,
					y: 0,
					z: (Math.floor( i / side ) - (side - 1) / 2) * GRID_SPACING,
					yaw: 0
				},
				clip: "effect",
				time: time + i * .137,
				loop: true,
				scale: 1
			}) );
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
async function measureEffects( seconds ) {
	const { renderer, resources, assets, actors, probe, tally, ready, width, height } = globalThis.__effectsBench;
	try {
		const frames = [], main = [];
		const started = performance.now();
		let last = started;
		for ( const key in tally ) delete tally[key];
		let measured = 0;
		while ( performance.now() - started < seconds * 1000 ) {
			await new Promise( requestAnimationFrame );
			const now = performance.now(), time = (now - started) / 1000;
			frames.push( now - last );
			last = now;
			renderer.setCharacterActors( actors( time ) );
			renderer.frame( { width, height }, time, undefined, probe );
			main.push( performance.now() - now );
			measured++;
			if ( renderer.error() ) throw Error( renderer.error() );
		}
		const sorted = frames.slice( 2 ).sort( ( a, b ) => a - b ),
			mean = list => list.reduce( ( a, b ) => a + b, 0 ) / Math.max( 1, list.length );
		const counts = {};
		for ( const key in tally ) counts[key] = Number( (tally[key] / measured).toFixed( 1 ) );
		return {
			effects: ready.length,
			fps: 1000 / mean( sorted ),
			p50: sorted[Math.floor( sorted.length * .5 )],
			p99: sorted[Math.floor( sorted.length * .99 )],
			main: mean( main.slice( 2 ) ),
			draws: renderer.characterStats().draws,
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
		seconds: 5,
		match: "^skill/",
		size: "1600x900",
		cpu: false,
		heap: false,
		out: "temp/artifacts/effects-bench"
	},
	USAGE
);
const [width, height] = options.size.split( "x" ).map( Number );
process.env.SRO_PROBE_UNLOCK_FPS = "1";
const { browser, page } = await launchProbeBrowser();
try {
	// A static document of the dev server's origin: the page imports the
	// renderer from it, and no client boots beside the measured one.
	await page.goto( CLIENT_NEXT_BASE_URL + "/assets/skillfx/manifest.json" );
	await page.evaluate( admitEffects, { count: options.count, match: options.match, width, height } );
	const captures = await createCaptures( page, { dir: options.out, cpu: options.cpu, heap: options.heap } );
	await captures.start();
	const result = await page.evaluate( measureEffects, options.seconds );
	await captures.stop( "effects" );
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
