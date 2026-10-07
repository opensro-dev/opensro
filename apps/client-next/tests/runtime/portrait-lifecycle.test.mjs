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
	let calls = 0, closes = 0, failRelease = false, actorTime = 0;
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
		actors
		================
		*/
		actors( actors ) {
			actorTime = actors[0]?.time ?? 0;
			characters.actors( actors );
		},
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
		actorTime: () => actorTime,
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

test("appending portrait clips preserves geometry, borrowed images and the preview clock", () => {
	const f = fixture();
	const initial = f.preview.prepare( f.source, f.geometry, f.images, { yaw: 0, seconds: 10 } );
	const handles = [ ...f.handles ];
	const clip = { name: "new-action", duration: 1, channels: [] };
	const extended = { ...f.source, model: { ...f.source.model, clips: [ ...f.source.model.clips, clip ] } };
	const next = f.preview.prepare( extended, f.geometry, f.images, { yaw: 0, seconds: 12 } );
	assert.equal( next[0], initial[0], "clip admission must not replace draw handles" );
	assert.deepEqual( [ ...f.handles ], handles );
	assert.equal( f.characters.stats().poseCreations, 0 );
	assert.equal( f.actorTime(), 2, "the preview clock must not restart on a new clip" );
	assert.equal( f.closes(), 0 );
	const switched = { ...extended, actor: { ...extended.actor, previewClip: "new-action" } };
	assert.equal( f.preview.prepare( switched, f.geometry, f.images, { yaw: 0, seconds: 13 } )[0], initial[0] );
	assert.equal( f.actorTime(), 3 );
	// A real skeleton replacement still follows the full resource lifecycle.
	const replaced = { ...extended, model: { ...extended.model, nodes: structuredClone( extended.model.nodes ) } };
	f.preview.prepare( replaced, f.geometry, f.images, { yaw: 0, seconds: 14 } );
	assert.equal( f.actorTime(), 0 );
	assert.ok( handles.every( handle => !f.handles.has( handle ) ) );
	f.preview.dispose( f.geometry, f.images );
	assert.equal( f.handles.size, 0 );
});

test("a body clip append cannot conceal replacement geometry on a retained portrait child", () => {
	const f = fixture();
	const child = { ...f.source, actor: { ...f.source.actor, gid: 2, model: "child" } };
	const source = { ...f.source, children: [ child ] };
	f.preview.prepare( source, f.geometry, f.images );
	const previous = [ ...f.handles ];
	const model = {
		...f.source.model,
		clips: [ ...f.source.model.clips, { name: "action", duration: 1, channels: [] } ]
	};
	const childModel = { ...child.model, primitives: structuredClone( child.model.primitives ) };
	f.preview.prepare( { ...source, model, children: [ { ...child, model: childModel } ] }, f.geometry, f.images );
	assert.ok( previous.every( handle => !f.handles.has( handle ) ) );
	assert.equal( f.characters.portraitSource( 2 )?.model, childModel );
	f.preview.dispose( f.geometry, f.images );
	assert.equal( f.handles.size, 0 );
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

test("an unchanged HUD portrait reuses its draws until a drawing input changes, even in place", () => {
	const f = fixture();
	const tint = /** @type {[number, number, number]} */ ([ 1, 1, 1 ]);
	/** @type {import("../../src/engine/contracts/portrait.ts").PortraitSource & { actor: any }} */
	const source = { ...f.source, actor: { ...f.source.actor, materialTint: tint } };
	const first = f.preview.prepare( source, f.geometry, f.images );
	const prepared = f.calls();
	for ( let frame = 0; frame < 10; frame++ ) assert.equal( f.preview.prepare( source, f.geometry, f.images ), first );
	assert.equal( f.calls(), prepared, "a frozen HUD pose must not prepare again" );
	// Fields the HUD preview replaces cannot change its draws.
	source.actor.pose = { regionId: 9, x: 5, y: 6, z: 7, yaw: radians( 1 ) };
	source.actor.time = 42;
	f.preview.prepare( source, f.geometry, f.images );
	assert.equal( f.calls(), prepared );
	// An in-place edit of a drawing input is seen: the snapshot is an owned copy.
	tint[0] = 0.5;
	f.preview.prepare( source, f.geometry, f.images );
	assert.equal( f.calls(), prepared + 1 );
	f.preview.prepare( source, f.geometry, f.images );
	assert.equal( f.calls(), prepared + 1 );
	// Device invalidation and the animated doll path always prepare.
	f.preview.invalidate();
	f.preview.prepare( source, f.geometry, f.images );
	assert.equal( f.calls(), prepared + 2 );
	f.preview.prepare( source, f.geometry, f.images, { yaw: 0, seconds: 1 } );
	f.preview.prepare( source, f.geometry, f.images, { yaw: 0, seconds: 1 } );
	assert.equal( f.calls(), prepared + 4, "the inventory doll is never retained" );
	// invalidate() models a lost device, whose old handles are dropped rather
	// than released, so resource retirement is covered by the tests above.
	f.preview.dispose( f.geometry, f.images );
});

test("a retained HUD portrait keeps drawing from its own uploads after the world closes the borrowed image", () => {
	const f = fixture();
	const first = f.preview.prepare( f.source, f.geometry, f.images );
	const owned = f.handles.size;
	assert.ok( owned > 0, "the preview uploaded its own GPU copies" );
	// The world retires its bitmap; the source still lists the same object.
	f.source.images[0].close();
	assert.equal( f.closes(), 1 );
	assert.equal( f.preview.prepare( f.source, f.geometry, f.images ), first );
	assert.equal( f.handles.size, owned, "retained draws still name the preview's live handles" );
	f.preview.dispose( f.geometry, f.images );
	assert.equal( f.handles.size, 0 );
});
