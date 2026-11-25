/*
===========================================================================

session-constants.test.mjs - entry answers after the first one (#4421)

The entry answer's constant sections (the reference snapshot and the
magic-option snapshot) ship once per session: a re-entry that stated
the digest it holds answers with refObjSnapshot nulled, and the worker
keeps the installed reference tables and commerce-seed item rows
instead of rebuilding them. Answers of the retired contract word are
refused outright.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createEntities } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/entities.ts"
);

const localPlayerEntry = { modelRef: 1933, startProfile: { regionId: 0x6b4f, x: 1, y: 2, z: 3, angle: 0 } };

function bootstrapFull(e) {
	e.bootstrap( {
		protocolVersion: 3,
		nativeResult: 1,
		refObjSnapshot: [ { refObjId: 3914, tidWord: 0x11c6, kind: "cos", name: "Guardian" } ],
		refItemSnapshot: [ { refObjId: 3810, typeFlags: 0x2ec, codename: "Gold" } ],
		localPlayerEntry
	} );
}

test("a digest-held answer keeps the reference tables and commerce rows", () => {
	const e = createEntities();
	bootstrapFull(e);
	// The commerce seed lands outside the entry answer (opcode 14).
	e.references( [ { refObjId: 7777, typeFlags: 0x8ac, name: "ShopSword" } ] );
	e.bootstrap( {
		protocolVersion: 3,
		nativeResult: 1,
		// The server nulls the constant snapshot when the entry stated
		// the digest this worker already holds.
		refObjSnapshot: null,
		sessionConstants: "54c6f4af",
		refItemSnapshot: [ { refObjId: 3810, typeFlags: 0x2ec, codename: "Gold" } ],
		localPlayerEntry
	} );
	// The held catalogues still answer through the reference lookups:
	// the entry's own item row and the commerce-seed row both resolve.
	assert.deepEqual( e.itemReference( 3810 ), { typeFlags: 0x2ec, name: "Gold" } );
	assert.deepEqual( e.itemReference( 7777 ), { typeFlags: 0x8ac, name: "ShopSword" } );
	// A conflicting type for a held commerce ref is refused, the same
	// rule references() keeps.
	assert.throws(
		() => e.references( [ { refObjId: 7777, typeFlags: 0x2ec, name: "Other" } ] ),
		/Conflicting public item reference/
	);
	// The same type repeated is idempotent, as before.
	e.references( [ { refObjId: 7777, typeFlags: 0x8ac, name: "ShopSword" } ] );
});

test("the retired bootstrap contract word is refused", () => {
	const e = createEntities();
	assert.throws(
		() =>
			e.bootstrap( {
				protocolVersion: 2,
				nativeResult: 1,
				refObjSnapshot: [],
				localPlayerEntry
			} ),
		/Invalid world bootstrap/
	);
});

test("a malformed constant snapshot is refused on the new contract", () => {
	const e = createEntities();
	assert.throws(
		() =>
			e.bootstrap( {
				protocolVersion: 3,
				nativeResult: 1,
				refObjSnapshot: 42,
				localPlayerEntry
			} ),
		/Invalid world bootstrap/
	);
});
