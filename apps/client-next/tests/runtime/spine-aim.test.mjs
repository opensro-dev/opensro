/*
===========================================================================

spine-aim.test.mjs - tests for spine-aim.ts, spine-aims.ts and the pose's
bone rotation

CIDecoSkill_Update (8DC440) turns a caster's Spine_Base toward a target
above or below at 1.745 rad/s, level within 15 units and clamped to 30
degrees; the final stage or the cast's end eases it home (A9ADD0, A8FA60).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const aim = await import( sourceFileUrl( "src/engine/foundation/animation/spine-aim.ts" ).href );
const { createSpineAims } = await import(
	sourceFileUrl( "src/engine/runtime/characters/effects/spine-aims.ts" ).href
);
const { createCharacterPose } = await import(
	sourceFileUrl( "src/engine/foundation/animation/animation-pose.ts" ).href
);

const LIMIT = Math.fround( 0.52359879 );
const CASTER = 1, TARGET = 2, SKILL = 70, TOKEN = 9;

test("the aim is level within 15 units and clamped to 30 degrees beyond", () => {
	assert.equal( aim.spineAimAngle( 15, 50, 0, 0 ), 0, "the 15 unit edge is level" );
	assert.ok( Math.abs( aim.spineAimAngle( 100, 10, 0, 0 ) - Math.atan( .1 ) ) < 1e-6 );
	assert.equal( aim.spineAimAngle( 20, 1000, 0, 0 ), LIMIT );
	assert.equal( aim.spineAimAngle( 20, -1000, 0, 0 ), -LIMIT );
	assert.ok( Math.abs( aim.spineAimAngle( 100, 0, 0, -10 ) + Math.atan( .1 ) ) < 1e-6, "height joins dy" );
});

test("the axis token picks the angle's component and sign", () => {
	const a = .25;
	assert.deepEqual( aim.spineAimTargets( 1, a ), [ 0, 0, -a ] );
	assert.deepEqual( aim.spineAimTargets( 2, a ), [ a, 0, 0 ] );
	assert.deepEqual( aim.spineAimTargets( 3, a ), [ 0, a, 0 ] );
	assert.deepEqual( aim.spineAimTargets( 4, a ), [ 0, 0, a ] );
	assert.deepEqual( aim.spineAimTargets( 5, a ), [ -a, 0, 0 ] );
	assert.deepEqual( aim.spineAimTargets( 6, a ), [ 0, -a, 0 ] );
});

test("the rotator steps 1.745 rad/s, lands on its target and drops once released home", () => {
	const rotator = { current: [ 0, 0, 0 ], target: [ 0, 0, .5 ], releasing: false };
	assert.equal( aim.stepSpineAim( rotator, 100 ), true );
	assert.ok( Math.abs( rotator.current[2] - .174532914 ) < 1e-6, "one 100 ms step" );
	assert.equal( aim.stepSpineAim( rotator, 1000 ), true );
	assert.equal( rotator.current[2], .5, "within a step it lands" );
	rotator.releasing = true;
	rotator.target.fill( 0 );
	assert.equal( aim.stepSpineAim( rotator, 100 ), true, "a released rotator still eases" );
	assert.equal( aim.stepSpineAim( rotator, 1000 ), false, "home and released: erased" );
});

test("the rotation is D3DX yaw-pitch-roll carried across the exporter's Z mirror", () => {
	const roll = .4, rotator = { current: [ 0, 0, roll ], target: [ 0, 0, 0 ], releasing: false };
	const [x, y, z, w] = aim.spineAimRotation( rotator );
	assert.ok( Math.abs( x ) < 1e-9 && Math.abs( y ) < 1e-9 );
	assert.ok( Math.abs( z - Math.sin( roll / 2 ) ) < 1e-9 && Math.abs( w - Math.cos( roll / 2 ) ) < 1e-9 );
	const pitched = aim.spineAimRotation( { current: [ 0, roll, 0 ], target: [ 0, 0, 0 ], releasing: false } );
	assert.ok( Math.abs( pitched[0] + Math.sin( roll / 2 ) ) < 1e-9, "native +x pitch is -x in model space" );
});

/*
================
aimFrame

One presentation frame: a caster shooting at a target dy above at range,
with a Roll-axis bow record.
================
*/
function aimFrame(
	now,
	{ casts, triggers = /** @type {import("../../src/engine/contracts/effects.ts").EffectTrigger[]} */ ([]), dy = 30 }
) {
	const actor = ( gid, x, y ) => ({
		gid,
		model: "m",
		clip: "",
		time: 0,
		loop: false,
		scale: 1,
		height: 18,
		pose: { regionId: 1, x, y, z: 0, yaw: 0 }
	});
	return {
		casts,
		catalog: {
			[SKILL]: {
				clips: [],
				stages: [],
				spineAim: {
					axis: 1,
					start: { bone: "Bip01 R Hand", offsetY: 0, addHeight: false },
					target: { bone: null, offsetY: 10, addHeight: false },
					release: { phase: "SHOT", event: 1 }
				}
			}
		},
		triggers,
		presented: [ actor( CASTER, 0, 0 ), actor( TARGET, 100, dy ) ],
		now
	};
}

