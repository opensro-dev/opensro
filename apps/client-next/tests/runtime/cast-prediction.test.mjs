/*
===========================================================================

cast-prediction.test.mjs - tests for cast-prediction.ts, its adoption in
combat.ts and its hand-over in the character presenter: the local cast's
animation starts at the press and never restarts when the server answers

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { goldDropModels } from "../helpers/gold-drop-models.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { createCastPrediction, releaseHeldEvents } = await import(
	"../../src/engine/foundation/gameplay/cast-prediction.ts"
);
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
				value = { format: "sro-mission-itemdrop-models", models: goldDropModels() };
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
				admit: { weaponKinds: [ 255, 255 ] },
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

test("a sequence root holds the prediction past its own action time until the server closes it", () => {
	// Salamander Blow roots six linked stages that the server runs on the
	// root's open cast (extendChainBracket): a press between them used to
	// predict a new cast the server then holds back.
	const fixture = JSON.parse( readFileSync( FIXTURE, "utf8" ) ), row = fixture.scenarios[0];
	const payload = Buffer.from( row.payloadHex, "hex" );
	payload.writeUInt32LE( 30, 2 );
	payload.writeUInt32LE( LOCAL_GID, 6 );
	const castAt = at => game => game.receive( { opcode: row.opcode, payload }, at );
	assert.equal( pressAt( 50, { chain: true }, castAt( 0 ) ), undefined, "a running sequence must hold" );
	assert.equal( pressAt( 50, {}, castAt( 0 ) )?.skill, 30, "a plain row's finished action held the press" );
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

test("a stunned caster's press predicts neither its cast nor its cooldown (BUG-066)", () => {
	const stun = Buffer.alloc( 12 );
	stun.writeUInt32LE( LOCAL_GID );
	stun[6] = 4; // the abnormal word follows
	stun.writeUInt32LE( 0x4000, 7 ); // stun; 58DAEF refuses the cast (0x3009)
	stun[11] = 1; // stun carries a level byte
	for ( const stunned of [ false, true ] ) {
		/** @type {ReturnType<ReturnType<typeof createGameplay>["take"]>[]} */
		const taken = [];
		pressAt( 50, { cooldownMs: 5000 }, game => {
			if ( stunned ) game.receive( { opcode: 0x33a6, payload: stun }, 900 );
		}, game => {
			taken.push( game.take() );
		} );
		const state = taken[0];
		const predicted = state?.castPrediction?.skill === 30;
		const standIn = (state?.skillCooldowns ?? []).some( row =>
			row.skill === 30 && row.provisionalUntilMs !== undefined
		);
		// The free caster proves the press would predict both; the stunned one must not.
		assert.equal( predicted, !stunned, `cast prediction when ${stunned ? "stunned" : "free"}` );
		assert.equal( standIn, !stunned, `cooldown stand-in when ${stunned ? "stunned" : "free"}` );
	}
});

test("a press for a weapon the caster lacks predicts neither its cast nor its cooldown (BR-261007-0624)", () => {
	// A sword row (TID4 2) pressed bare-handed (TID4 1): 58D480 refuses 0x300D.
	for ( const weaponKinds of [ [ 255, 255 ], [ 1, 2 ], [ 2, 2 ] ] ) {
		/** @type {ReturnType<ReturnType<typeof createGameplay>["take"]>[]} */
		const taken = [];
		pressAt( 50, { cooldownMs: 5000, admit: { weaponKinds } }, () => {}, game => {
			taken.push( game.take() );
		} );
		const state = taken[0], admitted = weaponKinds[0] !== 2;
		const standIn = (state?.skillCooldowns ?? []).some( row =>
			row.skill === 30 && row.provisionalUntilMs !== undefined
		);
		assert.equal( state?.castPrediction?.skill === 30, admitted, `cast prediction for ${weaponKinds}` );
		assert.equal( standIn, admitted, `cooldown stand-in for ${weaponKinds}` );
	}
});

