/*
===========================================================================

ribbon-index-reuse.test.mjs - a ribbon's indices are uploaded when they change

A ribbon's strips move every frame, but which vertices its triangles join
changes only when an element is born or expires. These tests drive the
production character renderer (characters.ts) with a trail of three
births and require an index upload on exactly the frames the strip's
points changed, holding the indices the full rebuild would hold (#4692).

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacters } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/characters/characters.ts" ).href
);

const identity = () => Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
const VIEW = Float32Array.of( .01, 0, 0, 0, 0, .01, 0, 0, 0, 0, .001, 0, 0, 0, .5, 1 );

// A trail: three elements born .05 s apart, each living .5 s.
const TRAIL = {
	nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
	images: [],
	clips: [ { name: "effect", duration: 10, channels: [] } ],
	primitives: [ {
		name: "trail",
		node: 0,
		joints: [ 0 ],
		inverseBind: identity(),
		image: -1,
		ribbon: { fps: 20, widths: Float32Array.of( 1 ), spline: false },
		emission: { births: [ 0, .05, .1 ], lifetime: .5, capacity: 3 },
		materialFrames: {
			fps: 20,
			sampling: "step",
			colors: Float32Array.of( 1, 1, 1, 1 ),
			windows: Float32Array.of( 1, 1, 0, 0 )
		},
		geometry: {
			positions: new Float32Array( 9 ),
			indices: Uint32Array.of( 0, 1, 2 ),
			transform: identity(),
			material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: true, doubleSided: true }
		}
	} ]
};

/*
================
renderer

The character renderer with the trail, on a device that records each index
upload and how many vertex updates it saw.
================
*/
function renderer() {
	const owner = createCharacters(), uploads = [];
	let moved = 0;
	owner.model( "trail", TRAIL, [] );
	const gpu = {
		upload: () => ({}),
		release() {},
		updatePositions() {
			moved++;
		},
		updateIndices( _draw, indices ) {
			uploads.push( [ ...indices ] );
		},
		updateTransform() {},
		updateInstances: draw => draw,
		updateBones() {}
	};
	return {
		uploads,
		moved: () => moved,
		// The emitter moves one unit a tick, so each birth has its own point.
		frame( time, emissionEnd ) {
			owner.actors( [ {
				...(emissionEnd === undefined ? {} : { emissionEnd }),
				gid: 1,
				model: "trail",
				pose: { regionId: 257, x: time * 20, y: 0, z: 0, yaw: 0 },
				clip: "effect",
				time,
				loop: false,
				scale: 1
			} ] );
			owner.prepare( gpu, {}, 257, VIEW, false, time );
		},
		dispose() {
			owner.dispose( gpu, null );
		}
	};
}

// The six indices of each segment of a strip of points points from vertex 0.
const strip = points =>
	Array.from( { length: (points - 1) * 6 }, ( _, i ) => Math.floor( i / 6 ) * 2 + [ 0, 1, 2, 2, 1, 3 ][i % 6] );

test("a ribbon uploads its indices only on the frames its strips gain or lose a point", () => {
	const r = renderer();
	try {
		const seen = [];
		for ( const time of [ 0, .05, .1, .12, .14, .3, .49, .51, .53, .56, .58 ] ) {
			const before = r.uploads.length;
			r.frame( time );
			if ( r.uploads.length > before ) seen.push( time );
			assert.ok( r.uploads.length - before <= 1 );
		}
		// One, two, three points as the births arrive; then each expiry.
		assert.deepEqual( seen, [ 0, .05, .1, .51, .56 ] );
		assert.deepEqual( r.uploads, [ [], strip( 2 ), strip( 3 ), strip( 2 ), [] ] );
		assert.ok( r.moved() >= 9, "the strip's vertices are still written every frame" );
	} finally {
		r.dispose();
	}
});

test("a ribbon whose strips all go at once uploads the empty index list", () => {
	const r = renderer();
	try {
		for ( const time of [ 0, .05, .1, .12 ] ) r.frame( time );
		assert.deepEqual( r.uploads.at( -1 ), strip( 3 ) );
		const before = r.uploads.length;
		// The emission is cut before its first birth: no element is drawn.
		r.frame( .14, 0 );
		assert.deepEqual( r.uploads.slice( before ), [ [] ] );
		r.frame( .16, 0 );
		assert.equal( r.uploads.length, before + 1, "and stays uploaded" );
	} finally {
		r.dispose();
	}
});
