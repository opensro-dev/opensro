/*
===========================================================================

item-cooldowns.test.mjs - tests for item-cooldowns.ts,
quickslot-cooldown.ts, inventory.ts, gameplay.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
const p = await import( "../../src/engine/foundation/gameplay/item-cooldowns.ts" );
const { quickslotItemCooldownQuads, inventoryItemCooldownQuads } = await import(
	"../../src/engine/foundation/ui/quickslot-cooldown.ts"
);
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const context = { country: 0, abnormal: 0 }, word = category => 0xec | (category << 11);
/*
================
fixture
================
*/
function fixture( quantity = 3 ) {
	return {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ 1, 2, 3, 4 ].map( id => ({
			refObjId: id,
			typeFlags: word( id === 4 ? 2 : id ),
			nativeFields: { itemParam2_2a0: 0, itemParam4_2a8: 0 }
		}) ),
		equipItems: [ 1, 2, 3, 4 ].map( id => ({ refObjId: id, slot: id + 12, body: [ id, 0, 0, 0, quantity, 0 ] }) )
	};
}
const receipt = ( slot, quantity, category ) => Uint8Array.of( 1, slot, quantity, 0, 0xec, category << 3 );
test("native recovery receipt timings, abnormal bits and category boundaries", () => {
	for ( const country of [ 0, 1 ] ) {
		for ( const category of [ 1, 2, 3 ] ) {
			assert.equal( p.recoveryCooldownMs( category, {}, country, 0 ), country === 0 ? 1000 : 15000 );
			for ( const key of [ "itemParam2_2a0", "itemParam4_2a8" ] ) {
				assert.equal( p.recoveryCooldownMs( category, { [key]: 1 }, country, 0 ), 4000 );
			}
		}
	}
	assert.equal( p.recoveryCooldownMs( 1, {}, 0, 0x200000 ), 5000 );
	assert.equal( p.recoveryCooldownMs( 2, {}, 1, 0x400000 ), 19000 );
	assert.equal( p.recoveryCooldownMs( 1, {}, 0, 0x400000 ), 1000 );
	assert.equal( p.recoveryCooldownMs( 3, {}, 0, 0x600000 ), 1000 );
	assert.throws( () => p.recoveryCooldownMs( 2, {}, 3, 0 ) );
	for ( const type of [ 0x6c, 0xec, 0x9ec, 0x216c, 0x116e ] ) assert.equal( p.recoveryCategory( type ), null );
	assert.equal( p.recoveryCategory( 0x10ec ), 2 );
});
test("only acknowledged consumption starts a shared lane; rejected and duplicate replies cannot restart it", () => {
	const sent = [], owner = createInventory( f => sent.push( f ) );
	owner.bootstrap( fixture() );
	owner.use( 14, 100 );
	assert.deepEqual( owner.state().itemCooldowns, [] );
	owner.receive( 0xb5bd, Uint8Array.of( 2, 91 ), 101, context );
	assert.deepEqual( owner.state().itemCooldowns, [] );
	owner.use( 14, 200 );
	owner.receive( 0xb5bd, receipt( 14, 2, 2 ), 210, context );
	const snapshot = owner.state().itemCooldowns;
	assert.deepEqual( snapshot, [ { category: 2, startedAtMs: 210, durationMs: 1000 } ] );
	assert.equal( owner.use( 16, 1209 ), null );
	assert.equal( sent.length, 2, "another grade/slot cannot bypass lane" );
	assert.throws( () => owner.receive( 0xb5bd, receipt( 14, 2, 2 ), 500, context ), /Stale/ );
	assert.equal( owner.state().itemCooldowns, snapshot );
	owner.use( 13, 500 );
	assert.equal( sent.length, 3, "HP is independent" );
	owner.receive( 0xb5bd, receipt( 13, 2, 1 ), 501, context );
	assert.equal( snapshot.length, 1, "published timer arrays are immutable" );
	assert.equal( owner.state().itemCooldowns.length, 2 );
	owner.use( 16, 1210 );
	assert.equal( sent.length, 4, "exact client deadline permits request" );
});
test("last stack removal preserves category timer; step expiry and clear invalidate it", () => {
	const owner = createInventory( () => {} );
	owner.bootstrap( fixture( 1 ) );
	owner.use( 14, 0 );
	owner.receive( 0xb5bd, receipt( 14, 0, 2 ), 10, context );
	assert.equal( owner.state().inventory.some( i => i.slot === 14 ), false );
	assert.equal( owner.use( 16, 1009 ), null );
	assert.equal( owner.step( 1009 ), false );
	assert.equal( owner.step( 1010 ), true );
	assert.deepEqual( owner.state().itemCooldowns, [] );
	owner.use( 16, 1010 );
	owner.receive( 0xb5bd, receipt( 16, 0, 2 ), 1011, context );
	owner.clear();
	assert.deepEqual( owner.state().itemCooldowns, [] );
	owner.bootstrap( fixture() );
	assert.deepEqual( owner.state().itemCooldowns, [] );
});
test("invalid recovery context fails before inventory/timer mutation", () => {
	const owner = createInventory( () => {} );
	owner.bootstrap( fixture() );
	const before = owner.state().inventory;
	assert.throws( () => owner.receive( 0xb5bd, receipt( 14, 2, 2 ), 0 ), /country/ );
	assert.equal( owner.state().inventory, before );
	assert.deepEqual( owner.state().itemCooldowns, [] );
});
test("native item atlas animates with countdown, no skill flash at expiry", () => {
	const rows = [ { category: 2, startedAtMs: 100, durationMs: 1000 } ],
		r = [ 10, 20, 32, 32 ],
		clip = [ 0, 0, 100, 100 ];
	const start = quickslotItemCooldownQuads( rows, 0x10ec, 100, r, clip );
	assert.deepEqual( start[0].uv, [ 0, 0, 1 / 16, 1 / 16 ] );
	assert.ok( start[1].texture.endsWith( "cool_time_1.png" ) );
	const half = quickslotItemCooldownQuads( rows, 0x10ec, 600, r, clip );
	assert.deepEqual( half[0].uv, [ 7 / 16, 7 / 16, 1 / 16, 1 / 16 ] );
	assert.ok( half[1].texture.endsWith( "cool_time_0.png" ) );
	assert.deepEqual( quickslotItemCooldownQuads( rows, 0x8ec, 600, r, clip ), [] );
	assert.deepEqual( quickslotItemCooldownQuads( rows, 0x10ec, 1100, r, clip ), [] );
	assert.deepEqual( quickslotItemCooldownQuads( rows, 0x10ec, 1200, r, clip ), [] );
});
test("gameplay publishes receipt timers and automatic use observes the same owner gate", () => {
	const sent = [],
		g = createGameplay( f => sent.push( f ) ),
		local = { gid: 1, countryByte9c: 0, regionId: 257, x: 0, y: 0, z: 0, heading: 0, appearanceState: [ 1, 0, 0 ] };
	g.bootstrap( {
		...fixture(),
		character: {
			hp: 100,
			mp: 10,
			maxHp: 100,
			maxMp: 100,
			autoPotion: { hp: 0x3211, mp: 0xb211, cure: 0x13, timing: 0x81 },
			quickSlots: [ { slot: 1, kind: 0x46, payload: 1 } ]
		}
	} );
	g.seed( local );
	g.receive( { opcode: 0x33a6, payload: Uint8Array.of( 1, 0, 0, 0, 0, 0, 2, 10, 0, 0, 0 ) }, 0 );
	g.step( 0, local );
	assert.equal( sent.length, 1 );
	g.receive( { opcode: 0xb5bd, payload: receipt( 14, 2, 2 ) }, 10 );
	assert.deepEqual( defined( g.take() ).itemCooldowns, [ { category: 2, startedAtMs: 10, durationMs: 1000 } ] );
	for ( let now = 100; now <= 1000; now += 100 ) g.step( now, local );
	assert.equal( sent.length, 1, "auto-potion cannot spam requests while icon is active" );
	g.step( 1100, local );
	assert.equal( sent.length, 2 );
	g.reset();
	assert.deepEqual( defined( g.take() ).itemCooldowns, [] );
	g.dispose();
});