test("a row without admission inputs is sent but never predicted", () => {
	/** @type {ReturnType<ReturnType<typeof createGameplay>["take"]>[]} */
	const taken = [];
	pressAt( 50, { cooldownMs: 5000, admit: undefined }, () => {}, game => {
		taken.push( game.take() );
	} );
	assert.equal( taken[0]?.castPrediction, undefined );
	assert.equal( (taken[0]?.skillCooldowns ?? []).some( row => row.provisionalUntilMs !== undefined ), false );
});

test("held release events fire at the answer, in order, never back-dated", () => {
	// A bow press predicted at 2.0 s holds its SHOT launch (cursor 2.1 s)
	// and hit (2.3 s); the server's answer adopts them at 2.4 s. Stamped
	// at 2.1 s the arrow's flight began 0.3 s along and landed unseen.
	const held = [
		{ phase: "SHOT", event: 1, at: 2.1 },
		{ phase: "SHOT", event: 2, at: 2.3 },
		{ phase: "SHOT", event: 3, at: 2.6 }
	];
	const released = releaseHeldEvents( held, 2.4 );
	assert.deepEqual( released.map( event => event.at ), [ 2.4, 2.4, 2.6 ] );
	assert.deepEqual( released.map( event => event.event ), [ 1, 2, 3 ] );
	assert.ok( released.every( event => event.adopted === true ) );
	assert.equal( held[0]?.at, 2.1, "the held events were mutated" );
});

/**
 * @param {number} id
 * @param {{
 *   group?: number,
 *   targetRequired?: boolean,
 *   targetSelf?: boolean,
 *   groundTarget?: boolean,
 *   targets?: number,
 *   admit?: object,
 *   cooldownMs?: number,
 *   actionMs?: number,
 *   range?: number,
 *   holdsCaster?: boolean,
 *   chain?: boolean
 * }} [over]
 */
function queuedChainSkillRef( id, over = {} ) {
	return {
		id,
		group: over.group ?? id,
		level: 1,
		status: false,
		effectRider: false,
		ui: {
			name: `CHAIN_${id}`,
			trainable: true,
			spCost: 1,
			targetRequired: over.targetRequired ?? true,
			targetSelf: over.targetSelf === true,
			groundTarget: over.groundTarget === true,
			targets: over.targets ?? (1 | 4),
			admit: over.admit ?? { weaponKinds: [ 255, 255 ] },
			cooldownMs: over.cooldownMs ?? 0,
			actionMs: over.actionMs,
			range: over.range ?? 200,
			...(over.holdsCaster ? { holdsCaster: true } : {}),
			...(over.chain ? { chain: true } : {}),
			masteries: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ],
			prerequisites: [ { ID: 0, Level: 0 }, { ID: 0, Level: 0 }, { ID: 0, Level: 0 } ]
		}
	};
}

/**
 * @param {number} skillId
 * @param {number} casterGid
 * @param {number} token
 * @param {number} targetGid
 */
function buildCastOpenPayload( skillId, casterGid, token, targetGid ) {
	const buf = Buffer.alloc( 19 );
	buf[0] = 1;
	buf[1] = 0;
	buf.writeUInt32LE( skillId, 2 );
	buf.writeUInt32LE( casterGid, 6 );
	buf.writeUInt32LE( token, 10 );
	buf.writeUInt32LE( targetGid, 14 );
	buf[18] = 0;
	return new Uint8Array( buf );
}

/** @param {number} token */
function buildCastClosePayload( token ) {
	const buf = Buffer.alloc( 6 );
	buf[0] = 2;
	buf.writeUInt32LE( token, 2 );
	return new Uint8Array( buf );
}

/**
 * @param {{
 *   spellA?: ReturnType<typeof queuedChainSkillRef>,
 *   spellB?: ReturnType<typeof queuedChainSkillRef>,
 *   extraSkills?: ReturnType<typeof queuedChainSkillRef>[],
 *   actorOverrides?: Partial<import("../../src/engine/contracts/world").EntityState>,
 *   mobOverrides?: Partial<import("../../src/engine/contracts/world").EntityState>
 * }} [opts]
 */
