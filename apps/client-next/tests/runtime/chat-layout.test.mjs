/*
===========================================================================

chat-layout.test.mjs - native and compact chat geometry

Check the published controls, wrapping and editor bounds together so a
smaller viewport cannot leave input geometry behind the visible artwork.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";

const { chatLayout } = await import( "../../src/engine/foundation/ui/chat-layout.ts" );
const { decodeAuthoredLayout } = await import( "../../src/engine/foundation/ui/authored-layout.ts" );
const layout = decodeAuthoredLayout(
	JSON.parse( readFileSync( CLIENT_PUBLIC_ROOT + "/assets/cif/layouts/ifchatviewer.json", "utf8" ) )
);

/*
================
input
================
*/
/**
 * @param {Partial<import('../../src/engine/foundation/ui/chat-layout.ts').ChatLayoutInput>} overrides
 * @returns {import('../../src/engine/foundation/ui/chat-layout.ts').ChatLayoutInput}
 */
function input( overrides = {} ) {
	return {
		layout,
		width: 1024,
		height: 768,
		rows: 2,
		tab: 0,
		input: "",
		lines: [],
		welcome: "Welcome",
		copy: key => key,
		size: () => [ 16, 16 ],
		text: () => [],
		hover: null,
		pressed: null,
		measure: value => value.length * 7,
		...overrides
	};
}

test("omitted compact preserves native geometry even in a narrow viewport", () => {
	for ( const width of [ 375, 1024 ] ) {
		const result = chatLayout( input( { width } ) );
		assert.deepEqual( result.scrolling.bounds, [ 0, 542, 399, 154 ] );
		assert.deepEqual( result.controls.find( c => c.id === "chat-text" )?.rect, [ 22, 699, 373, 14 ] );
		assert.deepEqual( result.controls.find( c => c.id === "chat-tab:3" )?.rect, [ 198, 542, 52, 20 ] );
		assert.ok( result.blocks.some( r => r.join() === "18,696,381,20" ) );
	}
	const native = chatLayout( input() );
	assert.deepEqual( chatLayout( input( { compact: { bottom: 52, maxRows: 6 } } ) ), native );
});

test("compact portrait fits chrome, controls and editor above the reserved bottom", () => {
	const state = input( {
		width: 375,
		height: 667,
		rows: 6,
		compact: { bottom: 140, maxRows: 2 },
		input: "x".repeat( 90 ),
		editState: { focused: true, start: 85, end: 90, composing: true }
	} );
	const result = chatLayout( state );
	assert.equal( state.rows, 6 );
	assert.deepEqual( result.scrolling.bounds, [ 0, 353, 375, 154 ] );
	const edit = result.controls.find( c => c.id === "chat-text" );
	assert.deepEqual( edit?.rect, [ 22, 510, 349, 14 ] );
	for ( const r of [ ...result.blocks, ...result.controls.map( c => c.rect ) ] ) {
		assert.ok( r[0] >= 0 && r[1] >= 0 );
		assert.ok( r[0] + r[2] <= 375 && r[1] + r[3] <= 527, JSON.stringify( r ) );
	}
	const ink = result.quads.filter( q => q.texture === "" );
	assert.ok( ink.length >= 3, "selection, caret and composition remain clipped to the resized editor" );
	for ( const q of ink ) assert.deepEqual( q.clip, edit?.rect );
});

test("compact wrapping retains sender identity and scrolls by displayed rows", () => {
	const lines = Array.from( { length: 10 }, () => ({
		channel: 6,
		name: "Peer",
		text: "x".repeat( 46 ),
		outgoing: false
	}) );
	const native = chatLayout( input( { lines, welcome: "" } ) );
	const state = input( {
		width: 375,
		height: 667,
		lines,
		welcome: "",
		compact: { bottom: 100, maxRows: 1 }
	} );
	const compact = chatLayout( state );
	const visible = compact.controls.filter( c => c.id.startsWith( "chat-line:" ) );
	assert.equal( visible.length, 4 );
	assert.ok( compact.scrolling.range > native.scrolling.range );
	for ( const row of visible ) {
		assert.equal( row.whisperTarget, "Peer" );
		assert.equal( row.rect[2], 341 );
		assert.ok( state.measure( row.label ) <= row.rect[2] );
	}
	const older = chatLayout( { ...state, offset: 999 } );
	assert.equal( older.controls.filter( c => c.id.startsWith( "chat-line:" ) ).length, 4 );
});

test("compact landscape limits row groups by height without changing the requested size", () => {
	const state = input( { width: 667, height: 375, rows: 6, compact: { bottom: 140, maxRows: 6 } } );
	const result = chatLayout( state );
	assert.equal( state.rows, 6 );
	assert.deepEqual( result.scrolling.bounds, [ 0, 5, 399, 210 ] );
	assert.deepEqual( result.controls.find( c => c.id === "chat-text" )?.rect, [ 22, 218, 373, 14 ] );
	assert.deepEqual( chatLayout( { ...state, height: 667 } ).scrolling.bounds, [ 0, 129, 399, 378 ] );
});

