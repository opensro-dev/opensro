/*
===========================================================================

arrow-keys.test.mjs - tests for arrow-keys.ts and its gameplay wiring:
walking and turning with the arrow keys (GH #1574)

Up walks the facing (0x877F30, no resend while a direction walk is under
way) and its release stops with 0x72F5 [u16 heading]; Left and Right turn
at pi radians a second (0x86D480), sending 0x72CF every eighth of a turn
and once on release when a degree or more off (0x877540). A rider sends
the vehicle's 0x769E tags 3 and 4 instead.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { product } from "../helpers/navigation-fixture.mjs";
const { createArrowKeys, OP_STEER, OP_DIRECTION_STOP, OP_COS_COMMAND } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/arrow-keys/arrow-keys.ts"
);
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);

const REGION = 257;
const POSE = { regionId: REGION, x: 100, y: 0, z: 100, angle: 1000 };
// Half a turn a second, in heading words.
const WORDS_PER_MS = 65535 / 2 / 1000;

/*
================
fakeMover

A mover that records what the arrow keys asked of it.
================
*/
/**
 * @param {{ walking?: boolean, vehicle?: number }} [options]
 */
function fakeMover( { walking = false, vehicle = undefined } = {} ) {
	/** @type {{ direct: number[], steer: number[], stop: number, cancel: number }} */
	const calls = { direct: [], steer: [], stop: 0, cancel: 0 };
	let pose = { ...POSE };
	return {
		calls,
		mover: {
			pose: () => pose,
			walkingDirection: () => walking,
			direct: ( heading, now ) => {
				calls.direct.push( heading );
				walking = true;
				return { opcode: 9, payload: Uint8Array.of( heading & 255 ) };
			},
			steer: heading => {
				calls.steer.push( heading );
				pose = { ...pose, angle: heading };
			},
			stop: () => {
				calls.stop++;
				walking = false;
			},
			vehicle: () => vehicle,
			cancelCast: () => {
				calls.cancel++;
				return true;
			}
		}
	};
}

/*
================
heading16

The u16 heading a frame carries at offset.
================
*/
function heading16( frame, offset = 0 ) {
	return new DataView( frame.payload.buffer, frame.payload.byteOffset ).getUint16( offset, true );
}

test("Up walks the facing once, and its release stops with 0x72F5 at the heading", () => {
	const sent = [], keys = createArrowKeys( f => sent.push( f ) ), { mover, calls } = fakeMover();
	assert.ok( keys.key( "up", true, 0, mover, "open" ) );
	assert.deepEqual( calls.direct, [ POSE.angle ] );
	// A second press while held changes nothing.
	assert.equal( keys.key( "up", true, 10, mover, "open" ), null );
	assert.deepEqual( calls.direct, [ POSE.angle ] );
	const stop = keys.key( "up", false, 500, mover, "open" );
	assert.equal( stop?.opcode, OP_DIRECTION_STOP );
	assert.equal( heading16( stop ), POSE.angle );
	assert.deepEqual( sent.map( f => f.opcode ), [ OP_DIRECTION_STOP ] );
	assert.equal( calls.stop, 1 );
});

test("Up does not resend while a direction walk is already under way (0x877F30 bFlag 0)", () => {
	const keys = createArrowKeys( () => {} ), { mover, calls } = fakeMover( { walking: true } );
	assert.equal( keys.key( "up", true, 0, mover, "open" ), null );
	assert.deepEqual( calls.direct, [] );
});

test("a refused press latches nothing, so its release sends nothing", () => {
	const sent = [], keys = createArrowKeys( f => sent.push( f ) ), { mover, calls } = fakeMover();
	assert.equal( keys.key( "up", true, 0, mover, "closed" ), null );
	assert.equal( keys.key( "up", false, 100, mover, "open" ), null );
	assert.equal( keys.key( "left", true, 0, mover, "closed" ), null );
	keys.step( 400, mover, true );
	assert.equal( keys.key( "left", false, 500, mover, "open" ), null );
	assert.deepEqual( sent, [] );
	assert.deepEqual( calls, { direct: [], steer: [], stop: 0, cancel: 0 } );
});

