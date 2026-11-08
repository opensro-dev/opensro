/*
===========================================================================
penguin-blizzard.test.mjs - owned storm art, five-second lifetime and cast rig.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const { createProceduralEffect } = await import(
	"../../src/engine/runtime/characters/resources/procedural-effects.ts"
);
const { decodeNativeTextureLevel } = await import( "../../src/engine/foundation/assets/native-texture.ts" );
const { createModelDecoder } = await import( "../../src/engine/runtime/assets/worker/model/model.ts" );
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { withPenguinBlizzard } = await import( "../../src/engine/foundation/animation/bear-roar.ts" );
const { penguinEffectActors } = await import( "../../src/engine/foundation/animation/penguin-effects.ts" );

test("Blizzard uses four bounded detailed meshes and real transparent texture gutters", () => {
	for (
		const name of [
			"penguin-frost-sigil",
			"penguin-blizzard-wind",
			"penguin-blizzard-snow",
			"penguin-blizzard-ice"
		]
	) {
		const resource = defined( createProceduralEffect( "/assets/effects/" + name ) );
		assert.ok( resource.model.nodes.length <= 64 );
		for ( const primitive of resource.model.primitives ) {
			const geometry = primitive.geometry;
			assert.ok( geometry.positions.length / 3 < 3000 );
			assert.ok( geometry.positions.every( Number.isFinite ) );
			assert.ok( geometry.indices.every( i => i < geometry.positions.length / 3 ) );
			assert.equal( defined( geometry.material ).fadeAlphaOnly, false );
		}
		for ( const clip of resource.model.clips ) {
			for ( const channel of clip.channels ) assert.ok( channel.values.every( Number.isFinite ) );
		}
		const texture = resource.images[0];
		if ( texture.format !== "bc3-rgba-unorm" ) continue;
		const rgba = decodeNativeTextureLevel( texture, 0 );
		for ( const x of [ 0, 255, 256, 511 ] ) {
			for ( let y = 0; y < 512; y++ ) assert.equal( rgba[(y * 512 + x) * 4 + 3], 0 );
		}
	}
});

test("all Blizzard books draw until five seconds, while the caster bloom is brief", () => {
	const origin = { regionId: 257, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
		ids = Array.from( { length: 17 }, ( _, i ) => 100 + i );
	for ( const action of [ 0xd3, 0xd4, 0xd5 ] ) {
		assert.equal( penguinEffectActors( action, 4.8, origin, ids ).length, 4 );
		assert.deepEqual( penguinEffectActors( action, 5, origin, ids ), [] );
	}
	assert.equal( penguinEffectActors( 34, .4, origin, ids ).length, 3 );
	assert.deepEqual( penguinEffectActors( 34, 1.25, origin, ids ), [] );
});

for ( const rig of [ "p_penguin01", "p_penguin02" ] ) {
	test( rig + ": cast pose lifts flippers and keeps both feet planted", () => {
		const decoder = createModelDecoder(),
			original = decoder.character(
				decoder.decode( readPublishedAssetBytesSync( "/assets/npc/cos/" + rig + ".glb", CLIENT_PUBLIC_ROOT ) )
			);
		const model = withPenguinBlizzard( { ...original, images: [] } ), pose = createCharacterPose( model );
		assert.equal( model.nodes, original.nodes );
		assert.equal( withPenguinBlizzard( model ), model );
		pose.evaluate( "stand", 0, false );
		const feet = [ "L", "R" ].map( side => defined( pose.socket( "Bip01 " + side + " Foot" ) ) );
		const hands = [ "L", "R" ].map( side => defined( pose.socket( "Bip01 " + side + " Hand" ) ) );
		pose.evaluate( "pet-blizzard", .55, false );
		for ( const [i, side] of [ "L", "R" ].entries() ) {
			const foot = defined( pose.socket( "Bip01 " + side + " Foot" ) );
			for ( const axis of [ 12, 13, 14 ] ) assert.ok( Math.abs( foot[axis] - feet[i][axis] ) < .02 );
			assert.ok(
				defined( pose.socket( "Bip01 " + side + " Hand" ) )[13] > hands[i][13] + .1,
				"flipper should rise"
			);
		}
	} );
}
