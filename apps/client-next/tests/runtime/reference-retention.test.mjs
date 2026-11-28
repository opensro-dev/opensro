/*
===========================================================================

reference-retention.test.mjs - delta-taught references survive a re-entry

The commerce seed is published once per session; a teleport bootstrap
rebuilds the item tables from its snapshot alone (the published tables
and the character's own rows). A shop-only item - the guild managers'
crest goods are the reported case (#4698) - holds no other reference,
so its icon must stand on what the seed taught, not on the snapshot.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createInventory } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/inventory/inventory.ts"
);

// The guild crest registration item as the commerce seed teaches it:
// shop-only, in no drop table, so no published row ever names it.
const CREST = {
	refObjId: 9268,
	typeFlags: 5740,
	name: "Guild emblem registration item",
	codename: "ITEM_ETC_GUILD_CREST_USER",
	icon: "item\\etc\\guild_crest_user.ddj"
};

/*
================
fixture

An empty 45-slot bag (13 sockets, 32 slots), as a fresh character enters.
================
*/
function fixture() {
	const owner = createInventory( () => {} );
	owner.bootstrap( { inventorySlotCount: 45, equipmentSlotCount: 13, refItemSnapshot: [], equipItems: [] } );
	return owner;
}

test("a reference taught by a delta survives a continued re-entry", () => {
	const owner = fixture();
	owner.references( [ CREST ] );
	assert.equal( owner.reference( CREST.refObjId ).icon, CREST.icon );
	// A teleport re-enters the world under a held answer: the bootstrap
	// rebuilds the tables from its snapshot, and the seed is not
	// published again. The crest's icon and name stand.
	const entry = { inventorySlotCount: 45, equipmentSlotCount: 13, refItemSnapshot: [], equipItems: [] };
	owner.bootstrap( entry, 0, true );
	assert.equal( owner.reference( CREST.refObjId ).icon, CREST.icon );
	assert.equal( owner.reference( CREST.refObjId ).name, CREST.name );
	owner.bootstrap( entry, 0, true );
	assert.equal( owner.reference( CREST.refObjId ).icon, CREST.icon );
});

test("a new login retires what a delta taught", () => {
	const owner = fixture();
	owner.references( [ CREST ] );
	owner.bootstrap( { inventorySlotCount: 45, equipmentSlotCount: 13, refItemSnapshot: [], equipItems: [] } );
	assert.equal( owner.reference( CREST.refObjId ).icon, undefined );
	assert.equal( owner.reference( CREST.refObjId ).name, undefined );
});

test("the bootstrap's own rows still land and a conflicting delta still throws", () => {
	const owner = fixture();
	owner.bootstrap( {
		inventorySlotCount: 45,
		equipmentSlotCount: 13,
		refItemSnapshot: [ {
			refObjId: CREST.refObjId,
			typeFlags: CREST.typeFlags,
			name: CREST.name,
			codename: CREST.codename,
			icon: CREST.icon
		} ],
		equipItems: []
	} );
	assert.equal( owner.reference( CREST.refObjId ).icon, CREST.icon );
	assert.throws(
		() => owner.references( [ { ...CREST, typeFlags: CREST.typeFlags + 1 } ] ),
		/Conflicting inventory item reference/
	);
});
