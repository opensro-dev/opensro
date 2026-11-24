/*
===========================================================================

world-entry-gate.test.mjs - the world entry names what it waits on

world-entry-gate.ts keeps, per entry, when each worldReady condition last
turned true and which are still false. The diagnostics line and the
runtime's worldEntry() read it, so a slow entry says which condition held
it instead of "loading none".

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import test from "node:test";
import assert from "node:assert/strict";
const { createWorldEntryGate, worldEntryLine, WORLD_ENTRY_CONDITIONS } = await import(
	"../../src/engine/runtime/frontend/world-entry-gate.ts"
);

/*
================
conditions

Every condition true except the named ones.
================
*/
function conditions( ...pending ) {
	return Object.fromEntries( WORLD_ENTRY_CONDITIONS.map( name => [ name, !pending.includes( name ) ] ) );
}

test("a waiting entry names its false conditions and how long it has waited", () => {
	const gate = createWorldEntryGate();
	assert.equal( gate.report(), null );
	assert.equal( worldEntryLine( gate.report() ), "World entry: none" );
	gate.step( true, conditions( "textures", "navigation" ), 1000 );
	gate.step( true, conditions( "navigation" ), 4000 );
	const report = gate.report();
	assert.equal( report.ready, false );
	assert.deepEqual( report.waiting, [ "navigation" ] );
	assert.equal( report.elapsedMs, 3000 );
	assert.equal( report.settledMs.textures, 3000 );
	assert.equal( worldEntryLine( report ), "World entry: waiting 3.0 s on navigation" );
});

test("a condition that falls back resettles: the time kept is when it stopped blocking", () => {
	const gate = createWorldEntryGate();
	gate.step( true, conditions( "textures" ), 0 );
	gate.step( true, conditions(), 2000 );
	// The finished entry is frozen; a new group's textures arrive later in a
	// new entry only.
	assert.equal( gate.report().ready, true );
	const next = createWorldEntryGate();
	next.step( true, conditions(), 0 );
	assert.equal( next.report().ready, true, "all true on the first frame is a finished entry" );
	const bouncing = createWorldEntryGate();
	bouncing.step( true, conditions( "audio" ), 0 );
	bouncing.step( true, conditions( "audio", "textures" ), 1000 );
	bouncing.step( true, conditions( "audio" ), 6000 );
	assert.equal( bouncing.report().settledMs.textures, 6000, "textures blocked until 6 s, not 0" );
});

test("a finished entry reports its slowest conditions and stays frozen", () => {
	const gate = createWorldEntryGate();
	gate.step( true, conditions( "textures", "navigation", "audio" ), 10_000 );
	gate.step( true, conditions( "navigation", "audio" ), 15_000 );
	gate.step( true, conditions( "navigation" ), 18_000 );
	gate.step( true, conditions(), 31_000 );
	// world-ready is sent: the session is no longer entering.
	gate.step( false, conditions(), 40_000 );
	const report = gate.report();
	assert.equal( report.ready, true );
	assert.equal( report.elapsedMs, 21_000 );
	assert.deepEqual( report.waiting, [] );
	assert.equal(
		worldEntryLine( report ),
		"World entry: ready in 21.0 s; last navigation 21.0 s, audio 8.0 s, textures 5.0 s"
	);
});

test("a new entry starts a new timeline", () => {
	const gate = createWorldEntryGate();
	gate.step( true, conditions(), 0 );
	gate.step( false, conditions(), 1000 );
	gate.step( true, conditions( "scene" ), 50_000 );
	const report = gate.report();
	assert.equal( report.elapsedMs, 0 );
	assert.deepEqual( report.waiting, [ "scene" ] );
});
