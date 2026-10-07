/*
===========================================================================

ghost-walk-handoff.test.mjs - walking before and after an accepted displacement

Position skills send their ground request while walking. The accepted dash
replaces that walk permanently; an older cast hold must never resurrect it.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { product } from "../helpers/navigation-fixture.mjs";
const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const { createMovement } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/movement/movement.ts"
);
const { createEntityMotion } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/entities/motion/motion.ts"
);
const { createPosePresentation } = await import( "../../src/engine/runtime/characters/pose-presentation.ts" );
const START = { regionId: 257, x: 100, y: 0, z: 100, angle: 0 };
const WALK_END = { ...START, x: 900 };
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const LOCAL = { ...START, gid: 1, refObjId: 1907, kind: "local-player", name: "dash", heading: 0 };
const QUERY = { originRegion: 257, ray: { start: [ 0, 100, 0 ], delta: [ 1, 0, 0 ] }, terrainDepth: null };

/*
================
navigation
================
*/
function navigation() {
	const bundle = product();
	bundle.objects = [];
	return bundle;
}

/*
================
reference

The actual rank metadata matters: Phantom and Shadow 1 are activity 2;
Shadow 2-4 are activity 1. Earlier position tests omitted haltsWalk.
================
*/
function reference( id, haltsWalk ) {
	return {
		id,
		group: id === 114 ? 261 : 768,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: "Ghost Walk",
			trainable: true,
			spCost: 1,
			targetRequired: true,
			groundTarget: true,
			haltsWalk,
			cooldownMs: 5000,
			cooldownGroup: 59,
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		}
	};
}

/*
================
draw

Apply the character owner's admitted-path publication policy.
================
*/
function draw( presentation, state, now ) {
	assert.ok( state?.pose && state.poseAtMs !== undefined );
	const admitted = state.movementTransition?.pathEligible !== false;
	presentation.samples(
		new Map( [ [ 1, {
			atMs: state.poseAtMs,
			revision: state.movementRevision ?? 0,
			moving: !!state.moving && admitted,
			transition: state.movementTransition,
			...state.movementPath,
			from: admitted ? state.movementPath?.from : undefined
		} ] ] )
	);
	return presentation.pose( 1, state.pose, now / 1000 );
}

for ( const [skillId, haltsWalk] of /** @type {const} */ ([ [ 114, true ], [ 19636, true ], [ 19639, false ] ]) ) {
	test(`Ghost Walk ${skillId} keeps walking while its ground request awaits acceptance`, () => {
		const sent = [], game = createGameplay( frame => sent.push( frame ) );
		game.bootstrap( {
			simulationProtocolVersion: 1,
			character: { skills: [ skillId ] },
			refSkillSnapshot: [ reference( skillId, haltsWalk ) ]
		} );
		game.seed( LOCAL );
		game.command( { kind: "navigation", regionId: 257, bundle: navigation() }, 0, undefined );
		game.command( { kind: "move", destination: WALK_END }, 0, undefined );
		game.step( 100 );
		const before = game.take();
		assert.equal( before?.pose?.x, 105 );
		game.command( { kind: "skill", skillId, query: QUERY }, 100, undefined );
		assert.equal( sent.at( -1 )?.opcode, 0x72cd );
		assert.equal( sent.at( -1 )?.payload[6], 2 );
		game.step( 200 );
		const pending = game.take();
		assert.equal( pending?.moving, true );
		assert.equal( pending?.pose?.x, 110, "the press must not introduce a latency-length stop" );
		assert.equal( pending?.movementRevision, before?.movementRevision );
		game.receive( { opcode: 0xb245, payload: Uint8Array.of( 2, 4 ) }, 210 );
		game.step( 300 );
		const refused = game.take();
		assert.equal( refused?.pose?.x, 115, "a refused ground request leaves the existing walk alone" );
		game.command( { kind: "skill", skillId, query: QUERY }, 300, undefined );
		const presentation = createPosePresentation();
		presentation.origin( 0 );
		draw( presentation, refused, 300 );
		const atAcceptance = draw( presentation, refused, 400 );
		const packet = Buffer.alloc( 27 );
		packet[0] = 1;
		packet.writeUInt32LE( skillId, 2 );
		packet.writeUInt32LE( 1, 6 );
		packet.writeUInt32LE( 9, 10 );
		packet[18] = 8;
		packet.writeUInt16LE( 257, 19 );
		packet.writeInt16LE( 330, 21 );
		packet.writeInt16LE( 100, 25 );
		game.receive( { opcode: 0xb245, payload: packet }, 400 );
		const travel = game.takeDisplacements()[0];
		assert.ok( travel );
		const arrival = game.displace( travel, 400 );
		assert.equal( arrival, 820, "dash timing starts at the live x120, never the older press pose" );
		const accepted = game.take();
		assert.equal( accepted?.movementPath?.displacement, true );
		assert.equal(
			draw( presentation, accepted, 400 ).x,
			atAcceptance.x,
			"no backward displayed step at acceptance"
		);
		game.step( 416 );
		assert.equal( draw( presentation, game.take(), 416 ).x, 128 );
		game.step( 2000 );
		assert.equal( game.take()?.pose?.x, 330, "accepted travel permanently replaces the old walk" );
		game.dispose();
	});
}

