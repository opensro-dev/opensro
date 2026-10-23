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
const { alchemySelection, alchemyStoneRefusal, STONE_FAMILY_BITS } = await import(
	"../../src/engine/foundation/ui/alchemy-selection.ts"
);
const { nativeWindowSections } = await import( "../../src/engine/foundation/ui/native-window-sections.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const { shopCatalog } = await import( "../../src/engine/foundation/gameplay/commerce.ts" );
test("reagents preserve every existing alchemy operation without a synthetic mode menu", () => {
	const item = ( slot, typeFlags ) => ({ slot, typeFlags });
	assert.equal( alchemySelection( "reinforce", [], item( 13, 0x8ec ) ), null );
	assert.deepEqual( alchemySelection( "reinforce", [], item( 13, 0x32c ) ), { mode: "reinforce", slots: [ 13 ] } );
	// 62B6D0 / 6245A0: job suits (3/1/7/* 0x03ac, 550410), avatar items
	// (3/1/13/* 0x06ac, 550940) and fortress hammers (3/1/6/16 0x832c, 5504C0)
	// are rejected from both reinforcement and dissolution slots.
	for ( const excluded of [ 0x03ac, 0x0bac, 0x06ac, 0x0eac, 0x16ac, 0x1eac, 0x832c ] ) {
		assert.equal( alchemySelection( "reinforce", [], item( 13, excluded ) ), null );
		assert.equal( alchemySelection( "compound", [], item( 13, excluded ) ), null );
	}
	assert.deepEqual( alchemySelection( "reinforce", [], item( 13, 0x0bac ^ 0x0800 ^ 0x0080 ) ), {
		mode: "reinforce",
		slots: [ 13 ]
	} );
	// 62B6D0: magic stones are 3/3/11/1 (0xdec) and 3/3/11/7 (0x3dec); attribute
	// stones are 3/3/11/2 (0x15ec). Swapping them sent every blue stone as an
	// attribute operation, which the server refuses.
	for (
		const [flags, mode] of [ [ 0xd6c, "reinforce" ], [ 0xdec, "magic" ], [ 0x3dec, "magic" ], [
			0x15ec,
			"attribute"
		] ]
	) {
		assert.deepEqual( alchemySelection( "reinforce", [ 13 ], item( 14, flags ) ), { mode, slots: [ 13, 14 ] } );
	}
	for ( const [flags, mode] of [ [ 0x32c, "dissolve" ], [ 0x1dec, "advanced" ], [ 0x25ec, "compound" ] ] ) {
		assert.equal( defined( alchemySelection( "compound", [], item( 13, flags ) ) ).mode, mode );
	}
	assert.equal( alchemySelection( "advanced", [ 13 ], item( 14, 0x8ec ) ), null );
	assert.deepEqual( defined( alchemySelection( "magic", [ 13, 14 ], item( 13, 0x32c ) ) ).slots, [] );
	assert.equal( alchemySelection( "reinforce", [], item( 6, 0x32c ) ), null );
});
test("stone placement refuses degree first, then family, and binds without published data", () => {
	const B = STONE_FAMILY_BITS;
	// (tid3, tid4) packed the native way: tid3 at bits 7..10, tid4 at 11..15.
	const equip = ( tid3, tid4 ) => 0x2c | (tid3 << 7) | (tid4 << 11);
	const target = ( typeFlags, fields, magicReferences ) => ({
		slot: 13,
		typeFlags,
		tooltip: { fields },
		...(magicReferences ? { magicReferences } : {})
	});
	const stone = ( fields ) => ({ slot: 14, typeFlags: 0x0dec, tooltip: { fields } });
	const weapon = target( equip( 6, 1 ), { itemClass: 4 } );

	// Degree gate first: a degree mismatch reports the degree message even
	// when the family mask would also refuse.
	assert.equal(
		alchemyStoneRefusal( weapon, stone( { itemParam1_29c: 3, stoneFamilyMask: 0 } ) ),
		"UIIT_MSG_ENCHANT_EQUIPCLASS_MISMATCH"
	);
	// Signed truncation: target class 0 is degree class 1, not an underflow.
	assert.equal(
		alchemyStoneRefusal(
			target( equip( 6, 1 ), { itemClass: 0 } ),
			stone( { itemParam1_29c: 1, stoneFamilyMask: B.weapon } )
		),
		null
	);
	// Weapon family admitted by the weapon bit only.
	assert.equal( alchemyStoneRefusal( weapon, stone( { itemParam1_29c: 2, stoneFamilyMask: B.weapon } ) ), null );
	assert.equal(
		alchemyStoneRefusal( weapon, stone( { itemParam1_29c: 2, stoneFamilyMask: B.shield } ) ),
		"UIIT_STRGERR_CANNOT_BE_USED_TO_TARGET"
	);
	// A published zero mask (the folded kind gate) refuses every target.
	assert.equal(
		alchemyStoneRefusal( weapon, stone( { itemParam1_29c: 2, stoneFamilyMask: 0 } ) ),
		"UIIT_STRGERR_CANNOT_BE_USED_TO_TARGET"
	);
	// Armour targets try the generic 'armor' word before the slot word.
	const boots = target( equip( 1, 6 ), { itemClass: 4 } );
	assert.equal( alchemyStoneRefusal( boots, stone( { itemParam1_29c: 2, stoneFamilyMask: B.armor } ) ), null );
	assert.equal( alchemyStoneRefusal( boots, stone( { itemParam1_29c: 2, stoneFamilyMask: B.boots } ) ), null );
	assert.equal(
		alchemyStoneRefusal( boots, stone( { itemParam1_29c: 2, stoneFamilyMask: B.mail } ) ),
		"UIIT_STRGERR_CANNOT_BE_USED_TO_TARGET"
	);
	// Accessories try 'accessory' before the subtype word.
	const ring = target( equip( 12, 3 ), { itemClass: 4 } );
	assert.equal( alchemyStoneRefusal( ring, stone( { itemParam1_29c: 2, stoneFamilyMask: B.accessory } ) ), null );
	assert.equal( alchemyStoneRefusal( ring, stone( { itemParam1_29c: 2, stoneFamilyMask: B.ring } ) ), null );
	// A repair-carrying target is searched as 'honor' for a repair stone
	// and as 'weapon' for any other stone.
	const repairWeapon = target( equip( 6, 1 ), { itemClass: 4 }, [ { optionName: "MATTR_REPAIR" } ] );
	assert.equal( alchemyStoneRefusal( repairWeapon, stone( { itemParam1_29c: 2, stoneFamilyMask: B.honor } ) ), null );
	assert.equal(
		alchemyStoneRefusal( repairWeapon, stone( { itemParam1_29c: 2, stoneFamilyMask: B.weapon } ) ),
		null
	);
	assert.equal(
		alchemyStoneRefusal( repairWeapon, stone( { itemParam1_29c: 2, stoneFamilyMask: B.shield } ) ),
		"UIIT_STRGERR_CANNOT_BE_USED_TO_TARGET"
	);
	// Without the published mask (or any tooltip) the stone binds as before
	// and the server fuse stays the only gate.
	assert.equal( alchemyStoneRefusal( weapon, stone( { itemParam1_29c: 2 } ) ), null );
	assert.equal(
		alchemyStoneRefusal(
			{ slot: 13, typeFlags: equip( 6, 1 ) },
			stone( { itemParam1_29c: 2, stoneFamilyMask: 0 } )
		),
		null
	);

	// The selection carries the refusal and binds nothing; without a target
	// item the historical bind still happens.
	const refused = defined(
		alchemySelection( "reinforce", [ 13 ], stone( { itemParam1_29c: 3, stoneFamilyMask: 0 } ), weapon )
	);
	assert.equal( refused.refusal, "UIIT_MSG_ENCHANT_EQUIPCLASS_MISMATCH" );
	assert.deepEqual( refused.slots, [ 13 ] );
	assert.deepEqual( alchemySelection( "reinforce", [ 13 ], { slot: 14, typeFlags: 0x0dec } ), {
		mode: "magic",
		slots: [ 13, 14 ]
	} );
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
	// 6A9420 creates the transport's TradeInfo section (the total, the stars
	// and the sell-all button 35) as a second section; the window draws it
	// for a transport only.
	const bag = read( "ifcosinventory" );
	assert.equal( Object.values( bag ).find( n => n.id === 35 )?.creationSection, 1 );
	assert.ok( Object.values( bag ).every( n => n.id < 20 ? n.creationSection === 0 : n.creationSection === 1 ) );
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
