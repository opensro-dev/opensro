/*
===========================================================================

wolf-howl.test.mjs - Hunting Howl / Savage Rend clips and routing

Samples every retail wolf rig through the shipped pose evaluator: hind paws
stay planted, front paws stay near the ground, the head lifts and recovery
returns to the neutral stance.

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
const { withWolfHowl, withWolfRend } = await import( "../../src/engine/foundation/animation/bear-roar.ts" );
const { createPresentationState } = await import( "../../src/engine/runtime/characters/presentation-state.ts" );

for ( const rig of [ "p_wolf_01", "p_wolf_02", "p_wolf_white_01", "p_wolf_white_02" ] ) {
	test(`${rig}: the howl lifts the muzzle, opens the jaw and keeps the paws down`, () => {
		const decoder = createModelDecoder();
		const decoded = decoder.character( decoder.decode( readPublishedAssetBytesSync(
			`/assets/npc/cos/${rig}.glb`,
			CLIENT_PUBLIC_ROOT
		) ) );
		const original = { ...decoded, images: [] };
		const model = withWolfHowl( original );
		assert.equal( model.nodes, original.nodes );
		assert.equal( model.clips.length, original.clips.length + 1 );
		assert.equal( withWolfHowl( model ), model );
		const howl = defined( model.clips.find( clip => clip.name === "pet-howl" ) );
		for ( const channel of howl.channels ) assert.ok( channel.values.every( Number.isFinite ) );
		const pose = createCharacterPose( model );
		pose.evaluate( "stand", 0, false );
		const at = name => [ ...defined( pose.socket( name ) ).slice( 12, 15 ) ];
		const head = at( "Bip01 Head" ), pelvis = at( "Bip01 Pelvis" ), restJaw = at( "Bip01 Ponytail1" );
		const body = Math.hypot( head[0] - pelvis[0], head[1] - pelvis[1], head[2] - pelvis[2] );
		const hind = [ "Bip01 L Foot", "Bip01 R Foot" ].map( at );
		const front = [ "Bip01 L Hand", "Bip01 R Hand" ].map( at );
		for ( const time of [ .15, .55, .85, 1.15, 1.6 ] ) {
			pose.evaluate( "pet-howl", time, false );
			hind.forEach( ( foot, i ) => {
				const now = at( [ "Bip01 L Foot", "Bip01 R Foot" ][i] );
				assert.ok(
					Math.hypot( now[0] - foot[0], now[1] - foot[1], now[2] - foot[2] ) < body * .03,
					`hind slid at ${time}`
				);
			} );
			front.forEach( ( paw, i ) => {
				const now = at( [ "Bip01 L Hand", "Bip01 R Hand" ][i] );
				assert.ok( Math.abs( now[1] - paw[1] ) < body * .25, `front paw left the ground at ${time}` );
			} );
			if ( time === .85 ) {
				// The head's forward axis (matrix column 0) points at the sky and the
				// jaw climbs; the chest dips instead of rearing like the bear.
				assert.ok( defined( pose.socket( "Bip01 Head" ) )[1] > .5, "muzzle not raised to the sky" );
				assert.ok( at( "Bip01 Ponytail1" )[1] > restJaw[1] + body * .05, "jaw did not climb" );
				assert.ok( at( "Bip01 Pelvis" )[1] <= pelvis[1] + body * .01, "the wolf reared up" );
			}
		}
		const recovered = at( "Bip01 Head" );
		assert.ok( Math.hypot( recovered[0] - head[0], recovered[1] - head[1], recovered[2] - head[2] ) < body * .01 );
		const jaw = defined(
			howl.channels.find( channel =>
				channel.node === model.nodes.findIndex( n => n.name === "Bip01 Ponytail1" ) &&
				channel.path === "rotation"
			)
		);
		assert.notDeepEqual( [ ...jaw.values.slice( 0, 4 ) ], [ ...jaw.values.slice( 12, 16 ) ] );
	});
}

for ( const rig of [ "p_wolf_01", "p_wolf_02", "p_wolf_white_01", "p_wolf_white_02" ] ) {
	test(`${rig}: the rend lunges on attack1's timing, shakes the head and plants the hind feet`, () => {
		const decoder = createModelDecoder();
		const decoded = decoder.character( decoder.decode( readPublishedAssetBytesSync(
			`/assets/npc/cos/${rig}.glb`,
			CLIENT_PUBLIC_ROOT
		) ) );
		const model = withWolfRend( withWolfHowl( { ...decoded, images: [] } ) );
		const rend = defined( model.clips.find( clip => clip.name === "pet-rend" ) );
		assert.equal( rend.duration, defined( model.clips.find( clip => clip.name === "attack1" ) ).duration );
		assert.equal( withWolfRend( model ), model );
		for ( const channel of rend.channels ) assert.ok( channel.values.every( Number.isFinite ) );
		const pose = createCharacterPose( model );
		pose.evaluate( "stand", 0, false );
		const at = name => [ ...defined( pose.socket( name ) ).slice( 12, 15 ) ];
		const head = at( "Bip01 Head" ), pelvis = at( "Bip01 Pelvis" );
		const body = Math.hypot( head[0] - pelvis[0], head[1] - pelvis[1], head[2] - pelvis[2] );
		const hind = [ "Bip01 L Foot", "Bip01 R Foot" ].map( at );
		const sides = [];
		for ( const f of [ .12, .25, .45, .55, .65, .75, 1 ] ) {
			pose.evaluate( "pet-rend", f * rend.duration, false );
			[ "Bip01 L Foot", "Bip01 R Foot" ].forEach( ( name, i ) => {
				const now = at( name );
				assert.ok(
					Math.hypot( now[0] - hind[i][0], now[1] - hind[i][1], now[2] - hind[i][2] ) < body * .03,
					`hind slid at ${f}`
				);
			} );
			sides.push( at( "Bip01 Head" ) );
		}
		// The tear swings the head both ways between 55 % and 65 %.
		const [left, right] = [ sides[3], sides[4] ];
		assert.ok( Math.hypot( left[0] - right[0], left[2] - right[2] ) > body * .03, "the head did not shake" );
		const done = sides[6];
		assert.ok( Math.hypot( done[0] - head[0], done[1] - head[1], done[2] - head[2] ) < body * .01 );
	});
}

test("a wolf's howl emote selects pet-howl; Savage Rend and other pets do not", () => {
	const state = createPresentationState();
	const entity = {
		name: "LabWolf",
		gid: 1,
		refObjId: 1,
		kind: "cos",
		regionId: 1,
		x: 0,
		y: 0,
		z: 0,
		heading: 0,
		emote: { action: 3, atMs: 0, revision: 1 }
	};
	const body = { refObjId: 1, codename: "COS_P_WOLF_090", glb: "/wolf.glb", clips: [ "stand", "attack1", "emote0" ] };
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
		resources: { duration: ( _path, role ) => role === "pet-howl" ? 1.6 : 1 },
		random: { range: () => 0 },
		active: new Set( [ 1 ] ),
		pendingDeaths: new Set(),
		uncensored: false
	};
	state.step( frame, { failure: null }, published );
	const posture = defined( defined( state.idleStates.get( 1 ) ).posture );
	assert.equal( posture.kind === "emote" ? posture.clip : "", "pet-howl" );
	state.step( { ...frame, seconds: 3 }, { failure: null }, published );
	assert.equal( defined( state.idleStates.get( 1 ) ).posture, undefined );
	// Rend plays through the bite's cast; 0xF0+ signals only draw effects.
	/** @type {[number, string][]} */
	const signals = [ [ 5, "COS_P_WOLF_090" ], [ 1, "COS_P_BEAR_090" ], [ 0xf1, "COS_P_WOLF_090" ], [
		0xf4,
		"COS_P_WOLF_090"
	] ];
	for ( const [action, codename] of signals ) {
		entity.emote.action = action;
		entity.emote.revision++;
		body.codename = codename;
		state.step( { ...frame, seconds: 4 }, { failure: null }, published );
		assert.equal( defined( state.idleStates.get( 1 ) ).posture, undefined, `${codename} action ${action}` );
	}
});