test("inventory shares category sweep and expiry but native countdown digits are quickslot-only", () => {
	const rows = [ { category: 2, startedAtMs: 100, durationMs: 1000 } ],
		r = [ 10, 20, 32, 32 ],
		clip = [ 0, 0, 100, 100 ];
	for ( const now of [ 100, 101, 400, 600, 1099 ] ) {
		const bag = inventoryItemCooldownQuads( rows, 0x10ec, now, r, clip ),
			bar = quickslotItemCooldownQuads( rows, 0x10ec, now, r, clip );
		assert.equal( bag.length, 1 );
		assert.deepEqual( bag[0], bar[0] );
		assert.ok( bar.length > 1 );
	}
	for ( const now of [ 1100, 1101, 1200 ] ) {
		assert.deepEqual( inventoryItemCooldownQuads( rows, 0x10ec, now, r, clip ), [] );
	}
	assert.deepEqual( inventoryItemCooldownQuads( rows, 0x8ec, 600, r, clip ), [], "MP cooldown does not shade HP" );
	assert.deepEqual( inventoryItemCooldownQuads( [], 0x10ec, 600, r, clip ), [] );
});

test("every companion and cure category starts on success and blocks only its lane", () => {
	const families = [
		[ 1, 4, 4, 1000 ],
		[ 1, 5, 5, 1000 ],
		[ 1, 7, 6, 1000 ],
		[ 1, 9, 7, 1000 ],
		[ 2, 1, 13, 20000 ],
		[ 2, 6, 14, 1000 ],
		[ 2, 7, 15, 1000 ]
	];
	for ( const [group, subtype, category, durationMs] of families ) {
		const tid = 0x6c | group << 7 | subtype << 11;
		assert.equal( p.potionCategory( tid ), category );
		for ( const country of [ 0, 1 ] ) {
			assert.equal( p.potionCooldownMs( category, {}, country, 0x600000 ), durationMs );
		}
		const owner = createInventory( () => {} );
		owner.bootstrap( {
			refItemSnapshot: [ { refObjId: 1, typeFlags: tid } ],
			equipItems: [ { refObjId: 1, slot: 13, body: [ 1, 0, 0, 0, 3, 0 ] } ]
		} );
		owner.receive( 0xb5bd, Uint8Array.of( 1, 13, 2, 0, tid & 255, tid >>> 8 ), 100, context );
		assert.deepEqual( owner.state().itemCooldowns, [ { category, startedAtMs: 100, durationMs } ] );
		assert.equal( owner.use( 13, 100 + durationMs - 1 ), null );
		assert.equal( p.itemCooldown( owner.state().itemCooldowns, tid, 100 + durationMs ), undefined );
		assert.equal( p.itemCooldown( owner.state().itemCooldowns, word( 1 ), 100 ), undefined );
	}
	for ( const subtype of [ 0, 6, 8, 10, 31 ] ) assert.equal( p.potionCategory( word( subtype ) ), null );
});

