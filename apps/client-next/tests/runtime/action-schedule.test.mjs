/*
===========================================================================

action-schedule.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( file ).href );
}
const { advanceAction, actionLayers, reconcileActionInstallations } = await load(
	"src/engine/foundation/animation/action-schedule.ts"
);
const { createAnimationDispatch } = await load( "src/engine/foundation/animation/animation-dispatch.ts" );
const { createCombat } = await load( "src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts" );
/*
================
phase
================
*/
const phase = clip => ({
	clip,
	definition: {
		durationMs: 1000,
		trackEvents: [ { cursorMs: 200, eventCode: 1, param0: 0, param1: 0 } ],
		soundEvents: [],
		timeWarpCurve: { scale: 0, records: [] }
	}
});
/*
================
clock
================
*/
const clock = phases => ({ phases, phase: 0, started: 0, previous: 0, entered: false });
test("READY callbacks precede WAIT, which holds until the server releases SHOT", () => {
	const c = clock( [ phase( "ready" ), phase( "wait" ), phase( "shot" ) ] );
	assert.deepEqual( advanceAction( c, 0 ).events, [ { phase: "READY", event: 0, at: 0 } ] );
	assert.deepEqual( advanceAction( c, .401 ).events, [ { phase: "READY", event: 1, at: .4 } ] );
	assert.deepEqual( advanceAction( c, 1.2 ).events, [ { phase: "WAIT", event: 0, at: 1.2 } ] );
	assert.equal( advanceAction( c, 10 ).loop, true );
	assert.equal( c.phase, 1 );
	const released = advanceAction( c, 10.5, 10 );
	assert.deepEqual( released.events, [ { phase: "SHOT", event: 0, at: 10 }, { phase: "SHOT", event: 1, at: 10.4 } ] );
	assert.deepEqual( advanceAction( c, 10.5, 10 ).events, [] );
	advanceAction( c, 11.201 );
	assert.equal( c.phase, 3 );
});
test("empty phases fire once; late frames preserve event times and release can interrupt READY", () => {
	const c = clock( [ null, null, phase( "shot" ) ] );
	assert.deepEqual( advanceAction( c, 2 ).events.map( e => [ e.phase, e.event, e.at ] ), [
		[ "READY", 0, 0 ],
		[ "WAIT", 0, 0 ],
		[ "SHOT", 0, 0 ],
		[ "SHOT", 1, .4 ]
	] );
	assert.deepEqual( advanceAction( c, 3 ).events, [] );
	const d = clock( [ phase( "ready" ), phase( "wait" ), phase( "shot" ) ] );
	const events = advanceAction( d, .6, .1 ).events;
	assert.deepEqual( events.map( e => [ e.phase, e.event ] ), [ [ "READY", 0 ], [ "SHOT", 0 ], [ "SHOT", 1 ] ] );
	assert.equal( d.started, .1 );
});
test("a B505 release cannot hold or restart SHOT when WAIT has no motion (8DF180)", () => {
	const c = clock( [ null, null, phase( "shot" ) ] );
	assert.deepEqual( advanceAction( c, 0 ).events.map( e => e.phase ), [ "READY", "WAIT", "SHOT" ] );
	assert.deepEqual( advanceAction( c, .401, .25 ).events, [ { phase: "SHOT", event: 1, at: .4 } ] );
	assert.equal( c.started, 0, "empty WAIT does not change the authored callback origin" );
	assert.deepEqual( advanceAction( c, .5, .25 ).events, [] );
});

