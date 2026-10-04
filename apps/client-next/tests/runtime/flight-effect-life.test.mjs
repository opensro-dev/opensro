/*
===========================================================================

flight-effect-life.test.mjs - tests for effects.ts: a flying effect lives
until its arrival

Cold Wave Arrest (skill 96) flies cold_gigongjang_shot_a.efp, a 10-frame
program, at 200 units a second. Played once it vanished mid-air, about half
a second before the impact effect appeared at the target.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const manifest = JSON.parse( readFileSync( "../../.generated/client-public/assets/skillfx/manifest.json", "utf8" ) );
const raw = JSON.parse( readFileSync( "../../.generated/client-public/assets/skill/effectRecords.json", "utf8" ) );
const COLD_WAVE_ARREST = 96;
const SHOT = "cold_gigongjang_shot_a.efp", HIT = "cold_gigongjang_hit_a.efp";

/*
================
castAt

Steps a Cold Wave Arrest cast at a monster distance units away and returns
the effect names drawn at each step time.
================
*/
function castAt( distance ) {
	const catalog = createEffectDecoder().decode(
		new TextEncoder().encode( JSON.stringify( { [COLD_WAVE_ARREST]: raw[COLD_WAVE_ARREST] } ) )
	);
	let serial = 0;
	const jobs = new Map();
	// The stub loader covers only what the presenter calls here.
	const owner = createCharacterEffects(
		/** @type {any} */ ({
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog } :
						{ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer }
				);
				return serial;
			},
			take( id ) {
				const job = jobs.get( id );
				jobs.delete( id );
				return job;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		}),
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	/** @type {any[]} */
	const entities = [ { gid: 1, kind: "local-player", regionId: 257, x: 0, y: 0, z: 0, heading: 0 }, {
		gid: 2,
		kind: "monster",
		regionId: 257,
		x: distance,
		y: 0,
		z: 0,
		heading: 0
	} ];
	const cast = {
		token: 1,
		caster: 1,
		target: 2,
		skill: COLD_WAVE_ARREST,
		receivedAtMs: 0,
		results: [ { target: 2, impacts: [ { damage: 10 } ] } ]
	};
	/** @type {any} Only the fields the presenter reads. */
	const game = { localGid: 1, inventory: [], casts: [ cast ] };
	const step = ( now, events = [] ) =>
		owner.step(
			entities,
			game,
			now,
			() => true,
			() => 10,
			events,
			// Every socket sits at its actor's feet.
			/** @type {any} */ (gid => ({ regionId: 257, x: gid === 1 ? 0 : distance, y: 0, z: 0, yaw: 0 })),
			[],
			2
		);
	step( 0 );
	step( .1 );
	const frames = [];
	for ( let i = 0; i <= 40; i++ ) {
		const now = .2 + i * .05;
		const actors = step( now, i === 0 ? [ { cast, phase: "SHOT", event: 1, at: .2 } ] : [] );
		frames.push( {
			now,
			drawn: actors.map( actor => decodeURIComponent( actor.model ?? "" ) ),
			looping: actors.some( actor => decodeURIComponent( actor.model ?? "" ).endsWith( SHOT ) && actor.loop )
		} );
	}
	return frames;
}

test("the projectile is drawn, looping, until the impact replaces it", () => {
	// 150 units at 200 a second: 0.75 s of flight against a 0.33 s program.
	const frames = castAt( 150 );
	const impact = frames.findIndex( frame => frame.drawn.some( name => name.endsWith( HIT ) ) );
	assert.ok( impact > 0, "no impact effect" );
	for ( const frame of frames.slice( 0, impact ) ) {
		assert.ok( frame.drawn.some( name => name.endsWith( SHOT ) ), `no projectile at ${frame.now.toFixed( 2 )} s` );
		assert.ok( frame.looping, `a one-shot projectile at ${frame.now.toFixed( 2 )} s` );
	}
});
