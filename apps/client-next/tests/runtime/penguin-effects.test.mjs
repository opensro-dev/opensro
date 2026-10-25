/*
===========================================================================
penguin-effects.test.mjs - original frost geometry and admitted ice flights.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
const {
	createPenguinBolts,
	penguinEffectActors,
	PENGUIN_ICE_SHARD,
	PENGUIN_ICE_LANCE,
	PENGUIN_FROST_SIGIL,
	PENGUIN_EFFECT_SOUND
} = await import( "../../src/engine/foundation/animation/penguin-effects.ts" );
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createCharacterResources } = await import( "../../src/engine/runtime/characters/resources/resources.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );
const pose = { regionId: 257, x: 20, y: 0, z: 30, yaw: radians( 0 ) };
const ids = Array.from( { length: 17 }, ( _, i ) => 100 + i );

test("all frost signals use original bounded crystal effects and published sounds", () => {
	for ( let action = 0xd0; action <= 0xd6; action++ ) {
		const storm = action >= 0xd3 && action <= 0xd5;
		for ( const age of storm ? [ .1, 1.1, 2.1, 3.1 ] : [ .1, .3 ] ) {
			const actors = penguinEffectActors( action, age, pose, ids );
			assert.equal( actors.length, storm ? 13 : 8 );
			assert.equal( actors.some( actor => actor.model === PENGUIN_FROST_SIGIL ), storm );
			assert.equal( new Set( actors.map( actor => actor.gid ) ).size, actors.length );
			assert.ok(
				actors.every( actor => actor.model === PENGUIN_ICE_SHARD || actor.model === PENGUIN_FROST_SIGIL )
			);
			assert.ok(
				actors.every( actor =>
					actor.opacity !== undefined && actor.opacity >= 0 && actor.opacity <= 1 &&
					Number.isFinite( actor.pose.y )
				)
			);
		}
		assert.deepEqual( penguinEffectActors( action, storm ? 4 : .55, pose, ids ), [] );
		assert.ok( readPublishedAssetBytesSync( PENGUIN_EFFECT_SOUND[action].path, CLIENT_PUBLIC_ROOT ).length > 44 );
	}
	assert.deepEqual( penguinEffectActors( 0xcf, .2, pose, ids ), [] );
	assert.deepEqual( penguinEffectActors( 0xd7, .2, pose, ids ), [] );
});

test("crystal and snowflake resources are generated locally with valid geometry and gradients", () => {
	const built = new Map();
	const owner = createCharacterResources(
		{
			progress: () => null,
			health: () => ({ phase: "running" }),
			textureFeatures() {},
			install() {},
			dispose() {},
			available: () => 1,
			request() {
				throw Error( "must not fetch retail effects" );
			},
			take: () => null,
			cancel() {}
		},
		{
			setCharacterModel( path, model, images ) {
				built.set( path, { model, images } );
			},
			retainCharacterModels() {},
			setCharacterAnimation: () => 0
		},
		"http://fixture.invalid"
	);
	owner.begin( 0 );
	for ( const path of [ PENGUIN_ICE_SHARD, PENGUIN_ICE_LANCE, PENGUIN_FROST_SIGIL ] ) {
		assert.equal( owner.ready( path ), true );
		const { model, images } = built.get( path ), geometry = model.primitives[0].geometry;
		assert.ok( geometry.indices.every( index => index < geometry.positions.length / 3 ) );
		assert.ok( geometry.positions.every( Number.isFinite ) );
		assert.equal( images[0].levels[0].length, images[0].width * images[0].height * 4 );
		assert.ok( new Set( images[0].levels[0] ).size > 20 );
		if ( path === PENGUIN_FROST_SIGIL ) {
			for ( let i = 0; i < geometry.positions.length; i += 3 ) {
				const radius = Math.hypot( geometry.positions[i], geometry.positions[i + 2] );
				assert.ok( radius > .87 && radius < .95, "ring has no filled center or square corners" );
			}
			const pixels = images[0].levels[0];
			assert.deepEqual( [ ...pixels.slice( 0, 4 ) ], [ 0, 0, 0, 0 ] );
		}
	}
	owner.dispose();
});

test("basic casts fly small ice bolts, echoes have separate IDs, and canceled/disabled casts do not shoot", () => {
	let serial = 0;
	const owner = createPenguinBolts( () => ++serial );
	/** @type {Map<number, import("../../src/engine/contracts/world").EntityState>} */
	const entities = new Map( [
		[ 1, { gid: 1, name: "Penguin", kind: "cos", refObjId: 23028, ...pose, heading: 0 } ],
		[ 2, { gid: 2, name: "Target", kind: "monster", refObjId: 1956, ...pose, x: 120, heading: 0 } ]
	] );
	const cast = { token: 7, caster: 1, target: 2, skill: 25303, damage: 0, fatal: false };
	const step = ( casts, at, pet = 1 ) => owner.step( casts, pet, entities, new Map(), at );
	assert.equal( step( [ cast ], 0 ).length, 0 );
	const bolt = step( [ cast ], .35 );
	assert.equal( bolt.length, 1 );
	assert.equal( bolt[0].model, PENGUIN_ICE_SHARD );
	assert.ok( bolt[0].pose.x > 20 && bolt[0].pose.x < 120 );
	assert.equal( bolt[0].scale, 2 );
	assert.ok( step( [ cast ], .7 ).every( actor => actor.model !== PENGUIN_FROST_SIGIL ) );
	assert.deepEqual( step( [ cast ], 2 ), [] );
	const echo = { ...cast, token: 8, shotAtMs: 2000 };
	step( [ cast, echo ], 2 );
	assert.notEqual( step( [ cast, echo ], 2.1 )[0].gid, bolt[0].gid );
	owner.reset();
	assert.deepEqual( step( [ { ...cast, cancelledAtMs: 0 } ], 3 ), [] );
	assert.deepEqual( step( [ cast ], 3, 0 ), [] );
	assert.deepEqual( step( [ { ...cast, skill: 1 } ], 3 ), [] );
	const target = entities.get( 2 );
	assert.ok( target );
	entities.set( 2, { ...target, regionId: 0x8001 } );
	assert.deepEqual( step( [ cast ], 3 ), [] );
});

