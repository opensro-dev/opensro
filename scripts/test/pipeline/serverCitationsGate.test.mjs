/*
===========================================================================

serverCitationsGate.test.mjs - shrinking-ledger gate for reference-server
names in the server's Go (#1701)

The gate keeps its prefixes as digests, and this test must not spell them
either: it drives the matcher with digests of made-up prefixes, then checks
the shipped table's shape, the client exemptions and the shared ratchet.
The family pin holds digests too, so the families the #1701 sweep
reworded cannot silently leave the table.

===========================================================================
*/

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import {
	CLIENT_SYMBOLS,
	countAddressCitations,
	countServerCitations,
	createCitationMatcher,
	evaluateCitationRatchet,
	parseBaseline,
	SERVER_PREFIX_DIGESTS
} from "../../checks/check_server_citations.mjs";

/*
================
digest
================
*/
function digest( prefix ) {
	return [ prefix.length, createHash( "sha256" ).update( prefix ).digest( "hex" ) ];
}

test("the matcher counts words that begin with a listed prefix, in comments, strings and code", () => {
	const matches = createCitationMatcher( [ digest( "QxServ" ), digest( "QzK_" ) ], [ "QxServData" ] );
	const sample = [
		"// QxServ_Tick runs the reference loop",
		't.Fatal("QzK_Keeper mismatch")',
		"QxServPC := 1",
		"QxServData_Read(x) // a client symbol sharing the prefix",
		"QxServing := 2 // a lower-case letter after the prefix: an ordinary word",
		"QxSer := 3 // shorter than the prefix"
	].join( "\n" );
	assert.equal( countServerCitations( sample, matches ), 3 );
	assert.ok( matches( "QxServ" ), "the bare prefix counts" );
	assert.ok( !matches( "QxServing" ) );
	assert.ok( !matches( "QxServData_Read" ) );
});

test("the shipped table is digests only, and the client exemptions never count", () => {
	assert.ok( SERVER_PREFIX_DIGESTS.length > 0 );
	for ( const [length, hex] of SERVER_PREFIX_DIGESTS ) {
		assert.ok( Number.isInteger( length ) && length >= 3, `prefix length ${length}` );
		assert.match( hex, /^[0-9a-f]{64}$/ );
	}
	const matches = createCitationMatcher();
	for ( const symbol of CLIENT_SYMBOLS ) {
		assert.ok( !matches( symbol ), `${symbol} is a client symbol` );
		assert.ok( !matches( symbol + "_ParseRow" ), `${symbol}_ParseRow is a client symbol` );
	}
	for ( const ordinary of [ "Character", "CursorManager", "QuestState", "SkillCombatant", "context", "CAIRO" ] ) {
		assert.ok( !matches( ordinary ), `${ordinary} is an ordinary word` );
	}
});

test("a new citation fails the ratchet and a reworded file asks for --update", () => {
	const baseline = parseBaseline( "# header\napps/server/a.go 2\n" );
	const exists = () => true;
	const introduced = evaluateCitationRatchet(
		new Map( [ [ "apps/server/b.go", 1 ], [ "apps/server/a.go", 2 ] ] ),
		baseline,
		exists
	);
	assert.match( introduced.problems.join( "\n" ), /introduce reference-server class or function names/ );
	const raised = evaluateCitationRatchet( new Map( [ [ "apps/server/a.go", 3 ] ] ), baseline, exists );
	assert.match( raised.problems.join( "\n" ), /exceed their server-citations-baseline\.txt count/ );
	const reworded = evaluateCitationRatchet( new Map( [ [ "apps/server/a.go", 1 ] ] ), baseline, exists );
	assert.match( reworded.problems.join( "\n" ), /check_server_citations\.mjs --update/ );
	const updated = evaluateCitationRatchet( new Map(), baseline, exists, true );
	assert.deepEqual( updated.problems, [] );
	assert.equal( updated.removedCount, 1 );
});

// Digests of the families the #1701 sweep reworded: quest engine, mission
// dialogue, tactics, job, guild, flag world and skill manager.
const SWEPT_FAMILIES = [
	[ 11, "7bd0eaa0ab35c1f52528d0b79b83467919a3c2c216209ecc877196633d444f9c" ],
	[ 14, "76a4f4519cd6cdea2fb8670ba67711847b7053eaf358593ddc099c2a37c7e283" ],
	[ 10, "9b955dfe6280347ce298e8a695bfe3e7576372a30bd53abd77ee902b72f5ecec" ],
	[ 8, "2244e4f45459f9e8569d9f62b4455c4666efff240e4f98903c7cba65886d8d5c" ],
	[ 9, "861c4738533d2bfdc6ca280eea991149c587c514ac1a3353fe2a4c24a76547b9" ],
	[ 10, "559dd4d38b1d6d5f52288548f840957d4cddb2c9f0dcbb325f77571073762a6d" ],
	[ 13, "8418708620dfcefba1df3ca02ab1656eb29a742bf21388ba490ecc30bfc09a38" ]
];

test("the table keeps every family the #1701 sweep reworded", () => {
	const table = new Set( SERVER_PREFIX_DIGESTS.map( ( [length, hex] ) => length + ":" + hex ) );
	for ( const [length, hex] of SWEPT_FAMILIES ) {
		assert.ok( table.has( length + ":" + hex ), `family digest ${hex.slice( 0, 12 )} is missing` );
	}
});

test("addresses count in comments and strings, except on a client line or as masks", () => {
	const sample = [
		"x := 1 // see 4F2B50 and 0x005DA1B0, then sub_6a7f70",
		"y := 0x4F2B50 // code is not counted, nor 0x800000, 450000 or deface",
		'z := "6A7BE0"',
		"// the client's 5D9100 draws the row (v1.150 6FEF10 sends it)",
		"/* 4DF290 */"
	].join( "\n" );
	assert.equal( countAddressCitations( sample ), 5 );
	assert.equal( countServerCitations( sample, () => false ), 5 );
	// An escaped quote does not end the literal: the address stays inside it.
	assert.equal( countAddressCitations( 'a := "it' + "\\" + '"4F2B50"' ), 1 );
	assert.equal( countAddressCitations( "a := 4 // " + "4F2B50 in a client comment" ), 0 );
});
