/*
===========================================================================

most-wanted.test.mjs - the Most Wanted buff-board row (port-only 0x3FB2)

The worker decodes the server's wanted state whole or not at all, the
board row shows the level the state reports with its effects, and the
row exists only while its Experimental switch is on.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createMostWantedState, OP_MOST_WANTED_STATE } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/most-wanted/most-wanted.ts"
);
const { mostWantedRow } = await import( "../../src/engine/foundation/gameplay/most-wanted.ts" );
const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { experimentalOptions } = await import( "../../src/engine/foundation/ui/experimental-options.ts" );

/*
================
stateFrame

The server's body (wire.EncodeMostWantedState).
================
*/
function stateFrame( level, killsIntoLevel, job, damagePercent, killsPerLevel = 10 ) {
	return {
		opcode: OP_MOST_WANTED_STATE,
		payload: new Uint8Array( [ level, killsIntoLevel, killsPerLevel, job, damagePercent ] )
	};
}

test("the worker decodes a wanted state and drops a cleared one", () => {
	const owner = createMostWantedState();
	assert.equal( owner.receive( { opcode: 0x3fbc, payload: new Uint8Array( 5 ) } ), false );
	assert.equal( owner.receive( stateFrame( 4, 7, 2, 1 ) ), true );
	assert.deepEqual( owner.current(), { level: 4, killsIntoLevel: 7, killsPerLevel: 10, job: 2, damagePercent: 1 } );

	// Progress below the first level is real state but grants nothing yet.
	assert.equal( owner.receive( stateFrame( 0, 3, 2, 0 ) ), true );
	assert.equal( owner.current(), null );

	// The suit coming off clears the row even at a level.
	assert.equal( owner.receive( stateFrame( 6, 0, 1, 2 ) ), true );
	assert.equal( owner.receive( stateFrame( 0, 0, 0, 0 ) ), true );
	assert.equal( owner.current(), null );

	owner.receive( stateFrame( 1, 0, 3, 0 ) );
	owner.clear();
	assert.equal( owner.current(), null );
});

test("a malformed wanted state is a protocol error and keeps the last state", () => {
	const owner = createMostWantedState();
	owner.receive( stateFrame( 4, 7, 2, 1 ) );
	for (
		const bad of [
			new Uint8Array( [ 4, 7, 10, 2 ] ),
			new Uint8Array( [ 4, 7, 10, 2, 1, 0 ] ),
			new Uint8Array( [ 11, 0, 10, 2, 3 ] ),
			new Uint8Array( [ 4, 7, 10, 4, 1 ] ),
			new Uint8Array( [ 4, 11, 10, 2, 1 ] ),
			new Uint8Array( [ 4, 7, 0, 2, 1 ] ),
			new Uint8Array( [ 4, 7, 10, 2, 4 ] )
		]
	) {
		assert.throws( () => owner.receive( { opcode: OP_MOST_WANTED_STATE, payload: bad } ), /Invalid most wanted/ );
	}
	assert.equal( owner.current()?.level, 4 );
});

test("the gameplay worker publishes the state it decoded", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( {} );
	game.receive( stateFrame( 2, 5, 1, 0 ), 0 );
	assert.deepEqual( game.take()?.mostWanted, {
		level: 2,
		killsIntoLevel: 5,
		killsPerLevel: 10,
		job: 1,
		damagePercent: 0
	} );
	game.receive( stateFrame( 0, 0, 0, 0 ), 0 );
	assert.equal( game.take()?.mostWanted, null );
	game.dispose();
});

test("the row names its level, lists its effects and fills toward the next", () => {
	const row = mostWantedRow( { level: 4, killsIntoLevel: 7, killsPerLevel: 10, job: 2, damagePercent: 1 } );
	assert.equal( row?.label, "Most Wanted Lv 4" );
	assert.equal( row?.icon, "com_job_thief" );
	assert.equal( row?.fraction, 0.7 );
	assert.match( row?.helpText ?? "", /Strength \+4, Intellect \+4/ );
	assert.match( row?.helpText ?? "", /Movement speed \+4% on foot/ );
	assert.match( row?.helpText ?? "", /Damage dealt \+1%, damage taken -1%/ );
	assert.match( row?.helpText ?? "", /7\/10 counted kills to Lv 5/ );

	// Below level 3 the damage line is absent, as the server grants none.
	const low = mostWantedRow( { level: 2, killsIntoLevel: 0, killsPerLevel: 10, job: 1, damagePercent: 0 } );
	assert.equal( low?.icon, "com_job_merchant" );
	assert.doesNotMatch( low?.helpText ?? "", /Damage dealt/ );

	// At the cap the bar is full and there is no next level.
	const max = mostWantedRow( { level: 10, killsIntoLevel: 10, killsPerLevel: 10, job: 3, damagePercent: 3 } );
	assert.equal( max?.icon, "com_job_hunter" );
	assert.equal( max?.fraction, 1 );
	assert.match( max?.helpText ?? "", /Highest wanted level/ );
	assert.match( max?.helpText ?? "", /Damage dealt \+3%, damage taken -3%/ );

	assert.equal( mostWantedRow( null ), null );
	assert.equal( mostWantedRow( { level: 0, killsIntoLevel: 3, killsPerLevel: 10, job: 2, damagePercent: 0 } ), null );
	assert.equal( mostWantedRow( { level: 5, killsIntoLevel: 0, killsPerLevel: 10, job: 0, damagePercent: 2 } ), null );
});

test("the board draws the row only while the Experimental switch is on", () => {
	const game = {
		localGid: 7,
		vitals: [],
		mostWanted: { level: 4, killsIntoLevel: 7, killsPerLevel: 10, job: 2, damagePercent: 1 }
	};
	assert.equal( buffBoard( game, 0 ).some( row => row.id === "most-wanted" ), false );
	const rows = buffBoard( game, 0, new Set(), undefined, true, true );
	const wanted = rows.find( row => row.id === "most-wanted" );
	assert.equal( wanted?.path, "/assets/images/Media_extracted/interface/ifcommon/com_job_thief.png" );
	assert.equal( wanted?.label, "Most Wanted Lv 4" );
	assert.equal( wanted?.fraction, 0.7 );
	assert.match( wanted?.helpText ?? "", /7\/10 counted kills to Lv 5/ );
	// No state, no row, even switched on.
	assert.equal(
		buffBoard( { ...game, mostWanted: null }, 0, new Set(), undefined, true, true )
			.some( row => row.id === "most-wanted" ),
		false
	);
});

test("the Experimental row is off unless explicitly turned on", () => {
	assert.equal( experimentalOptions().mostWantedBuff, false );
	assert.equal( experimentalOptions( { mostWantedBuff: 1 } ).mostWantedBuff, false );
	assert.equal( experimentalOptions( { mostWantedBuff: true } ).mostWantedBuff, true );
});
