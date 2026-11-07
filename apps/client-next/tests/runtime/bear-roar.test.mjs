/*
===========================================================================

bear-roar.test.mjs - dedicated roar geometry and presentation routing

Samples both retail bear rigs through the shipped pose evaluator: hind paws
stay planted, the head rises, and recovery returns to the neutral stance.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";

const { createModelDecoder } = await import( "../../src/engine/runtime/assets/worker/model/model.ts" );
const { createCharacterPose } = await import( "../../src/engine/foundation/animation/animation-pose.ts" );
const { withBearRoar } = await import( "../../src/engine/foundation/animation/bear-roar.ts" );
const { createPresentationState } = await import( "../../src/engine/runtime/characters/presentation-state.ts" );

for ( const rig of [ "p_bear01", "p_bear02" ] ) {
	test(`${rig}: a separate roar stands up, opens its jaw and plants its hind feet`, () => {
		const decoder = createModelDecoder();
		const decoded = decoder.character( decoder.decode( readPublishedAssetBytesSync(
			`/assets/npc/cos/${rig}.glb`,
			CLIENT_PUBLIC_ROOT
		) ) );
		const original = { ...decoded, images: [] };
		const model = withBearRoar( original );
		assert.equal( model.nodes, original.nodes );
		assert.equal( model.clips.length, original.clips.length + 1 );
		assert.equal( withBearRoar( model ), model );
		const roar = model.clips.find( clip => clip.name === "pet-roar" );
		assert.ok( roar );
		assert.notEqual( roar.channels, defined( original.clips.find( clip => clip.name === "attack1" ) ).channels );
		for ( const channel of roar.channels ) assert.ok( channel.values.every( Number.isFinite ) );
		const pose = createCharacterPose( model );
		pose.evaluate( "stand", 0, false );
		const head = defined( pose.socket( "Bip01 Head" ) );
		const torsoLength = Math.abs( head[14] - defined( pose.socket( "Bip01 Pelvis" ) )[14] );
		const feet = [ "Bip01 L Foot", "Bip01 R Foot" ].map( name => defined( pose.socket( name ) ) );
		for ( const time of [ .10, .25, .50, .75, 1, 1.20, 1.40 ] ) {
			pose.evaluate( "pet-roar", time, false );
			for ( const [index, name] of [ "Bip01 L Foot", "Bip01 R Foot" ].entries() ) {
				const foot = defined( pose.socket( name ) );
				for ( const axis of [ 12, 13, 14 ] ) {
					assert.ok(
						Math.abs( foot[axis] - feet[index][axis] ) < .02,
						`${name} slid at ${time}`
					);
				}
			}
			if ( time === 1 ) assert.ok( defined( pose.socket( "Bip01 Head" ) )[13] > head[13] + torsoLength * .5 );
		}
		for ( const axis of [ 12, 13, 14 ] ) {
			assert.ok( Math.abs( defined( pose.socket( "Bip01 Head" ) )[axis] - head[axis] ) < .02 );
		}
		const jaw = roar.channels.find( channel =>
			channel.node === model.nodes.findIndex( n => n.name === "Bone01" ) && channel.path === "rotation"
		);
		assert.ok( jaw );
		assert.notDeepEqual( [ ...jaw.values.slice( 0, 4 ) ], [ ...jaw.values.slice( 16, 20 ) ] );
	});
}

test("only a bear's roar emote selects the dedicated clip, and the posture expires", () => {
	const state = createPresentationState();
	const entity = {
		name: "LabBear",
		gid: 1,
		refObjId: 1,
		kind: "cos",
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		emote: { action: 18, atMs: 0, revision: 1 }
	};
	const body = { refObjId: 1, codename: "COS_P_BEAR_090", glb: "/bear.glb", clips: [ "stand", "attack1" ] };
	const published = {
		catalog: new Map( [ [ 1, body ] ] ),
		recoveryByCodename: new Map(),
		animationStates: new Map(),
		deathModels: new Map()
	};
	const frame = {
		entities: [ entity ],
		seconds: 0,
		simulationMs: 0,
		logicalPose: () => ({ x: 0, y: 0, z: 0, regionId: 1, angle: 0 }),
		appearanceRef: () => 1,
		states: new Map(),
		health: undefined,
		deadGids: new Set(),
		hitByActor: new Map(),
		castByActor: new Map(),
		resources: { duration: ( _path, role ) => role === "pet-roar" ? 1.4 : 1 },
		random: { range: () => 0 },
		active: new Set( [ 1 ] ),
		pendingDeaths: new Set(),
		uncensored: false
	};
	state.step( frame, { failure: null }, published );
	const posture = defined( defined( state.idleStates.get( 1 ) ).posture );
	assert.equal( posture.kind, "emote" );
	assert.equal( posture.kind === "emote" ? posture.clip : "", "pet-roar" );
	state.step( { ...frame, seconds: 3 }, { failure: null }, published );
	assert.equal( defined( state.idleStates.get( 1 ) ).posture, undefined );
	for ( const action of [ 19, 20, 21 ] ) {
		entity.emote.action = action;
		entity.emote.revision++;
		state.step( { ...frame, seconds: 3.1 }, { failure: null }, published );
		assert.equal(
			defined( state.idleStates.get( 1 ) ).posture,
			undefined,
			"shield casts keep the bear on all fours"
		);
	}
	entity.emote.action = 18;
	body.codename = "COS_P_WOLF_090";
	entity.emote.revision++;
	state.step( { ...frame, seconds: 4 }, { failure: null }, published );
	assert.equal( defined( state.idleStates.get( 1 ) ).posture, undefined );
});

test("roar debris travels outward without stretching particles or exposing card edges", async () => {
	const { createProceduralEffect } = await import(
		"../../src/engine/runtime/characters/resources/procedural-effects.ts"
	);
	const debris = defined( createProceduralEffect( "/assets/effects/bear-roar-debris" ) );
	const primitive = debris.model.primitives[0];
	assert.equal( debris.model.nodes.length, 48 );
	assert.equal( primitive.joints.length, 48 );
	assert.equal( primitive.inverseBind.length, 48 * 16 );
	assert.equal( defined( primitive.geometry.joints ).length, primitive.geometry.positions.length / 3 * 4 );
	assert.equal(
		defined( primitive.geometry.material ).fadeAlphaOnly,
		false,
		"dust must preserve texture coverage when fading"
	);
	assert.deepEqual( defined( primitive.geometry.material ).blendPair, { source: 5, destination: 6 } );
	for ( const channel of debris.model.clips[0].channels ) {
		assert.equal( channel.path, "translation", "fixed-size dust and leaves, never growing cards" );
		assert.ok( Math.hypot( defined( channel.values.at( -3 ) ), defined( channel.values.at( -1 ) ) ) > 220 );
		assert.ok( channel.values.every( Number.isFinite ) );
	}
	const pixels = debris.images[0].levels[0];
	for ( const x of [ 0, 31, 32, 63 ] ) assert.equal( pixels[x * 4 + 3], 0 );
	const mouth = defined( createProceduralEffect( "/assets/effects/challenging-roar-aura" ) );
	const vertices = mouth.model.primitives[0].geometry.positions;
	assert.ok( vertices.every( Number.isFinite ) );
	assert.equal( Math.max( ...Array.from( vertices ).filter( ( _, index ) => index % 3 === 0 ) ), 22 );
});

test("roar rings stay thin and on the ground at full radius without changing counter ripples", async () => {
	const { createProceduralEffect } = await import(
		"../../src/engine/runtime/characters/resources/procedural-effects.ts"
	);
	const ring = defined( createProceduralEffect( "/assets/effects/bear-roar-ring" ) );
	const vertices = ring.model.primitives[0].geometry.positions;
	for ( let i = 0; i < vertices.length; i += 3 ) {
		assert.ok( vertices[i + 1] * 300 < .31 );
		assert.ok( Math.hypot( vertices[i], vertices[i + 2] ) >= .993 );
	}
	const counter = defined( createProceduralEffect( "/assets/effects/pet-counter-ripple" ) );
	assert.ok( counter.model.primitives[0].geometry.positions[1] > .02 );
});
