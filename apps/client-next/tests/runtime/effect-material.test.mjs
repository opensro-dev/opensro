/*
===========================================================================

effect-material.test.mjs - tests for effect-script.ts, effect-material.ts,
effects.ts, random.ts, ...

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { defined } from "../helpers/defined.mjs";
const { effectScript } = await import( "../../src/engine/foundation/animation/effect-script.ts" );
const { stepMaterial, stageEffectScale, stepHwanScale } = await import(
	"../../src/engine/foundation/animation/effect-material.ts"
);
const { createCharacterEffects } = await import( "../../src/engine/runtime/characters/effects/effects.ts" );
const { createPresentationRandom } = await import( "../../src/engine/runtime/random/random.ts" );
const { createEffectDecoder } = await import( "../../src/engine/runtime/assets/worker/effects/effects.ts" );
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const pose = { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 };
const stage = {
	phase: "ACT_L",
	resource: null,
	action: "AT_ONE_FOLLOW",
	move: "MOV_NONE",
	scripts: [ "SCT_MAT", "64", "16", "0", "140", "32", "0", "1000" ],
	startEvent: 0,
	count: 1,
	offset: [ 0, 0, 0 ],
	life: 0
};
function fixture( catalog = { 1: { stages: [ stage ] } } ) {
	let serial = 0;
	const jobs = new Map(), sounds = [];
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, type ) {
				jobs.set(
					++serial,
					type === "effects" ?
						{ kind: "effects", catalog } :
						{
							kind: "bytes",
							buffer: new TextEncoder().encode(
								JSON.stringify( {
									format: "sro-skill-stage-models",
									models: {
										"model.bsr": {
											glb: "/assets/model.glb",
											clips: [ "effect" ],
											clipLoop: false
										}
									}
								} )
							).buffer
						}
				);
				return serial;
			},
			take( id ) {
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel() {}
		},
		"http://localhost",
		v => sounds.push( v ),
		createPresentationRandom( 7 )
	);
	const entities = [ 1, 2 ].map( gid => ({
			gid,
			kind: "player",
			regionId: 257,
			x: gid === 1 ? 0 : 100,
			y: 0,
			z: 0,
			heading: 0
		})
		),
		bodies = entities.map( e => ({ gid: e.gid, pose, scale: 1, heightFactor: 2, height: 40 }) );
	const game = { casts: [], attachedEffects: [ { gid: 1, skill: 1, token: 7, phase: 0 } ] };
	const step = ( at, triggers = [], ready = true ) =>
		owner.step( entities, game, at, () => ready, () => 1, triggers, undefined, bodies );
	step( 0 );
	step( .1 );
	step( .2 );
	return { owner, game, step, entities, bodies, sounds };
}
test("234 original x86 material samples match including float stores, unsigned wrap and long-frame clamping", () => {
	const reference = JSON.parse( readFileSync( "tests/fixtures/native/material-reference.json", "utf8" ) );
	assert.equal( reference.binarySha256, "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" );
	let count = 0;
	for ( const row of reference.cases ) {
		let clock = { elapsed: 0, forward: true };
		const script = { kind: "material", from: row.start, to: row.end, durationMs: row.duration };
		for ( const expected of row.samples ) {
			const { delta, ...answer } = expected;
			const actual = stepMaterial( script, clock, delta );
			assert.deepEqual( actual, answer, JSON.stringify( { row, expected } ) );
			clock = actual;
			count++;
		}
	}
	assert.equal( count, 234 );
});
test("scale scripts are decoded but retail-inert; HWAN uses its state transition instead", () => {
	assert.deepEqual( effectScript( [ "SCT_CHAR_SCALE", "1.1", "1000" ] ), {
		kind: "parsed-scale",
		operation: "SCT_CHAR_SCALE",
		scale: Math.fround( 1.1 ),
		durationSeconds: 1
	} );
	assert.deepEqual( effectScript( [ "SCT_EFFECT_SCALE", "2" ] ), {
		kind: "parsed-scale",
		operation: "SCT_EFFECT_SCALE",
		scale: 2
	} );
	assert.throws( () => effectScript( [ "SCT_MAT", "256", "0", "0", "0", "0", "0", "10" ] ), /parameter/ );
	assert.throws( () => effectScript( [ "SCT_CHAR_SCALE", "bad", "1" ] ), /parameter/ );
	assert.equal( stageEffectScale( "CHAR_BASE", 3, 2 ), 6 );
	assert.equal( stageEffectScale( "MOB_BASE", 3, 2 ), 3 );
	assert.equal( stageEffectScale( null, 3, 2 ), 1 );
	assert.throws( () => stageEffectScale( "CHAR_BASE", 1, undefined ), /height/ );
	assert.equal( stepHwanScale( 1, Math.fround( 1.1 ), 0, .01, true ).value, Math.fround( 1.1 ) );
	assert.equal( stepHwanScale( Math.fround( 1.1 ), 1, 0, 1, false ).value, 1 );
});
test("120 native HWAN transition samples match both directions and mid-transition reversals", () => {
	const rows = JSON.parse( readFileSync( "tests/fixtures/native/material-reference.json", "utf8" ) ).scales;
	assert.equal( rows.length, 120 );
	for ( const row of rows ) {
		assert.deepEqual( stepHwanScale( row.initial, row.target, row.progress, row.delta, row.active ), {
			value: row.value,
			progress: row.next
		}, JSON.stringify( row ) );
	}
});
test("resource-less material stages replace one model register; destroying an older owner resets it without reviving tints", () => {
	const f = fixture( {
		1: { stages: [ stage ] },
		2: { stages: [ { ...stage, scripts: [ "SCT_MAT", "0", "255", "0", "0", "0", "255", "100" ] } ] }
	} );
	const red = effectScript( stage.scripts ).from;
	assert.deepEqual( f.owner.appearance( 1 ).materialTint, red );
	assert.equal( f.owner.appearance( 2 ).materialTint, undefined );
	f.step( .7 );
	assert.deepEqual( f.owner.appearance( 1 ).materialTint, red, "sample before advancing" );
	f.step( .71 );
	assert.notDeepEqual( f.owner.appearance( 1 ).materialTint, red );
	f.game.attachedEffects.push( { gid: 1, skill: 2, token: 8, phase: 0 } );
	f.step( .8 );
	assert.deepEqual( f.owner.appearance( 1 ).materialTint, [ 0, 1, 0 ] );
	f.game.attachedEffects.shift();
	f.step( .9 );
	assert.equal( f.owner.appearance( 1 ).materialTint, undefined );
	f.step( 1 );
	assert.equal( f.owner.appearance( 1 ).materialTint, undefined );
	f.owner.reset();
	assert.deepEqual( f.owner.appearance( 1 ), { weaponHidden: false, materialTint: undefined, scale: 1 } );
	f.owner.dispose();
});
test("all seven published material stages and the HWAN state effect admit with native scale modes", () => {
	const catalog = createEffectDecoder().decode(
		readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json" )
	);
	for ( const skill of [ 10268, 10269, 10270, 10271, 10272, 10275 ] ) {
		const f = fixture( catalog );
		f.game.attachedEffects = [ { gid: 1, skill, token: skill, phase: 0 } ];
		f.step( 1 );
		assert.ok( f.owner.appearance( 1 ).materialTint, JSON.stringify( { skill, error: f.owner.error() } ) );
		f.owner.dispose();
	}
	const f = fixture( catalog );
	f.game.attachedEffects = [];
	f.entities[0].appearanceState = [ 1, 0, 1 ];
	f.step( 20000 );
	const actors = f.step( 20000.02 );
	assert.ok( actors.length > 0 );
	assert.equal( f.owner.appearance( 1 ).scale, Math.fround( 1.1 ) );
	assert.ok( f.owner.appearance( 1 ).materialTint );
	assert.equal( f.owner.error(), null );
	f.entities[0].appearanceState = [ 1, 0, 0 ];
	f.step( 20000.12 );
	assert.equal( f.owner.appearance( 1 ).materialTint, undefined );
	f.step( 20001.12 );
	assert.equal( f.owner.appearance( 1 ).scale, 1 );
	assert.equal( f.owner.error(), null );
	f.owner.dispose();
});
test("resource-less slot kill stops every matching loop; unaffected slots and other casts survive", () => {
	const loop = {
		...stage,
		phase: "SHOT",
		resource: "loop.efp",
		scripts: [],
		action: "AT_LOOP",
		native: { slot: 1, fadeInMs: 200 }
	};
	const f = fixture( {
		1: {
			stages: [ loop, { ...loop, native: { slot: 2 } }, {
				...loop,
				startEvent: 1,
				resource: null,
				native: { kill: 1 }
			} ]
		}
	} );
	f.game.attachedEffects = [];
	const cast = { token: 1, caster: 1, target: 2, skill: 1 };
	f.game.casts = [ cast ];
	assert.equal( f.step( 1, [ { cast, phase: "SHOT", event: 0, at: 1 } ] ).length, 2 );
	assert.equal( f.step( 10 ).length, 2 );
	const released = f.step( 10, [ { cast, phase: "SHOT", event: 1, at: 10 } ] );
	assert.equal( released.length, 2 );
	assert.equal( released.filter( a => a.emissionEnd !== undefined ).length, 1 );
	assert.equal( f.step( 10.25 ).length, 1 );
	f.game.casts = [];
	f.step( 11 );
	assert.equal( f.step( 12.1 ).length, 0 );
	assert.equal( f.owner.error(), null );
	f.owner.dispose();
});
test("trade transfers first staged payload to independent flight without replacing its resource or clock", () => {
	const start = {
		...stage,
		phase: "SHOT",
		resource: "retained.efp",
		scripts: [],
		action: "AT_LOOP",
		native: { slot: 1 }
	};
	const launch = {
		...start,
		startEvent: 1,
		resource: "new.efp",
		action: "AT_MOV_1TAR",
		move: "MOV_STRAIGHT",
		movement: { delayMs: 0, startSpeed: 100, endSpeed: 100 },
		native: { trade: 1 }
	};
	const f = fixture( { 1: { stages: [ start, launch ] } } );
	f.game.attachedEffects = [];
	const cast = { token: 1, caster: 1, target: 2, skill: 1 };
	f.game.casts = [ cast ];
	const first = f.step( 1, [ { cast, phase: "SHOT", event: 0, at: 1 } ] )[0];
	const flying = f.step( 1.5, [ { cast, phase: "SHOT", event: 1, at: 1.5 } ] );
	assert.equal( flying.length, 1 );
	assert.equal( flying[0].gid, first.gid );
	assert.equal( flying[0].model, first.model );
	assert.equal( flying[0].time, .5 );
	f.game.casts = [];
	const next = f.step( 1.75 );
	assert.equal( next[0].pose.x, 25 );
	assert.equal( next[0].time, .75 );
	assert.equal( f.step( 2.5 ).length, 0 );
	assert.equal( f.owner.error(), null );
	f.owner.dispose();
});
test("cold height metadata retains the callback and captures scale once; despawn clears model material state", () => {
	const scaled = {
		...stage,
		phase: "SHOT",
		resource: "scaled.efp",
		scripts: [],
		action: "AT_ONE_FOLLOW",
		native: { scale: "CHAR_BASE" }
	};
	const f = fixture( { 1: { stages: [ stage, scaled ] } } ), cast = { token: 3, caster: 1, target: 2, skill: 1 };
	f.game.casts = [ cast ];
	delete f.bodies[0].heightFactor;
	f.bodies[0].scale = 3;
	assert.equal( f.step( 1, [ { cast, phase: "SHOT", event: 0, at: 1 } ] ).length, 0 );
	assert.equal( f.owner.error(), null );
	f.bodies[0].heightFactor = 2;
	const actor = f.step( 1.1 )[0];
	assert.equal( actor.scale, 6 );
	assert.equal( actor.absoluteEffectScale, true );
	f.bodies[0].scale = 10;
	assert.equal( f.step( 1.2 )[0].scale, 6 );
	f.entities.shift();
	f.step( 1.3 );
	assert.equal( f.owner.appearance( 1 ).materialTint, undefined );
	f.owner.dispose();
});
test("one-shot attached material owners reset after draining even while their deduplication tombstone remains", () => {
	const f = fixture();
	f.game.attachedEffects = [];
	f.step( .3 );
	f.game.attachedEffects = [ { gid: 1, skill: 1, token: 0, phase: 0 } ];
	f.step( .4 );
	assert.equal( f.owner.appearance( 1 ).materialTint, undefined );
	f.step( .5 );
	assert.equal( f.owner.appearance( 1 ).materialTint, undefined );
	f.owner.dispose();
});
const I = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
test("GPU actor snapshots isolate tints and retire tint batches; absolute EFP size never inherits parent scale twice", () => {
	const owner = createCharacters(),
		model = {
			nodes: [ {
				name: "root",
				parent: -1,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ 1, 1, 1 ]
			} ],
			images: [],
			clips: [],
			primitives: [ {
				name: "body",
				node: 0,
				image: -1,
				joints: [ 0 ],
				inverseBind: I(),
				geometry: {
					positions: Float32Array.of( -1, -1, 0, 1, -1, 0, 0, 1, 0 ),
					indices: Uint32Array.of( 0, 1, 2 ),
					transform: I()
				}
			} ]
		};
	owner.model( "body", model, [] );
	const gpu = {
		upload( data ) {
			return { material: data.material, instances: data.instances.slice() };
		},
		updateInstances( draw, value, opacity, appearance ) {
			draw.instances = value.slice();
			draw.appearance = appearance?.slice();
			return draw;
		},
		updateBones() {},
		release() {}
	};
	const a = { gid: 1, model: "body", pose, scale: 3, time: 0, clip: "", loop: false, materialTint: [ .5, .25, 1 ] },
		b = { ...a, gid: 2, materialTint: undefined },
		effect = {
			...b,
			gid: 3,
			scale: 2,
			absoluteEffectScale: true,
			attachment: { gid: 1, bone: "", root: true, offset: [ 1, 0, 0 ] }
		};
	owner.actors( [ a, b, effect ] );
	a.materialTint[0] = 0;
	let draws = owner.prepare( gpu, {}, 257 );
	const tint = draws.find( d => d.material.instanceMaterialTint );
	assert.deepEqual( [ ...defined( tint ).appearance.slice( 0, 4 ) ], [ .5, .25, 1, 1 ] );
	const normal = draws.find( d => !d.material.instanceMaterialTint );
	assert.equal( defined( normal ).instances[16], 2 );
	assert.equal( defined( normal ).instances[28], 3 );
	owner.actors( [ { ...a, materialTint: undefined }, b ] );
	draws = owner.prepare( gpu, {}, 257 );
	assert.equal( draws.length, 1 );
	assert.equal( draws[0].material.instanceMaterialTint, false );
	assert.equal( draws[0].appearance, undefined );
	owner.dispose( gpu, null );
});