test("B505 no-steering release is retained once, finalization removes it, malformed packets do not mutate", () => {
	const combat = createCombat(), p = new Uint8Array( 34 ), v = new DataView( p.buffer );
	p[0] = 1;
	v.setUint32( 2, 7, true );
	v.setUint32( 6, 1, true );
	v.setUint32( 10, 3, true );
	v.setUint32( 14, 2, true );
	p[18] = 1;
	p[19] = p[20] = 1;
	v.setUint32( 21, 2, true );
	assert.equal( combat.receive( 0xb245, p, 100 ), true );
	const release = new Uint8Array( 10 ), r = new DataView( release.buffer );
	release[0] = 1;
	r.setUint32( 1, 3, true );
	r.setUint32( 5, 2, true );
	combat.receive( 0xb505, release, 200 );
	combat.receive( 0xb505, release, 300 );
	assert.equal( combat.state().casts[0].shotAtMs, 200 );
	assert.throws( () => combat.receive( 0xb505, release.slice( 0, 9 ) ), /Truncated/ );
	release[9] = 8;
	assert.throws( () => combat.receive( 0xb505, release ), /Truncated/ );
	assert.equal( combat.state().casts[0].shotAtMs, 200 );
	const stop = Uint8Array.of( 2, 0, 3, 0, 0, 0 );
	combat.receive( 0xb505, stop, 400 );
	assert.equal( combat.state().casts[0].cancelledAtMs, 400 );
	combat.receive( 0xb505, stop, 500 );
	combat.step( 599 );
	assert.equal( combat.state().casts.length, 1 );
	combat.step( 600 );
	assert.deepEqual( combat.state().casts, [] );
});

test("cancellation caps callbacks and preserves an exit cursor without replay", () => {
	const c = clock( [ phase( "ready" ), phase( "wait" ), phase( "shot" ) ] );
	assert.deepEqual( advanceAction( c, 10, undefined, .1 ).events, [ { phase: "READY", event: 0, at: 0 } ] );
	assert.equal( c.cancelledAt, .1 );
	assert.deepEqual( advanceAction( c, 20 ).events, [] );
	assert.equal( advanceAction( c, 20 ).time, 0 );
});

test("action entry, phase change, natural end and early cancellation retain native blend envelopes", async () => {
	const { actionLayers } = await load( "src/engine/foundation/animation/action-schedule.ts" );
	const c = clock( [ null, null, phase( "attack" ) ] );
	advanceAction( c, 0 );
	assert.deepEqual( actionLayers( c, 0 ), [] );
	assert.ok( Math.abs( actionLayers( c, .1 )[0].weight - .5 ) < 1e-9 );
	advanceAction( c, 1.2 );
	assert.equal( c.phase, 3 );
	assert.equal( actionLayers( c, 1.2 )[0].clip, "attack" );
	assert.equal( actionLayers( c, 1.2 )[0].weight, 1 );
	assert.ok( Math.abs( actionLayers( c, 1.3 )[0].weight - .5 ) < 1e-9 );
	assert.deepEqual( actionLayers( c, 1.401 ), [] );
	const d = clock( [ phase( "ready" ), phase( "wait" ), phase( "shot" ) ] );
	advanceAction( d, 0 );
	advanceAction( d, 1.3 );
	const transition = actionLayers( d, 1.3 );
	assert.deepEqual( transition.map( x => x.clip ), [ "wait", "ready" ] );
	assert.equal( transition[0].weight, 1 );
	assert.equal( transition[0].lane, "timed" );
	assert.ok( Math.abs( transition[1].weight - .5 ) < 1e-9 );
	const e = clock( [ null, null, phase( "attack" ) ] );
	advanceAction( e, 0 );
	advanceAction( e, .1, undefined, .1 );
	assert.ok( Math.abs( actionLayers( e, .1 )[0].weight - .5 ) < 1e-9 );
	assert.equal( actionLayers( e, .2 )[0].weight, 1 );
	assert.equal( actionLayers( e, .301 )[0].weight, 1 );
	assert.deepEqual( actionLayers( e, 1.401 ), [] );
});

/*
================
entryHold
================
*/
test("one-shot entry holds pose and keyed callbacks until its blend completes", () => {
	const c = clock( [ null, null, phase( "shot" ) ] );
	c.phases[2].definition.trackEvents = [
		{ cursorMs: 0, eventCode: 1, param0: 0, param1: 0 },
		{ cursorMs: 96, eventCode: 1, param0: 0, param1: 0 }
	];
	advanceAction( c, 0 );
	for ( const at of [ .05, .1, .199, .2 ] ) {
		assert.deepEqual( advanceAction( c, at ).events, [] );
		assert.equal( actionLayers( c, at )[0].time, 0 );
	}
	assert.deepEqual( advanceAction( c, .25 ).events, [ { phase: "SHOT", event: 1, at: .2 } ] );
	assert.ok( Math.abs( actionLayers( c, .25 )[0].time - .05 ) < 1e-9 );
	assert.deepEqual( advanceAction( c, .296 ).events, [] );
	assert.deepEqual( advanceAction( c, .298 ).events, [ { phase: "SHOT", event: 2, at: .296 } ] );
	advanceAction( c, 1 );
	assert.equal( c.phase, 2, "entry time is additional to the authored clip length" );
	advanceAction( c, 1.2 );
	assert.equal( c.phase, 3 );
});

