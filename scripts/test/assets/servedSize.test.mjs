/*
===========================================================================

servedSize.test.mjs - the served-size gate's arithmetic

The full download is every pack's bytes and the startup share is the
startup groups only; a pack without a byte count fails the gate instead of
counting as free.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { ORIGINAL_PK2_BYTES, SERVED_CEILING_BYTES, servedSize } from "../../build/shared/servedSize.mjs";

const manifest = {
	groups: [
		{ name: "native-ui", load: "startup", packs: [ { path: "/assets/packs/a.bin", bytes: 100 } ] },
		{
			name: "game-data",
			load: "startup",
			packs: [ { path: "/assets/packs/b.bin", bytes: 50 }, { path: "/assets/packs/c.bin", bytes: 25 } ]
		},
		{ name: "outdoor-world", load: "manual", packs: [ { path: "/assets/packs/d.bin", bytes: 1000 } ] },
		{ name: "empty", load: "lazy", packs: [] }
	]
};

test("the full download is every pack and startup is the startup groups", () => {
	const size = servedSize( manifest );
	assert.equal( size.total, 1175 );
	assert.equal( size.startup, 175 );
	assert.deepEqual( size.groups.map( group => [ group.name, group.bytes, group.packs ] ), [
		[ "native-ui", 100, 1 ],
		[ "game-data", 75, 2 ],
		[ "outdoor-world", 1000, 1 ],
		[ "empty", 0, 0 ]
	] );
});

test("a pack without a byte count is a broken manifest", () => {
	assert.throws(
		() => servedSize( { groups: [ { name: "x", load: "lazy", packs: [ { path: "/p.bin" } ] } ] } ),
		/no byte count/
	);
	assert.throws(
		() => servedSize( { groups: [ { name: "x", load: "lazy", packs: [ { path: "/p.bin", bytes: -1 } ] } ] } ),
		/no byte count/
	);
	assert.throws( () => servedSize( {} ), /no groups/ );
});

test("the ceiling is 80% of the original PK2 payload", () => {
	assert.equal( SERVED_CEILING_BYTES, Math.floor( ORIGINAL_PK2_BYTES * 0.8 ) );
});
