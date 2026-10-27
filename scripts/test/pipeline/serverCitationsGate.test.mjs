/*
===========================================================================

serverCitationsGate.test.mjs - shrinking-ledger gate for reference-server
names in the server's Go (#1701)

The gate keeps its prefixes as digests, and this test must not spell them
either: it drives the matcher with digests of made-up prefixes, then checks
the shipped table's shape, the client exemptions and the shared ratchet.

===========================================================================
*/

import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import {
	CLIENT_SYMBOLS,
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
