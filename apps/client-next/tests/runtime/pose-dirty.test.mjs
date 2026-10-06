import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { build } from "esbuild";
import { readFileSync } from "node:fs";
import path from "node:path";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
async function load( entry, contents ) {
	const r = await build( {
		...contents ?
			{ stdin: { contents, loader: "ts", resolveDir: path.resolve( "src/engine/foundation/animation" ) } } :
			{ entryPoints: [ entry ] },
		bundle: true,
		platform: "node",
		format: "esm",
		write: false
	} );
	return import( "data:text/javascript;base64," + Buffer.from( r.outputFiles[0].contents ).toString( "base64" ) );
}
const { createCharacterPose } = await load( "src/engine/foundation/animation/animation-pose.ts" );
const { createCharacterPose: reference } = await load(
	null,
	readFileSync( "tests/fixtures/animation-pose-before-dirty.ts", "utf8" )
);
const { createModelDecoder } = await load( "src/engine/runtime/assets/worker/model/model.ts" );
test("current pose evaluator remains bit-identical to the frozen reference across published clips and blends", () => {
	const decoder = createModelDecoder(),
		model = decoder.character(
			decoder.decode( readPublishedAssetBytesSync( "/assets/npc/mob/china/mangnyang.glb", CLIENT_PUBLIC_ROOT ) )
		);
	const actual = createCharacterPose( model ), expected = reference( model );
	let samples = 0;
	function compare( name, time, loop, layers ) {
		assert.equal( actual.evaluate( name, time, loop, layers ), expected.evaluate( name, time, loop, layers ) );
		for ( const primitive of model.primitives ) {
			const a = new Float32Array( primitive.joints.length * 16 ), b = a.slice();
			actual.palette( primitive, a );
			expected.palette( primitive, b );
			assert.deepEqual( new Uint32Array( a.buffer ), new Uint32Array( b.buffer ), name + ":" + time );
		}
		for ( const node of model.nodes ) assert.deepEqual( actual.socket( node.name ), expected.socket( node.name ) );
		samples++;
	}
	for ( const clip of model.clips ) {
		for ( const loop of [ false, true ] ) {
			for ( const fraction of [ 0, .01, .25, .5, .5, .75, .99, 1, 1.01, 3, .3, 0 ] ) {
				compare( clip.name, clip.duration * fraction, loop );
			}
		}
	}
	const a = model.clips[0], b = model.clips[1];
	for ( let i = 0; i < 40; i++ ) {
		compare( a.name, i / 60, true, [
			{ clip: a.name, time: i / 60, loop: true, weight: 1 - i / 40, lane: "timed" },
			{ clip: b.name, time: i / 90, loop: false, weight: i / 40, lane: "event" }
		] );
	}
	assert.ok( samples > 100 );
});
