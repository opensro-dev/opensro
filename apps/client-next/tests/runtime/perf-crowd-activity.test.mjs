/*
===========================================================================

perf-crowd-activity.test.mjs - a living crowd's shares and seeded scripts

The two places a reader would not see a fault: role counts that do not
sum to the crowd, and a script that changes with timer interleaving
instead of only with the seed.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { apportionRoles, assignRoles, parseActivity, peerScript } from "../../tools/perf/core/crowd-activity.mjs";

const SPOT = Object.freeze( { regionId: 25000, x: 1404, y: -6, z: 1409 } );

/*
================
draw

n steps of each peer's script, drawn in the given peer order per round.
================
*/
function draw( seed, peers, rounds, order ) {
	const scripts = peers.map( index => peerScript( { role: index % 2 ? "chat" : "walk", index, seed, spot: SPOT } ) );
	const steps = peers.map( () => [] );
	for ( let round = 0; round < rounds; round++ ) {
		for ( const index of order( round ) ) steps[index].push( scripts[index]() );
	}
	return steps;
}

test("role counts always sum to the crowd and stay within one of each share", () => {
	assert.deepEqual( apportionRoles( 3, parseActivity( "walk=0.5,chat=0.5" ) ), { walk: 2, chat: 1, still: 0 } );
	for ( let count = 1; count <= 40; count++ ) {
		for ( const act of [ "walk=0.5,chat=0.2", "walk=0.7,chat=0.3", "walk=0.1", "chat=0.33,walk=0.33", "still" ] ) {
			const shares = parseActivity( act ), counts = apportionRoles( count, shares );
			assert.equal( counts.walk + counts.chat + counts.still, count, `${act} x ${count}` );
			assert.ok( Math.abs( counts.walk - shares.walk * count ) < 1, `${act} x ${count} walk` );
			assert.ok( Math.abs( counts.chat - shares.chat * count ) < 1, `${act} x ${count} chat` );
			const roles = assignRoles( count, shares, 7 );
			assert.equal( roles.filter( role => role === "walk" ).length, counts.walk );
		}
	}
	assert.throws( () => parseActivity( "walk=0.8,chat=0.3" ), /sum past 1/ );
	assert.throws( () => parseActivity( "fight=0.1" ), /bad --act part/ );
});

test("each peer's script depends on the seed, not on the order peers draw in", () => {
	const peers = [ 0, 1, 2, 3 ];
	const forward = draw( 42, peers, 12, () => peers );
	const shuffled = draw( 42, peers, 12, round => round % 2 ? [ 3, 1, 0, 2 ] : [ 2, 0, 3, 1 ] );
	assert.deepEqual( shuffled, forward );
	assert.notDeepEqual( draw( 43, peers, 12, () => peers ), forward );
	assert.deepEqual(
		assignRoles( 32, parseActivity( "walk=0.5,chat=0.2" ), 42 ),
		assignRoles( 32, parseActivity( "chat=0.2,walk=0.5" ), 42 )
	);
	for ( const step of forward[0] ) {
		assert.equal( step.kind, "walk" );
		assert.ok( Math.hypot( step.to.x - SPOT.x, step.to.z - SPOT.z ) <= 25 && step.delayMs <= 6000 );
	}
});

test("a peer acknowledges its stream position at the client's cadence", async () => {
	const { createStreamAck } = await import( "../../tools/perf/core/crowd.mjs" );
	let clock = 0;
	const sent = [], stream = createStreamAck( position => sent.push( position ), () => clock );
	// The first applied frame is told at once; the next two wait out the second.
	stream.counted( 100 );
	stream.counted( 100 );
	stream.counted( 100 );
	assert.deepEqual( sent, [ 1 ] );
	clock = 999;
	stream.flush();
	assert.deepEqual( sent, [ 1 ] );
	clock = 1000;
	stream.flush();
	stream.flush();
	assert.deepEqual( sent, [ 1, 3 ], "told once the interval passed, and not again while nothing moved" );
	// A frame of a megabyte does not wait: the server's writer is parked behind it.
	clock = 1100;
	stream.counted( 1 << 20 );
	assert.deepEqual( sent, [ 1, 3, 4 ] );
});

test("a walker beside its region's edge never clicks outside the region", () => {
	const corner = { regionId: 25000, x: 3, y: 0, z: 1917 };
	const next = peerScript( { role: "walk", index: 0, seed: 5, spot: corner } );
	let onEdge = 0;
	for ( let n = 0; n < 200; n++ ) {
		const { to } = next();
		assert.ok( to.x >= 0 && to.x < 1920 && to.z >= 0 && to.z < 1920, `${to.x}, ${to.z}` );
		if ( to.x === 0 || to.z === 1919 ) onEdge++;
	}
	assert.ok( onEdge > 0, "some draws reached past the edge and were held on it" );
});
