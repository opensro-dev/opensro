/*
===========================================================================

resourceBuildGraph.test.mjs - producer entry points and the build fingerprint

Character producers expose their entry points without starting a build on
import, and the resource fingerprint records every ownership root. The full
build's dependency order is run with stub steps in
scripts/test/pipeline/resourceBuildOrder.test.mjs.

===========================================================================
*/
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { computeResourceBuildFingerprint, movedRoots } from "../../build/shared/resourceBuildFingerprint.mjs";
import { STUB_FINGERPRINT_KEYS } from "../helpers/fingerprintKeys.mjs";

const rebuildRoot = path.resolve( "." );

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
	const fingerprint = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
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
	const labels = (await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS )).roots.map( ( root ) => root.label );
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

test("the server's exports, not its Go sources, key the resource fingerprint", async () => {
	// A server change that leaves every export's bytes alone keeps the
	// fingerprint; one that moves an export misses it (#4337).
	const same = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	const again = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
	const moved = await computeResourceBuildFingerprint( { serverExports: () => "a moved roster" } );
	assert.equal( again.hash, same.hash );
	assert.notEqual( moved.hash, same.hash );
	assert.deepEqual(
		movedRoots( same, moved ).filter( ( label ) => !label.startsWith( "code:" ) ),
		[ "server-exports" ]
	);
});

test("a pack-layout baseline changes the resource fingerprint", async ( t ) => {
	const before = await computeResourceBuildFingerprint( STUB_FINGERPRINT_KEYS );
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
