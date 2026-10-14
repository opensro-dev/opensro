/*
===========================================================================

moonlit-thief-suit.test.mjs - the server's SRO_MOONLIT_THIEF_SUIT

Port-only, not native (owner, 2026-10-15). A level-20 thief suit draws its
Moonlit Ash model only while the server publishes the rule; off, and for
every suit without a Moonlit model, the retail model is drawn. The flag
reaches the gameplay snapshot and clears on a native reentry.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { assembleEquipmentAppearance } = await import(
	sourceFileUrl( "src/engine/foundation/animation/equipment-appearance.ts" ).href
);
const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);

// ITEM_CH_M_TRADE_THIEF_04 (level 20, thief_02) and _07 (thief_03).
const LEVEL_20_SUIT = 9026, HIGHER_SUIT = 2164;
const JOB_SLOT = 8;

/*
================
dress

The two suits as the visuals build publishes them for a Chinese man: the
level-20 suit carries the Moonlit entry, the higher suit does not.
================
*/
function dress() {
	const record = ( model, entry ) => ({
		slot: JOB_SLOT,
		armorClass: 0,
		thiefSuit: true,
		visualMask: 0x73f,
		visualPriority: 50,
		model,
		source: null,
		bodies: { CH_M: entry }
	});
	const higher = { glb: "/assets/char/equipment/thief_03.glb", parts: [ "EQ0" ], covers: { EQ0: [ 0, 2 ] } };
	return {
		equipment: {
			[LEVEL_20_SUIT]: record( "res/item/china/man_item/thief_02.bsr", {
				glb: "/assets/char/equipment/thief_02.glb",
				parts: [ "EQ0" ],
				covers: { EQ0: [ 0 ] },
				moonlit: { ...higher, glb: "/assets/char/equipment/thief_03_moonlit.glb" }
			} ),
			[HIGHER_SUIT]: record( "res/item/china/man_item/thief_03.bsr", higher )
		}
	};
}

/*
================
suitModel

The model one worn suit draws.
================
*/
function suitModel( refObjId, moonlitThiefSuit ) {
	const assembly = assembleEquipmentAppearance( {
		resource: { codename: "CHAR_CH_MAN_ADVENTURER", cover: { EQ0: 0 } },
		dress: dress(),
		equipment: [ { slot: JOB_SLOT, refObjId, plus: 0 } ],
		avatars: [],
		hwanHair: false,
		mounted: false,
		weaponHidden: false,
		attachmentsHidden: false,
		fortressIndex: -1,
		player: true,
		ownerless: false,
		committedWear: [],
		freezeWear: true,
		moonlitThiefSuit
	} );
	return assembly.parts.filter( p => p.model.includes( "thief" ) ).map( p => p.model );
}

test("a level-20 thief suit draws Moonlit Ash only with the server's rule", () => {
	assert.deepEqual( suitModel( LEVEL_20_SUIT, false ), [ "/assets/char/equipment/thief_02.glb" ] );
	assert.deepEqual( suitModel( LEVEL_20_SUIT, undefined ), [ "/assets/char/equipment/thief_02.glb" ] );
	assert.deepEqual( suitModel( LEVEL_20_SUIT, true ), [ "/assets/char/equipment/thief_03_moonlit.glb" ] );
	// The higher suit keeps its retail look either way.
	assert.deepEqual( suitModel( HIGHER_SUIT, true ), [ "/assets/char/equipment/thief_03.glb" ] );
});

test("the server's Moonlit rule reaches the snapshot and clears on a native reentry", () => {
	const game = createGameplay( () => {} );
	const pose = {
		gid: 7,
		refObjId: 1907,
		kind: "player",
		name: "Tester",
		regionId: 0x6b4f,
		x: 60,
		y: 10,
		z: 100,
		heading: 0
	};
	game.bootstrap( { simulationProtocolVersion: 1, moonlitThiefSuit: true } );
	game.seed( pose );
	assert.equal( game.take()?.moonlitThiefSuit, true );
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( pose );
	assert.equal( game.take()?.moonlitThiefSuit, undefined );
	game.dispose();
});