test("Left turns at half a turn a second and steers the server every eighth of a turn", () => {
	const sent = [], keys = createArrowKeys( f => sent.push( f ) ), { mover, calls } = fakeMover();
	keys.key( "left", true, 0, mover, "open" );
	for ( let now = 16; now <= 512; now += 16 ) keys.step( now, mover, true );
	// 512 ms at pi rad/s: a quarter turn and a bit, from heading 1000.
	const expected = Math.round( POSE.angle + 512 * WORDS_PER_MS );
	assert.equal( calls.steer.at( -1 ), expected );
	// Two eighths were crossed: two steers, each at least an eighth past the last.
	assert.equal( sent.length, 2 );
	assert.ok( sent.every( f => f.opcode === OP_STEER ) );
	assert.ok( heading16( sent[0] ) - POSE.angle >= 65535 / 8 - 1 );
	assert.ok( heading16( sent[1] ) - heading16( sent[0] ) >= 65535 / 8 - 1 );
});

test("Right turns the other way and wraps below zero; Left wins when both are held", () => {
	const keys = createArrowKeys( () => {} ), { mover, calls } = fakeMover();
	keys.key( "right", true, 0, mover, "open" );
	keys.step( 100, mover, true );
	assert.equal( calls.steer.at( -1 ), Math.round( 65535 + POSE.angle - 100 * WORDS_PER_MS ) % 65535 );
	keys.key( "left", true, 100, mover, "open" );
	keys.step( 200, mover, true );
	// Left wins over Right (0x86D480 tests 0x10 first): 100 ms back the
	// other way returns to the start, across the wrap.
	assert.equal( calls.steer.at( -1 ), POSE.angle );
});

test("a release sends the heading once if it is a degree or more off, and not under a degree", () => {
	const sent = [], keys = createArrowKeys( f => sent.push( f ) ), { mover } = fakeMover();
	keys.key( "left", true, 0, mover, "open" );
	// 20 ms: about 655 words, over a degree (182) and under an eighth.
	keys.step( 20, mover, true );
	keys.key( "left", false, 20, mover, "open" );
	assert.deepEqual( sent.map( f => f.opcode ), [ OP_STEER ] );
	sent.length = 0;
	keys.key( "right", true, 100, mover, "open" );
	// 4 ms: about 131 words, under a degree.
	keys.key( "right", false, 104, mover, "open" );
	assert.deepEqual( sent, [] );
});

test("a rider steers and stops the vehicle with 0x769E tags 4 and 3", () => {
	const sent = [], keys = createArrowKeys( f => sent.push( f ) ), { mover } = fakeMover( { vehicle: 0x01020304 } );
	keys.key( "up", true, 0, mover, "open" );
	keys.key( "left", true, 0, mover, "open" );
	keys.step( 300, mover, true );
	keys.key( "left", false, 300, mover, "open" );
	keys.key( "up", false, 300, mover, "open" );
	assert.ok( sent.length >= 2 );
	for ( const frame of sent ) {
		assert.equal( frame.opcode, OP_COS_COMMAND );
		assert.equal( new DataView( frame.payload.buffer ).getUint32( 0, true ), 0x01020304 );
	}
	assert.equal( sent.at( -2 )?.payload[4], 4, "steer tag" );
	assert.equal( sent.at( -1 )?.payload[4], 3, "stop tag" );
	assert.equal( heading16( sent.at( -1 ), 5 ), heading16( sent.at( -2 ), 5 ) );
});

