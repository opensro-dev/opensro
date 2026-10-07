/*
===========================================================================

crowd-timedemo.mjs - replay recorded actors through the shipping renderer

Usage: node tools/perf/bench/crowd-timedemo.mjs URL CAPTURE CAMERA OUTPUT
CAMERA is an explicit WorldCamera JSON file; viewport/video/replaySeed are capture
metadata. This measures isolated character rendering, not live-world FPS or
network correctness. Run under the benchmark lock. Assets stay native.

===========================================================================
*/
import assert from "node:assert/strict";
import { readFile, stat, mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { launchProbeBrowser } from "../../../../../scripts/lib/probeBrowser.mjs";
import { CROWD_CAPTURE_VERSION, decodeCrowdValue } from "../core/crowd-capture.mjs";

const MAX_CAPTURE_BYTES = 65 * 1024 * 1024;
const DEFAULT_WARMUP_FRAMES = 200;

/*
================
replayCharacters

The complete sequential trajectory is used once: no backward clock resets or
loop-boundary teleports. Warmup frames advance the same cloth/particle owners.
================
*/
async function replayCharacters( { capture, camera, warmup } ) {
	const { decodeCrowdValue } = await import( "/tools/perf/core/crowd-capture.mjs" );
	const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
	const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
	const { loadCrowdModels } = await import( "/tools/perf/core/crowd-models.mjs" );
	const metadata = decodeCrowdValue( capture.metadata );
	const frames = capture.frames.map( row => decodeCrowdValue( row ) );
	const canvas = document.createElement( "canvas" );
	Object.assign( canvas, metadata.viewport );
	document.body.append( canvas );
	const renderer = createRenderer( canvas, createPresentationRandom( metadata.replaySeed ) );
	let loaded;
	try {
		const deadline = performance.now() + 15000;
		while ( renderer.phase() === "starting" && performance.now() < deadline ) {
			await new Promise( requestAnimationFrame );
		}
		if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "Renderer startup timeout" );
		renderer.videoOptions( metadata.videoOptions );
		loaded = await loadCrowdModels(
			renderer,
			frames.flatMap( frame => frame.actors )
		);
		renderer.setWorld( { id: "crowd-timedemo", originRegion: camera.originRegion, groups: [], warnings: [] } );
		renderer.setWorldCamera( camera );
		const rows = [];
		for ( const [index, frame] of frames.entries() ) {
			const started = performance.now();
			renderer.setCharacterActors( frame.actors );
			await renderer.frame( metadata.viewport, frame.atMs / 1000 );
			const elapsedMs = performance.now() - started;
			if ( renderer.error() ) throw Error( renderer.error() );
			if ( index >= warmup ) {
				rows.push( {
					index,
					atMs: frame.atMs,
					elapsedMs,
					stats: structuredClone( renderer.characterStats() )
				} );
			}
		}
		return {
			scope: "isolated-character-renderer",
			cameraMode: "explicit-fixed",
			camera,
			metadata,
			assets: loaded.assets,
			warmup,
			rows,
			userAgent: navigator.userAgent,
			screenshot: canvas.toDataURL( "image/png" )
		};
	} finally {
		renderer.dispose();
		loaded?.close();
		canvas.remove();
	}
}

/*
================
main
================
*/
async function main() {
	const [url, capturePath, cameraPath, output] = process.argv.slice( 2 );
	assert.ok( url && capturePath && cameraPath && output, "Expected URL CAPTURE CAMERA OUTPUT" );
	assert.ok( (await stat( capturePath )).size <= MAX_CAPTURE_BYTES, "Capture exceeds byte limit" );
	const capture = JSON.parse( await readFile( capturePath, "utf8" ) );
	const camera = JSON.parse( await readFile( cameraPath, "utf8" ) );
	assert.equal( capture.version, CROWD_CAPTURE_VERSION );
	assert.equal( capture.scope, "character-renderer" );
	assert.ok( !capture.failure, capture.failure );
	assert.ok( capture.frames.length > DEFAULT_WARMUP_FRAMES, "Capture needs measured frames after warmup" );
	const metadata = decodeCrowdValue( capture.metadata );
	assert.ok(
		Number.isInteger( metadata.replaySeed ) && metadata.videoOptions && metadata.viewport,
		"Missing replay seed, video options or viewport"
	);
	assert.ok(
		Number.isInteger( camera.originRegion ) && camera.eye?.length === 3 && camera.target?.length === 3,
		"Explicit world camera required"
	);
	let previous = -Infinity;
	for ( const row of capture.frames ) {
		const frame = decodeCrowdValue( row );
		assert.ok(
			Number.isFinite( frame.atMs ) && frame.atMs > previous && Array.isArray( frame.actors ),
			"Invalid frame"
		);
		previous = frame.atMs;
	}
	const { browser, page } = await launchProbeBrowser( { deviceScaleFactor: 1 } );
	try {
		await page.goto( new URL( "/assets/skillfx/manifest.json", url ).href );
		const result = await page.evaluate( replayCharacters, { capture, camera, warmup: DEFAULT_WARMUP_FRAMES } );
		const directory = path.resolve( output );
		await mkdir( directory, { recursive: true } );
		await writeFile(
			path.join( directory, "last-frame.png" ),
			Buffer.from( result.screenshot.split( "," )[1], "base64" )
		);
		delete result.screenshot;
		await writeFile( path.join( directory, "timedemo.json" ), JSON.stringify( result, null, 2 ) + "\n" );
		console.log( `Replayed ${result.rows.length} measured character frames; results: ${directory}` );
	} finally {
		await browser.close();
	}
}

await main();
