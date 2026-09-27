/*
===========================================================================

attached-effect-residency.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const { createCharacterEffects, createCharacterResources, createPresentationRandom, CHARACTER_MODELS } = {
	...(await load( "src/engine/runtime/characters/effects/effects.ts" )),
	...(await load( "src/engine/runtime/characters/resources/resources.ts" )),
	...(await load( "src/engine/runtime/random/random.ts" )),
	...(await load( "src/engine/foundation/animation/character-budget.ts" ))
};

test("live attached effects retain duration and release tails under a full model cache", () => {
	const effectPath = "/assets/effects/programs.json#test.efp";
	const stage = {
		phase: "ACT_L",
		resource: "test.efp",
		action: "AT_LOOP",
		move: "MOV_NONE",
		scripts: [],
		startEvent: 0,
		count: 1,
		offset: [ 0, 0, 0 ],
		life: 0
	};
	const model = { nodes: [], primitives: [], images: [], clips: [ { name: "effect", duration: 1, channels: [] } ] };
	let serial = 0;
	const jobs = new Map(), requested = [];
	const assets = {
		available: () => 4,
		request( url, limit, decode ) {
			requested.push( url );
			const result = decode === "effects" ?
				{ kind: "effects", catalog: { 1: { stages: [ stage ] } } } :
				decode === "effect" || decode === "character" ?
				{ kind: "character", model, images: [] } :
				{
					kind: "bytes",
					buffer:
						new TextEncoder().encode( JSON.stringify( { format: "sro-skill-stage-models", models: {} } ) )
							.buffer
				};
			jobs.set( ++serial, result );
			return serial;
		},
		take( id ) {
			const r = jobs.get( id );
			jobs.delete( id );
			return r;
		},
		cancel( id ) {
			jobs.delete( id );
		}
	};
	const resources = createCharacterResources(
		assets,
		{ setCharacterModel() {}, retainCharacterModels() {} },
		"http://fixture.invalid"
	);
	const effects = createCharacterEffects( assets, "http://fixture.invalid", () => {}, createPresentationRandom( 1 ) );
	const entity = { gid: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0 },
		body = { gid: 1, pose: { ...entity, yaw: 0 }, scale: 1, height: 20 };
	const game = { casts: [], attachedEffects: [ { gid: 1, skill: 1, token: 7, phase: 0 } ] };
	let tick = 0, actors = [];
	function frame( crowd = [] ) {
		resources.begin( tick );
		resources.poll();
		actors = effects.step( [ entity ], game, tick, resources.ready, resources.duration, [], undefined, [ body ] );
		for ( const path of crowd ) resources.ready( path );
		resources.retainWanted( [] );
		tick += .001;
		return actors;
	}
	try {
		for ( let i = 0; i < 8; i++ ) frame();
		assert.equal( actors.length, 1 );
		const gid = actors[0].gid;
		// Rotate more sources through the cache than it can hold. Assembly names
		// are deliberately not model-plan claims, as in the production parent.
		for ( let i = 0; i < CHARACTER_MODELS + 4; i++ ) {
			const crowd = Array.from( { length: i + 1 }, ( _, j ) => "/assets/crowd-" + j + ".glb" );
			frame( crowd );
			frame( crowd );
		}
		assert.equal(
			resources.duration( effectPath, "effect" ),
			1,
			"a live buff must not become inactive cache data"
		);
		assert.equal( frame()[0].gid, gid );
		assert.equal( effects.error(), null );
		game.attachedEffects = [];
		const releasedAt = tick;
		assert.equal( frame().length, 1 );
		tick = releasedAt + .5;
		assert.equal( frame().length, 1, "release tail keeps the actual EFP duration" );
		tick = releasedAt + 1.01;
		assert.equal( frame().length, 0 );
		assert.equal(
			requested.filter( url => url.endsWith( "#test.efp" ) ).length,
			1,
			"pressure must not evict and reload a live effect"
		);
	} finally {
		effects.dispose();
		resources.dispose();
	}
});
