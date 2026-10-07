/*
===========================================================================

follow-camera.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
/*
================
load
================
*/
async function load( path ) {
	return import( sourceFileUrl( path ).href );
}
const {
	cameraCollisionParts,
	cameraSegmentHit,
	createCameraQueryCache,
	clearCameraQueryCache,
	followDistance,
	resolveFollowCamera,
	refitAnimatedCameraParts,
	animatedCameraCandidates
} = await load( "src/engine/foundation/rendering/follow-camera.ts" );
const { geometryVertex } = await load( "src/engine/foundation/rendering/picking.ts" );
const { createInput } = await load( "src/engine/runtime/input/input.ts" );
const { cameraWheelDelta, zoomCamera } = await load( "src/engine/foundation/rendering/camera-wheel.ts" );
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
/*
================
group
================
*/
function group( positions, indices = [ 0, 1, 2 ] ) {
	return {
		id: "wall",
		center: [ 0, 0, 0 ],
		radius: 100,
		material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true },
		geometry: {
			positions: new Float32Array( positions ),
			indices: new Uint32Array( indices ),
			instances: identity(),
			transform: identity()
		}
	};
}
/*
================
parts
================
*/
function parts( groups ) {
	return cameraCollisionParts( { id: "test", originRegion: 0, groups, warnings: [] } );
}

test("camera query skips remote animation bounds and refits stale bounds before re-entry", async () => {
	const { createCharacterPose } = await load( "src/engine/foundation/animation/animation-pose.ts" );
	const mesh = group( [ -10, -10, 0, 10, -10, 0, 0, 10, 0 ] );
	mesh.geometry.joints = new Uint32Array( 12 );
	mesh.geometry.weights = Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 );
	mesh.geometry.bones = identity();
	const primitive = { joints: [ 0 ], inverseBind: identity(), geometry: mesh.geometry };
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		primitives: [ primitive ],
		clips: [ {
			name: "move",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1 ),
				values: Float32Array.of( 0, 0, 20, 0, 0, 80 )
			} ]
		} ]
	};
	mesh.animation = { model: "m", primitive: 0, clip: "move" };
	const product = cameraCollisionParts( {
			id: "animated",
			originRegion: 0,
			groups: [ mesh ],
			models: { m: model },
			warnings: []
		} ),
		pose = createCharacterPose( model );
	assert.ok( product[0].sweep );
	const reference = product.map( part => ({
		...part,
		sweep: undefined,
		min: [ ...part.min ],
		max: [ ...part.max ]
	}) );
	const near = { start: [ 0, 0, 0 ], delta: [ 0, 0, 100 ] }, far = { start: [ 5000, 0, 0 ], delta: [ 0, 0, 100 ] };
	for ( let frame = 0; frame < 100; frame++ ) {
		pose.evaluate( "move", frame / 100, false );
		pose.palette( primitive, mesh.geometry.bones );
		const before = [ ...product[0].min ];
		refitAnimatedCameraParts( product, new Set( [ mesh.geometry.bones ] ), far );
		assert.deepEqual( product[0].min, before );
		assert.equal( product[0].boundsDirty, true );
		assert.equal( cameraSegmentHit( product, far ), null );
		// The camera enters the envelope without a new animation revision.
		refitAnimatedCameraParts( product, new Set(), near );
		refitAnimatedCameraParts( reference );
		assert.equal( product[0].boundsDirty, false );
		assert.equal( cameraSegmentHit( product, near ), cameraSegmentHit( reference, near ) );
	}
	primitive.inverseBind[3] = .01;
	const unsupported = cameraCollisionParts( {
		id: "projective",
		originRegion: 0,
		groups: [ mesh ],
		models: { m: model },
		warnings: []
	} );
	assert.equal( unsupported[0].sweep, undefined );
});

