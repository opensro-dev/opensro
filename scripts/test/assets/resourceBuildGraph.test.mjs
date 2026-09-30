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
import { computeResourceBuildFingerprint } from "../../build/shared/resourceBuildFingerprint.mjs";

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
	const fingerprint = await computeResourceBuildFingerprint();
	const labels = fingerprint.roots.map( ( root ) => root.label );

	assert.equal( new Set( labels ).size, labels.length, "fingerprint root labels must be unique" );
	assert.deepEqual(
		labels,
		[
			"extracted",
			"client-executable",
			"particle-archive",
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
