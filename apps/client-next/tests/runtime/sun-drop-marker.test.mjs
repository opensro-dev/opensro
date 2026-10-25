/*
===========================================================================

sun-drop-marker.test.mjs - tests for ground-item.ts, system-notices.ts,
experimental-options.ts, entities.ts, sun-drop-markers.ts

Port-only, not native (#1640): the tiered seal notices' Sun highlight and
the Experimental marker that rings the player's own degree-9+ Seal of
Sun drops. Loads the TypeScript sources directly through the shared
native loader (tests/helpers/native-source-loader.mjs), so the tests
exercise the same modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { sunSealDrop } = await import( "../../src/engine/foundation/gameplay/ground-item.ts" );
const {
	isSunDropNoticeText,
	SEAL_NOTICE_COLOR_MOON,
	SEAL_NOTICE_COLOR_STAR,
	SEAL_NOTICE_COLOR_SUN,
	sealNoticeColorArgb
} = await import( "../../src/engine/foundation/gameplay/system-notices.ts" );
const { sealNoticeChatColor } = await import( "../../src/engine/foundation/ui/chat-layout.ts" );
const { experimentalOptions, experimentalGameplay } = await import(
	"../../src/engine/foundation/ui/experimental-options.ts"
);
const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);
const { createSunDropMarkers } = await import( "../../src/engine/runtime/characters/sun-drop-markers.ts" );

/*
================
eventEntity

The stamped entity a spawn or state event carries.
================
*/
/** @param {import("../../src/engine/contracts/world.ts").WorldEvent} event */
function eventEntity( event ) {
	return /** @type {{ entity: import("../../src/engine/contracts/world.ts").EntityState }} */ (event).entity;
}

test("sunSealDrop admits degree-9+ Seal of Sun equipment codenames only", () => {
	for (
		const codename of [
			"ITEM_CH_BLADE_09_C_RARE",
			"ITEM_EU_SWORD_10_C_RARE",
			"ITEM_CH_SPEAR_11_C_RARE",
			"ITEM_CH_M_CLOTHES_09_AA_C_RARE",
			"ITEM_CH_W_LIGHT_10_BA_C_RARE"
		]
	) {
		assert.equal( sunSealDrop( codename ), true, codename );
	}
	for (
		const codename of [
			"ITEM_CH_BLADE_08_C_RARE",
			"ITEM_CH_BLADE_09_B_RARE",
			"ITEM_CH_BLADE_09_A_RARE",
			"ITEM_CH_BLADE_09_C",
			"ITEM_CH_BLADE_09_C_RARE_BLUE",
			"ITEM_ETC_GOLD_01",
			""
		]
	) {
		assert.equal( sunSealDrop( codename ), false, codename );
	}
});

test("isSunDropNoticeText reads the tiered lead and the older parenthetical, never a plus notice", () => {
	assert.equal( isSunDropNoticeText( "SEAL OF SUN: Tiger Girl has dropped Sun Blade for Looter" ), true );
	assert.equal( isSunDropNoticeText( "Looter has dropped Sun Blade (Seal Of Sun) from Tiger Girl" ), true );
	assert.equal( isSunDropNoticeText( "Tiger Girl has dropped Moon Blade (Seal Of Moon) for Looter" ), false );
	assert.equal( isSunDropNoticeText( "Looter has dropped Star Blade (Seal Of Star) from Tiger Girl" ), false );
	assert.equal( isSunDropNoticeText( "Looter advanced Sun Blade (Seal Of Sun) to +8" ), false );
	assert.equal( isSunDropNoticeText( "SEAL OF MOON: Tiger Girl has dropped Moon Blade for Looter" ), false );
});

test("sealNoticeColorArgb colours Star, Moon and Sun by tier and leaves unsealed items alone", () => {
	assert.equal( SEAL_NOTICE_COLOR_STAR, 0xffffd953 );
	assert.equal( sealNoticeColorArgb( 2, 16 ), SEAL_NOTICE_COLOR_STAR );
	assert.equal( sealNoticeColorArgb( 2, 17 ), SEAL_NOTICE_COLOR_MOON );
	assert.equal( sealNoticeColorArgb( 2, 18 ), SEAL_NOTICE_COLOR_SUN );
	assert.equal( sealNoticeColorArgb( 2, 1 ), SEAL_NOTICE_COLOR_STAR );
	assert.equal( sealNoticeColorArgb( 2, 2 ), SEAL_NOTICE_COLOR_MOON );
	assert.equal( sealNoticeColorArgb( 2, 3 ), SEAL_NOTICE_COLOR_SUN );
	assert.equal( sealNoticeColorArgb( 1, 18 ), undefined );
	assert.equal( sealNoticeColorArgb( 0, 18 ), undefined );
	assert.equal( sealNoticeColorArgb( 2, 0 ), undefined );
	assert.equal( sealNoticeColorArgb( undefined, 18 ), undefined );
	assert.equal( sealNoticeColorArgb( 2, undefined ), undefined );
});