test("animated collision tree matches full refits across query movement, seeks and unsupported colliders", async () => {
	const { createCharacterPose } = await load( "src/engine/foundation/animation/animation-pose.ts" );
	const mesh = group( [ -10, -10, 0, 10, -10, 0, 0, 10, 0 ] );
	Object.assign( mesh.geometry, {
		joints: new Uint32Array( 12 ),
		weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ),
		bones: identity()
	} );
	const primitive = { joints: [ 0 ], inverseBind: identity(), geometry: mesh.geometry };
	const model = {
		nodes: [ { name: "root", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		primitives: [ primitive ],
		clips: [ {
			name: "move",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1 ),
				values: Float32Array.of( 0, 0, 20, 0, 0, 80 )
			} ]
		} ]
	};
	const groups = Array.from( { length: 192 }, ( _, i ) => {
		const matrix = identity();
		matrix[12] = (i % 16 - 8) * 100;
		matrix[13] = (Math.floor( i / 16 ) % 4 - 2) * 100;
		matrix[14] = Math.floor( i / 64 ) * 20;
		return {
			...mesh,
			id: String( i ),
			geometry: { ...mesh.geometry, instances: matrix },
			animation: i % 11 ? { model: "m", primitive: 0, clip: "move" } : undefined,
			collision: [ {
				instance: 0,
				object: String( i % 64 ),
				order: 2 - Math.floor( i / 64 ),
				indexStart: 0,
				indexCount: 3
			} ]
		};
	} );
	const product = cameraCollisionParts( { id: "tree", originRegion: 0, groups, models: { m: model }, warnings: [] } ),
		pose = createCharacterPose( model );
	const brute = product.map( part => ({ ...part, sweep: undefined, min: [ ...part.min ], max: [ ...part.max ] }) );
	for ( let frame = 0; frame < 384; frame++ ) {
		pose.evaluate( "move", (frame * 37 % 101) / 100, false );
		pose.palette( primitive, mesh.geometry.bones );
		const ray = {
			start: [ (frame % 16 - 8) * 100, (Math.floor( frame / 16 ) % 4 - 2) * 100, -30 ],
			delta: [ frame % 3 - 1, 0, 180 ]
		};
		const candidates = animatedCameraCandidates( product, ray );
		assert.ok( candidates.length < product.length / 4 );
		refitAnimatedCameraParts( candidates );
		refitAnimatedCameraParts( brute );
		assert.equal( cameraSegmentHit( product, ray ), cameraSegmentHit( brute, ray ), `frame ${frame}` );
	}
});

test("collision preparation keeps the old scene until complete and cancels obsolete work", async () => {
	const { createWorldRenderer } = await load( "src/engine/runtime/renderer/world/world.ts" );
	const wall = group( [ -100, -100, 30, 100, -100, 30, 0, 100, 30 ] );
	wall.geometry.normals = new Float32Array( 9 );
	wall.geometry.uvs = new Float32Array( 6 );
	const scene = { id: "old", originRegion: 0, groups: [ wall ], warnings: [] };
	const owner = createWorldRenderer(),
		geometry = { upload: () => ({}), release() {} },
		textures = { upload: () => ({}), release() {} };
	owner.camera( {
		eye: [ 0, 0, 80 ],
		target: [ 0, 0, 0 ],
		follow: { yaw: 0, pitch: 0, distance: 80 },
		fov: 1,
		near: 1,
		far: 3500
	} );
	owner.scene( scene );
	owner.prepare( geometry, textures, 1 );
	const large = {
		...wall,
		id: "large",
		geometry: { ...wall.geometry, indices: new Uint32Array( Array.from( { length: 30000 }, ( _, i ) => i % 3 ) ) }
	};
	owner.scene( { ...scene, id: "pending", groups: [ large ] } );
	owner.prepare( geometry, textures, 1 );
	assert.equal( owner.stats().sceneId, "old" );
	assert.ok( owner.stats().pendingGroups > 0, "collision work is part of admission readiness" );
	owner.cancelPending();
	owner.prepare( geometry, textures, 1 );
	assert.equal( owner.stats().sceneId, "old" );
	assert.equal( owner.stats().pendingGroups, 0 );
	owner.scene( { ...scene, id: "replacement", groups: [ large ] } );
	let frames = 0;
	while ( owner.stats().sceneId !== "replacement" && frames++ < 100 ) owner.prepare( geometry, textures, 1 );
	assert.ok( frames > 1 && frames < 100 );
	assert.equal( owner.stats().sceneId, "replacement" );
	assert.equal( owner.stats().pendingGroups, 0 );
	owner.dispose( geometry, textures );
});

