/*
===========================================================================

profile-lines.test.mjs - line attribution refuses minified bundle lines

A CPU profile's line ticks carry no column. A generated line that covers
several source lines cannot name the hot one, so the analyzer must report it
as unattributable rather than charge whatever source starts the line.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { createSourceMap } from "../../tools/perf/core/symbols.mjs";
import { forEachLine } from "../../tools/perf/core/profile.mjs";

// Generated line 0 maps source lines 1 and 2 (columns 0 and 2); generated
// line 1 maps source line 3 alone. VLQ: [0,0,0,0], [2,0,1,0]; then [0,0,1,0].
const MAPPINGS = "AAAA,EACA;AACA";

test("a source map reports how many source lines a generated line covers", () => {
	const map = createSourceMap( { version: 3, sources: [ "src/a.ts" ], names: [], mappings: MAPPINGS } );
	assert.equal( map.span( 0 ), 2 );
	assert.equal( map.span( 1 ), 1 );
	assert.equal( map.span( 7 ), 0 );
});

test("line ticks on a multi-line generated line are unattributable", () => {
	const map = createSourceMap( { version: 3, sources: [ "src/a.ts" ], names: [], mappings: MAPPINGS } );
	// A symbolizer over the one map, as createSymbolizer exposes it.
	const symbolizer = {
		frame: () => ({ name: "hot", file: "a.ts", line: 1 }),
		position( url, line, column ) {
			const found = map.lookup( line - 1, column - 1 );
			return { file: "a.ts", line: found.line, span: map.span( line - 1 ) };
		}
	};
	const node = {
		id: 1,
		callFrame: { functionName: "hot", url: "bundle.js", lineNumber: 0, columnNumber: 0 },
		positionTicks: [ { line: 1, ticks: 3 }, { line: 2, ticks: 1 } ]
	};
	const profile = { nodes: new Map( [ [ 1, node ] ] ), weights: new Map( [ [ 1, 8 ] ] ) };
	const lines = new Map();
	forEachLine( profile, symbolizer, "hot", ( key, weight ) => lines.set( key, (lines.get( key ) ?? 0) + weight ) );
	assert.equal( lines.get( "a.ts:3" ), 2, "a one-to-one generated line keeps its source line" );
	const refused = [ ...lines.keys() ].find( key => key.includes( "unattributable" ) );
	assert.ok( refused, "the two-line generated line is refused" );
	assert.equal( lines.get( refused ), 6 );
	assert.ok( ![ ...lines.keys() ].includes( "a.ts:1" ), "never charged to the line that starts it" );
});
