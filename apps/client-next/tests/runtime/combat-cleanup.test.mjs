/*
===========================================================================

combat-cleanup.test.mjs - authoritative residue and bounded benchmark cleanup

Use deterministic clocks and injected I/O so failures cannot wait three minutes
or attack anything outside the scene that the benchmark actually recorded.

===========================================================================
*/
import test from "node:test";
import assert from "node:assert/strict";
import { combatResidue, cleanupCombat } from "../../tools/perf/core/combat-cleanup.mjs";

/*
================
population
================
*/
function population( entries, truncated = false ) {
	return { monsters: new Map( entries.map( ( [gid, hp] ) => [ gid, { hp } ] ) ), truncated };
}

test("authoritative positive HP is alive, missing truncated rows are unknown, and duplicate GIDs cannot fill a request", () => {
	assert.deepEqual(
		combatResidue( { count: 4, gids: [ 1, 2, 3, 3 ] }, population( [ [ 1, 50 ], [ 2, 0 ] ], true ) ),
		{
			alive: [ 1 ],
			dead: [ 2 ],
			unknown: [ 3 ],
			unaccounted: 1
		}
	);
	assert.deepEqual( combatResidue( { count: 3, gids: [ 1, 2, 3 ] }, population( [ [ 1, NaN ], [ 2, 0 ] ] ) ), {
		alive: [],
		dead: [ 2 ],
		unknown: [ 1 ],
		unaccounted: 0
	} );
});

/*
================
fixture
================
*/
function fixture( populations, alive = true ) {
	let clock = 0, reads = 0;
	const attacks = [];
	const options = {
		scene: { count: 2, gids: [ 1, 2 ] },
		now: () => clock,
		limitMs: 2100,
		/*
		================
		read
		================
		*/
		read() {
			return populations[Math.min( reads++, populations.length - 1 )];
		},
		/*
		================
		attack
		================
		*/
		attack( gids, turn ) {
			attacks.push( { gids, turn } );
		},
		isAlive: () => alive,
		/*
		================
		pause
		================
		*/
		pause( ms ) {
			clock += ms;
		}
	};
	return { options, attacks, reads: () => reads };
}

test("cleanup attacks only its own living GIDs and requires a later authoritative empty result", async () => {
	const f = fixture( [
		population( [ [ 1, 50 ], [ 2, 70 ], [ 99, 100 ] ] ),
		population( [ [ 1, 0 ], [ 2, 30 ], [ 99, 100 ] ] ),
		population( [ [ 2, 0 ], [ 99, 100 ] ] )
	] );
	const result = await cleanupCombat( f.options );
	assert.equal( result.status, "clean" );
	assert.equal( result.rounds, 2 );
	assert.equal( f.reads(), 3 );
	assert.deepEqual( f.attacks, [ { gids: [ 1, 2 ], turn: 0 }, { gids: [ 2 ], turn: 1 } ] );
});

test("unkillable monsters hit the time bound and remain residue", async () => {
	const f = fixture( [ population( [ [ 1, 50 ], [ 2, 70 ] ] ) ] );
	const result = await cleanupCombat( f.options );
	assert.equal( result.status, "timeout" );
	assert.equal( result.elapsedMs, 2100 );
	assert.equal( result.rounds, 3 );
	assert.deepEqual( result.alive, [ 1, 2 ] );
});

test("death, partial loads and truncated absence fail without reviving or attacking unrelated monsters", async () => {
	const dead = fixture( [ population( [ [ 1, 50 ], [ 2, 70 ] ] ) ], false );
	assert.equal( (await cleanupCombat( dead.options )).status, "character-dead" );
	assert.deepEqual( dead.attacks, [] );
	const partial = fixture( [ population( [] ) ] );
	partial.options.scene.gids = [ 1 ];
	assert.equal( (await cleanupCombat( partial.options )).unaccounted, 1 );
	assert.deepEqual( partial.attacks, [] );
	const truncated = fixture( [ population( [], true ) ] );
	assert.deepEqual( (await cleanupCombat( truncated.options )).unknown, [ 1, 2 ] );
	assert.deepEqual( truncated.attacks, [] );
});

test("a failed server read is not converted into successful cleanup", async () => {
	const f = fixture( [ population( [] ) ] );
	f.options.read = () => {
		throw Error( "observatory unavailable" );
	};
	await assert.rejects( cleanupCombat( f.options ), /observatory unavailable/ );
	assert.deepEqual( f.attacks, [] );
});

test("confirmed server deaths survive corpse removal, but later positive HP revokes that proof", async () => {
	const f = fixture( [
		population( [ [ 1, 0 ], [ 2, 70 ] ], true ),
		population( [ [ 2, 0 ] ], true )
	] );
	const result = await cleanupCombat( f.options );
	assert.equal( result.status, "clean" );
	assert.deepEqual( result.dead, [ 1, 2 ] );
	const revived = fixture( [
		population( [ [ 1, 0 ], [ 2, 70 ] ], true ),
		population( [ [ 1, 50 ], [ 2, 0 ] ], true )
	] );
	const unresolved = await cleanupCombat( revived.options );
	assert.equal( unresolved.status, "timeout" );
	assert.deepEqual( unresolved.alive, [ 1 ] );
	assert.deepEqual( unresolved.dead, [ 2 ] );
});

test("a transient truncated observation is retried within the bound without speculative attacks", async () => {
	const f = fixture( [
		population( [], true ),
		population( [ [ 1, 0 ], [ 2, 70 ] ] ),
		population( [ [ 2, 0 ] ] )
	] );
	assert.equal( (await cleanupCombat( f.options )).status, "clean" );
	assert.deepEqual( f.attacks, [ { gids: [ 2 ], turn: 0 } ] );
});