test("all empty and populated phase combinations preserve the entry hold", () => {
	for ( let mask = 0; mask < 8; mask++ ) {
		const phases = [ "ready", "wait", "shot" ].map( ( name, i ) => mask & (1 << i) ? phase( name ) : null );
		const c = clock( phases );
		advanceAction( c, 0 );
		const first = actionLayers( c, .1 )[0];
		if ( first ) assert.equal( first.time, first.loop ? .1 : 0, `mask ${mask}` );
		advanceAction( c, 2, 1.5 );
		const callbacks = advanceAction( c, 20, 1.5 ).events;
		assert.ok( callbacks.every( row => Number.isFinite( row.at ) ) );
		assert.deepEqual( advanceAction( c, 21, 1.5 ).events, [] );
	}
});

/*
================
nativeEntryOracle

Frozen original-byte execution covers entry, crossing-frame remainder,
natural completion and the final-pose clamp. Optional regeneration fails
on truncated execution and uses the installed licensed client only.
================
*/
test("skill pose clocks match original one-shot execution across entry and natural exit", () => {
	const fixture = JSON.parse(
		readFileSync( new URL( "../fixtures/native/action-entry.json", import.meta.url ), "utf8" )
	);
	if ( process.env.SRO_NATIVE_ACTION_ORACLE === "1" ) {
		const result = spawnSync( "python", [ "tools/native-action-entry.py" ], {
			input: JSON.stringify( fixture.cases.map( ( { frames, ...row } ) => row ) ),
			encoding: "utf8",
			timeout: 30000
		} );
		assert.equal( result.status, 0, result.stderr );
		assert.deepEqual( JSON.parse( result.stdout ), fixture );
	}
	for ( const row of fixture.cases ) {
		const p = phase( "shot" );
		p.definition.durationMs = row.durationMs;
		p.definition.trackEvents = [];
		const c = { ...clock( [ null, null, p ] ), animationRate: row.rate ?? 1 };
		advanceAction( c, 0 );
		for ( const frame of row.frames ) {
			const at = frame.elapsedMs / 1000;
			advanceAction(
				c,
				at,
				undefined,
				row.cancelAtMs !== undefined && at * 1000 > row.cancelAtMs ? row.cancelAtMs / 1000 : undefined
			);
			const layer = actionLayers( c, at )[0];
			if ( frame.weight < 1e-6 && !layer ) {
				assert.equal( layer, undefined );
				continue;
			}
			assert.ok( layer );
			assert.ok( Math.abs( layer.time * 1000 - frame.sampleMs ) < 1e-6, `cursor at ${frame.elapsedMs}` );
			assert.ok( Math.abs( layer.weight - frame.weight ) < 1e-6, `weight at ${frame.elapsedMs}` );
			if ( row.cancelAtMs === undefined ) {
				assert.equal( c.phase === 3, frame.mode === 5, `completion at ${frame.elapsedMs}` );
			}
		}
	}
});

test("new installations capture action speed without scaling the entry blend or retiming an old clip", () => {
	const c = { ...clock( [ phase( "ready" ), phase( "wait" ), phase( "shot" ) ] ), animationRate: .5 };
	advanceAction( c, 0 );
	assert.equal( actionLayers( c, .1 )[0].time, 0 );
	assert.equal( actionLayers( c, .1 )[0].weight, .5 );
	assert.equal( actionLayers( c, .6 )[0].time, .2 );
	c.animationRate = 1;
	assert.deepEqual( advanceAction( c, .603 ).events, [ { phase: "READY", event: 1, at: .6 } ] );
	assert.equal( actionLayers( c, .8 )[0].time, .3 );
	advanceAction( c, 1, 1 );
	assert.equal( actionLayers( c, 1.1 )[0].time, 0 );
	assert.deepEqual( advanceAction( c, 1.403, 1 ).events, [ { phase: "SHOT", event: 1, at: 1.4 } ] );
	assert.ok( Math.abs( actionLayers( c, 1.6 )[0].time - .4 ) < 1e-9 );
});