test("joint envelopes preserve exact camera hits through pose, scale and placement changes", () => {
	const mesh = group( [ -10, -10, 0, 10, -10, 0, 0, 10, 0, -10, -10, 8, 10, -10, 8, 0, 10, 8 ], [
		0,
		1,
		2,
		3,
		4,
		5
	] );
	mesh.geometry.bones = new Float32Array( [ ...identity(), ...identity() ] );
	mesh.geometry.joints = new Uint32Array( Array.from( { length: 24 }, ( _, i ) => i % 2 ) );
	mesh.geometry.weights = new Float32Array(
		Array.from( { length: 24 }, ( _, i ) => i % 4 === 0 ? .2 : i % 4 === 1 ? .65 : i % 4 === 2 ? .15 : 0 )
	);
	const product = parts( [ mesh ] );
	assert.ok( product[0].skin );
	let seed = 19;
	const random = () => ((seed = (Math.imul( seed, 1664525 ) + 1013904223) >>> 0) / 2 ** 32);
	for ( let frame = 0; frame < 120; frame++ ) {
		const bones = mesh.geometry.bones;
		for ( let b = 0; b < 32; b += 16 ) {
			bones.set( identity(), b );
			bones[b] = random() * 3 - 1.5;
			bones[b + 4] = random() - .5;
			bones[b + 5] = random() * 2;
			bones[b + 12] = random() * 8 - 4;
			bones[b + 13] = random() * 8 - 4;
			bones[b + 14] = random() * 60;
		}
		mesh.geometry.instances[0] = frame % 2 ? -1.5 : 2;
		mesh.geometry.instances[12] = random() * 10 - 5;
		refitAnimatedCameraParts( product, new Set( [ bones ] ) );
		const exact = product.map( part => {
			const min = [ Infinity, Infinity, Infinity ], max = [ -Infinity, -Infinity, -Infinity ];
			for ( const index of part.geometry.indices ) {
				const point = geometryVertex( part.geometry, part.matrix, index, bones );
				for ( let axis = 0; axis < 3; axis++ ) {
					assert.ok(
						point[axis] >= part.min[axis] && point[axis] <= part.max[axis],
						`enclosure ${frame}/${axis}`
					);
					min[axis] = Math.min( min[axis], point[axis] );
					max[axis] = Math.max( max[axis], point[axis] );
				}
			}
			return { ...part, min, max };
		} );
		for ( let ray = 0; ray < 12; ray++ ) {
			const segment = {
				start: [ random() * 40 - 20, random() * 40 - 20, -20 ],
				delta: [ random() * 10 - 5, random() * 10 - 5, 120 ]
			};
			assert.equal( cameraSegmentHit( product, segment ), cameraSegmentHit( exact, segment ) );
		}
	}
	// Unsupported signed weights keep the exact refit rather than weakening it.
	mesh.geometry.weights[0] = -.2;
	const signed = parts( [ mesh ] );
	assert.equal( signed[0].skin, undefined );
});
test("native pitch rails, signed wheel step, defaults and zero wheel", () => {
	const input = createInput();
	const commit = commands => {
		for ( const command of commands ) input.accept( { ...command, timeMs: 0 } );
		input.drain();
	};
	assert.equal( input.camera().distance, 80 );
	// CApp_ResetCameraDefaults (818790): +0x370 = 0.19f.
	assert.equal( input.camera().pitch, Math.fround( 0.19 ) );
	commit( [ { kind: "wheel", delta: 0 } ] );
	assert.equal( input.camera().distance, 80 );
	commit( [ { kind: "wheel", delta: -120 } ] );
	assert.equal( input.camera().distance, 74 );
	commit( [ { kind: "wheel", delta: 1 } ] );
	assert.equal( input.camera().distance, Math.fround( 74.05 ) );
	commit( Array.from( { length: 30 }, () => ({ kind: "wheel", delta: 120 }) ) );
	assert.equal( input.camera().distance, 150 );
	commit( Array.from( { length: 30 }, () => ({ kind: "wheel", delta: -120 }) ) );
	assert.equal( input.camera().distance, 10 );
	commit( [ { kind: "pointer", x: 0, y: 0, buttons: 2 }, { kind: "pointer", x: 10, y: -10000, buttons: 2 } ] );
	assert.equal( input.camera().pitch, -1.0707963705062866 );
	commit( [ { kind: "pointer", x: 10, y: 10000, buttons: 2 } ] );
	assert.equal( input.camera().pitch, 1.0707963705062866 );
});
test("negative pitch cap bottoms at native near band and rises past it", () => {
	assert.equal( followDistance( 0, 180 ), 180 );
	assert.equal( followDistance( -.8999999761581421, 180 ), 40 );
	assert.ok( followDistance( -1.0707963705062866, 180 ) > 40 );
	assert.equal( followDistance( -.9, 24 ), 24 );
});

