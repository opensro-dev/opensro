/*
===========================================================================

character-heading.test.mjs - tests for characters.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";

const { createCharacters } = await import(
	sourceFileUrl( "src/engine/runtime/renderer/characters/characters.ts" ).href
);
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
test("character instance rotation converts native headings to radians", () => {
	const characters = createCharacters();
	let uploaded;
	characters.model( "fixture", {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		clips: [],
		images: [],
		primitives: [ {
			name: "mesh",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			geometry: {
				positions: new Float32Array( 9 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	}, [] );
	const gpu = {
		upload( data ) {
			uploaded = data;
			return {};
		},
		updateInstances( draw, instances ) {
			uploaded = { ...uploaded, instances };
			return draw;
		},
		updateBones() {},
		release() {}
	};
	for ( const angle of [ 0, 16384, 32768, 49151, 65535 ] ) {
		characters.actors( [ {
			gid: 1,
			model: "fixture",
			pose: { regionId: 2, x: 10, y: 20, z: 30, yaw: angle / 65535 * 2 * Math.PI },
			clip: "",
			time: 0,
			loop: true,
			scale: 1
		} ] );
		characters.prepare( gpu, {
			upload() {
				return {};
			},
			release() {}
		}, 1 );
		const yaw = angle / 65535 * 2 * Math.PI;
		assert.ok( Math.abs( defined( uploaded ).instances[0] - Math.cos( yaw ) ) < 1e-6 );
		assert.ok( Math.abs( defined( uploaded ).instances[8] - Math.sin( yaw ) ) < 1e-6 );
		assert.equal( defined( uploaded ).instances[12], 1930 );
	}
	characters.dispose( gpu, null );
});

test("a rider whose vehicle has no drawn row stands on its own placement", () => {
	const characters = createCharacters();
	let uploaded;
	characters.model( "fixture", {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		clips: [],
		images: [],
		primitives: [ {
			name: "mesh",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			image: -1,
			geometry: {
				positions: new Float32Array( 9 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform: identity()
			}
		} ]
	}, [] );
	const gpu = {
		upload( data ) {
			uploaded = data;
			return {};
		},
		updateInstances( draw, instances ) {
			uploaded = { ...uploaded, instances };
			return draw;
		},
		updateBones() {},
		release() {}
	};
	// 777F60 binds the ride before the vehicle (gid 99) has spawned or loaded.
	characters.actors( [ {
		gid: 1,
		model: "fixture",
		mountedOn: 99,
		pose: { regionId: 2, x: 10, y: 20, z: 30, yaw: 0 },
		clip: "",
		time: 0,
		loop: true,
		scale: 1
	} ] );
	characters.prepare( gpu, {
		upload() {
			return {};
		},
		release() {}
	}, 1 );
	assert.equal( defined( uploaded ).instances[12], 1930, "drawn at its own x, not hidden" );
	characters.dispose( gpu, null );
});
