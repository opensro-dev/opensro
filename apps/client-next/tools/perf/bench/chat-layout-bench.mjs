/*
===========================================================================

chat-layout-bench.mjs - the cost of one chat layout rebuild (issue #3176)

Usage:
  node tools/perf/bench/chat-layout-bench.mjs

One rebuild is what a new chat line costs on a full log: the retained
layout in the HUD drops its product, chatLayout wraps every visible
line again, and the frame pays for all of them. The bench holds the
log at its 128-line cap, appends one fresh line per rebuild (the
oldest falls off) and times chatLayout over REBUILDS rebuilds; the
median is the row. The done-when of #3176 is a median under 0.5 ms,
so the bench exits 1 above it.

The measure is the production one's shape without its string memo:
text.run sums one bitmap advance per glyph, and a memo hit only
changes the constant, so the bench sums a fixed advance table glyph
by glyph. Lines are deterministic (a seeded generator over word
shapes), channels and names mixed the way the log mixes them.

===========================================================================
*/
import "../../../tests/helpers/native-source-loader.mjs";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../../scripts/lib/generatedRoot.mjs";

const { chatLayout } = await import( "../../../src/engine/foundation/ui/chat-layout.ts" );
const { decodeAuthoredLayout } = await import( "../../../src/engine/foundation/ui/authored-layout.ts" );

const LOG_LINES = 128;
const REBUILDS = 80;
const WARMUPS = 5;
const BUDGET_MS = 0.5;

/*
================
advanceOf

One glyph's advance in the bench's fixed table (5..8 px), so the
measure below is a sum of glyph advances exactly as text.run is.
================
*/
function advanceOf( char ) {
	return 5 + (char.codePointAt( 0 ) % 4);
}

/*
================
measure

The additive measure the bench wraps with: every string costs the
sum of its glyphs, prefixes included, which is the cost shape the
old prefix-measuring wrap paid per character.
================
*/
function measure( value ) {
	let width = 0;
	for ( const char of value ) width += advanceOf( char );
	return width;
}

/*
================
makeLines

Deterministic chat traffic: a seeded walk over word shapes, names
and channels, LOG_LINES lines long. Each line's text differs, so a
rebuild that wraps the whole log cannot reuse a previous line's
wrap by accident.
================
*/
function makeLines() {
	let seed = 0x2f6e2b1;
	const next = () => (seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff;
	const words = [
		"silk",
		"road",
		"trade",
		"caravan",
		"hunter",
		"thief",
		"guild",
		"party",
		"donwhang",
		"jangan",
		"hotan",
		"bandit",
		"premium",
		"ticket",
		"stall",
		"price",
		"gold",
		"scroll"
	];
	const lines = [];
	for ( let i = 0; i < LOG_LINES; i++ ) {
		const count = 6 + Math.floor( next() * 14 ), parts = [];
		for ( let w = 0; w < count; w++ ) parts.push( words[Math.floor( next() * words.length )] );
		lines.push( {
			channel: [ 1, 4, 5, 6, 6, 6 ][Math.floor( next() * 6 )],
			name: "Peer" + (i % 23),
			text: parts.join( " " ),
			outgoing: i % 5 === 0
		} );
	}
	return lines;
}

/*
================
main

Times the rebuilds and reports the median against the budget.
================
*/
function main() {
	const layout = decodeAuthoredLayout(
		JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifchatviewer.json", "utf8" ) )
	);
	const wrapCache = new Map();
	const lines = makeLines();
	const input = () => ({
		layout,
		width: 1024,
		height: 768,
		rows: 2,
		tab: 0,
		input: "",
		lines,
		welcome: "",
		copy: key => key,
		size: () => [ 16, 16 ],
		text: () => [],
		hover: null,
		pressed: null,
		measure: ( value, style ) => measure( value ) + (style?.fontStyle === 2 ? value.length : 0),
		wrapCache
	});
	for ( let i = 0; i < WARMUPS; i++ ) chatLayout( input() );
	const times = [];
	for ( let i = 0; i < REBUILDS; i++ ) {
		lines.push( {
			channel: 6,
			name: "Peer" + (i % 23),
			text: "fresh line " + i + " over the silk road again",
			outgoing: false
		} );
		lines.shift();
		const start = performance.now();
		chatLayout( input() );
		times.push( performance.now() - start );
	}
	times.sort( ( a, b ) => a - b );
	const median = times[Math.floor( times.length / 2 )], p90 = times[Math.floor( times.length * 0.9 )];
	console.log(
		`chat rebuild on a full log: median ${median.toFixed( 3 )} ms, p90 ${
			p90.toFixed( 3 )
		} ms over ${REBUILDS} rebuilds (budget ${BUDGET_MS} ms)`
	);
	if ( median >= BUDGET_MS ) {
		console.error( "over the #3176 budget" );
		process.exitCode = 1;
	}
}

main();