const EVENT_LINES = [
	{ channel: 1, name: "Peer", text: "hello all", outgoing: false },
	{ channel: 4, name: "Peer", text: "hello party", outgoing: false },
	{
		channel: 7,
		name: "",
		text: "Looter has dropped D6 Star Blade (Seal Of Star) from Tiger Girl",
		outgoing: false
	}
];

/*
================
visibleLabels
================
*/
/** @param {ReturnType<typeof chatLayout>} result */
function visibleLabels( result ) {
	return result.controls.filter( c => c.id.startsWith( "chat-line:" ) ).map( c => c.label );
}

test("without the Event tab the four native tabs keep notices in All", () => {
	const result = chatLayout( input( { lines: EVENT_LINES, welcome: "" } ) );
	assert.deepEqual(
		result.controls.filter( c => c.id.startsWith( "chat-tab:" ) ).map( c => c.id ),
		[ "chat-tab:0", "chat-tab:1", "chat-tab:2", "chat-tab:3" ]
	);
	assert.ok( visibleLabels( result ).some( label => label.includes( "Seal Of Star" ) ) );
});

test("the Event tab holds channel-7 notices and All skips them", () => {
	const tabs = chatLayout( input( { lines: EVENT_LINES, welcome: "", eventTab: true } ) );
	assert.deepEqual(
		tabs.controls.filter( c => c.id.startsWith( "chat-tab:" ) ).map( c => [ c.id, c.label ] ),
		[
			[ "chat-tab:0", "UIIT_CTL_CHAT_ALL" ],
			[ "chat-tab:1", "UIIT_CTL_PARTY" ],
			[ "chat-tab:2", "UIIT_CTL_GUILD" ],
			[ "chat-tab:3", "UIIT_STT_GUILD_RESPECT_ALLY" ],
			[ "chat-tab:4", "Event" ]
		]
	);
	const all = visibleLabels( chatLayout( input( { lines: EVENT_LINES, welcome: "", eventTab: true, tab: 0 } ) ) );
	assert.ok( all.some( label => label.includes( "hello all" ) ) );
	assert.ok( !all.some( label => label.includes( "Seal Of Star" ) ) );
	const party = visibleLabels( chatLayout( input( { lines: EVENT_LINES, welcome: "", eventTab: true, tab: 1 } ) ) );
	assert.deepEqual( party.filter( label => label.includes( "hello" ) ).length, 1 );
	assert.ok( !party.some( label => label.includes( "Seal Of Star" ) ) );
	const event = visibleLabels( chatLayout( input( { lines: EVENT_LINES, welcome: "", eventTab: true, tab: 4 } ) ) );
	assert.ok( event.length >= 1 );
	assert.ok( event.some( label => label.includes( "Seal Of Star" ) ) );
	assert.ok( !event.some( label => label.includes( "hello" ) ) );
});

test("a saved Event selection clamps when the row is off, and five tabs fit narrow chat", () => {
	const clamped = chatLayout( input( { lines: EVENT_LINES, welcome: "", tab: 4 } ) );
	assert.equal( clamped.controls.find( c => c.id === "chat-tab:3" )?.selected, true );
	const narrow = chatLayout( input( {
		width: 375,
		height: 667,
		lines: EVENT_LINES,
		welcome: "",
		eventTab: true,
		tab: 4,
		compact: { bottom: 100, maxRows: 1 }
	} ) );
	const tabRects = narrow.controls.filter( c => c.id.startsWith( "chat-tab:" ) ).map( c => c.rect );
	assert.equal( tabRects.length, 5 );
	for ( const r of tabRects ) assert.ok( r[0] >= 0 && r[0] + r[2] <= 375, JSON.stringify( r ) );
	const desktop = chatLayout( input( { lines: EVENT_LINES, welcome: "", eventTab: true, tab: 4 } ) );
	const last = desktop.controls.find( c => c.id === "chat-tab:4" )?.rect;
	assert.ok( last && last[0] + last[2] <= 399, JSON.stringify( last ) );
});

test("zero displayed rows keeps only the input strip and preserves requested history size", () => {
	for ( const maxRows of [ 0, -1 ] ) {
		const state = input( { width: 375, height: 667, rows: 6, compact: { bottom: 140, maxRows } } );
		const result = chatLayout( state );
		assert.equal( state.rows, 6 );
		assert.deepEqual( result.controls.map( c => c.id ), [ "chat-size", "chat-text" ] );
		assert.equal( result.scrolling.range, 0 );
		assert.deepEqual( result.blocks, [ [ 0, 507, 16, 20 ], [ 18, 507, 357, 20 ] ] );
	}
});
