/*
===========================================================================

pose-inputs.test.mjs - a pose's inputs rebuild its skeleton on another evaluator

An off cloth frame keeps only its pose's inputs (animation-pose.ts inputs);
the cloth's restart rebuilds that frame's palette on a scratch evaluator
(restore, characters.ts keptClothPalette). On a shipped character, through
clips, loops, blended event and timed layers, held samples, body volume,
a bone rotator and clips admitted after the evaluator was made, the
palette of every primitive and every socket an evaluator restored from the
inputs builds must be the pose's, word for word: on a fresh evaluator, on
one reused across restores, for inputs kept many samples earlier, and on
the pose itself given an older frame's inputs and then its own back.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createModelDecoder } = await import( "../../src/engine/runtime/assets/worker/model/model.ts" );
const { createCharacterPose, createCharacterPoseInputs } = await import(
	"../../src/engine/foundation/animation/animation-pose.ts"
);

const STEPS = 240;

/*
================
skeleton

Every primitive's palette and every node's socket, as words.
================
*/
function skeleton( pose, model ) {
	const words = [];
	for ( const primitive of model.primitives ) {
		const palette = new Float32Array( primitive.joints.length * 16 );
		pose.palette( primitive, palette, 0 );
		words.push( ...new Uint32Array( palette.buffer ) );
	}
	for ( const node of model.nodes ) {
		const socket = pose.socket( node.name );
		if ( socket ) words.push( ...new Uint32Array( socket.buffer ) );
	}
	return Uint32Array.from( words );
}

test("a pose's inputs restore its exact skeleton on a fresh or reused evaluator", () => {
	const decoder = createModelDecoder();
	// The decoder's source is the model the renderer admits (images aside).
	/** @type {import("../../src/engine/contracts/character.ts").CharacterModel} */
	const model = /** @type {any} */ (decoder.character(
		decoder.decode(
			readPublishedAssetBytesSync( "/assets/char/china/chinaman_adventurer.glb", CLIENT_PUBLIC_ROOT )
		)
	));
	assert.ok( model.clips.length > 4, "a character with several clips" );
	// The pose starts with one clip and admits the rest later, as a body's
	// catalogue grows; the reused evaluator knows only the first clip.
	const first = { ...model, clips: model.clips.slice( 0, 1 ) };
	const pose = createCharacterPose( first ), reused = createCharacterPose( first );
	const bone = model.nodes[Math.floor( model.nodes.length / 3 )].name;
	const kept = [];
	let held = 0, layered = 0, rotated = 0;
	for ( let step = 0; step < STEPS; step++ ) {
		if ( step === 30 ) { for ( const clip of model.clips.slice( 1 ) ) pose.admitClip( clip ); }
		const clips = step < 30 ? 1 : model.clips.length,
			clip = model.clips[(step >> 3) % clips],
			other = model.clips[(step >> 2) % clips];
		if ( step % 37 === 0 ) pose.bodyVolume( 1 + step % 3, step % 2 === 1 );
		const rotation = step % 23 < 6 ?
			[ Math.sin( step * .1 ), 0, 0, Math.cos( step * .1 ) ] :
			null;
		// The level of detail holds a sample: no evaluation, the pose keeps it.
		if ( step % 5 === 3 ) held++;
		else {
			pose.setBoneRotation( bone, rotation );
			if ( rotation ) rotated++;
			/** @type {import("../../src/engine/contracts/character.ts").CharacterLayer[] | undefined} */
			const layers = step % 7 < 3 ?
				[
					{ clip: other.name, time: step * .013, loop: false, weight: .35, lane: "event" },
					{ clip: clip.name, time: step * .029, loop: true, weight: 1, lane: "timed" }
				] :
				undefined;
			if ( layers ) layered++;
			pose.evaluate( clip.name, step * .029 + .5, step % 11 !== 0, layers );
		}
		const inputs = createCharacterPoseInputs();
		assert.ok( pose.inputs( inputs ), `step ${step}: an evaluated pose has inputs` );
		const expected = skeleton( pose, model );
		const fresh = createCharacterPose( model );
		fresh.restore( inputs );
		assert.deepEqual( skeleton( fresh, model ), expected, `step ${step}: a fresh evaluator` );
		reused.restore( inputs );
		assert.deepEqual( skeleton( reused, model ), expected, `step ${step}: a reused evaluator` );
		if ( step % 13 === 0 ) kept.push( { inputs, expected, step } );
		// A restart borrows the pose itself: an older kept skeleton, then its own back.
		if ( step % 17 === 9 && kept.length ) {
			const older = kept[step % kept.length], own = createCharacterPoseInputs();
			assert.ok( pose.inputs( own ) );
			pose.restore( older.inputs );
			assert.deepEqual(
				skeleton( pose, model ),
				older.expected,
				`step ${step}: the pose rebuilds step ${older.step}`
			);
			pose.restore( own );
			assert.deepEqual( skeleton( pose, model ), expected, `step ${step}: the pose has its own skeleton back` );
		}
	}
	// Inputs kept long ago still rebuild their own skeleton.
	for ( const { inputs, expected, step } of kept.reverse() ) {
		reused.restore( inputs );
		assert.deepEqual( skeleton( reused, model ), expected, `inputs of step ${step}` );
	}
	assert.ok( held > 0 && layered > 0 && rotated > 0, `${held} held, ${layered} layered, ${rotated} rotated` );
	assert.equal( createCharacterPose( model ).inputs( createCharacterPoseInputs() ), false, "never evaluated" );
});
