/*
===========================================================================

journal-cost.test.mjs - the world journal budget keeps a flat value's size

Given the journal's map, a flat value (only strings, numbers, booleans and
nulls) is measured once: the gameplay snapshot republishes the same chat
lines, inventory rows and path points about 8 times a second. Kept sizes
must equal a fresh walk, and the batch's shared set must still count a
repeat as a reference.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";

const { journalCost } = await import(
	sourceFileUrl( path.join( root, "src/engine/foundation/gameplay/journal-cost.ts" ) ).href
);

/*
================
counted

A proxy of value that counts how often its keys are listed: how many times
the budget walked it.
================
*/
function counted( value ) {
	const reads = { keys: 0 };
	const proxy = new Proxy( value, {
		ownKeys( target ) {
			reads.keys++;
			return Reflect.ownKeys( target );
		}
	} );
	return { proxy, reads };
}

test("a flat value costs its JSON size, measured again or not", () => {
	const flat = new WeakMap();
	const line = { kind: 3, sender: "Selene", text: "hello", at: 1234, gm: false, color: null };
	const points = [ 1, 22, 333, -4 ];
	for ( const value of [ line, points, { nested: line, points } ] ) {
		const json = JSON.stringify( value ).length * 2;
		assert.equal( journalCost( value, undefined, flat ), json );
		assert.equal( journalCost( value, undefined, flat ), json, "the second measure equals the first" );
	}
});

test("a flat value is walked once per journal, with or without the batch's set", () => {
	const { proxy, reads } = counted( { gid: 7, hp: 100, name: "Mangyang" } );
	const flat = new WeakMap();
	const first = journalCost( { row: proxy }, undefined, flat );
	assert.equal( journalCost( { row: proxy }, undefined, flat ), first );
	assert.equal( journalCost( { row: proxy }, new WeakSet(), flat ), first );
	assert.equal( reads.keys, 1 );
	assert.equal( journalCost( { row: proxy } ), first, "without the map: walked again" );
	assert.equal( reads.keys, 2 );
});

test("a value holding another object is walked every time", () => {
	const { proxy, reads } = counted( { gid: 7, pose: { x: 1, y: 2 } } );
	const flat = new WeakMap();
	journalCost( proxy, undefined, flat );
	journalCost( proxy, undefined, flat );
	assert.equal( reads.keys, 2 );
});

test("a flat value kept from before still counts once per batch when repeated", () => {
	const row = { id: 1, name: "Blade", level: 8 }, flat = new WeakMap();
	const alone = journalCost( row, undefined, flat );
	const pair = { first: row, second: row };
	const set = new WeakSet();
	const shared = journalCost( pair, set, flat );
	assert.ok( shared < journalCost( pair, undefined, flat ), `the repeat is a reference: ${shared}` );
	assert.equal( journalCost( row, set, flat ), 8, "already in the batch: a reference" );
	assert.equal( journalCost( row, undefined, flat ), alone );
});