test("through gameplay, Up walks the local player's facing and its release stops the walk with 0x72F5", () => {
	const sent = [], game = createGameplay( f => sent.push( f ) );
	const local = { ...POSE, gid: 7, refObjId: 1907, kind: "local-player", name: "walker", heading: POSE.angle };
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	const bundle = product( REGION );
	bundle.objects = [];
	game.command( { kind: "navigation", regionId: REGION, bundle }, 0, undefined, local );
	sent.length = 0;
	game.command( { kind: "arrow-key", key: "up", down: true }, 0, undefined, local );
	assert.equal( sent.length, 1, "the direction walk goes out" );
	for ( let now = 16; now <= 1000; now += 16 ) game.step( now, local );
	const walked = game.take()?.pose;
	assert.ok( walked && Math.hypot( walked.x - POSE.x, walked.z - POSE.z ) > 10, "walked the facing" );
	game.command( { kind: "arrow-key", key: "up", down: false }, 1000, undefined, local );
	assert.equal( sent.at( -1 )?.opcode, OP_DIRECTION_STOP );
	for ( let now = 1016; now <= 2000; now += 16 ) game.step( now, local );
	const stopped = game.take()?.pose ?? walked;
	assert.ok( stopped && Math.hypot( stopped.x - walked.x, stopped.z - walked.z ) < 1, "stopped where released" );
	game.dispose();
});

test("through gameplay, turning while Up is held curves the walk onto the new heading", () => {
	const sent = [], game = createGameplay( f => sent.push( f ) );
	const local = { ...POSE, angle: 0, gid: 7, refObjId: 1907, kind: "local-player", name: "walker", heading: 0 };
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	const bundle = product( REGION );
	bundle.objects = [];
	game.command( { kind: "navigation", regionId: REGION, bundle }, 0, undefined, local );
	game.command( { kind: "arrow-key", key: "up", down: true }, 0, undefined, local );
	for ( let now = 16; now <= 400; now += 16 ) game.step( now, local );
	const straight = game.take()?.pose;
	game.command( { kind: "arrow-key", key: "left", down: true }, 400, undefined, local );
	for ( let now = 416; now <= 900; now += 16 ) game.step( now, local );
	const turned = game.take()?.pose;
	assert.ok( straight && turned );
	// Heading 0 walks +x; a quarter turn later the walk has gained z.
	assert.ok( Math.abs( turned.z - straight.z ) > 3, `curved by ${Math.abs( turned.z - straight.z )}` );
	assert.ok( turned.angle > 65535 / 8, "the facing turned" );
	assert.ok( sent.some( f => f.opcode === OP_STEER ), "the server was steered" );
	game.dispose();
});

test("the main-thread mover starts the Up walk on the press frame and ends it on the release", async () => {
	const { createLocalMover } = await import( "../../src/engine/runtime/movement/local-mover.ts" );
	const mover = createLocalMover();
	const view = {
		available: () => true,
		block: () => 0,
		floor: () => undefined,
		pick: () => null,
		clip: ( _from, to, query ) => {
			if ( query ) query.status = 0;
			return to;
		},
		surface: pose => pose
	};
	const world = /** @type {any} */ ({
		pose: { ...POSE, angle: 0 },
		poseAtMs: 0,
		revision: 0,
		moving: false,
		aim: undefined,
		speedFactor: 1,
		walkPredictable: true,
		speed: 50,
		inputSeq: 0,
		navigationRequestId: 1,
		navigationOwner: undefined,
		originMs: 0,
		view: { requestId: 1, navigation: view }
	});
	assert.equal( mover.arrow( 1, true, { ...POSE, angle: 0 }, 0, world ), true );
	const walking = mover.frame( 200, world );
	assert.equal( walking.kind, "walking" );
	assert.ok( walking.kind === "walking" && walking.pose.x > POSE.x + 5, "walks the facing (+x) at once" );
	assert.equal( mover.arrow( 2, false, walking.pose, 200, world ), true );
	const after = mover.frame( 600, world );
	assert.ok( after.kind !== "idle" && Math.abs( after.pose.x - walking.pose.x ) < .5, "stops where released" );
});