/*
================
noticeLine

One channel-7 notice line, with the linked item's seal fields when given.
================
*/
/** @param {string} text @param {{ rarity: number, itemClass: number }} [fields] */
function noticeLine( text, fields ) {
	return /** @type {import("../../src/engine/contracts/gameplay.ts").ChatLine} */ ({
		channel: 7,
		name: "",
		text,
		outgoing: false,
		...(fields ? { notice: { item: { name: "Blade", tooltip: { fields } } } } : {})
	});
}

test("a drop or plus notice line takes its seal tier's colour; unsealed lines keep the channel colour", () => {
	const gold = [ 1, 0xd9 / 255, 0x53 / 255, 1 ],
		silverBlue = [ 0xc9 / 255, 0xd9 / 255, 0xf2 / 255, 1 ],
		orangeRed = [ 1, 0x45 / 255, 0x17 / 255, 1 ];
	// The linked item's fields decide, for drops and pluses alike.
	assert.deepEqual(
		sealNoticeChatColor(
			noticeLine( "Looter has dropped Blade (Seal Of Star) from Tiger Girl", { rarity: 2, itemClass: 16 } )
		),
		gold
	);
	assert.deepEqual(
		sealNoticeChatColor(
			noticeLine( "Looter has dropped Blade (Seal Of Moon) from Tiger Girl", { rarity: 2, itemClass: 17 } )
		),
		silverBlue
	);
	assert.deepEqual(
		sealNoticeChatColor(
			noticeLine( "SEAL OF SUN: Looter has dropped Blade from Tiger Girl", { rarity: 2, itemClass: 18 } )
		),
		orangeRed
	);
	assert.deepEqual(
		sealNoticeChatColor( noticeLine( "Looter advanced Blade (Seal Of Sun) to +9", { rarity: 2, itemClass: 18 } ) ),
		orangeRed
	);
	assert.deepEqual(
		sealNoticeChatColor(
			noticeLine( "Looter advanced Blade (Seal Of Star) to +11", { rarity: 2, itemClass: 16 } )
		),
		gold
	);
	// Unsealed linked items and other channels keep the channel colour.
	assert.equal(
		sealNoticeChatColor( noticeLine( "Looter has dropped Blade from Tiger Girl", { rarity: 0, itemClass: 0 } ) ),
		undefined
	);
	assert.equal(
		sealNoticeChatColor( {
			...noticeLine( "Looter has dropped Blade (Seal Of Sun) from Tiger Girl", { rarity: 2, itemClass: 18 } ),
			channel: 1
		} ),
		undefined
	);
	// Without a decoded item, a drop line reads its tier off the text; a
	// plus line cannot, and stays uncoloured.
	assert.deepEqual(
		sealNoticeChatColor( noticeLine( "Looter has dropped Blade (Seal Of Moon) from Tiger Girl" ) ),
		silverBlue
	);
	assert.deepEqual(
		sealNoticeChatColor( noticeLine( "SEAL OF SUN: Looter has dropped Blade from Tiger Girl" ) ),
		orangeRed
	);
	assert.equal( sealNoticeChatColor( noticeLine( "Looter advanced Blade (Seal Of Sun) to +8" ) ), undefined );
});

test("the Sun drop marker row is off unless explicitly turned on", () => {
	assert.equal( experimentalOptions().sunDropMarker, false );
	assert.equal( experimentalOptions( { sunDropMarker: true } ).sunDropMarker, true );
	assert.equal( experimentalOptions( { sunDropMarker: 1 } ).sunDropMarker, false );
	assert.equal( experimentalGameplay( experimentalOptions() ).sunDropMarker, false );
	assert.equal( experimentalGameplay( experimentalOptions( { sunDropMarker: true } ) ).sunDropMarker, true );
});