function createQueuedChainGame( {
	spellA = queuedChainSkillRef( 9101, { actionMs: 3000 } ),
	spellB = queuedChainSkillRef( 9102, { actionMs: 1000 } ),
	extraSkills = /** @type {ReturnType<typeof queuedChainSkillRef>[]} */ ([]),
	actorOverrides = {},
	mobOverrides = {},
	// The Experimental "Start queued skills early" row (#4740): on for the
	// prediction cases, off for the native default.
	predictedQueuedCast = true
} = {} ) {
	/** @type {import("../../src/engine/contracts/world").EntityState} */
	const actor = {
		gid: 7001,
		refObjId: 1907,
		name: "Wizard",
		kind: "player",
		regionId: 0x62a8,
		x: 100,
		y: 20,
		z: 100,
		heading: 0,
		appearanceState: [ 1, 0, 0 ],
		...actorOverrides
	};
	/** @type {import("../../src/engine/contracts/world").EntityState} */
	const mob = {
		gid: 8001,
		refObjId: 2001,
		name: "Mob",
		kind: "monster",
		regionId: 0x62a8,
		x: 150,
		y: 20,
		z: 100,
		heading: 0,
		appearanceState: [ 1, 0, 0 ],
		...mobOverrides
	};
	/** @type {Map<number, import("../../src/engine/contracts/world").EntityState>} */
	const entities = new Map( [ [ actor.gid, actor ], [ mob.gid, mob ] ] );
	const skills = [ spellA, spellB, ...extraSkills ];
	/** @type {unknown[]} */
	const sent = [];
	const game = createGameplay( frame => sent.push( frame ), () => {}, () => {}, gid => entities.get( gid ) );
	game.bootstrap( {
		simulationProtocolVersion: 1,
		character: {
			skills: skills.map( s => s.id ),
			hp: 1000,
			maxHp: 1000,
			mp: 1000,
			maxMp: 1000
		},
		refSkillSnapshot: skills
	} );
	game.experimental( { predictedQueuedCast } );
	game.worldReady();
	game.seed( actor );
	game.step( 0, actor );
	return { game, actor, mob, entities, sent, spellA, spellB };
}

test("queued press predicts Spell B one one-way latency early at 200 ms RTT and B245 adopts it", () => {
	const { game, actor, mob, spellA, spellB } = createQueuedChainGame();
	// Cast Spell A at t=0; 200 ms later (oneWay = 100 ms), B245 + B2CD [1, 1] arrive.
	game.command( { kind: "skill", skillId: spellA.id, gid: mob.gid }, 0, mob, actor );
	game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( spellA.id, actor.gid, 101, mob.gid ) }, 200 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 200 );

	// Press Spell B mid-cast at t=800; server queue ack arrives at t=1000.
	game.command( { kind: "skill", skillId: spellB.id, gid: mob.gid }, 800, mob, actor );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 1000 );
	assert.equal( game.take()?.castPrediction, undefined, "queue ack must cancel initial prediction" );

	// Spell A receivedAtMs=200, actionMs=3000, oneWay=100 => blockingEnd=3100.
	game.step( 3099, actor );
	assert.equal( game.take()?.castPrediction, undefined, "must stay silent 1 ms before blockingEnd" );

	game.step( 3100, actor );
	const predicted = game.take()?.castPrediction;
	assert.ok( predicted, "Spell B should start predicting at blockingEnd = 3100 ms" );
	assert.equal( predicted.skill, spellB.id );
	assert.equal( predicted.target, mob.gid );
	const predictedToken = predicted.token;

	// Server close for Spell A + open for Spell B arrive at t=3200 and adopt predictedToken.
	game.receive( { opcode: 0xb505, payload: buildCastClosePayload( 101 ) }, 3200 );
	game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( spellB.id, actor.gid, 102, mob.gid ) }, 3200 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 2, 1 ) }, 3200 );
	const adoptedState = game.take();
	assert.equal( adoptedState?.castPrediction, undefined, "adopted prediction must leave castPrediction slot" );
	const serverCastB = adoptedState?.casts.find( c => c.token === 102 );
	assert.equal( serverCastB?.predictedToken, predictedToken, "server cast B must adopt the queued prediction token" );
	game.dispose();
});

