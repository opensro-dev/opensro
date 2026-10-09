/*
===========================================================================

boothModelAssets.test.mjs - compile shipped stall booths into isolated output

Exercises the NPC compiler on the two native defaults and enabled mall booth
resources without publishing or changing any shared generated assets.

===========================================================================
*/

import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { bakeCharacterResource } from "../../build/char/buildNpcModelAssets.mjs";
import { loadBoothModelRoster } from "../../build/char/boothModelRoster.mjs";
import { bakeNpcSecondaryResources } from "../../build/char/npcSecondaryResources.mjs";
import { splitNpcManifestModels } from "../../build/shared/npcManifest.mjs";
import { parseBan } from "../../build/char/formats.mjs";
import { readPublishedAssetBytesSync, readPublishedAssetJsonSync } from "../../lib/publishedAsset.mjs";
import { retailTextdataRoot } from "../../build/world/paths.mjs";

test("all six shipped stall booths compile through the shared character resource path", async t => {
	const root = await fs.mkdtemp( path.join( os.tmpdir(), "sro-native-booths-" ) );
	t.after( () => fs.rm( root, { recursive: true, force: true } ) );
	const requests = loadBoothModelRoster( retailTextdataRoot );
	assert.deepEqual( requests.map( row => row.bsrPath ).sort(), [
		"res/item/avatar/booth_mob_bigeyeghost.bsr",
		"res/item/avatar/booth_mob_earthghost.bsr",
		"res/item/avatar/booth_mob_mangyang.bsr",
		"res/item/avatar/booth_special_monster_01.bsr",
		"res/item/china/item/cj_store.bsr",
		"res/item/europe/item/euro_streetstall01.bsr"
	] );
	const context = {
		publicAssetsRoot: root,
		models: [],
		bakedByBsr: new Map(),
		outputOwners: new Map(),
		retailAnimationModels: new Map(),
		retailAnimationResources: new Map(),
		bake: bakeCharacterResource
	};
	const counts = await bakeNpcSecondaryResources( context, requests );
	assert.deepEqual( context.models.filter( row => row.error ), [] );
	assert.deepEqual( counts, { built: requests.length, covered: requests.length, reused: 0 } );
	const { boothModels, resources } = splitNpcManifestModels( context.models );
	assert.deepEqual( resources, {} );
	for ( const [bsr, resource] of Object.entries( boothModels ) ) {
		assert.ok( Array.isArray( resource.clips ), bsr );
		assert.ok( Array.isArray( resource.particleModifiers ), bsr );
		assert.equal( typeof resource.animationStates, "object", bsr );
		assert.ok( resource.staticPose || resource.clips.includes( "stand" ), bsr );
		const bytes = await fs.readFile( path.join( root, resource.glb.slice( "/assets/".length ) ) );
		assert.equal( bytes.toString( "ascii", 0, 4 ), "glTF", bsr );
		assert.equal( bytes.byteLength, resource.bytes, bsr );
		assert.ok( bytes.byteLength > 0, bsr );
	}
});

test("every published player body exposes native default motion 80 for stall action 15", () => {
	// Owner-verified 8E6B50/8E64C0 start/stop motion 80 with 200 ms blends.
	// Blend selection belongs to the actor; the resource retains its authored BAN.
	const motionId = "80";
	const manifest = readPublishedAssetJsonSync( "/assets/anim/manifest.json" );
	const players = Object.entries( manifest.models ).filter( ( [code] ) => code.startsWith( "CHAR_" ) );
	assert.equal( players.length, 52, "shipped player body coverage changed" );
	const clips = new Map();
	for ( const [code, row] of players ) {
		const state = row.animationSets?.default?.[motionId];
		assert.ok( state?.url, `${code}: missing native:default:80` );
		assert.match( state.url, /^\/assets\/anim\/.+\.ban$/ );
		if ( !clips.has( state.url ) ) {
			const bytes = readPublishedAssetBytesSync( state.url );
			assert.equal( bytes.byteLength, state.bytes, code );
			clips.set( state.url, parseBan( bytes, state.path ) );
		}
		assert.equal( clips.get( state.url ).durationMs, state.durationMs, code );
		assert.ok( state.durationMs > 0, code );
	}
});
