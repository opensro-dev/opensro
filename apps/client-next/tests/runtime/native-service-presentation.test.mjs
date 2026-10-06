/*
===========================================================================

native-service-presentation.test.mjs - tests for alchemy-selection.ts,
native-window-sections.ts, authored-layout.ts, commerce.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { alchemySelection } = await import( "../../src/engine/foundation/ui/alchemy-selection.ts" );
const { nativeWindowSections } = await import( "../../src/engine/foundation/ui/native-window-sections.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const { shopCatalog } = await import( "../../src/engine/foundation/gameplay/commerce.ts" );
test("reagents preserve every existing alchemy operation without a synthetic mode menu", () => {
	const item = ( slot, typeFlags ) => ({ slot, typeFlags });
	assert.equal( alchemySelection( "reinforce", [], item( 13, 0x8ec ) ), null );
	assert.deepEqual( alchemySelection( "reinforce", [], item( 13, 0x32c ) ), { mode: "reinforce", slots: [ 13 ] } );
	for ( const [flags, mode] of [ [ 0xd6c, "reinforce" ], [ 0xdec, "attribute" ], [ 0x15ec, "magic" ] ] ) {
		assert.deepEqual( alchemySelection( "reinforce", [ 13 ], item( 14, flags ) ), { mode, slots: [ 13, 14 ] } );
	}
	for ( const [flags, mode] of [ [ 0x32c, "dissolve" ], [ 0x1dec, "advanced" ], [ 0x25ec, "compound" ] ] ) {
		assert.equal( defined( alchemySelection( "compound", [], item( 13, flags ) ) ).mode, mode );
	}
	assert.equal( alchemySelection( "advanced", [ 13 ], item( 14, 0x8ec ) ), null );
	assert.deepEqual( defined( alchemySelection( "magic", [ 13, 14 ], item( 13, 0x32c ) ) ).slots, [] );
	assert.equal( alchemySelection( "reinforce", [], item( 6, 0x32c ) ), null );
});
test("constructor admission excludes dormant pages and auxiliary Guild dialogs", () => {
	const read = name =>
		decodeAuthoredLayout(
			JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/" + name + ".json", "utf8" ) ),
			nativeWindowSections( name )
		);
	const guild = read( "ifguild" );
	assert.ok( Object.values( guild ).some( n => n.id === 82 ) );
	assert.ok( !Object.values( guild ).some( n => n.id === 140 || n.id === 150 || n.id === 600 || n.id === 700 ) );
	assert.ok( !Object.values( read( "ifcos" ) ).some( n => n.id >= 121 && n.id <= 123 ) );
	assert.ok( !Object.values( read( "ifcosinventory" ) ).some( n => n.id === 35 ) );
});
test("shop projection retains original tab identity and rejects ambiguous labels", () => {
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) );
	const base = { version: 1, npc: 1, name: "Shop", offers: [], tabs: [ { index: 3, labelSymbol: "SN_TAB_WEAPON" } ] };
	assert.deepEqual( shopCatalog( encode( base ) ).tabs, base.tabs );
	assert.throws(
		() => shopCatalog( encode( { ...base, tabs: [ ...base.tabs, ...base.tabs ] } ) ),
		/Duplicate shop tab/
	);
});
