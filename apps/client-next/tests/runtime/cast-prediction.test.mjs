/*
===========================================================================

cast-prediction.test.mjs - tests for cast-prediction.ts, its adoption in
combat.ts and its hand-over in the character presenter: the local cast's
animation starts at the press and never restarts when the server answers

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { createCastPrediction } = await import( "../../src/engine/foundation/gameplay/cast-prediction.ts" );
const { createCombat } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
);
const { createCharacterPresentation } = await import( "../../src/engine/runtime/characters/characters.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );

const FIXTURE = new URL(
	"../../../server/internal/game/item/wire/testdata/skill_action_result_fixture.json",
	import.meta.url
);

// ============================================================================
// cast-prediction.ts

test("a prediction is adopted only by the same caster's cast of the same skill", () => {
	const p = createCastPrediction();
	const cast = p.predict( 7, 30, 9, 1000, 1500 );
	assert.ok( cast.token < -1, "prediction tokens never meet server tokens or the -1 sentinel" );
	assert.equal( p.adopt( 8, 30 ), undefined );
	assert.equal( p.adopt( 7, 31 ), undefined );
	assert.equal( p.adopt( 7, 30 ), cast.token );
	assert.equal( p.state(), undefined );
	assert.equal( p.adopt( 7, 30 ), undefined, "a prediction is adopted once" );
});

test("a refusal or a missing answer blends the prediction out, then drops it", () => {
	const p = createCastPrediction();
	p.predict( 7, 30, 9, 1000, 1500 );
	assert.equal( p.cancel( 1200 ), true );
	assert.equal( p.state()?.cancelledAtMs, 1200 );
	assert.equal( p.adopt( 7, 30 ), undefined, "a cancelled prediction is not adopted" );
	assert.equal( p.step( 1699 ), false );
	assert.equal( p.step( 1700 ), true );
	assert.equal( p.state(), undefined );
	p.predict( 7, 30, 9, 2000, 2500 );
	assert.equal( p.step( 2500 ), true );
	assert.equal( p.state()?.cancelledAtMs, 2500 );
});

// ============================================================================
// combat.ts adoption against a real cast-start answer

test("the server's cast-start answer adopts the local prediction", () => {
	const fixture = JSON.parse( readFileSync( FIXTURE, "utf8" ) ), row = fixture.scenarios[0];
	const combat = createCombat();
	combat.cooldownReferences( fixture.expect.casterGid, [] );
	combat.predict( fixture.expect.skillId, fixture.expect.targetGid, 100, 600 );
	const predicted = combat.state().castPrediction;
	assert.ok( predicted && predicted.caster === fixture.expect.casterGid );
	assert.equal( combat.receive( row.opcode, Buffer.from( row.payloadHex, "hex" ), 300 ), true );
	const state = combat.state();
	assert.equal( state.castPrediction, undefined );
	assert.equal( state.casts[0]?.predictedToken, predicted.token );
});

test("a refused press cancels the prediction", () => {
	const combat = createCombat();
	combat.cooldownReferences( 7, [] );
	combat.predict( 30, 9, 100, 600 );
	combat.receive( 0xb245, Uint8Array.of( 2, 4 ), 300 );
	assert.equal( combat.state().castPrediction?.cancelledAtMs, 300 );
});

// ============================================================================
// The presenter hands the running action over

/*
================
presenter

The action-time harness: one actor whose skill 1 plays ready01, wait01,
attack1, each authored at one second.
================
*/
function presenter( stages = [], sounds = [] ) {
	const names = [ "stand", "ready01", "wait01", "attack1" ], pending = new Map();
	/** @type {any[]} */
	let actors = [];
	let id = 0;
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) ).buffer;
	const assets = {
		available: () => 4,
		request( url, limit, decode ) {
			pending.set( ++id, { url, decode } );
			return id;
		},
		cancel( id ) {
			pending.delete( id );
		},
		take( id ) {
			const job = pending.get( id );
			if ( !job ) return null;
			pending.delete( id );
			if ( job.decode === "effects" ) {
				return {
					kind: "effects",
					catalog: {
						"1": {
							clips: [ "attack1" ],
							phaseClips: [ [ "ready01" ], [ "wait01" ], [ "attack1" ] ],
							stages
						}
					}
				};
			}
			if ( job.decode === "character" ) {
				return {
					kind: "character",
					model: {
						nodes: [],
						primitives: [],
						images: [],
						clips: names.map( name => ({ name, duration: 1, channels: [] }) )
					},
					images: []
				};
			}
			let value = {};
			if ( job.url.endsWith( "/roster.json" ) ) {
				value = {
					models: [ {
						refObjId: 1,
						codename: "rider",
						glb: "/assets/rider.glb",
						clips: names,
						animationStates: Object.fromEntries( names.map( name => [ name, { durationMs: 1000 } ] ) )
					} ]
				};
			}
			if ( job.url.endsWith( "/skillfx/manifest.json" ) ) {
				value = { format: "sro-skill-stage-models", models: {} };
			}
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				value = { format: "sro-mission-itemdrop-models", models: {} };
			}
			return { kind: "bytes", buffer: encode( value ) };
		}
	};
	// The stub loader and renderer cover only what the presenter calls here.
	const p = createCharacterPresentation(
		/** @type {any} */ (assets),
		/** @type {any} */ ({
			setCharacterModel() {},
			setCharacterAssembly() {},
			retainCharacterModels() {},
			setCharacterActors( value ) {
				actors = value;
			}
		}),
		"http://localhost",
		event => sounds.push( event ),
		createPresentationRandom( 1 )
	);
	/** @type {any} */
	const entity = { gid: 1, refObjId: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 };
	/** @type {any} */
	const gameplay = { localGid: 1, inventory: [], casts: [], vitals: [] };
	const step = t => p.step( [ entity ], gameplay, t, t * 1000 );
	for ( let i = 0; i < 30; i++ ) step( i / 30 );
	return { p, gameplay, step, layer: () => actors[0].layers?.[0] };
}

