/*
===========================================================================

backgroundInstallAsset.test.mjs - the published background install list

buildBackgroundInstallAsset decides which presentation files the client
makes local after world entry, and in what order. These tests pin the tier
rules (interface art first, combat second, other effect sounds third, music
never) and the published document.

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

/*
================
tier

The tier of one path in a publication holding only the given siblings.
================
*/
function tier( publicPath, siblings = [] ) {
	return backgroundInstallTier( publicPath, new Set( [ publicPath, ...siblings ] ) );
}

test("tiers: the player's combat set first, other effect sounds second, music never", () => {
	assert.equal( tier( "/assets/audio/sfx/prim/snd/player/swing.wav" ), "combat" );
	assert.equal( tier( "/assets/audio/sfx/prim/snd/skill2/fire.wav" ), "combat" );
	assert.equal( tier( "/assets/audio/sfx/prim/snd/common/hit.wav" ), "combat" );
	assert.equal( tier( "/assets/images/particles_extracted/spark.png" ), "combat" );
	assert.equal( tier( "/assets/skillfx/item/blade.glb" ), "combat" );
	assert.equal( tier( "/assets/audio/sfx/prim/snd/monster/roar.wav" ), "world-sounds" );
	assert.equal( tier( "/assets/audio/sfx/prim/snd/env/wind.wav" ), "world-sounds" );
	assert.equal( tier( "/assets/audio/music/jangan_town.mp3" ), null );
	assert.equal( tier( "/assets/skillfx/manifest.json" ), null );
	assert.equal( tier( "/assets/world/china/object-textures/a.png" ), null );
});

test("tiers: interface art is what the native-ui and ui-icons packs hold", () => {
	const ROOT = "/assets/images/media_extracted/";
	assert.equal( tier( ROOT + "interface/store/store_window.png" ), "interface" );
	assert.equal( tier( ROOT + "icon/item/china/weapon/sword_01_a.png" ), "interface" );
	// A button's state images, and its normal image only beside one of them.
	assert.equal( tier( ROOT + "etc/ok_button_focus.png" ), "interface" );
	assert.equal( tier( ROOT + "etc/ok_button.png", [ ROOT + "etc/ok_button_press.png" ] ), "interface" );
	assert.equal( tier( ROOT + "etc/ok_button.png" ), null );
	// Interface art that is not an image, and images that are not interface.
	assert.equal( tier( ROOT + "interface/store/store_window.ddj" ), null );
	assert.equal( tier( "/assets/images/map_extracted/tile2d/1x1.png" ), null );
	// Particle textures stay in the combat tier.
	assert.equal( tier( "/assets/images/particles_extracted/spark.png" ), "combat" );
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
			"assets/audio/music/theme.mp3",
			"assets/images/Media_extracted/icon/item/b.png",
			"assets/images/Media_extracted/interface/store/a.png"
		];
		for ( const file of files ) {
			await mkdir( path.dirname( path.join( root, file ) ), { recursive: true } );
			await writeFile( path.join( root, file ), "x" );
		}

		const counts = await buildBackgroundInstallAsset( { publicRoot: root } );
		const document = JSON.parse(
			await readFile( path.join( root, ...BACKGROUND_INSTALL_PUBLIC_PATH.slice( 1 ).split( "/" ) ), "utf8" )
		);

		assert.deepEqual( counts, { "interface": 2, "combat": 3, "world-sounds": 1 } );
		assert.equal( document.format, BACKGROUND_INSTALL_FORMAT );
		assert.equal( document.version, BACKGROUND_INSTALL_VERSION );
		assert.deepEqual( document.tiers, [
			{
				name: "interface",
				paths: [
					"/assets/images/Media_extracted/icon/item/b.png",
					"/assets/images/Media_extracted/interface/store/a.png"
				]
			},
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
