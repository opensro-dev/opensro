/*
===========================================================================

most-wanted.test.mjs - the most-wanted buff row (port-only 0x3FBA, #1849)

The worker decodes the state frame whole or not at all, gameplay
publishes it, and the buff-board row follows the shard state alone
(the server's SRO_MOST_WANTED flag is the only switch, #2721). The
row's decay countdown is a duration counted from receipt on the frame
clock: the server owns the decay, so no client wall clock - skewed
either way - can age the row.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import { test } from "node:test";
const { createMostWanted, OP_MOST_WANTED_STATE } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/most-wanted/most-wanted.ts"
);
const { mostWantedRow, mostWantedDamagePercent, mostWantedDecayText, mostWantedJobIcon } = await import(
	"../../src/engine/foundation/gameplay/most-wanted.ts"
);
const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const DAY_MS = 86_400_000;

/*
================
wantedFrame

The server's body (action/mostwantedstate.go): level, kills into the
level, dressed job, kills per level, bounty gold, and the ms until
the next decay step (0 = none stamped).
================
*/
function wantedFrame( { level = 2, kills = 3, job = 2, per = 5, bounty = 200_000_000, decay = 0 } = {} ) {
	const payload = new Uint8Array( 20 ), view = new DataView( payload.buffer );
	payload[0] = level;
	payload[1] = kills;
	payload[2] = job;
	payload[3] = per;
	view.setBigUint64( 4, BigInt( bounty ), true );
	view.setBigUint64( 12, BigInt( decay ), true );
	return { opcode: OP_MOST_WANTED_STATE, payload };
}

test("the worker decodes a state whole, and only its own opcode", () => {
	const owner = createMostWanted();
	assert.equal( owner.receive( { opcode: 0x3fb7, payload: new Uint8Array( 20 ) }, 0 ), false );
	assert.equal( owner.receive( wantedFrame( { decay: 123_000 } ), 1000 ), true );
	assert.deepEqual(
		owner.current(),
		{
			level: 2,
			killsIntoLevel: 3,
			dressedJob: 2,
			killsPerLevel: 5,
			bounty: 200_000_000,
			decayInMs: 123_000,
			receivedAtMs: 1000
		}
	);

	const good = wantedFrame();
	for (
		const bad of [
			{ ...good, payload: good.payload.subarray( 0, 19 ) },
			wantedFrame( { level: 11 } ),
			wantedFrame( { job: 4 } ),
			wantedFrame( { per: 0 } ),
			wantedFrame( { kills: 6, per: 5 } )
		]
	) {
		assert.throws( () => owner.receive( bad, 2000 ), /Invalid/ );
	}
	assert.equal( owner.current()?.level, 2, "a refused state never replaced the last good one" );
	assert.equal( owner.current()?.receivedAtMs, 1000, "a refused state never restamped the receipt" );

	owner.receive( wantedFrame( { level: 0, kills: 0, job: 0 } ), 3000 );
	assert.equal( owner.current()?.level, 0, "the cleared state is published like any other" );
	owner.clear();
	assert.equal( owner.current(), undefined );
});

test("gameplay publishes the wanted state and drops it on session clear", () => {
	const game = createGameplay( () => {} );
	game.bootstrap( {} );
	game.receive( wantedFrame( { level: 3, kills: 1, job: 3 } ), 0 );
	assert.deepEqual(
		game.take()?.mostWanted,
		{
			level: 3,
			killsIntoLevel: 1,
			dressedJob: 3,
			killsPerLevel: 5,
			bounty: 200_000_000,
			decayInMs: 0,
			receivedAtMs: 0
		}
	);
	game.dispose();
});