test("level-19 Ice Lance I latches a spear and trail through queue clearing and its echo", () => {
	let serial = 0;
	const owner = createPenguinBolts( () => ++serial );
	/** @type {Map<number, import("../../src/engine/contracts/world").EntityState>} */
	const entities = new Map( [
		[ 1, { gid: 1, name: "Penguin", kind: "cos", refObjId: 23028, ...pose, heading: 0 } ],
		[ 2, { gid: 2, name: "Target", kind: "monster", refObjId: 1956, ...pose, x: 120, heading: 0 } ]
	] );
	const cast = { token: 1, caster: 1, target: 2, skill: 25232, damage: 0, fatal: false };
	owner.step( [ cast ], 1, entities, new Map(), 0, 31 );
	const actors = owner.step( [ cast ], 1, entities, new Map(), .35 );
	assert.equal( actors[0].model, PENGUIN_ICE_LANCE );
	assert.equal( actors.length, 4 );
	const echo = { ...cast, token: 2, shotAtMs: 400 };
	owner.step( [ cast, echo ], 1, entities, new Map(), .4 );
	assert.ok(
		owner.step( [ cast, echo ], 1, entities, new Map(), .5 ).some( actor => actor.model === PENGUIN_ICE_LANCE )
	);
	owner.reset();
	owner.step( [ cast ], 1, entities, new Map(), 1, 36 );
	assert.equal( owner.step( [ cast ], 1, entities, new Map(), 1.35 )[0].model, PENGUIN_ICE_SHARD );
});

test("Blizzard remains at its cast position after victim despawn, then expires", () => {
	let serial = 0;
	const jobs = new Map();
	const owner = createCharacterEffects(
		{
			progress: () => null,
			health: () => ({ phase: "running" }),
			textureFeatures() {},
			install() {},
			dispose() {},
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog: {} } :
						{
							kind: "bytes",
							buffer: new TextEncoder().encode(
								JSON.stringify( { format: "sro-skill-stage-models", models: {} } )
							).buffer
						}
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
		},
		"http://fixture.invalid",
		() => {},
		{
			range: () => 0,
			curve() {
				throw Error( "unexpected curve" );
			},
			orb() {
				throw Error( "unexpected orb" );
			},
			flicker: state => state,
			sky: () => ({ vertices: [] })
		}
	);
	const entity = { gid: 2, kind: "monster", ...pose, heading: 0, emote: { action: 0xd5, revision: 1 } };
	const step = ( entities, at ) =>
		owner.step(
			entities,
			{
				casts: [],
				vitals: [],
				questMarkers: [],
				inventory: [],
				attachedEffects: [],
				homingProjectiles: true,
				localGid: 0,
				pose: { ...pose, angle: 0 }
			},
			at,
			() => true,
			() => .8,
			[],
			undefined,
			[]
		);
	step( [ entity ], 0 );
	const field = step( [], 1.1 ).filter( actor => actor.model === PENGUIN_FROST_SIGIL );
	assert.equal( field.length, 1 );
	assert.equal( field[0].pose.x, 20 );
	assert.equal( field[0].pose.z, 30 );
	assert.equal( step( [], 4.1 ).filter( actor => actor.model === PENGUIN_FROST_SIGIL ).length, 0 );
	owner.dispose();
});
