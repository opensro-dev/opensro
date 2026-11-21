/*
===========================================================================

alchemy-status-lines.test.mjs - the alchemy box's start and cancel lines

#2285 PR A: the no-dissolvable line for a target-only compound or
advanced staging (6220A0), the not-enough-slot line for a dissolve
whose products would not fit (621D00), and the cancelled line for a
pending compound or a closed-on reinforce operation (624060/625550).
Each prints into the notice queue; a refused start sends nothing.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createGameplay } = await import(
	sourceFileUrl( "src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts" ).href
);
const { dissolveNeededSlots } = await import(
	sourceFileUrl( "src/engine/foundation/ui/alchemy-selection.ts" ).href
);

const u16 = n => [ n & 255, n >>> 8 & 255 ];
const u32 = n => [ n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 & 255 ];
const u64 = n => {
	const bytes = [];
	for ( let v = BigInt( n ), i = 0; i < 8; i++, v >>= 8n ) bytes.push( Number( v & 255n ) );
	return bytes;
};
// Equipment TID 3/1/7/1; rondo 0x35ec; compound elixir 0x25ec; advanced
// tablet 0x2dec; reinforce elixir 0x0d6c (alchemySelection's flat words).
const EQUIPMENT = 0x2c, RONDO = 0x35ec, ELIXIR = 0x25ec, TABLET = 0x2dec, POWDER = 0x0d6c;
const NO_DISSOLVABLE = "UIIT_MSG_ENCHANTERR_HAVE_NO_DISSOLVABLE_ITEM";
const NOT_ENOUGH = "UIIT_MSG_ALCHEMY_NOT_ENOUGH_SLOT";
const CANCELED = "UIIT_MSG_ALCHEMY_CANCELED_COMPOUND";

/*
================
itemBody

The wire body of one bag row: quantity for a stackable etc item, the
equipment fields (plus, variance, durability, magic options) otherwise.
================
*/
function itemBody( refObjId, typeFlags, magicCount = 0 ) {
	if ( (typeFlags & 0x60) === 0x20 ) {
		const bytes = [ ...u32( refObjId ), 0, ...u64( 0 ), ...u32( 100 ), magicCount ];
		for ( let i = 0; i < magicCount; i++ ) bytes.push( ...u64( 0 ) );
		return bytes;
	}
	return [ ...u32( refObjId ), ...u16( 5 ) ];
}

/*
================
world

A local player (gid 1) with an equipment at slot 13, a rondo at 14, a
compound elixir at 15, a reinforce powder at 16 and a tablet at 17.
The equipment's published price and class drive the dissolve estimate.
================
*/
/** @param {{ inventorySlotCount?: number, price?: number, itemClass?: number, magicCount?: number }} [options] */
function world( { inventorySlotCount = 45, price = 100, itemClass = 1, magicCount = 0 } = {} ) {
	const sent = [], game = createGameplay( f => sent.push( f ) );
	game.bootstrap( {
		inventorySlotCount,
		equipmentSlotCount: 13,
		refItemSnapshot: [
			{ refObjId: 1, typeFlags: EQUIPMENT, nativeFields: { price, itemClass } },
			{ refObjId: 2, typeFlags: RONDO },
			{ refObjId: 3, typeFlags: ELIXIR },
			{ refObjId: 4, typeFlags: TABLET },
			{ refObjId: 5, typeFlags: POWDER }
		],
		equipItems: [
			{ slot: 13, refObjId: 1, body: itemBody( 1, EQUIPMENT, magicCount ) },
			{ slot: 14, refObjId: 2, body: itemBody( 2, RONDO ) },
			{ slot: 15, refObjId: 3, body: itemBody( 3, ELIXIR ) },
			{ slot: 16, refObjId: 5, body: itemBody( 5, POWDER ) },
			{ slot: 17, refObjId: 4, body: itemBody( 4, TABLET ) }
		]
	} );
	game.seed( { gid: 1, regionId: 1, x: 0, y: 0, z: 0, heading: 0 } );
	game.command( { kind: "alchemy-open" }, 0 );
	return { game, sent };
}

const notices = game => game.take()?.notices ?? [];
const noticeKeys = game => notices( game ).map( notice => notice.key );

test("the dissolve estimate is the native 4 + attr + magic shape (621D00)", () => {
	// T[0] = 18750: price 187500 gives trunc(187500*10/100/18750) = 1.
	assert.equal( dissolveNeededSlots( 187500, 1, 0 ), 5 );
	assert.equal( dissolveNeededSlots( 187500, 1, 2 ), 7 );
	assert.equal( dissolveNeededSlots( 100, 1, 0 ), 4 );
	// Class 4 is degree 2 (T[1] = 34950); class 36 is degree 12.
	assert.equal( dissolveNeededSlots( 349500, 4, 0 ), 5 );
	assert.equal( dissolveNeededSlots( 2943860, 36, 0 ), 5 );
	// Both terms clamp at 2: the estimate never exceeds 4 + 2 + 2.
	assert.equal( dissolveNeededSlots( 4000000000, 1, 12 ), 8 );
	// Missing inputs have no estimate, and neither does a class past
	// the table (the original reads past its stack table there). A
	// class of 0 truncates to degree index 0, as the C division does.
	assert.equal( dissolveNeededSlots( undefined, 1, 0 ), null );
	assert.equal( dissolveNeededSlots( 100, undefined, 0 ), null );
	assert.equal( dissolveNeededSlots( 100, 0, 0 ), 4 );
	assert.equal( dissolveNeededSlots( 100, 37, 0 ), null );
});