test("a cast arms its caster's aim, a final-stage trigger releases it and it eases home", () => {
	const aims = createSpineAims();
	const cast = { token: TOKEN, caster: CASTER, skill: SKILL, target: TARGET, damage: 0, fatal: false };
	// The hand sits 12 above the caster's root and the target binding is the
	// target's root plus 10: height = 10 - 12.
	const socket = ( gid, bone, offset ) => ({
		regionId: 1,
		x: 0,
		y: (gid === CASTER ? 12 : 30) + offset[1],
		z: 0,
		yaw: 0
	});
	aims.step( aimFrame( 0, { casts: [ cast ] } ), socket );
	assert.ok( aims.rotation( CASTER ), "armed at the cast" );
	aims.step( aimFrame( 1, { casts: [ cast ] } ), socket );
	const expected = Math.atan( (10 - 12 + 30) / 100 );
	const roll = 2 * Math.asin( aims.rotation( CASTER )[2] );
	assert.ok( Math.abs( roll + expected ) < 1e-5, `rolled ${roll}, want ${-expected}` );
	aims.step(
		aimFrame( 1.1, { casts: [ cast ], triggers: [ { cast, phase: "SHOT", event: 1, at: 1.1 } ] } ),
		socket
	);
	assert.ok( aims.rotation( CASTER ), "the release eases, it does not snap" );
	aims.step( aimFrame( 3, { casts: [ cast ] } ), socket );
	assert.equal( aims.rotation( CASTER ), undefined, "home and released: dropped" );
});

test("the cast's end releases an aim the final stage never reached", () => {
	const aims = createSpineAims();
	const cast = { token: TOKEN, caster: CASTER, skill: SKILL, target: TARGET, damage: 0, fatal: false };
	aims.step( aimFrame( 0, { casts: [ cast ] } ) );
	aims.step( aimFrame( 1, { casts: [ cast ] } ) );
	aims.step( aimFrame( 1.1, { casts: [] } ) );
	aims.step( aimFrame( 3, { casts: [] } ) );
	assert.equal( aims.rotation( CASTER ), undefined );
});

test("a pose multiplies the bone rotation after its sampled local rotation and drops it again", () => {
	const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	const model = {
		nodes: [
			{ name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
			{ name: "Spine_Base", parent: 0, translation: [ 0, 10, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] },
			{ name: "head", parent: 1, translation: [ 0, 5, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] }
		],
		primitives: [ { joints: [ 0 ], inverseBind: identity() } ],
		clips: []
	};
	const pose = createCharacterPose( model );
	pose.evaluate( "", 0 );
	assert.deepEqual( Array.from( pose.socket( "head" ).subarray( 12, 15 ) ), [ 0, 15, 0 ] );
	// A quarter turn about z tips the head from +y to -x.
	pose.setBoneRotation( "Spine_Base", [ 0, 0, Math.SQRT1_2, Math.SQRT1_2 ] );
	assert.equal( pose.evaluate( "", 0 ), true, "a new rotation is a new pose" );
	const head = pose.socket( "head" );
	assert.ok( Math.abs( head[12] + 5 ) < 1e-5 && Math.abs( head[13] - 10 ) < 1e-5 );
	assert.equal( pose.gpuSample(), null, "a rotated pose samples on the CPU" );
	pose.setBoneRotation( "", null );
	assert.equal( pose.evaluate( "", 0 ), true );
	assert.deepEqual( Array.from( pose.socket( "head" ).subarray( 12, 15 ) ), [ 0, 15, 0 ] );
});