test("the row shows the level, its effects, the bounty and the kills into the next", () => {
	const state = {
		level: 3,
		killsIntoLevel: 2,
		dressedJob: 2,
		killsPerLevel: 5,
		bounty: 300_000_000,
		decayInMs: 0,
		receivedAtMs: 0
	};
	const row = mostWantedRow( state, 1000 );
	assert.equal( row?.label, "MOST WANTED Lv 3" );
	assert.equal( row?.fraction, 2 / 5 );
	assert.equal( row?.path, "/assets/images/Media_extracted/interface/ifcommon/com_job_thief.png" );
	assert.match( row?.helpText ?? "", /MOST WANTED Lv 3/ );
	assert.match( row?.helpText ?? "", /Strength \+3/ );
	assert.match( row?.helpText ?? "", /Damage dealt \+1%, damage taken -1%/ );
	assert.match( row?.helpText ?? "", /Next level: 2\/5 kills/ );
	assert.match( row?.helpText ?? "", /Bounty: 300,000,000 gold/ );

	assert.deepEqual(
		[ 0, 2, 3, 5, 9, 10 ].map( mostWantedDamagePercent ),
		[ 0, 0, 1, 2, 2, 3 ],
		"the damage table mirrors the server's"
	);
	assert.equal( mostWantedJobIcon( 1 ), "/assets/images/Media_extracted/interface/ifcommon/com_job_merchant.png" );
	assert.equal( mostWantedJobIcon( 3 ), "/assets/images/Media_extracted/interface/ifcommon/com_job_hunter.png" );
	assert.equal( mostWantedJobIcon( 0 ), null );
});

test("the row waits for a level and a suit, and never ages out on its own clock", () => {
	const base = {
		level: 2,
		killsIntoLevel: 0,
		dressedJob: 1,
		killsPerLevel: 5,
		bounty: 200_000_000,
		decayInMs: DAY_MS,
		receivedAtMs: 10_000
	};
	assert.equal( mostWantedRow( undefined, 0 ), null );
	assert.equal( mostWantedRow( { ...base, level: 0 }, 0 ), null );
	assert.equal( mostWantedRow( { ...base, dressedJob: 0 }, 0 ), null, "the suit off shelves the row" );

	// The countdown runs on the frame clock from receipt; at and past
	// the step the row still draws (the server sends the state that
	// drops the level), its text bottomed out.
	assert.match( mostWantedRow( base, 10_000 )?.helpText ?? "", /Next decay in 1 day/ );
	assert.match( mostWantedRow( base, 10_000 + 60_000 )?.helpText ?? "", /Next decay in 23 h 59 min/ );
	assert.match( mostWantedRow( base, 10_000 + DAY_MS )?.helpText ?? "", /Next decay in 1 min/ );
	assert.equal(
		mostWantedRow( base, 10_000 + 30 * DAY_MS )?.label,
		"MOST WANTED Lv 2",
		"long past the step the row waits for the server's next state"
	);
	assert.equal( mostWantedDecayText( 3 * DAY_MS ), "Next decay in 3 days" );

	const capped = mostWantedRow( { ...base, level: 10, killsIntoLevel: 5 }, 0 );
	assert.equal( capped?.fraction, 1 );
	assert.match( capped?.helpText ?? "", /Maximum level/ );
});

test("the countdown ignores the wall clock, skewed either way", () => {
	const state = {
		level: 2,
		killsIntoLevel: 1,
		dressedJob: 2,
		killsPerLevel: 5,
		bounty: 200_000_000,
		decayInMs: DAY_MS,
		receivedAtMs: 50_000
	};
	const at = 50_000 + 12 * 3_600_000;
	const honest = mostWantedRow( state, at );
	const realNow = Date.now;
	try {
		Date.now = () => realNow() + 12 * 3_600_000;
		assert.deepEqual( mostWantedRow( state, at ), honest, "wall clock 12 h fast" );
		Date.now = () => realNow() - 12 * 3_600_000;
		assert.deepEqual( mostWantedRow( state, at ), honest, "wall clock 12 h slow" );
	} finally {
		Date.now = realNow;
	}
	assert.match( honest?.helpText ?? "", /Next decay in 12 h 0 min/ );
});

test("the board draws the row from the shard state alone", () => {
	const state = {
		level: 2,
		killsIntoLevel: 1,
		dressedJob: 2,
		killsPerLevel: 5,
		bounty: 200_000_000,
		decayInMs: 0,
		receivedAtMs: 0
	};
	const game = /** @type {import("../../src/engine/contracts/gameplay.ts").GameplayState} */ ({
		localGid: 1,
		mostWanted: state
	});
	const rows = buffBoard( game, 0 );
	const row = rows.find( entry => entry.id === "most-wanted" );
	assert.equal( row?.label, "MOST WANTED Lv 2" );
	assert.equal( row?.fraction, 1 / 5 );
	const quiet = /** @type {import("../../src/engine/contracts/gameplay.ts").GameplayState} */ ({
		localGid: 1
	});
	assert.equal(
		buffBoard( quiet, 0 ).some( entry => entry.id === "most-wanted" ),
		false,
		"no shard state, no row: the server flag is the only switch"
	);
});