test("queued prediction stays blocked behind unclosed chain roots and holding walls", () => {
	for ( const mode of [ "chain", "holdsCaster" ] ) {
		const spellA = queuedChainSkillRef( 9111, {
			actionMs: 1000,
			chain: mode === "chain",
			holdsCaster: mode === "holdsCaster"
		} );
		const { game, actor, mob, spellB } = createQueuedChainGame( { spellA } );
		game.command( { kind: "skill", skillId: spellA.id, gid: mob.gid }, 0, mob, actor );
		game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( spellA.id, actor.gid, 201, mob.gid ) }, 200 );
		game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 200 );
		game.command( { kind: "skill", skillId: spellB.id, gid: mob.gid }, 500, mob, actor );
		game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 700 );

		game.step( 1200, actor );
		assert.equal( game.take()?.castPrediction, undefined, `${mode} must block queued cast prediction` );
		game.dispose();
	}
});

test("queued prediction stays silent if target dies or moves out of reach before blockingEnd", () => {
	for ( const reason of [ "dead", "out-of-range" ] ) {
		const { game, actor, mob, entities, spellA, spellB } = createQueuedChainGame();
		game.command( { kind: "skill", skillId: spellA.id, gid: mob.gid }, 0, mob, actor );
		game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( spellA.id, actor.gid, 301, mob.gid ) }, 200 );
		game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 200 );
		game.command( { kind: "skill", skillId: spellB.id, gid: mob.gid }, 800, mob, actor );
		game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 1000 );

		if ( reason === "dead" ) {
			entities.set( mob.gid, { ...mob, appearanceState: [ 2, 0, 0 ] } );
		} else {
			entities.set( mob.gid, { ...mob, x: 500 } );
		}
		game.step( 3100, actor );
		assert.equal( game.take()?.castPrediction, undefined, `${reason} target must block queued prediction` );
		game.dispose();
	}
});

test("queued prediction scales front cast duration by local.animationRate", () => {
	const slowActor = { animationRate: 0.8 };
	const { game, actor, mob, spellA, spellB } = createQueuedChainGame( {
		spellA: queuedChainSkillRef( 9121, { actionMs: 2000 } ),
		actorOverrides: slowActor
	} );
	game.command( { kind: "skill", skillId: spellA.id, gid: mob.gid }, 0, mob, actor );
	game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( spellA.id, actor.gid, 401, mob.gid ) }, 200 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 200 );
	game.command( { kind: "skill", skillId: spellB.id, gid: mob.gid }, 800, mob, actor );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 1000 );

	// 2000 / 0.8 = 2500 ms => blockingEnd = 200 - 100 + 2500 = 2600 ms (not unscaled 2100).
	game.step( 2599, actor );
	assert.equal( game.take()?.castPrediction, undefined, "must wait for animationRate-scaled end at 2600 ms" );
	game.step( 2600, actor );
	assert.equal( game.take()?.castPrediction?.skill, spellB.id );
	game.dispose();
});

test("queued prediction latches once per B2CD queue slot even after a B245 refusal", () => {
	const { game, actor, mob, spellA, spellB } = createQueuedChainGame();
	game.command( { kind: "skill", skillId: spellA.id, gid: mob.gid }, 0, mob, actor );
	game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( spellA.id, actor.gid, 501, mob.gid ) }, 200 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 200 );
	game.command( { kind: "skill", skillId: spellB.id, gid: mob.gid }, 800, mob, actor );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 1000 );

	game.step( 3100, actor );
	assert.equal( game.take()?.castPrediction?.skill, spellB.id );
	// Refusal arrives while B2CD count hasn't drained yet; prediction blends out
	// cleanly and must not re-predict on subsequent steps.
	game.receive( { opcode: 0xb245, payload: Uint8Array.of( 2, 0x05 ) }, 3150 );
	assert.equal( game.take()?.castPrediction?.cancelledAtMs, 3150 );
	for ( let t = 3200; t <= 4500; t += 100 ) {
		game.step( t, actor );
		const pred = game.take()?.castPrediction;
		assert.ok(
			!pred || pred.cancelledAtMs === 3150,
			"single-prediction latch must prevent re-prediction after B245 refusal"
		);
	}
	assert.equal( game.take()?.castPrediction, undefined, "cancelled prediction must finish blending out" );
	game.dispose();
});

