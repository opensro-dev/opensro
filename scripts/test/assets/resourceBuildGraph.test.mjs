/*
===========================================================================

resourceBuildGraph.test.mjs - producer entry points and the build fingerprint

Character producers expose their entry points without starting a build on
import, and the resource fingerprint records every ownership root. What
keys the server's part of it is resourceFingerprintServer.test.mjs, a
file of its own so the two run side by side. The full
build's dependency order is run with stub steps in
scripts/test/pipeline/resourceBuildOrder.test.mjs.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";
import { STUB_FINGERPRINT_KEYS } from "../helpers/fingerprintKeys.mjs";

const rebuildRoot = path.resolve( "." );

// One fingerprint of the tree, shared by the cases that read it: each
// computation scans every root (about 147,000 files, 5 to 10 s), and seven
// of them held this file near 50 s (#4337).
let shared;

/*
================
treeFingerprint
================
*/
function treeFingerprint() {
	shared ??= computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	return shared;
}

test("character producers expose their entry points without starting a build", async () => {
	for (
		const [relativePath, producer] of [
			[ "scripts/build/char/buildRoster.mjs", "buildRoster" ],
			[ "scripts/build/char/buildLocomotionBanAssets.mjs", "buildLocomotionBanAssets" ],
			[ "scripts/build/char/buildDropModelAssets.mjs", "buildDropModelAssets" ],
			[ "scripts/build/char/buildEquipmentVisuals.mjs", "buildEquipmentVisuals" ]
		]
	) {
		// Importing a producer exposes its entry point and must not start a
		// build: its CLI body runs only when it is the main script.
		const producerModule = await import(
			pathToFileURL( path.join( rebuildRoot, ...relativePath.split( "/" ) ) ).href
		);
		assert.equal( typeof producerModule[producer], "function", producer );
	}
});

test("native animation registry is pipeline-owned and fingerprinted with the build scripts", () => {
	assert.ok(
		fs.existsSync( path.join( rebuildRoot, "scripts", "build", "char", "native", "scriptObjAnimationRegistry.ts" ) )
	);
});

test("resource fingerprint reports every ownership root independently", async () => {
	const fingerprint = await treeFingerprint();
	const labels = fingerprint.roots.map( ( root ) => root.label );

	assert.equal( new Set( labels ).size, labels.length, "fingerprint root labels must be unique" );
	assert.deepEqual(
		labels.filter( ( label ) => !label.startsWith( "code:" ) ),
		[
			"survival-art",
			"pet-art",
			"extracted",
			"client-executable",
			"particle-archive",
			"server-exports",
			"server-monster-data",
			"server-enterworld",
			"server-action-data",
			"server-pet-design",
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

test("resource fingerprint covers the code the build runs outside scripts/build", async () => {
	const labels = (await treeFingerprint()).roots.map( ( root ) => root.label );
	// A converter change once matched the old fingerprint and skipped the build.
	for (
		const file of [
			"scripts/convert_images.py",
			"scripts/sro_paths.py",
			"scripts/lib/generatedRoot.mjs",
			// Named through path.join pieces; the first closure missed both.
			"scripts/tools/refresh_native_window_images.py",
			"apps/client-next/tools/build-skill-ui.py"
		]
	) {
		assert.ok( labels.includes( `code:${file}` ), file );
	}
});

test("a pack-layout baseline changes the resource fingerprint", async ( t ) => {
	const before = await treeFingerprint();
	const saved = process.env.SRO_ASSET_PACK_BASELINE;
	t.after( () => {
		if ( saved === undefined ) delete process.env.SRO_ASSET_PACK_BASELINE;
		else process.env.SRO_ASSET_PACK_BASELINE = saved;
	} );
	// The release builds against the live manifest; an up-to-date tree must not skip that.
	process.env.SRO_ASSET_PACK_BASELINE = path.join( rebuildRoot, "package.json" );
	const after = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	assert.notEqual( after.hash, before.hash );
	assert.ok( after.roots.some( ( root ) => root.label === "pack-baseline" ) );
});