const PREDICTED = { token: -0x10001, caster: 1, target: 1, skill: 1, damage: 0, fatal: false };

test("the predicted action starts at the press and the server's cast continues it", () => {
	const { p, gameplay, step, layer } = presenter();
	gameplay.castPrediction = { ...PREDICTED, receivedAtMs: 2000 };
	step( 2 );
	step( 2.3 );
	assert.equal( layer()?.clip, "ready01", "the prediction did not animate" );
	const before = layer().time;
	// The server's answer arrives 360 ms after the press and adopts it.
	gameplay.castPrediction = undefined;
	gameplay.casts = [ {
		token: 1,
		caster: 1,
		target: 1,
		skill: 1,
		receivedAtMs: 2360,
		damage: 0,
		fatal: false,
		predictedToken: PREDICTED.token
	} ];
	step( 2.4 );
	assert.equal( layer()?.clip, "ready01" );
	assert.ok( layer().time > before, `the action restarted: ${before} -> ${layer().time}` );
	assert.ok( Math.abs( layer().time - 0.2 ) < 1e-6, "the clock is not the prediction's: " + layer().time );
	assert.equal( p.error(), null );
	p.dispose();
});

test("a refused prediction blends out and leaves no action", () => {
	const { p, gameplay, step, layer } = presenter();
	gameplay.castPrediction = { ...PREDICTED, receivedAtMs: 2000 };
	step( 2 );
	step( 2.3 );
	assert.equal( layer()?.clip, "ready01" );
	gameplay.castPrediction = { ...PREDICTED, receivedAtMs: 2000, cancelledAtMs: 2360 };
	step( 3.5 );
	gameplay.castPrediction = undefined;
	step( 3.6 );
	assert.equal( layer(), undefined );
	assert.equal( p.error(), null );
	p.dispose();
});

// ============================================================================
// gameplay.ts: when a press predicts

const { createGameplay } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts"
);
const LOCAL_GID = 7;
/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
const local = {
	gid: LOCAL_GID,
	refObjId: 1907,
	kind: "local-player",
	name: "caster",
	regionId: 257,
	x: 100,
	y: 0,
	z: 100,
	heading: 0
};

/*
================
pressAt

Presses skill 30 (range 60, an action of 1 s) at a monster dx away and
returns the published prediction. before runs ahead of the press, after
right behind it; ui.monster overrides the monster's fields.
================
*/
function pressAt( dx, ui = {}, before = game => {}, after = game => {} ) {
	const game = createGameplay( () => {} );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: { skills: [ 30 ] },
		refSkillSnapshot: [ {
			id: 30,
			group: 30,
			level: 1,
			status: false,
			effectRider: false,
			ui: {
				name: "SKILL_30",
				trainable: true,
				spCost: 1,
				targetRequired: true,
				// Animal and monster groups: an enemy skill, as the shipped rows author.
				targets: 6,
				cooldownMs: 0,
				actionMs: 1000,
				range: 60,
				masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
				prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
				...ui
			}
		} ]
	} );
	game.seed( local );
	before( game );
	/** @type {import("../../src/engine/contracts/world.ts").EntityState} */
	const monster = { ...local, gid: 9, kind: "monster", name: "mob", x: local.x + dx, ...ui.monster };
	game.command( { kind: "skill", skillId: 30, gid: 9 }, 1000, monster, local );
	after( game );
	const prediction = game.take()?.castPrediction;
	game.dispose();
	return prediction;
}

