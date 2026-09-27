/*
===========================================================================

backgroundInstallAsset.test.mjs - the published background install list

buildBackgroundInstallAsset decides which presentation files the client
makes local after world entry, and in what order. These tests pin the tier
rules (combat first, other effect sounds second, music never) and the
published document.

===========================================================================
*/

import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
	BACKGROUND_INSTALL_FORMAT,
	BACKGROUND_INSTALL_PUBLIC_PATH,
	BACKGROUND_INSTALL_VERSION,
	backgroundInstallTier,
	buildBackgroundInstallAsset
} from "../../build/data/buildBackgroundInstallAsset.mjs";

test("tiers: the player's combat set first, other effect sounds second, music never", () => {
	assert.equal( backgroundInstallTier( "/assets/audio/sfx/prim/snd/player/swing.wav" ), "combat" );
	assert.equal( backgroundInstallTier( "/assets/audio/sfx/prim/snd/skill2/fire.wav" ), "combat" );
	assert.equal( backgroundInstallTier( "/assets/audio/sfx/prim/snd/common/hit.wav" ), "combat" );
	assert.equal( backgroundInstallTier( "/assets/images/particles_extracted/spark.png" ), "combat" );
	assert.equal( backgroundInstallTier( "/assets/skillfx/item/blade.glb" ), "combat" );
	assert.equal( backgroundInstallTier( "/assets/audio/sfx/prim/snd/monster/roar.wav" ), "world-sounds" );
	assert.equal( backgroundInstallTier( "/assets/audio/sfx/prim/snd/env/wind.wav" ), "world-sounds" );
	assert.equal( backgroundInstallTier( "/assets/audio/music/jangan_town.mp3" ), null );
	assert.equal( backgroundInstallTier( "/assets/skillfx/manifest.json" ), null );
	assert.equal( backgroundInstallTier( "/assets/world/china/object-textures/a.png" ), null );
});

test("publishes the listed files by tier, sorted, without precompressed sidecars", async () => {
	const root = await mkdtemp( path.join( os.tmpdir(), "background-install-" ) );
	try {
		const files = [
			"assets/audio/sfx/prim/snd/player/b.wav",
			"assets/audio/sfx/prim/snd/player/a.wav",
			"assets/audio/sfx/prim/snd/monster/m.wav",
			"assets/skillfx/item/blade.glb",
			"assets/skillfx/item/blade.glb.br",
			"assets/audio/music/theme.mp3"
		];
		for ( const file of files ) {
			await mkdir( path.dirname( path.join( root, file ) ), { recursive: true } );
			await writeFile( path.join( root, file ), "x" );
		}

		const counts = await buildBackgroundInstallAsset( { publicRoot: root } );
		const document = JSON.parse(
			await readFile( path.join( root, ...BACKGROUND_INSTALL_PUBLIC_PATH.slice( 1 ).split( "/" ) ), "utf8" )
		);

		assert.deepEqual( counts, { "combat": 3, "world-sounds": 1 } );
		assert.equal( document.format, BACKGROUND_INSTALL_FORMAT );
		assert.equal( document.version, BACKGROUND_INSTALL_VERSION );
		assert.deepEqual( document.tiers, [
			{
				name: "combat",
				paths: [
					"/assets/audio/sfx/prim/snd/player/a.wav",
					"/assets/audio/sfx/prim/snd/player/b.wav",
					"/assets/skillfx/item/blade.glb"
				]
			},
			{ name: "world-sounds", paths: [ "/assets/audio/sfx/prim/snd/monster/m.wav" ] }
		] );
	} finally {
		await rm( root, { recursive: true, force: true } );
	}
});