test("published unlimited potions acknowledge once without spending or bypassing cooldown", () => {
	const owner = createInventory( () => {} ), unlimited = { ...context, unlimitedItems: [ 2 ] };
	owner.bootstrap( fixture() );
	owner.use( 14, 0 );
	assert.throws( () => owner.receive( 0xb5bd, receipt( 14, 3, 2 ), 10, context ), /Stale/ );
	owner.receive( 0xb5bd, receipt( 14, 3, 2 ), 10, unlimited );
	assert.equal( owner.state().inventory.find( item => item.slot === 14 )?.quantity, 3 );
	assert.equal( owner.state().inventoryPending, false );
	assert.deepEqual( owner.state().itemCooldowns, [ { category: 2, startedAtMs: 10, durationMs: 1000 } ] );
	assert.equal( owner.use( 14, 1009 ), null );
	assert.throws( () => owner.receive( 0xb5bd, receipt( 14, 3, 2 ), 500, unlimited ), /Stale/ );
	owner.use( 16, 1010 );
	assert.throws( () => owner.receive( 0xb5bd, receipt( 16, 3, 2 ), 1011, unlimited ), /Stale/ );
	assert.throws( () => owner.receive( 0xb5bd, receipt( 14, 3, 2 ), 1011, unlimited ), /Stale/ );
});
