/*
===========================================================================

nativeClaimsGate.test.mjs - planted violations for the native-claims gate

===========================================================================
*/

import assert from "node:assert/strict";
import { test } from "node:test";
import { citedAddresses, claimRowCount, commentTextByLine, evaluateClaims } from "../../checks/check_native_claims.mjs";

const EMPTY_TABLE =
	"## Native claims\n\n| Claim | Native evidence | Checked by | Status |\n| --- | --- | --- | --- |\n\n## Verification\n";
const FILLED_TABLE = EMPTY_TABLE.replace(
	"\n\n## Verification",
	"\n| A | 861580 | asm | verified |\n\n## Verification"
);

function diffFor( file, line ) {
	return `+++ b/${file}\n@@ -0,0 +${line} @@\n+x\n`;
}

test("a new citation with an empty table fails", () => {
	const verdict = evaluateClaims( {
		diff: diffFor( "apps/client-next/src/a.ts", 2 ),
		body: EMPTY_TABLE,
		files: () => "const a = 1;\n// 861580 seeds its best distance.\n"
	} );
	assert.equal( verdict.ok, false );
	assert.deepEqual( verdict.citations, [ "apps/client-next/src/a.ts:2 861580" ] );
});

test("a filled row satisfies the citation", () => {
	assert.equal(
		evaluateClaims( {
			diff: diffFor( "apps/client-next/src/a.ts", 1 ),
			body: FILLED_TABLE,
			files: () => "// 861580\n"
		} ).ok,
		true
	);
});

test("a citation in a banner counts, one in code or a test does not", () => {
	const banner = "/*\n====\nselect\n\nAction 1005 in 695420 selects it.\n====\n*/\n";
	assert.equal(
		evaluateClaims( { diff: diffFor( "apps/server/a.go", 5 ), body: "", files: () => banner } ).ok,
		false
	);
	assert.equal(
		evaluateClaims( { diff: diffFor( "apps/server/a.go", 1 ), body: "", files: () => "x := 861580\n" } ).ok,
		true
	);
	assert.equal(
		evaluateClaims( { diff: diffFor( "apps/server/a_test.go", 1 ), body: "", files: () => "// 861580\n" } ).ok,
		true
	);
});

test("an untouched citation asks for nothing", () => {
	assert.equal(
		evaluateClaims( { diff: diffFor( "apps/server/a.go", 2 ), body: "", files: () => "// 861580\nx := 1\n" } ).ok,
		true
	);
});

test("quantities are not addresses", () => {
	assert.deepEqual( citedAddresses( "100000 frames, 500000 ms, then 0x58E2F4" ), [ "58E2F4" ] );
});

test("padded citations count, hex-shaped words do not", () => {
	assert.deepEqual( citedAddresses( "at 0x0085EA70 and 0085EA71" ), [ "85EA70", "85EA71" ] );
	assert.deepEqual( citedAddresses( "the presence facade" ), [] );
	assert.deepEqual( citedAddresses( "0xBCBEEC and sub_BFBDAC" ), [ "BCBEEC", "BFBDAC" ] );
});

test("a comment's slashes inside a string are not a comment", () => {
	assert.equal( commentTextByLine( 'const u = "http://861580";\n' ).size, 0 );
});

test("the table's header, rule and quoted rows are not claims", () => {
	assert.equal( claimRowCount( EMPTY_TABLE ), 0 );
	assert.equal( claimRowCount( FILLED_TABLE ), 1 );
	assert.equal( claimRowCount( `<!--\n${FILLED_TABLE}-->` ), 0 );
	assert.equal( claimRowCount( "" ), 0 );
	assert.equal(
		claimRowCount( `<!-- open
${FILLED_TABLE}` ),
		0
	);
	assert.equal(
		claimRowCount( `<!-- a -->
${FILLED_TABLE}` ),
		1
	);
});
