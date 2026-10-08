/*
===========================================================================
compareEnvironmentGpu.mjs - zero-tolerance native environment comparison

Loads unmodified production code from both pinned trees and the same retail
fixture. Opt-in stages are disabled for every before/after capture.
===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { launchProbeBrowser } from "../lib/probeBrowser.mjs";
import {
	prepareEnvironmentFixture,
	captureEnvironment,
	captureFlatRelief
} from "../../apps/client-next/tests/helpers/environment-gpu.mjs";

const [baselineRoot, candidateRoot, baselineHead] = process.argv.slice( 2 );
if ( !baselineRoot || !candidateRoot || !baselineHead ) {
	throw Error(
		"Usage: node scripts/probes/compareEnvironmentGpu.mjs <baseline-root> <candidate-root> <baseline-head>"
	);
}
const roots = [ path.resolve( baselineRoot ), path.resolve( candidateRoot ) ];
const requireClient = createRequire( path.join( roots[1], "apps/client-next/package.json" ) );
const { createServer } = await import( pathToFileURL( requireClient.resolve( "vite" ) ).href );
const output = path.join( roots[1], ".state", "environment-gpu-comparison" );
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
		const launch = await launchProbeBrowser( { viewport: { width: 912, height: 424 } } );
		browser = launch.browser;
		const errors = [];
		launch.page.on( "pageerror", error => errors.push( error.message ) );
		const fixture = await prepareEnvironmentFixture( launch.page, origin );
		const retail = await launch.page.evaluate( captureEnvironment, fixture );
		const flat = await launch.page.evaluate( captureFlatRelief );
		assert.deepEqual(
			retail.rows[0].rgba,
			retail.rows[1].rgba,
			"Repeated native retail capture must match exactly"
		);
		assert.deepEqual( flat[0], flat[1], "Flat zero-light terrain must retain its albedo" );
		assert.deepEqual( errors, [] );
		const result = { head, retail, flat };
		await writeFile( `${output}/${head}.json`, JSON.stringify( result ) );
		captures.push( result );
		console.log( `Captured retail native frame and zero-light flat terrain from ${head}` );
	} finally {
		await browser?.close();
		await server.close();
	}
}
assert.deepEqual(
	captures[1].retail,
	captures[0].retail,
	"Native retail GPU channels must exactly match the baseline"
);
assert.deepEqual( captures[1].flat, captures[0].flat, "Flat terrain GPU channels must exactly match the baseline" );
await writeFile(
	`${output}/result.json`,
	JSON.stringify(
		{
			baseline: captures[0].head,
			candidate: captures[1].head,
			cases: [ "retail-native", "retail-native-repeat", "zero-light-flat-native", "zero-light-flat-relief" ],
			changedChannels: 0
		},
		null,
		2
	)
);
console.log( "PASS: native environment captures match the baseline at zero tolerance" );