test("mounted follow height uses rider quarter-height and mount anchor rather than ground lift", () => {
	const camera = {
		eye: [ 0, 0, 80 ],
		target: [ 10, 35, 20 ],
		follow: { yaw: 0, pitch: 0, distance: 80, height: 20, mounted: true },
		fov: 1,
		near: 1,
		far: 3500
	};
	const result = resolveFollowCamera( camera, [], null );
	const lift = Math.fround( 14.199999809265137 + (14.199999809265137 - 5) / 140 * (80 - 150) );
	assert.equal( result.camera.target[1], Math.fround( 35 + lift ) );
	assert.notEqual(
		result.camera.target[1],
		resolveFollowCamera( { ...camera, follow: { ...camera.follow, mounted: false } }, [], null ).camera.target[1]
	);
});

test("animated collider updates before camera query, including an unchanged camera and GPU recovery", async () => {
	const { createWorldRenderer } = await load( "src/engine/runtime/renderer/world/world.ts" );
	const { viewProjection } = await load( "src/engine/foundation/rendering/world-math.ts" );
	const wall = group( [ -100, -100, 30, 100, -100, 30, 0, 100, 30 ] );
	wall.geometry.normals = new Float32Array( 9 );
	wall.geometry.uvs = new Float32Array( 6 );
	wall.geometry.bones = identity();
	wall.geometry.joints = new Uint32Array( 12 );
	wall.geometry.weights = new Float32Array( [ 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 ] );
	wall.animation = { model: "door", primitive: 0, clip: "open" };
	const model = {
		nodes: [ { name: "door", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		primitives: [ {
			name: "door",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			geometry: wall.geometry,
			image: -1
		} ],
		images: [],
		clips: [ {
			name: "open",
			duration: 2,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: new Float32Array( [ 0, 1, 2 ] ),
				values: new Float32Array( [ 0, 0, 0, 500, 0, 0, 0, 0, 0 ] )
			} ]
		} ]
	};
	const world = createWorldRenderer(), geometry = { upload: () => ({}), release() {}, updateBones() {} };
	const camera = {
		eye: [ 0, 0, 80 ],
		target: [ 0, 0, 0 ],
		follow: { yaw: 0, pitch: 0, distance: 80 },
		fov: 1,
		near: 1,
		far: 3500
	};
	world.scene( { id: "door", originRegion: 0, groups: [ wall ], models: { door: model }, warnings: [] } );
	world.camera( camera );
	const closed = world.prepare( geometry, {}, 1, 0 ).matrix;
	world.prepare( geometry, {}, 1, 1 );
	const open = world.prepare( geometry, {}, 1, 1 ).matrix;
	assert.deepEqual( open, viewProjection( resolveFollowCamera( camera, [], null ).camera, 1 ) );
	assert.notDeepEqual( open, closed );
	world.invalidate();
	assert.deepEqual( world.prepare( geometry, {}, 1, 1 ).matrix, open );
	assert.notDeepEqual( world.prepare( geometry, {}, 1, 2 ).matrix, open );
	world.dispose( geometry, null );
});
test("world animation samples only draw/query demand and resumes shared poses at current time", async () => {
	const { createWorldRenderer } = await load( "src/engine/runtime/renderer/world/world.ts" );
	const near = group( [ -10, -10, 30, 10, -10, 30, 0, 10, 30 ] );
	near.id = "near";
	near.center = [ 0, 0, 30 ];
	near.radius = 30;
	Object.assign( near.geometry, {
		normals: new Float32Array( 9 ),
		uvs: new Float32Array( 6 ),
		bones: identity(),
		joints: new Uint32Array( 12 ),
		weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 )
	} );
	near.animation = { model: "door", primitive: 0, clip: "move" };
	const far = {
		...near,
		id: "far",
		center: [ 5000, 0, 30 ],
		geometry: { ...near.geometry, bones: identity(), instances: identity() }
	};
	far.geometry.instances[12] = 5000;
	const model = {
		nodes: [ { name: "door", parent: -1, translation: [ 0, 0, 0 ], rotation: [ 0, 0, 0, 1 ], scale: [ 1, 1, 1 ] } ],
		primitives: [ {
			name: "door",
			node: 0,
			joints: [ 0 ],
			inverseBind: identity(),
			geometry: near.geometry,
			image: -1
		} ],
		images: [],
		clips: [ {
			name: "move",
			duration: 1,
			channels: [ {
				node: 0,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.of( 0, 1 ),
				values: Float32Array.of( 0, 0, 0, 10, 0, 0 )
			} ]
		} ]
	};
	const updates = [],
		gpu = {
			upload: g => ({ x: g.instances[12] }),
			release() {},
			updateBones( draw, bones ) {
				updates.push( { x: draw.x, bones: [ ...bones ] } );
			}
		},
		world = createWorldRenderer();
	const camera = x => ({
		eye: [ x, 0, 80 ],
		target: [ x, 0, 0 ],
		follow: { yaw: 0, pitch: 0, distance: 80 },
		fov: 1,
		near: 1,
		far: 3500
	});
	world.scene( { id: "demand", originRegion: 0, groups: [ near, far ], models: { door: model }, warnings: [] } );
	world.camera( camera( 0 ) );
	for ( const time of [ 0, .2, .4 ] ) world.prepare( gpu, {}, 1, time );
	assert.ok( updates.length );
	assert.ok( updates.every( update => update.x === 0 ), "remote scene geometry does not request pose/palette work" );
	updates.length = 0;
	world.camera( camera( 5000 ) );
	world.prepare( gpu, {}, 1, .4 );
	assert.equal(
		updates.find( update => update.x === 5000 ).bones[12],
		4,
		"a newly demanded group receives the already-sampled shared pose"
	);
	updates.length = 0;
	world.prepare( gpu, {}, 1, .4 );
	assert.equal( updates.length, 0, "unchanged palette is not uploaded again" );
	world.invalidate();
	world.prepare( gpu, {}, 1, .4 );
	assert.equal( updates.find( update => update.x === 5000 ).bones[12], 4 );
	world.dispose( gpu, null );
});
test("nearest transformed object pulls camera five units forward; miss restores distance", () => {
	const wall = group( [ -100, -100, 0, 100, -100, 0, 0, 100, 0 ] );
	wall.geometry.instances[14] = 30;
	const camera = {
		eye: [ 0, 0, 80 ],
		target: [ 0, 0, 0 ],
		follow: { yaw: 0, pitch: 0, distance: 80 },
		fov: 1,
		near: 1,
		far: 3500
	};
	const result = resolveFollowCamera( camera, parts( [ wall ] ), null );
	assert.equal( result.collision, 25 );
	assert.equal( result.camera.eye[2], 25 );
	const released = resolveFollowCamera( camera, [], result.collision );
	assert.equal( released.collision, null );
	assert.equal( released.camera.eye[2], 80 );
	assert.notEqual( released.camera.target[1], result.camera.target[1] );
	wall.geometry.instances[12] = 1000;
	assert.equal( resolveFollowCamera( camera, parts( [ wall ] ), null ).collision, null );
});
test("terrain collision retains full heights across rendering seam mutations", () => {
	const terrain = group( [ 0, 0, 0, 20, 0, 0, 0, 0, 20 ] );
	terrain.material.terrain = true;
	terrain.ranges = [ {
		cell: [ 0, 0 ],
		lod: 0,
		indexStart: 0,
		indexCount: 3,
		vertexStart: 0,
		vertexCount: 3,
		center: [ 10, 0, 10 ],
		radius: 20,
		heights: Array( 289 ).fill( 4 )
	} ];
	const product = parts( [ terrain ] );
	terrain.geometry.positions.fill( 999 );
	assert.equal( cameraSegmentHit( product, { start: [ 5, 14, 5 ], delta: [ 0, -20, 0 ] } ), .5 );
	terrain.material.water = {};
	assert.equal( parts( [ terrain ] ).length, 0 );
});
test("live renderer resolves collision before projection and retires it on scene clear", async () => {
	const { createWorldRenderer } = await load( "src/engine/runtime/renderer/world/world.ts" );
	const { viewProjection } = await load( "src/engine/foundation/rendering/world-math.ts" );
	const wall = group( [ -100, -100, 30, 100, -100, 30, 0, 100, 30 ] );
	wall.geometry.normals = new Float32Array( 9 );
	wall.geometry.uvs = new Float32Array( 6 );
	const source = { id: "wall", originRegion: 0x694e, groups: [ wall ], warnings: [] };
	const camera = {
		originRegion: 0x694f,
		eye: [ -1920, 0, 80 ],
		target: [ -1920, 0, 0 ],
		follow: { yaw: 0, pitch: 0, distance: 80 },
		fov: 1,
		near: 1,
		far: 3500
	};
	const world = createWorldRenderer(),
		geometry = { upload: () => ({}), release() {} },
		textures = { upload: () => ({}), release() {} };
	world.scene( source );
	world.camera( camera );
	const local = { ...camera, target: [ 0, 0, 0 ] };
	const first = resolveFollowCamera( local, parts( [ wall ] ), null );
	assert.deepEqual( world.prepare( geometry, textures, 1 ).matrix, viewProjection( first.camera, 1 ) );
	const second = resolveFollowCamera( local, parts( [ wall ] ), first.collision );
	assert.deepEqual( world.prepare( geometry, textures, 1 ).matrix, viewProjection( second.camera, 1 ) );
	for ( let i = 0; i < 10; i++ ) {
		assert.deepEqual( world.prepare( geometry, textures, 1 ).matrix, viewProjection( second.camera, 1 ) );
	}
	world.invalidate();
	assert.deepEqual( world.prepare( geometry, textures, 1 ).matrix, viewProjection( second.camera, 1 ) );
	world.scene( { ...source, id: "clear", groups: [] } );
	assert.deepEqual(
		world.prepare( geometry, textures, 1 ).matrix,
		viewProjection( resolveFollowCamera( local, [], null ).camera, 1 )
	);
	world.dispose( geometry, textures );
});

