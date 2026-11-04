/*
===========================================================================

ui-census.test.mjs - the wave-0 census walker reads ladders, not text

The census is a gate for the ui.ts split: if its attribution quietly
degrades (a guard shape it no longer sees, a field it stops counting),
every move PR after it reviews against a fiction. These tests pin the
walker's contract on synthetic sources: exact and prefix guards become
routes, references land in the bucket of their nearest guard, a field
touched from two buckets is shared, and a shadowed name is never
misattributed to the closure field.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { census } from "../../tools/ui-census.mjs";

const SOURCE = `
export function createUi( assets, commands ) {
	let panel = "", focus = null, stallPage = 0;
	const FIXED = 1;
	function activate( id ) {
		if ( id === "chat-send" ) { focus = "chat-text"; return; }
		if ( id.startsWith( "stall-" ) ) { stallPage++; return; }
		if ( panel === "Guild" && id === "guild-close" ) { panel = ""; return; }
	}
	function event( event ) {
		if ( event.kind === "activate" && event.id === "open-one" ) activate( event.id );
		if ( event.kind === "key" ) focus = null;
	}
	function step( next, now ) {
		if ( panel ) focus = panel;
		return stallPage + FIXED;
	}
	return { activate, event, step };
}
`;

test("routes: exact and prefix guards in activate and event, with anchors", () => {
	const table = census( SOURCE );
	assert.deepEqual(
		table.routes.map( route => [ route.fn, route.kind, route.pattern ] ),
		[
			[ "activate", "exact", "chat-send" ],
			[ "activate", "prefix", "stall-" ],
			[ "activate", "exact", "guild-close" ],
			[ "event", "exact", "open-one" ]
		]
	);
	assert.ok( table.routes.every( route => route.line > 1 ) );
});

test("state: buckets follow the nearest guard, shared means two buckets", () => {
	const table = census( SOURCE );
	const byName = Object.fromEntries( table.fields.map( field => [ field.name, field ] ) );
	assert.deepEqual( Object.keys( byName ).sort(), [ "focus", "panel", "stallPage" ] );
	// focus is written under the chat guard, cleared in event, read in step.
	assert.deepEqual( Object.keys( byName.focus.buckets ).sort(), [ "chat", "event", "step" ] );
	assert.equal( byName.focus.shared, true );
	// stallPage lives under the stall prefix and in step: shared.
	assert.deepEqual( Object.keys( byName.stallPage.buckets ).sort(), [ "stall", "step" ] );
	assert.equal( byName.stallPage.shared, true );
	// panel is written under its panel gate and read in step.
	assert.deepEqual( Object.keys( byName.panel.buckets ).sort(), [ "panel:Guild", "step" ] );
	assert.equal( table.summary.fields, 3 );
	assert.equal( table.summary.sharedFields, 3 );
});

test("state: a shadowed name is not the closure field", () => {
	const shadowed = `
export function createUi( assets, commands ) {
	let focus = null;
	function helper( focus ) { return focus; }
	function step( next, now ) { return helper( 1 ) + (focus ?? 0); }
	return { step };
}
`;
	const table = census( shadowed );
	const focus = table.fields.find( field => field.name === "focus" );
	// Only the step read counts; the helper's parameter is its own.
	assert.deepEqual( Object.keys( focus.buckets ), [ "step" ] );
	assert.equal( focus.buckets.step.reads, 1 );
	assert.equal( focus.shared, false );
});

test("state: constants are not fields, and writes are told from reads", () => {
	const table = census( SOURCE );
	const stallPage = table.fields.find( field => field.name === "stallPage" );
	assert.equal( stallPage.buckets.stall.writes, 1 );
	assert.equal( stallPage.buckets.step.reads, 1 );
});