test("remote displacement publishes its ownership and clock through arrival and source receipts", () => {
	const motion = createEntityMotion();
	const destination = { ...START, x: 330 };
	const initial = motion.displace( LOCAL, { gid: 1, token: 9, kind: 8, destination }, 200 );
	assert.equal( initial.movementPath?.displacement, true );
	assert.equal( initial.movementPath?.durationMs, 460 );
	assert.equal( initial.poseAtMs, 200 );
	for ( const now of [ 216, 400, 660 ] ) {
		const step = motion.step( now )[0];
		assert.ok( step );
		assert.equal( step.movementPath?.displacement, true );
		assert.equal( step.movementPath?.durationMs, 460 );
		if ( now < 660 ) {
			const source = motion.source( { ...LOCAL, ...step }, { ...START, x: step.x }, now );
			assert.deepEqual( source.movementPath, step.movementPath );
		} else {
			assert.equal( step.x, 330 );
			assert.equal( step.moving, false );
		}
	}
	const settled = { ...LOCAL, ...initial, ...destination, heading: 0, moving: false, runSpeed: 50, movementMode: 3 };
	const idleSource = motion.source( settled, destination, 700 );
	assert.equal( idleSource.moving, false );
	const packet = Buffer.alloc( 14 );
	packet.writeUInt32LE( 1 );
	packet[4] = 1;
	packet.writeUInt16LE( 257, 5 );
	packet.writeInt16LE( 900, 7 );
	packet.writeInt16LE( 100, 11 );
	/** @type {import("../../src/engine/contracts/gameplay.ts").MovementPath} */
	const walk = motion.receive( packet, { ...settled, ...idleSource }, 800 );
	assert.equal( walk.displacement, undefined, "a new ordinary path replaces the completed displacement owner" );
	assert.equal( motion.step( 816 )[0]?.movementPath?.displacement, undefined );
});

for ( const kind of /** @type {const} */ ([ 2, 8 ]) ) {
	for ( const refused of [ false, true ] ) {
		test(`accepted displacement ${kind} retires an older cast hold, late refusal ${refused}`, () => {
			const movement = createMovement( () => {} );
			movement.seed( START );
			movement.navigation( 257, navigation() );
			movement.request( WALK_END, 0 );
			movement.step( 100 );
			movement.holdForCast( 100 );
			const destination = { ...START, x: 330 };
			const arrival = movement.displace( { gid: 1, token: 9, kind, destination }, 200 );
			assert.ok( arrival !== undefined );
			movement.step( arrival );
			assert.equal( movement.state().pose?.x, destination.x );
			if ( refused ) movement.castRefused( arrival + 1 );
			for ( const now of [ 1500, 1600, 2000, 3000 ] ) {
				movement.step( now );
				assert.equal( movement.state().pose?.x, destination.x, `old hold resumed at ${now} ms` );
				assert.equal( movement.state().moving, false );
			}
		});
	}
}