test("camera selects first hit part per object, then nearest across objects and terrain", () => {
	const triangle = z => [ -10, -10, z, 10, -10, z, 0, 10, z ];
	const far = group( triangle( 80 ) ), near = group( triangle( 20 ) ), other = group( triangle( 50 ) );
	far.id = "far";
	near.id = "near";
	other.id = "other";
	far.collision = [ { instance: 0, object: "one", order: 0, indexStart: 0, indexCount: 3 } ];
	near.collision = [ { instance: 0, object: "one", order: 1, indexStart: 0, indexCount: 3 } ];
	other.collision = [ { instance: 0, object: "two", order: 0, indexStart: 0, indexCount: 3 } ];
	const ray = { start: [ 0, 0, 0 ], delta: [ 0, 0, 100 ] };
	assert.equal(
		cameraSegmentHit( parts( [ near, far ] ), ray ),
		.8,
		"draw order must not override native part order"
	);
	assert.equal(
		cameraSegmentHit( parts( [ other, near, far ] ), ray ),
		.5,
		"far first part suppresses near later part even behind another object"
	);
	far.geometry.instances[12] = 100;
	assert.equal( cameraSegmentHit( parts( [ near, far ] ), ray ), .2, "miss advances to next part" );
	const merged = group( [ ...triangle( 80 ), ...triangle( 20 ) ], [ 0, 1, 2, 3, 4, 5 ] );
	merged.collision = [ { instance: 0, object: "one", order: 0, indexStart: 0, indexCount: 3 }, {
		instance: 0,
		object: "one",
		order: 1,
		indexStart: 3,
		indexCount: 3
	} ];
	assert.equal( cameraSegmentHit( parts( [ merged ] ), ray ), .8, "merged draw preserves separate collision ranges" );
	delete merged.collision;
	assert.equal( cameraSegmentHit( parts( [ merged ] ), ray ), .2, "triangles within one part still select nearest" );
});

