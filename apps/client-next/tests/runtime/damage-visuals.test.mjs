/*
===========================================================================

damage-visuals.test.mjs - tests for effects.ts, hit-light.ts,
damage-anchor.ts, effects.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { defined } from "../helpers/defined.mjs";
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" ),
	{ createHitLights } = await import( "../../src/engine/foundation/animation/hit-light.ts" );
test("native hit light seeds diffuse then ticks twice-RGB diffuse and unattenuated scaled ambient, expires and replaces", () => {
	const owner = createHitLights(),
		definition = { color: [ 1, .5, 0 ], duration: .3, range: 1000, attenuation: .2 },
		pose = { regionId: 257, x: 1, y: 2, z: 3 },
		alive = new Set( [ 1 ] );
	owner.start( 1, definition, pose, 0 );
	assert.deepEqual( defined( owner.get( 1 ) ).ambient, [ 0, 0, 0 ] );
	assert.deepEqual( defined( owner.get( 1 ) ).diffuse, [ 1, .5, 0 ] );
	owner.step( .15, alive );
	assert.ok( Math.abs( defined( owner.get( 1 ) ).diffuse[0] - 1 ) < 1e-6 );
	assert.ok( Math.abs( defined( owner.get( 1 ) ).ambient[0] - .5 / 1.67 ) < 1e-6 );
	owner.start( 1, { ...definition, color: [ 0, 0, 1 ] }, pose, .15 );
	assert.deepEqual( defined( owner.get( 1 ) ).diffuse, [ 0, 0, 1 ] );
	owner.step( .46, alive );
	assert.equal( owner.get( 1 ), undefined );
	owner.start( 1, { ...definition, duration: 0 }, pose, 1 );
	owner.step( 1.5, alive );
	assert.ok( owner.get( 1 ) );
	owner.step( 2.01, alive );
	assert.equal( owner.get( 1 ), undefined );
});
test("damage primary gate encloses both hit light and secondary with kind-7, projectile and blood-option branches", () => {
	for (
		const [kind, projectile, secondary, defensive, primary, bloodOn, paths, tinted] of [
			[ 0, false, true, false, true, true, [ "primary", "red" ], true ],
			[ 7, false, true, false, true, true, [ "primary" ], true ],
			[ 0, true, false, false, true, true, [ "primary" ], false ],
			[ 0, true, true, false, true, false, [ "primary", "green" ], false ],
			[ 0, false, true, true, true, true, [ "defense", "red" ], false ],
			[ 0, false, true, false, false, true, [], false ]
		]
	) {
		let id = 0;
		const jobs = new Map(),
			record = {
				clips: [],
				stages: [],
				damageEffect: primary ? "primary.efp" : null,
				attachedAction: { defense: "defense.efp", attack: "primary.efp", priority: 1 },
				hitLight: { color: [ 1, 0, 0 ], duration: .3, range: 1000, attenuation: .2 }
			};
		const fx = createCharacterEffects(
			{
				available: () => 4,
				request( url, limit, decode ) {
					jobs.set(
						++id,
						decode === "effects" ?
							{ kind: "effects", catalog: { 7: record } } :
							{
								kind: "bytes",
								buffer: new TextEncoder().encode(
									JSON.stringify( { format: "sro-skill-stage-models", models: {} } )
								).buffer
							}
					);
					return id;
				},
				take( id ) {
					const r = jobs.get( id );
					jobs.delete( id );
					return r;
				},
				cancel() {}
			},
			"http://fixture.invalid",
			() => {},
			{ range: () => 0 }
		);
		const entities = [ 1, 2 ].map( gid => ({ gid, regionId: 257, x: 0, y: 0, z: 0, heading: 0 }) ),
			step = at => fx.step( entities, { casts: [] }, at, () => true, () => 1 );
		step( 0 );
		step( .1 );
		step( .2 );
		const basis = [ 2, 0, 0, 0, 3, 0, 0, 0, 4 ], pose = { regionId: 257, x: 4, y: 5, z: 6, yaw: 0 };
		fx.damage(
			2,
			1,
			kind,
			7,
			defensive,
			projectile,
			secondary,
			pose,
			basis,
			[ "red.efp", "green.efp" ],
			bloodOn,
			1
		);
		assert.equal( !!fx.appearance( 2 ).pointLight, tinted );
		assert.equal( !!fx.appearance( 1 ).pointLight, tinted );
		const actors = step( 1 );
		assert.deepEqual(
			actors.map( a => decodeURIComponent( a.model.split( "#" )[1] ).replace( ".efp", "" ) ).sort(),
			paths.sort()
		);
		for ( const a of actors ) {
			assert.deepEqual( a.pose, pose );
			assert.deepEqual( a.effectBasis, basis );
		}
		fx.reset();
		assert.equal( fx.appearance( 2 ).pointLight, undefined );
		fx.dispose();
	}
});

const { damageAnchor } = await import( "../../src/engine/foundation/animation/damage-anchor.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
test("published basic melee lights both participants; incoming Mobia and Mangyang hits do not invent a light", () => {
	const catalog = createEffectDecoder().decode(
		readPublishedAssetBytesSync(
			"/assets/skill/effectRecords.json",
			CLIENT_PUBLIC_ROOT
		)
	);
	let serial = 0;
	const jobs = new Map();
	const fx = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, decode ) {
				jobs.set(
					++serial,
					decode === "effects" ?
						{ kind: "effects", catalog } :
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
				const value = jobs.get( id );
				jobs.delete( id );
				return value;
			},
			cancel() {}
		},
		"http://fixture.invalid",
		() => {},
		{ range: () => 0 }
	);
	const entities = [ 1, 2, 3 ].map( gid => ({ gid, regionId: 257, x: 0, y: 0, z: 0, heading: 0 }) );
	const step = now => fx.step( entities, { casts: [] }, now, () => true, () => 1 ),
		pose = { regionId: 257, x: 4, y: 5, z: 6, yaw: 0 };
	step( 0 );
	step( .1 );
	step( .2 );
	const damage = ( skill, caster, target, now ) => {
		const route = fx.impactSource( caster, target, [], { caster, skill, receivedAtMs: now * 1000 } );
		assert.equal( route.skill, skill );
		fx.damage(
			target,
			caster,
			0,
			route.skill,
			route.defensive,
			false,
			true,
			pose,
			[ 1, 0, 0, 0, 1, 0, 0, 0, 1 ],
			undefined,
			true,
			now
		);
	};
	try {
		for ( const skill of [ 1, 2, 40, 7127, 7128 ] ) {
			assert.deepEqual( defined( catalog[skill].hitLight ).color, [ 1, 1, 1 ] );
			damage( skill, 1, 2, 1 );
			assert.deepEqual( defined( fx.appearance( 1 ).pointLight ).diffuse, [ 1, 1, 1 ] );
			assert.deepEqual( defined( fx.appearance( 2 ).pointLight ).diffuse, [ 1, 1, 1 ] );
			assert.equal( fx.appearance( 3 ).pointLight, undefined );
			step( 1.15 );
			const fading = fx.appearance( 1 ).pointLight;
			assert.ok( defined( fading ).ambient[0] > 0 );
			// The counter-hit must neither start a light nor restart the outgoing one.
			damage( 3598, 2, 1, 1.15 );
			assert.equal( fx.appearance( 1 ).pointLight, fading );
			step( 1.31 );
			assert.equal( fx.appearance( 1 ).pointLight, undefined );
			assert.equal( fx.appearance( 2 ).pointLight, undefined );
		}
		for ( const skill of [ 160, 161, 3598, 3599, 3600, 3601 ] ) {
			assert.equal( catalog[skill].hitLight, undefined );
			damage( skill, 2, 1, 2 );
			assert.equal( fx.appearance( 1 ).pointLight, undefined );
			assert.equal( fx.appearance( 2 ).pointLight, undefined );
		}
	} finally {
		fx.dispose();
	}
});
test("damage anchor keeps native source ray, missing bone fallback, saddle transform and zero-length branch", () => {
	const target = { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 }, caster = { ...target, z: 100 };
	assert.deepEqual( damageAnchor( target, caster, [ 99, 5, 10 ] ), { ...target, y: 5, z: -10 } );
	assert.deepEqual( damageAnchor( target, target, [ 99, 5, 10 ] ), { ...target, y: 5 } );
	const bone = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 2, 3, -4, 1 ),
		saddle = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 7, -8, 1 );
	assert.deepEqual( damageAnchor( target, caster, [ 1, 5, 10 ], bone ), { ...target, y: 8, z: -14 } );
	assert.deepEqual( damageAnchor( target, caster, [ 1, 5, 10 ], bone, saddle ), { ...target, y: 15, z: -22 } );
});
