/*
===========================================================================

portrait-lifecycle.test.mjs - empty preview work and borrowed resource retirement

Exercise the real character owner with observable GPU handles. Empty slots may
skip work only after their last borrowed model and uploads have been retired.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";
const { createCharacters } = await import( "../../src/engine/runtime/renderer/characters/characters.ts" );
const { createPortrait } = await import( "../../src/engine/runtime/renderer/characters/portrait.ts" );
const { radians } = await import( "../../src/engine/foundation/math/angles.ts" );

/*
================
fixture

Keep the real preview owner, replacing only its GPU device capabilities.
================
*/
function fixture() {
	const characters = createCharacters(), handles = new Set();
	let calls = 0, closes = 0, failRelease = false;
	/*
	================
	upload
	================
	*/
	function upload() {
		const handle = {};
		handles.add( handle );
		return handle;
	}
	/*
	================
	release
	================
	*/
	function release( handle ) {
		if ( failRelease ) {
			failRelease = false;
			throw Error( "injected retirement failure" );
		}
		assert.ok( handles.delete( handle ), "retired an absent handle" );
	}
	const geometry = /** @type {any} */ ({
		upload,
		release,
		updateInstances: draw => draw,
		updateTransform() {},
		updateBones() {}
	});
	const images = /** @type {any} */ ({ upload, release });
	const preview = createPortrait( {
		...characters,
		/*
		================
		prepare
		================
		*/
		prepare( ...args ) {
			calls++;
			return characters.prepare( ...args );
		}
	} );
	const transform = new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
	/** @type {import("../../src/engine/contracts/character.ts").CharacterModel} */
	const model = {
		nodes: [ {
			name: "Bip01 Head",
			parent: -1,
			translation: [ 0, 0, 0 ],
			rotation: [ 0, 0, 0, 1 ],
			scale: [ 1, 1, 1 ]
		} ],
		clips: [ { name: "stand", duration: 1, channels: [] } ],
		images: [ { width: 1, height: 1 } ],
		primitives: [ {
			name: "body",
			node: 0,
			image: 0,
			joints: [ 0 ],
			inverseBind: transform,
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				transform
			}
		} ]
	};
	const bitmap = /** @type {ImageBitmap} */ (/** @type {unknown} */ ({
		width: 1,
		height: 1,
		/*
		================
		close
		================
		*/
		close() {
			closes++;
		}
	}));
	const source = {
		model,
		images: [ bitmap ],
		actor: {
			gid: 1,
			model: "body",
			clip: "stand",
			time: 0,
			loop: true,
			scale: 1,
			pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: radians( 0 ) }
		}
	};
	return {
		characters,
		preview,
		geometry,
		images,
		source,
		handles,
		calls: () => calls,
		closes: () => closes,
		/*
		================
		failNextRelease
		================
		*/
		failNextRelease() {
			failRelease = true;
		}
	};
}

test("empty portraits skip preparation after real model and GPU retirement, then re-borrow on re-entry", () => {
	const f = fixture();
	for ( let frame = 0; frame < 10; frame++ ) assert.deepEqual( f.preview.prepare( null, f.geometry, f.images ), [] );
	assert.equal( f.calls(), 0 );
	assert.equal( f.preview.prepare( f.source, f.geometry, f.images ).length, 1 );
	assert.ok( f.characters.hasModel( "body" ) );
	assert.equal( f.handles.size, 2 );
	f.preview.prepare( null, f.geometry, f.images );
	assert.equal( f.characters.hasModel( "body" ), false );
	assert.equal( f.characters.stats().draws, 0 );
	assert.equal( f.characters.stats().renderBytes, 0 );
	assert.equal( f.handles.size, 0 );
	const cleared = f.calls();
	for ( let frame = 0; frame < 10; frame++ ) f.preview.prepare( null, f.geometry, f.images );
	assert.equal( f.calls(), cleared );
	// Leaving and rejoining need not have a displayed empty frame in between.
	assert.equal( f.preview.prepare( f.source, f.geometry, f.images ).length, 1 );
	assert.equal( f.handles.size, 2 );
	f.preview.dispose( f.geometry, f.images );
	assert.equal( f.handles.size, 0 );
	assert.equal( f.closes(), 0, "borrowed world images stay open" );
});

test("failed retirement retries before empty work can be skipped", () => {
	const f = fixture();
	f.preview.prepare( f.source, f.geometry, f.images );
	f.failNextRelease();
	assert.throws( () => f.preview.prepare( null, f.geometry, f.images ), /injected retirement failure/ );
	assert.ok( f.characters.hasModel( "body" ) );
	assert.equal( f.handles.size, 2 );
	f.preview.prepare( null, f.geometry, f.images );
	assert.equal( f.characters.hasModel( "body" ), false );
	assert.equal( f.handles.size, 0 );
	const cleared = f.calls();
	f.preview.prepare( null, f.geometry, f.images );
	assert.equal( f.calls(), cleared );
	f.preview.dispose( f.geometry, f.images );
});

test("device invalidation forces an empty pass and restores a visible borrowed portrait", () => {
	const f = fixture();
	f.preview.invalidate();
	f.preview.prepare( null, f.geometry, f.images );
	assert.equal( f.calls(), 1 );
	f.preview.prepare( null, f.geometry, f.images );
	assert.equal( f.calls(), 1 );
	f.preview.prepare( f.source, f.geometry, f.images );
	assert.equal( f.handles.size, 2 );
	// Device destruction retires the old handles before a replacement device.
	f.handles.clear();
	f.preview.invalidate();
	assert.equal( f.preview.prepare( f.source, f.geometry, f.images ).length, 1 );
	assert.equal( f.handles.size, 2 );
	f.preview.prepare( null, f.geometry, f.images );
	assert.equal( f.handles.size, 0 );
	f.preview.invalidate();
	const before = f.calls();
	f.preview.prepare( null, f.geometry, f.images );
	f.preview.prepare( null, f.geometry, f.images );
	assert.equal( f.calls(), before + 1 );
	f.preview.dispose( f.geometry, f.images );
	assert.equal( f.closes(), 0 );
});
