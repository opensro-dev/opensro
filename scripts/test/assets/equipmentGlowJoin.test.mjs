/*
===========================================================================

equipmentGlowJoin.test.mjs - the equipment glow build joins, on fixtures

equipmentGlowCatalog joins itemtypenumber tiers and itemoption groups to
itemdata ids, marking the later client's tiers enhanced; the published-
catalog test in the client suite walks the real tables instead. These
fixtures pin the join itself (which tiers, which marking, which items fall
out) and the official enchant-spark table equipmentEnchantSparks prefers
when it is imported, including the Unsupported-row build error a short
row for a known item must raise (#1536).

===========================================================================
*/
import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

import { equipmentGlowCatalog } from "../../build/char/equipmentGlowMetadata.mjs";
import { equipmentEnchantSparks } from "../../build/char/equipmentParticles.mjs";

/*
================
fixtureRoot

One temp directory per fixture root, removed with the test.
================
*/
async function fixtureRoot( t, files ) {
	const root = await mkdtemp( path.join( os.tmpdir(), "sro-glow-join-" ) );
	t.after( () => rm( root, { recursive: true, force: true } ) );
	for ( const [name, text] of Object.entries( files ) ) {
		await mkdir( path.dirname( path.join( root, name ) ), { recursive: true } );
		await writeFile( path.join( root, name ), text );
	}
	return root;
}

test("the glow catalog joins native tiers and marks the later client's tiers enhanced", async ( t ) => {
	const tier = ( group, threshold, op ) =>
		`${group} ${threshold} 0.005,-0.005 res\\glow.ddj ${op} 7 255,0,0 0,255,0 1000 1`;
	const nativeResinfo = await fixtureRoot( t, {
		"itemtypenumber.txt": [ tier( 1, 3, 4 ), tier( 1, 5, 4 ), tier( 1, 7, 5 ), tier( 1, 9, 5 ) ].join( "\n" ) +
			"\n",
		"itemoption.txt": "ITEM_CH_SWORD_01_A 1\n"
	} );
	const vsroResinfo = await fixtureRoot( t, {
		"itemtypenumber.txt": [ tier( 2, 6, 5 ), tier( 2, 10, 5 ) ].join( "\n" ) + "\n",
		"itemoption.txt": "ITEM_CH_SWORD_01_A 2\n"
	} );
	// itemdata shards are native UTF-16LE textdata; the resinfo tables are latin1.
	const textdata = await fixtureRoot( t, {
		"itemdata_1.txt": Buffer.from(
			"1\t71\tITEM_CH_SWORD_01_A\n1\t72\tITEM_CH_BOW_01_A\n0\t73\tITEM_CH_SWORD_01_A\n",
			"utf16le"
		)
	} );
	const row = ( threshold, gain, enhanced ) => ({
		threshold,
		uv: [ .005, -.005 ],
		texture: "res/glow.ddj",
		gain,
		color1: [ 255, 0, 0 ],
		color2: [ 0, 255, 0 ],
		period: 1000,
		alphaTest: true,
		enhanced
	});
	const nativeRows = [ row( 3, 1, false ), row( 5, 1, false ), row( 7, 2, false ), row( 9, 2, false ) ];
	const catalog = equipmentGlowCatalog( vsroResinfo, nativeResinfo, textdata );
	assert.deepEqual( catalog.get( 71 ), [ ...nativeRows, row( 6, 2, true ), row( 10, 2, true ) ] );
	// No itemoption row: no glow. Inactive itemdata row: not an item.
	assert.equal( catalog.get( 72 ), undefined );
	assert.equal( catalog.get( 73 ), undefined );
	assert.equal( catalog.size, 1 );
	// Without the later client's tables the same item carries native rows only.
	const nativeOnly = equipmentGlowCatalog( await fixtureRoot( t, {} ), nativeResinfo, textdata );
	assert.deepEqual( nativeOnly.get( 71 ), nativeRows );
});

const SPARK_RECORDS = new Map( [
	[ 71, { id: 71, code: "ITEM_CH_SWORD_01_A", slot: 6 } ],
	[ 251, { id: 251, code: "ITEM_CH_SHIELD_01_A", slot: 7 } ]
] );

test("the official spark table joins by codename and skips items the data does not have", async ( t ) => {
	const vsro = await fixtureRoot( t, {
		"itemoptionefp.txt": "ITEM_CH_SWORD_01_A\tSystem\\System_Enchant_A.efp\t130\tBip01 R Hand\tnone\t8\n" +
			"ITEM_CH_SHIELD_01_A\tsystem\\system_enchantshield_a_01.efp\t100\tBip01 L Hand\tnone\t3\n" +
			"ITEM_LATER_01_A\tsystem\\system_enchant_a_01.efp\t100\tBip01 R Hand\tnone\t8\n"
	} );
	assert.deepEqual( equipmentEnchantSparks( SPARK_RECORDS, vsro ), {
		71: [ {
			effectPath: "system/system_enchant_a.efp",
			bone: "Bip01 R Hand",
			scale: Math.fround( 1.3 ),
			offset: [ 0, 0, 0 ],
			root: false,
			threshold: 8
		} ],
		251: [ {
			effectPath: "system/system_enchantshield_a_01.efp",
			bone: "Bip01 L Hand",
			scale: 1,
			offset: [ 0, 0, 0 ],
			root: false,
			threshold: 3
		} ]
	} );
});

test("a short spark row for a known item raises the build error; an unknown item's short row is skipped", async ( t ) => {
	const short = await fixtureRoot( t, { "itemoptionefp.txt": "ITEM_CH_SWORD_01_A\n" } );
	assert.throws(
		() => equipmentEnchantSparks( SPARK_RECORDS, short ),
		( error ) =>
			error instanceof Error && !(error instanceof TypeError) &&
			/^Unsupported itemoptionefp row/.test( error.message )
	);
	const unknown = await fixtureRoot( t, { "itemoptionefp.txt": "ITEM_LATER_01_A\n" } );
	assert.deepEqual( equipmentEnchantSparks( SPARK_RECORDS, unknown ), {} );
});
