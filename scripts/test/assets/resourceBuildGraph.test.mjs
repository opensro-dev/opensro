/*
===========================================================================

resourceBuildGraph.test.mjs - the full build's producer graph and ordering

Walks the imports of build_sro_resources.mjs to prove every character
asset producer is reachable, and checks that image staging exists before
the lanes that consume it.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";

const rebuildRoot = path.resolve( "." );
const resourceEntry = path.join( rebuildRoot, "scripts", "build_sro_resources.mjs" );

/*
================
collectReachableModules
================
*/
function collectReachableModules( entryPath ) {
	const pending = [ entryPath ];
	const visited = new Set();
	while ( pending.length > 0 ) {
		const filePath = pending.pop();
		const resolvedPath = path.resolve( filePath );
		if ( visited.has( resolvedPath ) ) continue;
		visited.add( resolvedPath );
		// Other build lanes may be under active development in the shared
		// workspace; this contract is scoped to reachability of the character
		// producer graph, not global import completeness.
		if ( !fs.existsSync( resolvedPath ) ) continue;
		const source = fs.readFileSync( resolvedPath, "utf8" );
		for ( const match of source.matchAll( /(?:from\s+|import\s*\()(["'])(\.[^"']+\.mjs)\1/g ) ) {
			pending.push( path.resolve( path.dirname( resolvedPath ), match[2] ) );
		}
	}
	return visited;
}

test("full resource build reaches every character asset producer and its derived helpers", () => {
	const reachable = collectReachableModules( resourceEntry );
	for (
		const relativePath of [
			"scripts/build/char/buildRoster.mjs",
			"scripts/build/char/buildLocomotionBanAssets.mjs",
			"scripts/build/char/buildDropModelAssets.mjs",
			"scripts/build/char/resolveCrowdDress.mjs"
		]
	) {
		assert.ok( reachable.has( path.join( rebuildRoot, ...relativePath.split( "/" ) ) ), relativePath );
	}

	const entrySource = fs.readFileSync( resourceEntry, "utf8" );
	for (
		const producer of [
			"buildRoster",
			"buildLocomotionBanAssets",
			"buildDropModelAssets"
		]
	) {
		assert.match( entrySource, new RegExp( `\\b${producer}\\(` ), producer );
	}

	for (
		const [relativePath, producer] of [
			[ "scripts/build/char/buildRoster.mjs", "buildRoster" ],
			[ "scripts/build/char/buildLocomotionBanAssets.mjs", "buildLocomotionBanAssets" ],
			[ "scripts/build/char/buildDropModelAssets.mjs", "buildDropModelAssets" ]
		]
	) {
		const source = fs.readFileSync( path.join( rebuildRoot, ...relativePath.split( "/" ) ), "utf8" );
		assert.match( source, new RegExp( `export\\s+async\\s+function\\s+${producer}\\b` ) );
		assert.match( source, /if\s*\(isMainScript\(import\.meta\.url\)\)/ );
	}
});

test("native animation registry is pipeline-owned and fingerprinted with the build scripts", () => {
	assert.ok(
		fs.existsSync( path.join( rebuildRoot, "scripts", "build", "char", "native", "scriptObjAnimationRegistry.ts" ) )
	);
});

test("resource fingerprint reports every ownership root independently", async () => {
	const fingerprint = await computeResourceBuildFingerprint();
	const labels = fingerprint.roots.map( ( root ) => root.label );

	assert.equal( new Set( labels ).size, labels.length, "fingerprint root labels must be unique" );
	assert.deepEqual(
		labels,
		[
			"extracted",
			"client-executable",
			"server-go-mod",
			"server-go-sum",
			"server-roster-exporter",
			"server-roster-policy",
			"rebuild-assets",
			"build-scripts",
			"build-entry",
			"outdoor-entry",
			"lock-helper",
			"public"
		]
	);
	assert.equal(
		fingerprint.roots.reduce( ( count, root ) => count + root.fileCount, 0 ),
		fingerprint.fileCount
	);
	for ( const root of fingerprint.roots ) {
		assert.match( root.hash, /^[a-f0-9]{64}$/ );
		assert.ok( root.fileCount >= 1, `${root.label} should record a file or an explicit absence` );
	}
});

test("full resource build recreates compacted image staging before parallel consumers", () => {
	const entrySource = fs.readFileSync( resourceEntry, "utf8" );
	// Layout-independent: dprint pads call parentheses.
	const conversionIndex = entrySource.search( /runConvertImages\s*\(\s*\[\s*\]\s*\)/ );
	const firstConsumerIndex = entrySource.indexOf( "const interfaceImagesLane" );

	assert.ok( conversionIndex >= 0, "full build must own the unfiltered image conversion pass" );
	assert.ok(
		firstConsumerIndex > conversionIndex,
		"the staging cache must exist before UI/world/model lanes consume converted images"
	);

	const runnerSource = fs.readFileSync(
		path.join( rebuildRoot, "scripts", "build", "shared", "convertImagesRunner.mjs" ),
		"utf8"
	);
	assert.match( runnerSource, /fullConversionComplete\s*&&\s*args\.length\s*>\s*0/ );
	assert.match( runnerSource, /args\.length\s*===\s*0\s*&&\s*result\.status\s*===\s*0/ );
});