test("queued prediction does not fire when measured oneWayMs is zero", () => {
	const { game, actor, mob, spellA, spellB } = createQueuedChainGame();
	// Immediate same-timestamp answer yields RTT=0, oneWayMs=0.
	game.command( { kind: "skill", skillId: spellA.id, gid: mob.gid }, 100, mob, actor );
	game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( spellA.id, actor.gid, 601, mob.gid ) }, 100 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 100 );
	game.command( { kind: "skill", skillId: spellB.id, gid: mob.gid }, 500, mob, actor );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 500 );

	game.step( 3100, actor );
	assert.equal( game.take()?.castPrediction, undefined, "zero latency must not predict early" );
	game.dispose();
});

test("queued prediction ignores lingering finished aura cast and anchors on the active blocking cast", () => {
	const aura = queuedChainSkillRef( 9130, { actionMs: 400, targetRequired: false } );
	const { game, actor, mob, spellA, spellB } = createQueuedChainGame( { extraSkills: [ aura ] } );
	// Primer press at 0->200 to seed oneWayMs = 100 and start lingering aura.
	game.command( { kind: "skill", skillId: aura.id }, 0, undefined, actor );
	game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( aura.id, actor.gid, 700, 0 ) }, 200 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 0 ) }, 200 );
	// Aura actionMs (400) finishes at 200 + 400 = 600, while aura cast entry lingers unclosed.
	// Start Spell A at 1000 (answer 1200) and queue Spell B at 1500 (queued ack 1700).
	game.command( { kind: "skill", skillId: spellA.id, gid: mob.gid }, 1000, mob, actor );
	game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( spellA.id, actor.gid, 701, mob.gid ) }, 1200 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 1200 );
	game.command( { kind: "skill", skillId: spellB.id, gid: mob.gid }, 1500, mob, actor );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 1700 );

	// Lingering aura finished at 600 <= queued.sinceMs (1700), so Spell A's end (1200 - 100 + 3000 = 4100) wins.
	game.step( 4099, actor );
	assert.equal( game.take()?.castPrediction, undefined );
	game.step( 4100, actor );
	assert.equal( game.take()?.castPrediction?.skill, spellB.id );
	game.dispose();
});

test("with the Experimental row off, a queued press waits for the server's B245, as natively (#4740)", () => {
	const { game, actor, mob, spellA, spellB } = createQueuedChainGame( { predictedQueuedCast: false } );
	game.command( { kind: "skill", skillId: spellA.id, gid: mob.gid }, 0, mob, actor );
	game.receive( { opcode: 0xb245, payload: buildCastOpenPayload( spellA.id, actor.gid, 101, mob.gid ) }, 200 );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 1 ) }, 200 );
	game.command( { kind: "skill", skillId: spellB.id, gid: mob.gid }, 800, mob, actor );
	game.receive( { opcode: 0xb2cd, payload: Uint8Array.of( 1, 2 ) }, 1000 );
	game.take();
	for ( const now of [ 3100, 3150, 3199 ] ) {
		game.step( now, actor );
		assert.equal( game.take()?.castPrediction, undefined, `no prediction at ${now} with the row off` );
	}
	game.dispose();
});

test("the queued-cast row maps to the gameplay slice and defaults off (#4740)", async () => {
	const { experimentalGameplay, experimentalOptions } = await import(
		"../../src/engine/foundation/ui/experimental-options.ts"
	);
	assert.equal( experimentalGameplay( experimentalOptions( {} ) ).predictedQueuedCast, false );
	assert.equal(
		experimentalGameplay( experimentalOptions( { predictedQueuedCast: true } ) ).predictedQueuedCast,
		true
	);
});
