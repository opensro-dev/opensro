/*
===========================================================================

effect-script.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
async function load( source ) {
	return import( sourceFileUrl( source ).href );
}
const { effectScript, hitRotation, projectNativeCommandRotation } = await load(
	"src/engine/foundation/animation/effect-script.ts"
);
const { createCharacterEffects } = await load( "src/engine/runtime/characters/effects/effects.ts" );
const { createEffectDecoder } = await load( "src/engine/runtime/assets/worker/effects/effects.ts" );
const { createPresentationRandom } = await load( "src/engine/runtime/random/random.ts" );
test("retail signed rotation selects weapon fallback; positive and axis-band values stay authored", () => {
	const neg = effectScript( [ "SCT_RUT", "-1" ] );
	assert.ok( neg.radians < 0 );
	for ( const value of [ 0, neg.radians ] ) {
		assert.equal( hitRotation( value, 3 << 11, 2, 0 ).rotation.angle, Math.fround( 45 * .01745329238474369 ) );
	}
	const authored = effectScript( [ "SCT_RUT", "90" ] );
	assert.equal( hitRotation( authored.radians, 3 << 11, 2, 0 ).rotation.angle, Math.fround( Math.PI / 2 ) );
	const x = hitRotation( effectScript( [ "SCT_RUT", "630" ] ).radians, undefined, 0, 0 );
	assert.equal( x.rotation.axis, "x" );
	assert.ok( Math.abs( x.rotation.angle - 3 * Math.PI / 2 ) < 1e-6 );
	assert.equal( hitRotation( 0, 4 << 11, 2, 0 ).pierce, true );
	assert.equal( hitRotation( 0, 4 << 11, 17, 0 ).pierce, false );
	assert.throws( () => effectScript( [ "SCT_RUT" ] ), /parameter/ );
	assert.throws( () => effectScript( [ "SCT_RUT", "bad" ] ), /parameter/ );
	assert.deepEqual( effectScript( [ "SCT_ARROW" ] ), { kind: "arrow" } );
});
test("published stage decoding retains lifecycle controls and negative fade sentinels", () => {
	const bytes = readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json" ),
		raw = JSON.parse( bytes ),
		decoded = createEffectDecoder().decode( bytes );
	let negative = 0, kills = 0;
	for ( const [id, record] of Object.entries( raw ) ) {
		record.authoredStages.forEach( ( stage, index ) => {
			const control = decoded[id].stages[index].native;
			assert.equal( control.slot, stage.id );
			assert.equal( control.kill, stage.kill );
			assert.equal( control.trade, stage.trade );
			assert.equal( control.rotation, stage.rotate );
			assert.equal( control.fadeOutMs, stage.fadeOutMs );
			assert.deepEqual( control.damageTypes, stage.damageTypes );
			assert.equal( control.scale, stage.scale );
			if ( control.fadeOutMs < 0 ) negative++;
			if ( control.kill ) kills++;
		} );
	}
	assert.ok( negative > 0 );
	assert.equal( kills, 1024 );
});

test("real sword stage waits for native anchor metadata, renders at the target anchor, and reports other script branches explicitly", () => {
	const catalog = createEffectDecoder().decode(
		readFileSync( CLIENT_PUBLIC_ROOT + "/assets/skill/effectRecords.json" )
	);
	let id = 0;
	const jobs = new Map();
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, kind ) {
				jobs.set(
					++id,
					kind === "effects" ?
						{ kind: "effects", catalog } :
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
			cancel( id ) {
				jobs.delete( id );
			}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ {
		gid: 1,
		regionId: 257,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		equipment: [ { slot: 6, typeFlags: 3 << 11 } ]
	}, { gid: 2, refObjId: 1933, regionId: 257, x: 10, y: 0, z: 0, heading: 0 } ];
	const cast = { token: 5, caster: 1, target: 2, skill: 2 }, game = { casts: [ cast ] }, bodies = [ { gid: 2 } ];
	const trigger = { cast, phase: "SHOT", event: 1, at: .2, attackKind: 2 };
	const step = ( t, events = [] ) => owner.step( entities, game, t, () => true, () => 1, events, undefined, bodies );
	step( 0 );
	step( .1 );
	assert.deepEqual( step( .2, [ trigger ] ), [] );
	assert.equal( owner.error(), null );
	bodies[0].effectAnchor = { bone: null, offset: [ 0, 10, -6 ] };
	const rows = step( .3 );
	assert.equal( rows.length, 1 );
	assert.equal( rows[0].pose.x, 4 );
	assert.equal( rows[0].pose.y, 10 );
	assert.equal( rows[0].effectRotation.axis, "z" );
	assert.equal( rows[0].pickable, false );
	assert.equal( owner.error(), null );
	assert.equal( step( .4, [ trigger ] ).length, 1, "a repeated snapshot cannot duplicate a stage" );
	assert.equal( step( 2 ).length, 0 );
	owner.dispose();
});

test("unsupported attachment scripts reject their own stage without hiding supported system effects", () => {
	const stage = {
		resource: "system/system_levelup.efp",
		phase: "ACT_S",
		startEvent: 0,
		action: "AT_ONE_FOLLOW",
		move: "MOV_NONE",
		offset: [ 0, 0, 0 ],
		life: 1,
		count: 1,
		scripts: []
	};
	const catalog = { "1": { stages: [ { ...stage, scripts: [ "SCT_UNKNOWN" ] } ] }, "2": { stages: [ stage ] } };
	let serial = 0;
	const jobs = new Map();
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, kind ) {
				jobs.set(
					++serial,
					kind === "effects" ?
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
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel() {}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const entities = [ { gid: 7 } ],
		body = { gid: 7, pose: { regionId: 257, x: 0, y: 0, z: 0, yaw: 0 } },
		game = { casts: [], attachedEffects: [ { gid: 7, skill: 1, token: 1, phase: 0 } ] };
	owner.system( 7, 2 );
	let rows = [];
	for ( let i = 0; i < 4; i++ ) {
		rows = owner.step( entities, game, i / 10, () => true, () => 1, [], undefined, [ body ] );
	}
	assert.equal( rows.length, 1 );
	assert.match( rows[0].model, /system_levelup/ );
	assert.match( owner.error(), /attached:1.*SCT_UNKNOWN/ );
	owner.dispose();
});

test("projectNativeCommandRotation mirrors 0x91F7B7 ASM axis selection and angle decoding", () => {
	assert.equal( projectNativeCommandRotation( 0 ), undefined );
	assert.equal( projectNativeCommandRotation( -5 ), undefined );
	const yaw = projectNativeCommandRotation( 180 );
	assert.equal( yaw.axis, "y" );
	assert.equal( yaw.angle, Math.fround( 180 / 360 * 6.283185482025146 ) );
	const pitch = projectNativeCommandRotation( 450 );
	assert.equal( pitch.axis, "x" );
	assert.equal( pitch.angle, Math.fround( 90 / 360 * 6.283185482025146 ) );
	const roll = projectNativeCommandRotation( 1035 );
	assert.equal( roll.axis, "z" );
	assert.equal( roll.angle, Math.fround( 315 / 360 * 6.283185482025146 ) );
});

test("follow stage with native rotation receives decoded effectRotation and root attachment", () => {
	const stage = {
		resource: "slash.efp",
		phase: "SHOT",
		startEvent: 1,
		action: "AT_ONE_FOLLOW",
		move: "MOV_NONE",
		bone: null,
		offset: [ 0, 10, -13 ],
		life: 1,
		count: 1,
		scripts: [],
		native: { rotation: 1035 }
	};
	const catalog = { "99": { clips: [ "attack1" ], stages: [ stage ] } };
	let serial = 0;
	const owner = createCharacterEffects(
		{
			available: () => 4,
			request( url, limit, kind ) {
				jobs.set(
					++serial,
					kind === "effects" ?
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
				const r = jobs.get( id );
				jobs.delete( id );
				return r;
			},
			cancel() {}
		},
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const jobs = new Map();
	const entities = [ { gid: 1, regionId: 257, x: 100, y: 50, z: 200, heading: 0 } ];
	const cast = { token: 12, caster: 1, target: 2, skill: 99 }, game = { casts: [ cast ] };
	const trigger = { cast, phase: "SHOT", event: 1, at: 0.5 };
	owner.step( entities, game, 0 );
	owner.step( entities, game, 0.1 );
	owner.step( entities, game, 0.2 );
	const rows = owner.step( entities, game, 0.5, () => true, () => 1, [ trigger ] );
	assert.equal( rows.length, 1 );
	assert.equal( rows[0].effectRotation.axis, "z" );
	assert.equal( rows[0].effectRotation.angle, Math.fround( 315 / 360 * 6.283185482025146 ) );
	assert.equal( rows[0].attachment.gid, 1 );
	assert.equal( rows[0].attachment.root, true );
	assert.equal( rows[0].attachment.basis, "native" );
	assert.deepEqual( rows[0].attachment.offset, [ 0, 10, -13 ] );
	owner.dispose();
});
