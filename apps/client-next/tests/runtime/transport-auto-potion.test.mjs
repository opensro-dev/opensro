/*
===========================================================================

transport-auto-potion.test.mjs - tests for transport-auto-potion.ts

The port-only Experimental "Transport auto-potion": the summoned trade
transport is healed at the auto-potion HP percentage with a transport HP
potion from the bag. Loads the TypeScript sources through the shared
native loader.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { transportAutoPotion, transportPotionBlocked, isTransportHpPotion, TRANSPORT_POTION_RETRY_MS } = await import(
	"../../src/engine/foundation/gameplay/transport-auto-potion.ts"
);
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

// ETC 3/3/1/4 (ITEM_ETC_COS_HP_POTION_01) and 3/3/1/1 (ITEM_ETC_HP_POTION_01).
const COS_HP_POTION = 0x6c | 1 << 7 | 4 << 11;
const HP_POTION = 0x6c | 1 << 7 | 1 << 11;
const TRANSPORT = 41, HORSE = 42, MAX_HP = 1000;

/*
================
bagItem
================
*/
const bagItem = ( slot, typeFlags, quantity, refObjId ) => ({
	slot,
	typeFlags,
	quantity,
	refObjId,
	plus: 0,
	durability: 0,
	variance: "0",
	magic: []
});

/*
================
input
================
*/
const input = ( change = {} ) => ({
	records: [
		{ gid: HORSE, refObjId: 2, band: 1, hp: 10, mp: 0, status: 0, dead: false },
		{ gid: TRANSPORT, refObjId: 1, band: 2, hp: 400, mp: 0, status: 0, dead: false }
	],
	maxHp: refObjId => refObjId === 1 || refObjId === 2 ? MAX_HP : 0,
	items: [
		bagItem( 20, COS_HP_POTION, 3, 9 ),
		bagItem( 14, HP_POTION, 9, 8 ),
		bagItem( 17, COS_HP_POTION, 1, 9 )
	],
	cooldowns: [],
	percent: 50,
	now: 10_000,
	blocked: false,
	...change
});

test("the option is off unless explicitly turned on", () => {
	for ( const value of [ undefined, {}, { transportAutoPotion: 1 }, { transportAutoPotion: "true" } ] ) {
		assert.equal( experimentalOptions( value ).transportAutoPotion, false );
	}
	assert.equal( experimentalOptions( { transportAutoPotion: true } ).transportAutoPotion, true );
});

test("a transport at or below the percentage drinks the lowest-slot transport potion", () => {
	assert.deepEqual( transportAutoPotion( input() ), { slot: 17, companionGid: TRANSPORT } );
	// 70C28F: trunc(1000 * fround(0.5)) = 500; 500 heals, 501 does not.
	const at = hp =>
		input( { records: [ { gid: TRANSPORT, refObjId: 1, band: 2, hp, mp: 0, status: 0, dead: false } ] } );
	assert.ok( transportAutoPotion( at( 500 ) ) );
	assert.equal( transportAutoPotion( at( 501 ) ), null );
	assert.ok( isTransportHpPotion( COS_HP_POTION ) );
	assert.equal( isTransportHpPotion( HP_POTION ), false );
});

test("only a living transport with a known maximum is watched", () => {
	// The riding horse at 1% is not the transport.
	const horseOnly = input( {
		records: [ { gid: HORSE, refObjId: 2, band: 1, hp: 10, mp: 0, status: 0, dead: false } ]
	} );
	assert.equal( transportAutoPotion( horseOnly ), null );
	const dead = input( {
		records: [ { gid: TRANSPORT, refObjId: 1, band: 2, hp: 0, mp: 0, status: 0, dead: true } ]
	} );
	assert.equal( transportAutoPotion( dead ), null );
	assert.equal( transportAutoPotion( input( { maxHp: () => 0 } ) ), null );
});

test("no potion, a cooling potion, a recent attempt or a blocking window sends nothing", () => {
	assert.equal(
		transportAutoPotion( input( { items: [ bagItem( 14, HP_POTION, 9, 8 ) ] } ) ),
		null
	);
	assert.equal( transportAutoPotion( input( { lastSentMs: 10_000 - TRANSPORT_POTION_RETRY_MS + 1 } ) ), null );
	assert.ok( transportAutoPotion( input( { lastSentMs: 10_000 - TRANSPORT_POTION_RETRY_MS } ) ) );
	assert.equal( transportAutoPotion( input( { blocked: true } ) ), null );
	assert.equal( transportPotionBlocked( {} ), false );
	assert.equal( transportPotionBlocked( { inventoryPending: true } ), true );
	assert.equal( transportPotionBlocked( { storage: { gid: 1 } } ), true );
	assert.equal( transportPotionBlocked( { stall: { phase: "owner" } } ), true );
	assert.equal( transportPotionBlocked( { stall: { phase: "naming" } } ), false );
	assert.equal( transportPotionBlocked( { npcConversation: { phase: "menu" } } ), true );
	assert.equal( transportPotionBlocked( { npcConversation: { phase: "closed" } } ), false );
});

const { uiFixture } = await import( "../helpers/ui-fixture.mjs" );

/*
================
uiRun

Drives the real UI for a second with one transport (refObjId 3914, a
published reference) at hp and a transport potion in slot 30; returns the
gameplay commands it sent.
================
*/
function uiRun( t, enabled, hp ) {
	const sent = [];
	const f = uiFixture( message => {
		if ( message.kind === "gameplay" ) sent.push( message.command );
	} );
	t.after( () => f.dispose() );
	Object.assign( f.state.gameplay, {
		cosRecords: [ { gid: 8, refObjId: 3914, band: 2, hp, mp: 0, status: 0, dead: false } ],
		inventory: [ ...f.state.gameplay.inventory, bagItem( 30, COS_HP_POTION, 2, 9 ) ],
		inventoryPending: false
	} );
	f.ui.event( { kind: "experimental-preferences", value: experimentalOptions( { transportAutoPotion: enabled } ) } );
	for ( let i = 0; i < 20; i++ ) f.ui.step( f.state, i * 100 );
	assert.equal( f.ui.stats().error, null );
	return sent.filter( command => command.kind === "item-use" );
}

test("the UI heals a low transport only with the row on, and never a healthy one", t => {
	assert.deepEqual( uiRun( t, false, 1 ), [], "off is the native client" );
	const uses = uiRun( t, true, 1 );
	assert.ok( uses.length >= 1, "a transport at 1 HP is healed" );
	assert.deepEqual( uses[0], { kind: "item-use", slot: 30, companionGid: 8 } );
	assert.deepEqual( uiRun( t, true, 87_829 ), [], "a full transport is left alone" );
});