test("through gameplay, a press the walk gate refuses sends nothing, and its release sends no stop", () => {
	for ( const refused of [ { movementMode: 4 }, { appearanceState: [ 2, 0, 0 ] } ] ) {
		const sent = [], game = createGameplay( f => sent.push( f ) );
		const local = {
			...POSE,
			gid: 7,
			refObjId: 1907,
			kind: "local-player",
			name: "walker",
			heading: POSE.angle,
			...refused
		};
		game.bootstrap( { simulationProtocolVersion: 1 } );
		game.seed( local );
		const bundle = product( REGION );
		bundle.objects = [];
		game.command( { kind: "navigation", regionId: REGION, bundle }, 0, undefined, local );
		sent.length = 0;
		// Seated (0x7017 posture 4) or dead: 0x680A30's guards return before
		// latching, so nothing walks and nothing stops.
		for ( const key of /** @type {const} */ ([ "up", "left" ]) ) {
			game.command( { kind: "arrow-key", key, down: true }, 0, undefined, local );
		}
		for ( let now = 16; now <= 500; now += 16 ) game.step( now, local );
		for ( const key of /** @type {const} */ ([ "up", "left" ]) ) {
			game.command( { kind: "arrow-key", key, down: false }, 500, undefined, local );
		}
		assert.deepEqual( sent.map( f => f.opcode ), [], JSON.stringify( refused ) );
		game.dispose();
	}
});

test("an Up press during a predicted direction walk keeps that walk, drawn exactly as without it", async () => {
	const { createLocalMover } = await import( "../../src/engine/runtime/movement/local-mover.ts" );
	const view = {
		available: () => true,
		block: () => 0,
		floor: () => undefined,
		pick: () => null,
		clip: ( _from, to, query ) => {
			if ( query ) query.status = 0;
			return to;
		},
		surface: pose => pose
	};
	const world = /** @type {any} */ ({
		pose: { ...POSE, angle: 0 },
		poseAtMs: 0,
		revision: 0,
		moving: false,
		aim: undefined,
		speedFactor: 1,
		walkPredictable: true,
		speed: 50,
		inputSeq: 0,
		navigationRequestId: 1,
		navigationOwner: undefined,
		originMs: 0,
		view: { requestId: 1, navigation: view }
	});
	/*
	================
	walk

	Up at 0, frames every 16 ms; again mid-frame at 104 ms when repeat is set
	(0x877F30 with bFlag 0 does not resend a direction walk under way).
	================
	*/
	const walk = repeat => {
		const mover = createLocalMover(), drawn = [];
		mover.arrow( 1, true, { ...POSE, angle: 0 }, 0, world );
		for ( let now = 16; now <= 400; now += 16 ) {
			if ( repeat && now === 112 ) mover.arrow( 2, true, { ...POSE, angle: 0 }, 104, world );
			const frame = mover.frame( now, world );
			if ( frame.kind !== "idle" ) drawn.push( [ frame.pose.x, frame.pose.z ] );
		}
		return drawn;
	};
	assert.deepEqual( walk( true ), walk( false ) );
});

test("a press during a cast asks it to end at most once per 2 s, and waits held (0x693190)", () => {
	const sent = [], keys = createArrowKeys( f => sent.push( f ) ), { mover, calls } = fakeMover();
	assert.equal( keys.key( "up", true, 0, mover, "cast" ), null );
	assert.equal( calls.cancel, 1, "the press asks the cast to end" );
	assert.deepEqual( calls.direct, [], "and does not walk yet" );
	// Released and pressed again inside 2 s: no second request.
	keys.key( "up", false, 500, mover, "cast" );
	keys.key( "up", true, 1999, mover, "cast" );
	assert.equal( calls.cancel, 1, "0x6931D9: one request per 2000 ms" );
	keys.key( "up", false, 2000, mover, "cast" );
	keys.key( "up", true, 2000, mover, "cast" );
	assert.equal( calls.cancel, 2, "2000 ms later it asks again" );
	assert.deepEqual( sent, [], "a key that never walked sends no stop" );
});

test("a key held through a cast acts the moment the gate opens, and its release then stops (0x67C960)", () => {
	const sent = [], keys = createArrowKeys( f => sent.push( f ) ), { mover, calls } = fakeMover();
	keys.key( "up", true, 0, mover, "cast" );
	keys.key( "left", true, 0, mover, "cast" );
	keys.step( 200, mover, false );
	assert.deepEqual( calls.direct, [], "still held back" );
	assert.deepEqual( calls.steer, [] );
	keys.step( 400, mover, true );
	assert.deepEqual( calls.direct, [ POSE.angle ], "Up walks once the cast lets go" );
	keys.step( 600, mover, true );
	assert.ok( calls.steer.length > 0, "Left turns once the cast lets go" );
	keys.key( "up", false, 700, mover, "open" );
	assert.equal( sent.at( -1 )?.opcode, OP_DIRECTION_STOP );
});