test("camera tree preserves ordered brute-force results across sparse scenes and signed rays", () => {
	const groups = [];
	for ( let i = 0; i < 96; i++ ) {
		const mesh = group( [ -9, -9, 0, 9, -9, 0, 0, 9, 0 ] );
		mesh.id = String( i );
		mesh.geometry.instances[12] = (i % 8 - 4) * 23;
		mesh.geometry.instances[13] = (Math.floor( i / 8 ) % 4 - 2) * 21;
		mesh.geometry.instances[14] = 20 + Math.floor( i / 32 ) * 60;
		mesh.collision = [ {
			instance: 0,
			object: String( i % 32 ),
			order: 2 - Math.floor( i / 32 ),
			indexStart: 0,
			indexCount: 3
		} ];
		groups.push( mesh );
	}
	const product = parts( groups ), brute = [ ...product ];
	let seed = 7;
	const random = () => ((seed = (Math.imul( seed, 1664525 ) + 1013904223) >>> 0) / 2 ** 32);
	for ( let i = 0; i < 500; i++ ) {
		const ray = {
			start: [ random() * 240 - 120, random() * 120 - 60, i % 2 ? 200 : -20 ],
			delta: [ random() * 50 - 25, random() * 30 - 15, i % 2 ? -240 : 240 ]
		};
		assert.equal( cameraSegmentHit( product, ray ), cameraSegmentHit( brute, ray ), `ray ${i}` );
	}
});

