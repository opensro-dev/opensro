/*
===========================================================================

hwan-level.test.mjs - the Blue Berserk level frame (port-only 0x3FC0)

The worker decodes the frame whole or not at all, keeps the latest level per
object, and drops an object told level 0.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createHwanLevel, OP_HWAN_LEVEL, HWAN_LEVEL_MAX } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/hwan-level/hwan-level.ts"
);

function levelFrame( gid, level ) {
	const payload = new Uint8Array( 5 );
	new DataView( payload.buffer ).setUint32( 0, gid, true );
	payload[4] = level;
	return { opcode: OP_HWAN_LEVEL, payload };
}

test("the level frame is 0x3FC0, five bytes: object id then level", () => {
	assert.equal( OP_HWAN_LEVEL, 0x3fc0 );
	const owner = createHwanLevel();
	assert.equal( owner.receive( { opcode: 0x3fba, payload: new Uint8Array( 5 ) } ), false );
	assert.equal( owner.receive( levelFrame( 0x01020304, 3 ) ), true );
	assert.equal( owner.levelOf( 0x01020304 ), 3 );
	assert.equal( owner.levelOf( 7 ), 0 );
});

test("a later frame replaces the level, and level 0 forgets the object", () => {
	const owner = createHwanLevel();
	owner.receive( levelFrame( 9, 1 ) );
	owner.receive( levelFrame( 9, 4 ) );
	assert.equal( owner.levelOf( 9 ), 4 );
	owner.receive( levelFrame( 9, 0 ) );
	assert.equal( owner.levelOf( 9 ), 0 );
	owner.receive( levelFrame( 9, HWAN_LEVEL_MAX ) );
	owner.clear();
	assert.equal( owner.levelOf( 9 ), 0 );
});

test("a malformed level frame is a protocol error", () => {
	const owner = createHwanLevel();
	assert.throws( () => owner.receive( { opcode: OP_HWAN_LEVEL, payload: new Uint8Array( 4 ) } ), /length/ );
	assert.throws( () => owner.receive( levelFrame( 1, HWAN_LEVEL_MAX + 1 ) ), /level/ );
});

const { hwanSkin } = await import( "../../src/engine/foundation/gameplay/hwan-skin.ts" );

test("a Blue Berserk names the blue twin of every gauge picture, and nothing else", () => {
	const root = "/assets/images/Media_extracted/interface/playerminiinfo/";
	for (
		const name of [
			"pmi_jahwan",
			"pmi_jahwan_burn",
			"pmi_jahwan_face",
			"pmi_jahwan_glow",
			"pmi_jahwan_button_focus"
		]
	) {
		const blue = hwanSkin( root + name + ".png", 2 );
		assert.equal( blue, root + name.replace( "pmi_jahwan", "pmi_jahwan_02" ) + ".png" );
		assert.equal( hwanSkin( blue, 2 ), blue, "already blue" );
		assert.equal( hwanSkin( root + name + ".png", 0 ), root + name + ".png" );
		assert.equal( hwanSkin( root + name + ".png", undefined ), root + name + ".png" );
	}
	assert.equal(
		hwanSkin( "/assets/images/Media_extracted/interface/playerminiinfo/pmi_hp.png", 3 ),
		"/assets/images/Media_extracted/interface/playerminiinfo/pmi_hp.png"
	);
});

test("the level frame does not share an opcode with another port frame", async () => {
	const { OP_CARAVAN_STARS } = await import(
		"../../src/engine/runtime/simulation/worker/session/world/gameplay/caravan-stars/caravan-stars.ts"
	);
	assert.notEqual( OP_HWAN_LEVEL, OP_CARAVAN_STARS );
});
