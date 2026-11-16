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
const { createPaletteStreams } = await load( "src/engine/runtime/renderer/characters/palette-streams.ts" );
const { createCharacterRenderPlan, characterBatchBytes, characterPoseBytes, createCharacterStateIndex } = {
	...(await load( "src/engine/foundation/animation/character-render-plan.ts" )),
	...(await load( "src/engine/foundation/animation/character-budget.ts" )),
	...(await load( "src/engine/runtime/characters/state-index.ts" ))
};
test("admission plans preserve every capacity reservation including irregular particle batches", () => {
	// A row glow's states exist from two rows on (characterBatchBytes).
	for ( const extra of [ {}, { emission: { capacity: 17, births: [ 0 ], lifetime: 1 } }, { equipmentGlow: {} } ] ) {
		const model = {
			nodes: [ {} ],
			clips: [ {
				name: "walk",
				duration: 1,
				channels: [ { times: new Float32Array( 1 ) }, { times: new Float32Array( 1 ) } ]
			} ],
			primitives: Array.from(
				{ length: 3 },
				( _, i ) => ({
					joints: Array( i + 1 ).fill( 0 ),
					inverseBind: new Float32Array( (i + 1) * 16 ),
					...extra,
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
test("a shared-palette batch is charged one palette per distinct binding, as palette-streams keeps them", () => {
	// Three skinned parts: the first two bind the same joints through the
	// same inverse binds (a body's parts), the third its own. distinct breaks
	// the second part's binding by one bit.
	const model = ( distinct, emitter ) => ({
		nodes: [ {}, {} ],
		clips: [],
		primitives: [ 0, 1, 2 ].map( i => ({
			joints: i === 2 ? [ 0 ] : [ 0, 1 ],
			inverseBind: Float32Array.from(
				{ length: (i === 2 ? 1 : 2) * 16 },
				( _, k ) => k % 5 === 0 ? 1 : distinct && i === 1 && k === 3 ? 2 ** -20 : 0
			),
			emission: emitter && i === 2 ? { capacity: 4, births: [ 0 ], lifetime: 1 } : undefined,
			geometry: {
				positions: new Float32Array( 9 ),
				indices: new Uint32Array( 3 ),
				joints: new Uint32Array( 12 ),
				weights: new Float32Array( 12 )
			}
		}) )
	});
	// Two joints of 64 bytes, its CPU stream and its GPU copy, per slot.
	const palette = 2 * 64 * 2;
	for ( const count of [ 1, 2, 3, 8 ] ) {
		const slots = 2 ** Math.ceil( Math.log2( count ) );
		assert.equal(
			characterBatchBytes( model( true, false ), count ) - characterBatchBytes( model( false, false ), count ),
			slots * palette,
			`count ${count}: the shared binding is charged once`
		);
		// The owner keeps exactly that many streams.
		const kept = new Set(
			createPaletteStreams( model( false, false ), slots ).streams.map( stream => stream.data )
		);
		assert.equal( kept.size, 2 );
		// An emitter batch keeps a palette per primitive: sharing changes nothing.
		assert.equal(
			characterBatchBytes( model( true, true ), count ),
			characterBatchBytes( model( false, true ), count ),
			`count ${count}: an emitter batch is charged per primitive`
		);
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
