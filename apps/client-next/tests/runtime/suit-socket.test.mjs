/*
===========================================================================

suit-socket.test.mjs - the local wearer's suit-socket warm models

The candidates each bag suit or cape would wear in JOB_SUIT_SLOT, the bare
socket's parts while one is worn, the bag key that rebuilds them, and the
order that admits the worn outfit before any warm part.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { LAST_EQUIPMENT_SLOT, readyThenWarm, suitBagKey, suitSocketParts } = await import(
	sourceFileUrl( "src/engine/foundation/animation/suit-socket.ts" ).href
);
const { JOB_SUIT_SLOT } = await import( sourceFileUrl( "src/engine/foundation/gameplay/party-matching.ts" ).href );

/*
================
typeFlags

TID1..TID4 packed as the item records carry them.
================
*/
function typeFlags( tid1, tid2, tid3, tid4 ) {
	return tid1 << 2 | tid2 << 5 | tid3 << 7 | tid4 << 11;
}

const CAPE = typeFlags( 3, 1, 7, 5 ), THIEF_SUIT = typeFlags( 3, 1, 7, 2 ), ARMOR = typeFlags( 3, 1, 1, 3 );

/*
================
recordingAssembly

A partsFor that records each equipment list it is handed and answers one
model per slot-8 row (or "bare" without one).
================
*/
function recordingAssembly() {
	const calls = [];
	return {
		calls,
		partsFor: equipment => {
			calls.push( equipment );
			const socket = equipment.find( row => row.slot === JOB_SUIT_SLOT );
			return [ socket ? "model:" + socket.refObjId : "model:bare" ];
		}
	};
}

test("a bag suit or cape is assembled with the item in the suit socket", () => {
	const { calls, partsFor } = recordingAssembly();
	const equipment = [
		{ slot: 1, refObjId: 10, typeFlags: ARMOR },
		{ slot: 20, refObjId: 77, typeFlags: CAPE }
	];
	assert.deepEqual( suitSocketParts( equipment, partsFor ), [ "model:77" ] );
	assert.equal( calls.length, 1 );
	assert.deepEqual( calls[0].map( row => [ row.slot, row.refObjId ] ), [ [ 1, 10 ], [ JOB_SUIT_SLOT, 77 ] ] );
});

test("a worn suit adds the bare socket's assembly", () => {
	const { calls, partsFor } = recordingAssembly();
	const equipment = [
		{ slot: JOB_SUIT_SLOT, refObjId: 55, typeFlags: THIEF_SUIT },
		{ slot: 21, refObjId: 77, typeFlags: CAPE }
	];
	assert.deepEqual( new Set( suitSocketParts( equipment, partsFor ) ), new Set( [ "model:77", "model:bare" ] ) );
	assert.equal( calls.length, 2 );
	assert.equal( calls[1].some( row => row.slot === JOB_SUIT_SLOT ), false, "the bare assembly has no socket row" );
});

test("non-suit bag items and worn rows are never candidates", () => {
	const { calls, partsFor } = recordingAssembly();
	const equipment = [
		{ slot: 20, refObjId: 30, typeFlags: ARMOR },
		{ slot: LAST_EQUIPMENT_SLOT, refObjId: 31, typeFlags: CAPE },
		{ slot: 3, refObjId: 32, typeFlags: THIEF_SUIT }
	];
	assert.deepEqual( suitSocketParts( equipment, partsFor ), [] );
	assert.equal( calls.length, 0 );
});

test("the bag key follows the bag's candidates, not their slots", () => {
	const bag = slot => [ { slot: 1, refObjId: 10, typeFlags: ARMOR }, { slot, refObjId: 77, typeFlags: CAPE } ];
	assert.equal( suitBagKey( bag( 20 ) ), suitBagKey( bag( 40 ) ), "a move inside the bag rebuilds nothing" );
	assert.notEqual( suitBagKey( bag( 20 ) ), suitBagKey( bag( 20 ).slice( 0, 1 ) ), "selling the cape rebuilds" );
	assert.equal( suitBagKey( [ { slot: JOB_SUIT_SLOT, refObjId: 77, typeFlags: CAPE } ] ), "", "worn is not bag" );
});

/*
================
budgetResources

A character resource owner with room for limit models: a path already
admitted stays ready, a new one is admitted while room remains, the rest
are refused (as plan refuses past the character budget).
================
*/
function budgetResources( limit ) {
	const admitted = new Set();
	return {
		admitted,
		ready( path ) {
			if ( admitted.has( path ) ) return true;
			if ( admitted.size >= limit ) return false;
			admitted.add( path );
			return true;
		}
	};
}

test("near the budget the worn outfit is admitted and the warm parts are refused", () => {
	const resources = budgetResources( 2 );
	assert.equal( readyThenWarm( resources, [ "worn:a", "worn:b" ], [ "warm:x", "warm:y" ] ), true );
	assert.deepEqual( [ ...resources.admitted ], [ "worn:a", "worn:b" ] );
});

test("warm parts wait until every worn part is ready", () => {
	const asked = [];
	const resources = { ready: path => (asked.push( path ), path !== "worn:b") };
	assert.equal( readyThenWarm( resources, [ "worn:a", "worn:b" ], [ "warm:x" ] ), false );
	assert.deepEqual( asked, [ "worn:a", "worn:b" ], "no warm part is asked while the outfit is loading" );
	const roomy = budgetResources( 10 );
	assert.equal( readyThenWarm( roomy, [ "worn:a" ], [ "warm:x" ] ), true );
	assert.deepEqual( [ ...roomy.admitted ], [ "worn:a", "warm:x" ] );
});