test("camera responds before worker acknowledgement, drains once and release prevents a stale drag delta", () => {
	const input = createInput(), start = input.camera().yaw;
	input.accept( { kind: "pointer", x: 10, y: 20, buttons: 2, timeMs: 0 } );
	input.accept( { kind: "pointer", x: 30, y: 30, buttons: 2, timeMs: 1 } );
	input.accept( { kind: "wheel", delta: -120, timeMs: 2 } );
	const immediate = input.camera();
	assert.equal( immediate.yaw, start + .1 );
	assert.equal( immediate.distance, 74 );
	// Camera input moves the camera here and sends the worker nothing.
	assert.equal( input.drain(), null );
	assert.deepEqual( input.camera(), immediate );
	input.accept( { kind: "release", timeMs: 3 } );
	assert.deepEqual( input.drain().commands.map( command => command.kind ), [ "release" ] );
	input.accept( { kind: "pointer", x: 999, y: 999, buttons: 2, timeMs: 4 } );
	assert.deepEqual( input.camera(), immediate );
	input.accept( { kind: "pointer", x: 1001, y: 999, buttons: 2, timeMs: 5 } );
	assert.equal( input.camera().yaw, start + .1 + .01 );
	input.accept( { kind: "wheel", delta: NaN, timeMs: 6 } );
	assert.match( input.error(), /Invalid/ );
	assert.equal( input.camera().distance, 74 );
});

test("wheel adapter retains native ticks and fractional/coalesced browser movement", () => {
	assert.equal( cameraWheelDelta( { deltaY: 100, deltaMode: 0, wheelDeltaY: -120 } ), 120 );
	assert.equal( cameraWheelDelta( { deltaY: -100, deltaMode: 0, wheelDeltaY: 120 } ), -120 );
	assert.equal( cameraWheelDelta( { deltaY: 3, deltaMode: 1 } ), 120 );
	assert.equal( cameraWheelDelta( { deltaY: 100, deltaMode: 0 } ), 120 );
	assert.equal( cameraWheelDelta( { deltaY: .25, deltaMode: 0 } ), .3 );
	assert.equal( zoomCamera( 80, cameraWheelDelta( { deltaY: 300, deltaMode: 0 } ) ), 98 );
	assert.equal( zoomCamera( 80, cameraWheelDelta( { deltaY: 0, deltaMode: 0 } ) ), 80 );
});

