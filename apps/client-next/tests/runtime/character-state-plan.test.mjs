/*
===========================================================================

character-state-plan.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
import { test } from "node:test";
import assert from "node:assert/strict";
const { createCharacterRenderPlan, characterBatchBytes, characterPoseBytes, createCharacterStateIndex } = {
	...(await load( "src/engine/foundation/animation/character-render-plan.ts" )),
	...(await load( "src/engine/foundation/animation/character-budget.ts" )),
	...(await load( "src/engine/runtime/characters/state-index.ts" ))
};
test("admission plans preserve every capacity reservation including irregular particle batches", () => {
	for ( const emission of [ undefined, { capacity: 17, births: [ 0 ], lifetime: 1 } ] ) {
		const model = {
			nodes: [ {} ],
			clips: [ { name: "walk", duration: 1, channels: [ {}, {} ] } ],
			primitives: Array.from(
				{ length: 3 },
				( _, i ) => ({
					joints: Array( i + 1 ).fill( 0 ),
					emission,
					geometry: {
						positions: new Float32Array( 9 + i * 3 ),
						indices: new Uint32Array( 3 ),
						joints: new Uint32Array( 12 )
					}
				})
			)
		};
		const plan = createCharacterRenderPlan( model );
		assert.equal( plan.poseBytes, characterPoseBytes( model ) );
		for ( let count = 0; count <= 512; count++ ) {
			assert.equal( plan.batchBytes( count ), characterBatchBytes( model, count ), `count ${count}` );
		}
		assert.throws( () => plan.batchBytes( 513 ), /capacity/ );
		assert.equal( plan.clips.get( "walk" ), model.clips[0] );
	}
});
test("state indices retain delivered rows across render frames and replace lifecycle state atomically", () => {
	const owner = createCharacterStateIndex(),
		entities = [ { gid: 1, name: "first" } ],
		gameplay = { casts: [ { caster: 1, token: 3 } ], vitals: [ { gid: 1, hp: 5 } ] };
	const first = owner.update( entities, gameplay );
	assert.equal( first, owner.update( entities, gameplay ) );
	assert.equal( first.entitiesByGid.get( 1 ), entities[0] );
	entities.push( { gid: 3 } );
	gameplay.casts[0].token = 4;
	owner.update( entities, gameplay );
	assert.equal( first.entitiesByGid.has( 3 ), true );
	assert.equal( first.castTokens.has( 3 ), false );
	assert.equal( first.castTokens.has( 4 ), true );
	gameplay.casts[0].token = 3;
	const next = owner.update( [ { gid: 2, name: "second" } ], { casts: [], vitals: [ { gid: 2, hp: 7 } ] } );
	assert.equal( next.entitiesByGid.has( 1 ), false );
	assert.equal( next.castByActor.size, 0 );
	assert.equal( next.castTokens.size, 0 );
	assert.equal( next.vitalsByGid.has( 1 ), false );
	owner.reset();
	assert.equal( next.entitiesByGid.size, 0 );
	owner.update( entities, gameplay );
	assert.equal( first.castTokens.has( 3 ), true );
});