/*
================
dropRow

One equipment drop row (decodeGroundItem, band 0x20) for the Sun blade
reference the admission test seeds: plus byte, then identity, pose,
owner and tint.
================
*/
function dropRow( gid, ownerJid ) {
	const row = Buffer.alloc( 32 );
	row.writeUInt32LE( 11459, 0 );
	row.writeUInt8( 0, 4 );
	row.writeUInt32LE( gid, 5 );
	row.writeUInt16LE( 0x6b4f, 9 );
	row.writeFloatLE( 10, 11 );
	row.writeFloatLE( 0, 15 );
	row.writeFloatLE( 10, 19 );
	row.writeUInt16LE( 0, 23 );
	row.writeUInt8( 1, 25 );
	row.writeUInt32LE( ownerJid, 26 );
	row.writeUInt8( 0, 30 );
	row.writeUInt8( 0, 31 );
	return row;
}

test("the simulation stamps only the player's own Sun drops, and restamps when the row flips", () => {
	const entities = createEntities();
	entities.bootstrap( {
		protocolVersion: 2,
		nativeResult: 1,
		refObjSnapshot: [],
		refItemSnapshot: [ {
			refObjId: 11459,
			typeFlags: 0x8ac,
			codename: "ITEM_CH_BLADE_09_C_RARE",
			name: "Sun Blade"
		} ],
		localPlayerEntry: { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } }
	} );
	const flush = () => {
		const batch = entities.take();
		if ( batch ) entities.ack( batch.sequence );
		return batch;
	};
	flush();
	entities.sunDropMarker( true );
	// The drop lands before the local player is latched: unmarked.
	entities.receive( { opcode: 0x30d7, payload: dropRow( 777, 4242 ) }, 100 );
	let batch = defined( flush() );
	assert.equal(
		defined( eventEntity( defined( batch.events.find( e => e.kind === "spawn" ) ) ).groundItem ).sunMarker,
		undefined
	);
	// The latch restamps the drops already on the ground.
	const latch = Buffer.alloc( 8 );
	latch.writeUInt32LE( 4242, 0 );
	entities.receive( { opcode: 0x32a6, payload: latch }, 101 );
	batch = defined( flush() );
	const restamped = eventEntity(
		defined( batch.events.find( e => e.kind === "state" && e.entity.gid === 777 ) )
	);
	assert.equal( defined( restamped.groundItem ).sunMarker, true );
	// Another player's Sun drop is never stamped.
	entities.receive( { opcode: 0x30d7, payload: dropRow( 778, 9999 ) }, 102 );
	batch = defined( flush() );
	const foreign = eventEntity( defined( batch.events.find( e => e.kind === "spawn" ) ) );
	assert.equal( defined( foreign.groundItem ).sunMarker, undefined );
	// Row off restamps again: the native frame carries no stamp.
	entities.sunDropMarker( false );
	batch = defined( flush() );
	const unmarked = eventEntity(
		defined( batch.events.find( e => e.kind === "state" && e.entity.gid === 777 ) )
	);
	assert.equal( defined( unmarked.groundItem ).sunMarker, undefined );
	entities.dispose();
});

test("the marker owner publishes the admitted set only when it changes, and clear empties it once", () => {
	const calls = [];
	const markers = createSunDropMarkers( {
		/*
		================
		setSunDropMarkers
		================
		*/
		setSunDropMarkers( rows ) {
			calls.push( rows );
		}
	} );
	const marked = /** @type {import("../../src/engine/contracts/world.ts").EntityState} */ ({
			gid: 42,
			refObjId: 11459,
			kind: "ground-item",
			regionId: 250,
			x: 1,
			y: 2,
			z: 3,
			heading: 0,
			name: "Sun Blade",
			groundItem: { typeFlags: 0x8ac, goldAmount: 0, tint: 0, sunMarker: true }
		}),
		unmarked = { ...marked, gid: 43, groundItem: { typeFlags: 0x8ac, goldAmount: 0, tint: 0 } };
	markers.step( [ unmarked ] );
	assert.deepEqual( calls, [] );
	markers.step( [ unmarked, marked ] );
	assert.deepEqual( calls, [ [ { gid: 42, pose: { regionId: 250, x: 1, y: 2, z: 3 } } ] ] );
	markers.step( [ marked ] );
	assert.equal( calls.length, 1 );
	markers.step( [] );
	assert.deepEqual( calls.at( -1 ), [] );
	markers.step( [ marked ] );
	markers.clear();
	assert.deepEqual( calls.at( -1 ), [] );
	const published = calls.length;
	markers.clear();
	assert.equal( calls.length, published );
});