test("unchanged camera segments retain static work while animated first-part precedence remains live", () => {
	const far = group( [ -10, -10, 80, 10, -10, 80, 0, 10, 80 ] );
	const near = group( [ -10, -10, 20, 10, -10, 20, 0, 10, 20 ] );
	far.geometry.bones = identity();
	far.geometry.joints = new Uint32Array( 12 );
	far.geometry.weights = Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 );
	far.collision = [ { instance: 0, object: "shared", order: 0, indexStart: 0, indexCount: 3 } ];
	near.collision = [ { instance: 0, object: "shared", order: 1, indexStart: 0, indexCount: 3 } ];
	const product = parts( [ near, far ] ), cache = createCameraQueryCache();
	const ray = { start: [ 0, 0, 0 ], delta: [ 0, 0, 100 ] };
	const fixed = product.find( part => !part.geometry.bones ).geometry;
	const positions = fixed.positions;
	let reads = 0;
	Object.defineProperty( fixed, "positions", {
		get() {
			reads++;
			return positions;
		}
	} );
	assert.equal( cameraSegmentHit( product, ray, cache ), .8 );
	far.geometry.bones[12] = 100;
	refitAnimatedCameraParts( product );
	assert.equal( cameraSegmentHit( product, ray, cache ), .2, "animated miss reveals the static later part" );
	assert.ok( reads > 0 );
	reads = 0;
	assert.equal( cameraSegmentHit( product, ray, cache ), .2 );
	assert.equal( reads, 0, "same segment does not revisit immutable triangle positions" );
	far.geometry.bones[12] = 0;
	refitAnimatedCameraParts( product );
	assert.equal( cameraSegmentHit( product, ray, cache ), .8, "animated first hit suppresses retained nearer part" );
	far.geometry.bones[12] = 100;
	refitAnimatedCameraParts( product );
	ray.delta[2] = 50;
	assert.equal( cameraSegmentHit( product, ray, cache ), .4, "in-place ray changes invalidate retained fractions" );
	assert.ok( reads > 0 );
	const replacement = parts( [ group( [ -10, -10, 30, 10, -10, 30, 0, 10, 30 ] ) ] );
	assert.equal(
		cameraSegmentHit( replacement, ray, cache ),
		.6,
		"scene replacement invalidates retained candidates"
	);
	clearCameraQueryCache( cache );
	assert.equal( cache.parts, null );
	assert.equal( cache.hits.size, 0 );
	assert.equal( cache.candidates.length, 0 );
});

test("retained camera query matches fresh collision through turns, misses and animated bounds changes", () => {
	const walls = [];
	for ( let i = 1; i <= 24; i++ ) {
		const wall = group( [ -10, -10, i * 4, 10, -10, i * 4, 0, 10, i * 4 ] );
		wall.id = "wall-" + i;
		wall.collision = [ { instance: 0, object: "object-" + (i % 6), order: i, indexStart: 0, indexCount: 3 } ];
		if ( i % 3 === 0 ) {
			wall.geometry.bones = identity();
			wall.geometry.joints = new Uint32Array( 12 );
			wall.geometry.weights = Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 );
		}
		walls.push( wall );
	}
	const product = parts( walls ), cache = createCameraQueryCache();
	const ray = { start: [ 0, 0, 0 ], delta: [ 0, 0, 100 ] };
	for ( let frame = 0; frame < 200; frame++ ) {
		if ( frame % 10 === 0 ) ray.start[0] = frame % 30 - 10;
		if ( frame % 20 === 0 ) ray.delta[0] = frame % 40 - 20;
		for ( const wall of walls ) {
			if ( wall.geometry.bones ) wall.geometry.bones[12] = frame % 4 === 0 ? 100 : 0;
		}
		refitAnimatedCameraParts( product );
		assert.equal( cameraSegmentHit( product, ray, cache ), cameraSegmentHit( product, ray ), "frame " + frame );
	}
});
