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
import { ts } from "../../tools/project.mjs";
import {
	ACTION_CENSUS_FILES,
	actionCensusClosure,
	actionCensusInputSet,
	actionCensusKey
} from "../../tools/ui-action-census-gate.mjs";

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

// ============================================================================
// Action census gate: the key watches the census's inputs, nothing else
// ============================================================================

/*
================
actionFixtureModel

A source model in memory: the census's roots over small synthetic files,
plus one file nothing reaches. Fabricated with the compiler the model
itself parses with, so the closure walk reads real import edges.
================
*/
function actionFixtureModel() {
	const file = text => ts.createSourceFile( "fixture.ts", text, ts.ScriptTarget.Latest, true );
	return new Map( [
		[
			"src/engine/runtime/ui/ui.ts",
			file(
				'import { view } from "./stall-view";\nimport { panels } from "../../foundation/ui/panels";\n' +
					'import leftPad from "left-pad";\nexport const ui = view + panels + leftPad;\n'
			)
		],
		[
			"src/engine/runtime/ui/stall-view.ts",
			file(
				'import { stall } from "../../foundation/gameplay/stall";\nexport const view = stall;\n'
			)
		],
		[
			"src/engine/foundation/ui/panels.ts",
			file(
				'import { fort } from "../gameplay/fortress";\nexport const panels = fort;\n'
			)
		],
		[ "src/engine/foundation/gameplay/stall.ts", file( "export const stall = 1;\n" ) ],
		[
			"src/engine/foundation/gameplay/fortress.ts",
			file(
				'import { prod } from "./fortress-production";\nexport const fort = prod;\n'
			)
		],
		[ "src/engine/foundation/gameplay/fortress-production.ts", file( "export const prod = 1;\n" ) ],
		[ "src/engine/runtime/world/world.ts", file( "export const world = 1;\n" ) ]
	] );
}

test("action gate closure: every transitively imported file, and no other", () => {
	const closure = actionCensusClosure( actionFixtureModel() );
	assert.deepEqual( [ ...closure ].sort(), [
		"src/engine/foundation/gameplay/fortress-production.ts",
		"src/engine/foundation/gameplay/fortress.ts",
		"src/engine/foundation/gameplay/stall.ts",
		"src/engine/foundation/ui/panels.ts",
		"src/engine/runtime/ui/stall-view.ts",
		"src/engine/runtime/ui/ui.ts"
	] );
});

test("action gate closure: a missing root fails loudly instead of unwatched", () => {
	const model = actionFixtureModel();
	model.delete( "src/engine/foundation/ui/panels.ts" );
	assert.throws( () => actionCensusClosure( model ), /foundation\/ui\/panels\.ts is not in the source model/ );
});

test("action gate key: moves with a watched input, stands for an unrelated one", () => {
	const closure = actionCensusClosure( actionFixtureModel() );
	const inputSet = actionCensusInputSet( closure );
	// The committed census and the tool are watched alongside the closure.
	assert.equal( inputSet.has( "apps/client-next/tests/runtime/ui-action-census.json" ), true );
	assert.equal( ACTION_CENSUS_FILES.includes( "apps/client-next/tools/ui-action-census.mjs" ), true );
	assert.equal( inputSet.has( "apps/client-next/src/engine/runtime/world/world.ts" ), false );
	const entries = [
		[ "apps/client-next/src/engine/runtime/ui/ui.ts", "blob-ui" ],
		[ "apps/client-next/src/engine/runtime/world/world.ts", "blob-world" ]
	];
	const key = actionCensusKey( entries, inputSet );
	// An unrelated file's new state cannot move the key...
	const unrelated = actionCensusKey(
		entries.map( ( [file, state] ) => [ file, file.endsWith( "world.ts" ) ? "blob-world-2" : state ] ),
		inputSet
	);
	assert.equal( unrelated, key );
	// ...and a watched file's new state must.
	const watched = actionCensusKey(
		entries.map( ( [file, state] ) => [ file, file.endsWith( "ui.ts" ) ? "blob-ui-2" : state ] ),
		inputSet
	);
	assert.notEqual( watched, key );
});
