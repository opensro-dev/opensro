/*
===========================================================================

model-particle-room.test.mjs - the model particles' own actor room

A crowd and the loot on the ground fill the scene's CHARACTER_ACTORS
room. The model particles (a Seal item's glow, a garment's ambient set)
take a room of their own beyond it, so a full scene no longer leaves the
player's glows without a slot, and what follows them (animation
particles, scenery) keeps the scene room they used to share.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { presentEmission } = await import(
	sourceFileUrl( "src/engine/runtime/characters/presentation-finalize.ts" ).href
);
const { createModelEmission, modelAmbientParticles } = await import(
	sourceFileUrl( "src/engine/foundation/animation/model-emission.ts" ).href
);
const { CHARACTER_ACTORS, MODEL_PARTICLE_ACTORS, RENDERED_ACTORS } = await import(
	sourceFileUrl( "src/engine/foundation/animation/character-budget.ts" ).href
);

const pose = { regionId: 1, x: 0, y: 0, z: 0, yaw: 0 };

/*
================
glowParticles

count ambient particles, as an equipped Seal item's glow publishes them.
================
*/
function glowParticles( count ) {
	return modelAmbientParticles( [ {
		kind: 2,
		stateId: -1,
		animationSetName: "ambient",
		entries: Array.from( { length: count }, ( _, i ) => ({
			field00: 1,
			effectPath: "item\\sun_glow_" + i + ".efp",
			boneName: "",
			vector3c: [ 0, 0, 0 ],
			field4c: 0,
			flags50: [ 0, 0, 0 ],
			flag53: 0
		}) )
	} ] );
}

/*
================
body
================
*/
function body( gid ) {
	return { gid, model: "body", pose, clip: "stand", time: 0, loop: true, scale: 1 };
}

/*
================
present

Runs one emission pass over a scene of sceneSize actors, the player
(gid 1) among them holding its glows, and returns the frame's actors.
================
*/
function present( sceneSize, holders, sceneryActors = 0 ) {
	const next = new Map();
	for ( let gid = 1; gid <= sceneSize; gid++ ) next.set( gid, body( gid ) );
	const scenery = {
		step: ( _source, _seconds, _ready, capacity ) =>
			Array.from( { length: Math.min( sceneryActors, capacity ) }, ( _, i ) => body( 900000 + i ) )
	};
	presentEmission( {
		entities: [],
		next,
		seconds: 1,
		hideSilkCos: false,
		particleHolders: holders.map( ( [gid, particles] ) => ({ actor: next.get( gid ), particles }) ),
		animationHolders: []
	}, {
		resources: { ready: () => true, duration: () => 1 },
		renderer: { presentationNight: () => true },
		modelEmission: createModelEmission( (() => {
			let id = -1;
			return () => id--;
		})() ),
		animationEmission: { step: () => [] },
		scenery,
		entityLod: { fraction: () => 0 },
		applySpawnFades: () => {}
	} );
	return next;
}

test("the renderer holds the scene's room and the model particles' room", () => {
	assert.equal( RENDERED_ACTORS, CHARACTER_ACTORS + MODEL_PARTICLE_ACTORS );
});

test("a full scene still shows the player's glows in the model particles' own room", () => {
	const next = present( CHARACTER_ACTORS, [ [ 1, glowParticles( 2 ) ] ] );
	const glows = [ ...next.values() ].filter( actor => actor.attachment?.gid === 1 );
	assert.equal( glows.length, 2, "a full scene left the player's glows no slot" );
	assert.equal( next.size, CHARACTER_ACTORS + 2 );
	assert.ok( next.size <= RENDERED_ACTORS );
});

test("the room is bounded, and the player's glows are admitted before a farther holder's", () => {
	// The player first, then MODEL_PARTICLE_ACTORS glows on farther holders
	// (eight pieces each): the room admits exactly its size, the player's
	// two among them.
	const crowd = Array.from( { length: MODEL_PARTICLE_ACTORS / 8 }, ( _, i ) => [ i + 2, glowParticles( 8 ) ] );
	const next = present( CHARACTER_ACTORS, [ [ 1, glowParticles( 2 ) ], ...crowd ] );
	const own = [ ...next.values() ].filter( actor => actor.attachment?.gid === 1 );
	assert.equal( own.length, 2 );
	assert.equal( next.size, CHARACTER_ACTORS + MODEL_PARTICLE_ACTORS );
});

test("glows no longer take the scene room what follows them used to share", () => {
	// 500 scene actors and 30 glows: the scenery after them keeps the
	// scene's last 12 slots, as it would with no glow at all.
	const next = present( 500, [ [ 1, glowParticles( 30 ) ] ], 50 );
	const scenery = [ ...next.keys() ].filter( gid => gid >= 900000 );
	assert.equal( scenery.length, CHARACTER_ACTORS - 500 );
});
