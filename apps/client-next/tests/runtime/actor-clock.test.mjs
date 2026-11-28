/*
===========================================================================

actor-clock.test.mjs - a published actor's clocks come from its entry when it has one

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { actorClock, carryClock, clocked, republished } = await import(
	"../../src/engine/foundation/animation/actor-clock.ts"
);
const { actorQueryRows } = await import( "../../src/engine/foundation/animation/actor-query-rows.ts" );
const { createAuxiliaryPresentation } = await import(
	"../../src/engine/runtime/characters/presentation-auxiliary.ts"
);

test("an actor's entry wins over its own clocks, and an actor with none carries its own", () => {
	const actor = Object.freeze( { gid: 1, time: 1, layers: [], modelAnimation: undefined } );
	const entry = { time: 2, layers: undefined, modelAnimation: undefined };
	assert.equal( actorClock( actor, new Map( [ [ actor, entry ] ] ) ), entry );
	assert.equal( actorClock( actor, new Map() ), actor );
	assert.equal( actorClock( actor, undefined ), actor );
});

test("a copy keeps its actor's entry, frozen, with its changes; an actor with none gives none", () => {
	const actor = Object.freeze( { gid: 1, time: 1, opacity: undefined } ), entry = { time: 4 };
	const clocks = new Map( [ [ actor, entry ] ] );
	const copy = republished( actor, { opacity: .5 }, clocks );
	assert.notEqual( copy, actor );
	assert.ok( Object.isFrozen( copy ) );
	assert.equal( copy.opacity, .5 );
	assert.equal( clocks.get( copy ), entry );
	const bare = Object.freeze( { gid: 2, time: 1 } );
	assert.equal( clocks.has( republished( bare, { opacity: 0 }, clocks ) ), false );
	const later = new Map();
	carryClock( actor, clocks, later );
	assert.equal( later.get( actor ), entry, "a held actor brings its entry into the later table" );
	carryClock( bare, clocks, later );
	assert.equal( later.has( bare ), false );
	assert.deepEqual( clocked( actor, clocks ), { gid: 1, time: 4, opacity: undefined } );
	assert.equal( clocked( bare, clocks ), bare );
});

test("a query resolves the rows it walks, the mount included, and none other", () => {
	const mount = Object.freeze( { gid: 2, time: 1 } ), rider = Object.freeze( { gid: 1, time: 1, mountedOn: 2 } );
	const other = Object.freeze( { gid: 3, time: 1 } );
	const displayed = new Map( [ [ 1, rider ], [ 2, mount ], [ 3, other ] ] );
	const clocks = new Map( [ [ rider, { time: 5 } ], [ mount, { time: 6 } ], [ other, { time: 7 } ] ] );
	const rows = actorQueryRows( displayed, [ rider ], clocks );
	assert.deepEqual( rows.map( row => [ row.gid, row.time ] ), [ [ 1, 5 ], [ 2, 6 ] ] );
	assert.equal( actorQueryRows( displayed, [ other ] )[0], other, "with no table a row is the actor itself" );
});

test("a saddle ride takes its rider's clocks from the entry, and the rider's copy keeps the entry", () => {
	const auxiliary = createAuxiliaryPresentation( () => -9 );
	const entity = { gid: 2, kind: "monster", appearanceState: [ 1, 0, 0 ], moving: true };
	const owner = Object.freeze( {
		gid: 2,
		model: "/assets/rider.glb",
		pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 },
		clip: "run",
		time: 0,
		loop: true,
		scale: 1
	} );
	const entry = { time: 4, layers: [ { clip: "run", time: 4, loop: true, weight: 1, lane: "timed" } ] };
	const clocks = new Map( [ [ owner, entry ] ] ), next = new Map( [ [ owner.gid, owner ] ] );
	auxiliary.step(
		/** @type {any} */ ({
			entities: [ entity ],
			seconds: 1,
			next,
			clocks,
			gameplay: null,
			localMover: () => false,
			castByActor: new Map(),
			actionLayersByActor: new Map(),
			particleHolders: []
		}),
		/** @type {any} */ ({
			resourceFor: () => ({ codename: "MOB_RIDER" }),
			dress: {},
			resources: { ready: () => true },
			ridesByRider: new Map( [ [ "MOB_RIDER", { glb: "/assets/ride.glb", clips: [ "stand", "run" ] } ] ] ),
			riderModes: new Map(),
			committedAuxiliary: new Map()
		})
	);
	const ride = [ ...next.values() ].find( actor => actor.pickOwner === entity.gid && actor.gid !== entity.gid );
	assert.ok( ride, "the ride is presented" );
	assert.equal( ride.time, 4 );
	assert.equal( ride.layers[0].time, 4 );
	const rider = next.get( entity.gid );
	assert.equal( rider.mountedOn, ride.gid );
	assert.equal( clocks.get( rider ), entry, "the seated rider is a copy that keeps its clocks" );
});