test("a target-only compound or advanced staging prints the no-dissolvable line and sends nothing", () => {
	const compound = world();
	const compoundFrame = compound.game.command(
		{ kind: "alchemy-start", mode: "compound", slots: [ 13 ] },
		1
	);
	assert.equal( compoundFrame, null );
	assert.deepEqual( noticeKeys( compound.game ), [ NO_DISSOLVABLE ] );
	assert.deepEqual( compound.sent, [] );
	const advanced = world();
	const advancedFrame = advanced.game.command(
		{ kind: "alchemy-start", mode: "advanced", slots: [ 13 ] },
		1
	);
	assert.equal( advancedFrame, null );
	assert.deepEqual( noticeKeys( advanced.game ), [ NO_DISSOLVABLE ] );
	assert.deepEqual( advanced.sent, [] );
	// A reagent staged sends as before and prints nothing.
	const staged = world();
	const frame = staged.game.command( { kind: "alchemy-start", mode: "compound", slots: [ 13, 15 ] }, 1 );
	assert.equal( frame.opcode, 0x716f );
	assert.deepEqual( noticeKeys( staged.game ), [] );
});

test("a dissolve whose products would not fit prints the needed count and sends nothing", () => {
	// Price 187500, class 1, two magic options: needed = 4 + 1 + 2 = 7.
	// Five bag slots (13..17) all occupied leave none free.
	const tight = world( { inventorySlotCount: 18, price: 187500, magicCount: 2 } );
	const frame = tight.game.command( { kind: "alchemy-start", mode: "dissolve", slots: [ 13, 14 ] }, 1 );
	assert.equal( frame, null );
	const printed = notices( tight.game );
	assert.deepEqual( printed.map( notice => notice.key ), [ NOT_ENOUGH ] );
	assert.equal( printed[0].value, 7 );
	assert.deepEqual( tight.sent, [] );
	// A roomier bag has the seven free slots and sends.
	const roomy = world( { inventorySlotCount: 45, price: 187500, magicCount: 2 } );
	const sentFrame = roomy.game.command( { kind: "alchemy-start", mode: "dissolve", slots: [ 13, 14 ] }, 1 );
	assert.equal( sentFrame.opcode, 0x7549 );
	assert.deepEqual( noticeKeys( roomy.game ), [] );
	// The boundary is strict: a cheap item needs four slots, and a bag
	// with exactly four free (nine slots, five rows) still sends.
	const cheap = world( { inventorySlotCount: 22, price: 100 } );
	const cheapFrame = cheap.game.command( { kind: "alchemy-start", mode: "dissolve", slots: [ 13, 14 ] }, 1 );
	assert.equal( cheapFrame.opcode, 0x7549 );
	assert.deepEqual( noticeKeys( cheap.game ), [] );
});

test("cancelling a pending compound prints the cancelled line; other process modes stay silent", () => {
	const compound = world();
	compound.game.command( { kind: "alchemy-start", mode: "compound", slots: [ 13, 15 ] }, 1 );
	compound.game.take();
	const frame = compound.game.command( { kind: "alchemy-cancel" }, 2 );
	assert.equal( frame.opcode, 0x716f, "the cancel itself still sends" );
	assert.deepEqual( noticeKeys( compound.game ), [ CANCELED ] );
	const dissolve = world();
	dissolve.game.command( { kind: "alchemy-start", mode: "dissolve", slots: [ 13, 14 ] }, 1 );
	dissolve.game.take();
	dissolve.game.command( { kind: "alchemy-cancel" }, 2 );
	assert.deepEqual( noticeKeys( dissolve.game ), [] );
	const advanced = world();
	advanced.game.command( { kind: "alchemy-start", mode: "advanced", slots: [ 13, 17 ] }, 1 );
	advanced.game.take();
	advanced.game.command( { kind: "alchemy-close" }, 2 );
	assert.deepEqual( noticeKeys( advanced.game ), [] );
});

test("closing on a pending reinforce prints the cancelled line; the cancel gesture does not", () => {
	const closed = world();
	closed.game.command( { kind: "alchemy-start", mode: "reinforce", slots: [ 13, 16 ] }, 1 );
	closed.game.take();
	const frame = closed.game.command( { kind: "alchemy-close" }, 2 );
	assert.equal( frame, null, "a reinforce close sends no cancel frame" );
	assert.deepEqual( noticeKeys( closed.game ), [ CANCELED ] );
	const cancelled = world();
	cancelled.game.command( { kind: "alchemy-start", mode: "reinforce", slots: [ 13, 16 ] }, 1 );
	cancelled.game.take();
	cancelled.game.command( { kind: "alchemy-cancel" }, 2 );
	assert.deepEqual( noticeKeys( cancelled.game ), [] );
	// Closing with nothing pending prints nothing.
	const idle = world();
	idle.game.command( { kind: "alchemy-close" }, 1 );
	assert.deepEqual( noticeKeys( idle.game ), [] );
});