test("WAIT starts beside READY and release preserves the previous one-shot's natural lifetime", () => {
	const c = clock( [ phase( "ready" ), phase( "wait" ), phase( "shot" ) ] );
	advanceAction( c, 0 );
	const entering = actionLayers( c, .1 );
	assert.deepEqual( entering.map( row => [ row.clip, row.lane, row.time, row.weight ] ), [
		[ "ready", "event", 0, .5 ],
		[ "wait", "timed", .1, .5 ]
	] );
	advanceAction( c, .3, .3 );
	const release = actionLayers( c, .4 );
	assert.ok( Math.abs( release.find( row => row.clip === "wait" ).weight - .5 ) < 1e-9 );
	assert.equal( release.find( row => row.clip === "ready" ).weight, 1 );
	advanceAction( c, .6, .3 );
	assert.deepEqual( actionLayers( c, .6 ).map( row => row.clip ), [ "shot", "ready" ] );
	assert.ok( actionLayers( c, 1.3 ).find( row => row.clip === "ready" ).weight > 0 );
	assert.ok( !actionLayers( c, 1.401 ).some( row => row.clip === "ready" ) );
});

test("WAIT carries its captured rate through the renderer's cursor owner", () => {
	for ( const rate of [ .5, 1, 2 ] ) {
		const c = { ...clock( [ null, phase( "wait" ), null ] ), animationRate: rate };
		advanceAction( c, 0 );
		const dispatch = createAnimationDispatch();
		for ( let frame = 1; frame <= 10; frame++ ) {
			const layers = actionLayers( c, frame / 100 );
			const sampled = dispatch.step( layers, 10, () => 1000 );
			assert.equal( sampled[0].layer.time, frame * 10 * rate / 1000 );
		}
	}
});

test("replaying a bound motion replaces its old producer without resurrecting it or affecting another actor", () => {
	for ( const loop of [ true, false ] ) {
		const phases = loop ? [ null, phase( "same" ), null ] : [ null, null, phase( "same" ) ];
		const old = { ...clock( phases ), caster: 1 };
		const peer = { ...clock( phases ), caster: 2 };
		const next = { ...clock( phases ), caster: 1, started: .3 };
		advanceAction( old, 0 );
		advanceAction( peer, 0 );
		advanceAction( next, .3 );
		// Zero entry weight still replaces the prior installation.
		reconcileActionInstallations( [ old, peer, next ] );
		assert.equal( actionLayers( old, .3 ).length, 0 );
		assert.equal( actionLayers( peer, .3 ).length, 1 );
		assert.equal( actionLayers( next, .4 ).length, 1 );
		assert.deepEqual( advanceAction( old, .6 ).events, [] );
		advanceAction( next, .6, undefined, .6 );
		advanceAction( old, .9 );
		reconcileActionInstallations( [ next, old, peer ] );
		assert.equal( actionLayers( old, .9 ).length, 0 );
		assert.equal( actionLayers( next, 2 ).length, 0 );
		assert.equal( actionLayers( old, 2 ).length, 0 );
	}
});

test("reinstalling a motion removes its retained natural-exit layer", () => {
	const old = { ...clock( [ phase( "same" ), phase( "wait" ), phase( "shot" ) ] ), caster: 1 };
	advanceAction( old, 0 );
	advanceAction( old, .3, .3 );
	const next = { ...clock( [ null, null, phase( "same" ) ] ), caster: 1, started: .5 };
	advanceAction( next, .5 );
	// Caller iteration order does not substitute for installation time.
	for ( const rows of [ [ old, next ], [ next, old ] ] ) reconcileActionInstallations( rows );
	assert.ok( !actionLayers( old, .6 ).some( row => row.clip === "same" ) );
	assert.equal( actionLayers( next, .6 )[0].clip, "same" );
});