test("a key released before the cast lets go never walks and sends no stop", () => {
	const sent = [], keys = createArrowKeys( f => sent.push( f ) ), { mover, calls } = fakeMover();
	keys.key( "up", true, 0, mover, "cast" );
	keys.key( "up", false, 100, mover, "cast" );
	keys.step( 400, mover, true );
	assert.deepEqual( calls.direct, [] );
	assert.deepEqual( sent, [] );
});

test("through gameplay, Up during the server's cast sends 0x72CD [2] and walks once the cast lets go", () => {
	const fixture = JSON.parse(
		readFileSync(
			path.resolve( root, "../server/internal/game/item/wire/testdata/skill_action_result_fixture.json" ),
			"utf8"
		)
	);
	const sent = [], game = createGameplay( f => sent.push( f ) );
	const local = {
		...POSE,
		gid: fixture.expect.casterGid,
		refObjId: 1907,
		kind: "local-player",
		name: "caster",
		heading: POSE.angle,
		appearanceState: [ 1, 0, 0 ]
	};
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	const bundle = product( REGION );
	bundle.objects = [];
	game.command( { kind: "navigation", regionId: REGION, bundle }, 0, undefined, local );
	// The server's own cast, and its one accepted command (B2CD count 1).
	const row = fixture.scenarios[0];
	game.receive( { opcode: row.opcode, payload: Buffer.from( row.payloadHex, "hex" ) }, 10 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 10 );
	sent.length = 0;
	game.command( { kind: "arrow-key", key: "up", down: true }, 11, undefined, local );
	assert.deepEqual( sent.map( f => [ f.opcode, ...f.payload ] ), [ [ 0x72cd, 2 ] ], "asks the cast to end, no walk" );
	// The cast releases (B505) with Up still held: the walk starts by itself.
	game.receive( { opcode: 0xb505, payload: Uint8Array.of( 2, 0, 1, 0, 0, 0 ) }, 13 );
	for ( let now = 14; now < 3000 && !sent.some( f => f.opcode === 9 ); now += 16 ) game.step( now, local );
	assert.ok( sent.some( f => f.opcode === 9 ), "walks once the cast lets go" );
	game.dispose();
});

test("with two commands queued the press sends no cancel (0x67D140), yet still walks once the cast lets go", () => {
	const fixture = JSON.parse(
		readFileSync(
			path.resolve( root, "../server/internal/game/item/wire/testdata/skill_action_result_fixture.json" ),
			"utf8"
		)
	);
	const sent = [], game = createGameplay( f => sent.push( f ) );
	const local = {
		...POSE,
		gid: fixture.expect.casterGid,
		refObjId: 1907,
		kind: "local-player",
		name: "caster",
		heading: POSE.angle,
		appearanceState: [ 1, 0, 0 ]
	};
	game.bootstrap( { simulationProtocolVersion: 1 } );
	game.seed( local );
	const bundle = product( REGION );
	bundle.objects = [];
	game.command( { kind: "navigation", regionId: REGION, bundle }, 0, undefined, local );
	const row = fixture.scenarios[0];
	game.receive( { opcode: row.opcode, payload: Buffer.from( row.payloadHex, "hex" ) }, 10 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 10 );
	sent.length = 0;
	game.command( { kind: "arrow-key", key: "up", down: true }, 11, undefined, local );
	assert.deepEqual( sent.map( f => f.opcode ), [], "two queued commands hold the request back" );
	game.receive( { opcode: 0xb505, payload: Uint8Array.of( 2, 0, 1, 0, 0, 0 ) }, 13 );
	for ( let now = 14; now < 3000 && !sent.some( f => f.opcode === 9 ); now += 16 ) game.step( now, local );
	assert.ok( sent.some( f => f.opcode === 9 ), "walks once the cast lets go" );
	game.dispose();
});