test("a press at a target within the skill's range starts its animation at once", () => {
	const prediction = pressAt( 50 );
	assert.equal( prediction?.skill, 30 );
	assert.equal( prediction?.caster, LOCAL_GID );
	assert.equal( prediction?.receivedAtMs, 1000 );
});

test("a press out of range, or for a weapon-reach skill, waits for the server", () => {
	assert.equal( pressAt( 61 ), undefined, "the server chases first" );
	assert.equal( pressAt( 10, { range: undefined } ), undefined, "the reach is the weapon's" );
});

test("a press at a dead monster waits for the server", () => {
	assert.equal( pressAt( 50, { monster: { appearanceState: [ 2, 0, 0 ] } } ), undefined );
});

test("a press during a walk the server leads waits for the server's stop", () => {
	const walk = Buffer.alloc( 14 );
	walk.writeUInt32LE( LOCAL_GID );
	walk[4] = 1;
	walk.writeUInt16LE( 257, 5 );
	walk.writeInt16LE( 400, 7 );
	walk.writeInt16LE( 0, 9 );
	walk.writeInt16LE( 100, 11 );
	const prediction = pressAt( 50, {}, game => {
		assert.equal( game.receive( { opcode: 0xb738, payload: walk }, 500 ), true );
		for ( let now = 516; now <= 1000; now += 16 ) game.step( now, local );
	} );
	assert.equal( prediction, undefined, "the action would slide along the walk" );
});

test("a press the server queues behind an open command stops predicting", () => {
	const prediction = pressAt( 50, {}, undefined, game => {
		// B2CD arm, count 2: the press waits for the open command.
		game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 1300 );
	} );
	assert.equal( prediction?.cancelledAtMs, 1300 );
});

test("another local cast opening first stops the prediction", () => {
	const fixture = JSON.parse( readFileSync( FIXTURE, "utf8" ) ), row = fixture.scenarios[0];
	const combat = createCombat();
	combat.cooldownReferences( fixture.expect.casterGid, [] );
	combat.predict( fixture.expect.skillId + 1, fixture.expect.targetGid, 100, 600 );
	assert.equal( combat.receive( row.opcode, Buffer.from( row.payloadHex, "hex" ), 300 ), true );
	assert.equal( combat.state().castPrediction?.cancelledAtMs, 300 );
	assert.equal( combat.state().casts[0]?.predictedToken, undefined );
});

test("a standing wall or aura holds the prediction only for its action time", () => {
	const fixture = JSON.parse( readFileSync( FIXTURE, "utf8" ) ), row = fixture.scenarios[0];
	// The fixture's cast, re-addressed to the local caster and skill 30 (an
	// action of 1 s) and never cancelled: a persistent cast stays in the
	// table for the object's whole life. The press comes at 1000 ms.
	const payload = Buffer.from( row.payloadHex, "hex" );
	payload.writeUInt32LE( 30, 2 );
	payload.writeUInt32LE( LOCAL_GID, 6 );
	const castAt = at => game => game.receive( { opcode: row.opcode, payload }, at );
	assert.equal( pressAt( 50, {}, castAt( 500 ) ), undefined, "an action still running must hold" );
	assert.equal( pressAt( 50, {}, castAt( 0 ) )?.skill, 30, "a finished action held the press" );
});

test("the windup sound plays at the press, not when the server answers", () => {
	// Soft Guard of Ice: READY authors csk_cold_ready.wav. Held until the
	// answer, its 0.25 s window had passed at production latency.
	const stages = [ {
		phase: "READY",
		action: "AT_LOOP",
		move: "MOV_NONE",
		startEvent: 0,
		sound: "/assets/audio/sfx/prim/snd/skill/csk_cold_ready.wav",
		scripts: []
	} ];
	const sounds = [];
	const { p, gameplay, step } = presenter( stages, sounds );
	gameplay.castPrediction = { ...PREDICTED, receivedAtMs: 2000 };
	step( 2 );
	step( 2.05 );
	const windup = () => sounds.filter( s => s.path.endsWith( "csk_cold_ready.wav" ) );
	assert.equal( windup().length, 1, "no windup sound at the press" );
	assert.ok( windup()[0].expires >= 2.05, "the windup sound is already stale" );
	gameplay.castPrediction = undefined;
	gameplay.casts = [ {
		token: 1,
		caster: 1,
		target: 1,
		skill: 1,
		receivedAtMs: 2360,
		damage: 0,
		fatal: false,
		predictedToken: PREDICTED.token
	} ];
	step( 2.4 );
	step( 2.5 );
	assert.equal( windup().length, 1, "the adopted cast replayed the windup" );
	assert.equal( p.error(), null );
	p.dispose();
});
