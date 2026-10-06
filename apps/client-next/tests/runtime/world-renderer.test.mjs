/*
===========================================================================

world-renderer.test.mjs - tests for world.ts, random.ts, world-scene.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import fc from "fast-check";
import { defined } from "../helpers/defined.mjs";

const { createWorldRenderer } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/renderer/world/world.ts" ) ).href
);

const { createPresentationRandom } = await import(
	sourceFileUrl( path.join( root, "src/engine/runtime/random/random.ts" ) ).href
);
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
test("retail object visibility advances offscreen and with a stationary camera, and survives GPU loss", () => {
	const world = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) ), uploads = [];
	const geometry = {
		upload: () => ({}),
		release() {},
		/*
		================
		updateInstances
		================
		*/
		updateInstances( draw, matrices, opacity ) {
			uploads.push( { count: matrices.length / 16, opacity: [ ...opacity ] } );
			return draw;
		}
	};
	const textures = { upload: () => ({}), release() {} };
	const value = scene( "fade" );
	value.groups[0].instanceRadius = 2;
	value.groups[0].material.objectFade = true;
	value.groups[0].geometry.instances[14] = 450;
	value.groups[0].visibility = [ {
		id: "placement",
		radius: 0,
		range: 480,
		cells: [ [ 100, 100 ], [ 0, 1 ] ],
		cellRadius: 7
	} ];
	world.scene( value );
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, -1 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	assert.equal( world.prepare( geometry, textures, 1, 0 ).draws.length, 0 );
	assert.equal( world.prepare( geometry, textures, 1, .25 ).draws.length, 0 );
	world.invalidate();
	assert.equal( world.prepare( geometry, textures, 1, .5 ).draws.length, 0 );
	// Turning cannot reset an already visible object; fade state is independent of frustum.
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	world.prepare( geometry, textures, 1, .75 );
	assert.deepEqual( uploads.at( -1 ).opacity, [ 1 ] );
	world.camera( { eye: [ 0, 0, -100 ], target: [ 0, 0, 1 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	world.prepare( geometry, textures, 1, 1 );
	world.prepare( geometry, textures, 1, 1.25 );
	assert.ok( uploads.at( -1 ).opacity[0] > 0 && uploads.at( -1 ).opacity[0] < 1 );
	assert.equal( world.prepare( geometry, textures, 1, 1.5 ).draws.length, 0 );
	world.scene( null );
	world.prepare( geometry, textures, 1, 2 );
	world.scene( value );
	assert.equal( world.prepare( geometry, textures, 1, 3 ).draws.length, 0 );
	world.dispose( geometry, textures );
});
/*
================
scene
================
*/
function scene( id, texture ) {
	return {
		id,
		originRegion: 1,
		warnings: [],
		groups: [ {
			id,
			center: [ 960, 0, 960 ],
			radius: 10000,
			material: { color: [ 1, 1, 1, 1 ], texture, alphaCutoff: 0, blend: false, doubleSided: true },
			geometry: {
				positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
				normals: new Float32Array( 9 ),
				uvs: new Float32Array( 6 ),
				indices: new Uint32Array( [ 0, 1, 2 ] ),
				instances: identity(),
				transform: identity()
			}
		} ]
	};
}

test("world admission shares elapsed CPU budget and keeps the resident scene until replacement completes", () => {
	const world = createWorldRenderer();
	let remaining = 2, uploads = 0;
	const work = {
		remaining: () => remaining,
		spend: ms => {
			remaining = Math.max( 0, remaining - ms );
		},
		level: () => 0
	};
	const geometry = {
		upload() {
			uploads++;
			// Deterministically model an expensive admission unit without sleeping.
			work.spend( 3 );
			return {};
		},
		release() {},
		updateInstances: draw => draw
	};
	const textures = { upload: () => ({}), release() {} };
	world.frameWork( work );
	world.scene( scene( "resident" ) );
	world.prepare( geometry, textures, 1, 0 );
	assert.equal( world.stats().sceneId, "resident" );
	const replacement = scene( "replacement" );
	replacement.groups.push( scene( "second" ).groups[0], scene( "third" ).groups[0] );
	world.scene( replacement );
	world.prepare( geometry, textures, 1, 1 );
	assert.equal( uploads, 1, "no admission after other work exhausts the shared budget" );
	for ( let frame = 0; frame < 3; frame++ ) {
		remaining = 2;
		world.prepare( geometry, textures, 1, frame + 2 );
		assert.equal( uploads, frame + 2, "one expensive unit per displayed frame" );
		assert.equal( world.stats().sceneId, frame === 2 ? "replacement" : "resident" );
	}
	assert.equal( world.stats().pendingGroups, 0 );
	world.dispose( geometry, textures );
});

test("resident alpha is prepared before hover and retained across device recreation", () => {
	let reads = 0;
	const world = createWorldRenderer( undefined, () => {
		reads++;
		return { width: 1, height: 1, pixels: new Uint8Array( [ 255 ] ) };
	} );
	const geometry = { upload: () => ({}), release() {}, updateInstances: draw => draw },
		textures = { upload: () => ({}), release() {} };
	world.scene( scene( "alpha", "/alpha.png" ) );
	world.texture( "/alpha.png", { width: 1, height: 1, close() {} } );
	world.camera( { eye: [ .25, .25, 2 ], target: [ .25, .25, 0 ], near: .1, far: 10000, fov: Math.PI / 3 } );
	world.prepare( geometry, textures, 1, 0 );
	assert.equal( reads, 1 );
	world.pick( { start: [ .25, .25, 1 ], delta: [ 0, 0, -2 ] }, 1 );
	assert.equal( reads, 1 );
	world.invalidate();
	world.prepare( geometry, textures, 1, 1 );
	assert.equal( reads, 1 );
	world.dispose( geometry, textures );
});

test("animated instances use the pose envelope rather than an unskinned vertex box", () => {
	const world = createWorldRenderer(),
		geometry = { upload: () => ({}), release() {}, updateInstances: draw => draw },
		textures = { upload: () => ({}), release() {} };
	const value = scene( "skin" ), group = value.groups[0];
	group.instanceRadius = 600;
	group.geometry.instances[14] = 450;
	group.visibility = [ { id: "placement", radius: 0, range: 1000, cells: [ [ 0, 1 ] ], cellRadius: 7 } ];
	const bones = identity();
	bones[14] = -500;
	value.groups.push( {
		...group,
		id: "animated",
		geometry: {
			...group.geometry,
			bones,
			joints: new Uint32Array( 12 ),
			weights: Float32Array.of( 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0 )
		}
	} );
	world.scene( value );
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, -1 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	assert.equal(
		world.prepare( geometry, textures, 1, 0 ).draws.length,
		1,
		"posed geometry enters view although the rest vertices are behind the camera"
	);
	world.dispose( geometry, textures );
});

test("settled fade reuse preserves native frame freshness when the camera leaves its range", () => {
	const world = createWorldRenderer(), uploads = [];
	const geometry = {
			upload: () => ({}),
			release() {},
			/*
			================
			updateInstances
			================
			*/
			updateInstances( draw, m, a ) {
				uploads.push( [ ...a ] );
				return draw;
			}
		},
		textures = { upload: () => ({}), release() {} };
	const value = scene( "settled" );
	const group = value.groups[0];
	group.instanceRadius = 2;
	group.material.objectFade = true;
	group.geometry.instances[14] = 450;
	group.visibility = [ { id: "placement", radius: 0, range: 1000, cells: [ [ 0, 0 ] ], cellRadius: 15 } ];
	world.scene( value );
	const camera = z =>
		world.camera( { eye: [ 0, 0, z ], target: [ 0, 0, 450 ], near: 1, far: 5000, fov: Math.PI / 3 } );
	camera( 0 );
	for ( let i = 0; i < 120; i++ ) world.prepare( geometry, textures, 1, i / 240 );
	assert.deepEqual( uploads, [ [ 1 ] ], "stationary full-opacity instances are uploaded once" );
	camera( -1000 );
	world.prepare( geometry, textures, 1, .5 );
	assert.deepEqual( uploads.at( -1 ), [ 1 ], "crossing the range starts a fade rather than a stale-frame snap" );
	world.prepare( geometry, textures, 1, .75 );
	assert.ok( Math.abs( defined( uploads.at( -1 ) )[0] - 127 / 255 ) < 1e-6 );
	world.prepare( geometry, textures, 1, 1 );
	assert.equal( world.stats().visibleGroups, 0 );
	world.dispose( geometry, textures );
});
test("retained world selection matches forced full walks through fades, cell gaps and recovery", () => {
	/*
	================
	fixture
	================
	*/
	function fixture() {
		const world = createWorldRenderer();
		let serial = 0;
		const geometry = {
				upload: () => ({ id: serial++, matrices: [], alpha: [] }),
				release() {},
				/*
				================
				updateInstances
				================
				*/
				updateInstances( draw, m, a ) {
					draw.matrices = [ ...m ];
					draw.alpha = a ? [ ...a ] : [];
					return draw;
				}
			},
			textures = { upload: () => ({}), release() {} };
		return { world, geometry, textures };
	}
	const fast = fixture(), reference = fixture(), value = scene( "full-walk" );
	const base = value.groups[0];
	base.instanceRadius = 2;
	base.material.objectFade = true;
	base.geometry.instances = Float32Array.from( [ ...identity(), ...identity() ] );
	base.geometry.instances[14] = 450;
	base.geometry.instances[30] = 1250;
	base.visibility = [ { id: "one", radius: 0, range: 1000, cells: [ [ 0, 1 ] ], cellRadius: 7 }, {
		id: "two",
		radius: 0,
		range: 1400,
		cells: [ [ 0, 3 ] ],
		cellRadius: 7
	} ];
	value.groups.push( { ...base, id: "second-part" } );
	fast.world.scene( value );
	reference.world.scene( value );
	for ( let frame = 0; frame < 420; frame++ ) {
		const z = frame < 120 ? 0 : frame < 260 ? -700 : 0,
			target = frame >= 260 && frame < 340 ? 9600 : 450,
			camera = { eye: [ 0, 0, z ], target: [ 0, 0, target ], near: 1, far: 12000, fov: Math.PI / 3 };
		if ( frame === 180 || frame === 355 ) {
			fast.world.invalidate();
			reference.world.invalidate();
		}
		fast.world.camera( camera );
		// A dungeon-block metadata change invalidates the reference's view cache;
		// this outdoor fixture has no block visibility, so draw semantics stay equal.
		reference.world.camera( { ...camera, dungeonBlock: frame % 2 } );
		const a = fast.world.prepare( fast.geometry, fast.textures, 1, frame / 240 ),
			b = reference.world.prepare( reference.geometry, reference.textures, 1, frame / 240 );
		assert.deepEqual( a.matrix, b.matrix );
		assert.deepEqual( a.draws, b.draws, `frame ${frame}` );
		assert.equal( fast.world.stats().triangles, reference.world.stats().triangles );
	}
	for ( const f of [ fast, reference ] ) f.world.dispose( f.geometry, f.textures );
});

test("native residency follows target-cell changes even when the view matrix is identical", () => {
	const world = createWorldRenderer(),
		geometry = { upload: () => ({}), release() {}, updateInstances: draw => draw },
		textures = { upload: () => ({}), release() {} };
	const value = scene( "association" );
	const group = value.groups[0];
	group.instanceRadius = 2;
	group.material.objectFade = true;
	group.geometry.instances[14] = 450;
	group.visibility = [ { id: "placement", radius: 0, range: 1000, cells: [ [ 0, 1 ] ], cellRadius: 7 } ];
	world.scene( value );
	const prepare = ( target, time ) => {
		world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, target ], near: 1, far: 5000, fov: Math.PI / 3 } );
		return world.prepare( geometry, textures, 1, time );
	};
	const visible = prepare( 450, 0 ), absent = prepare( 9600, .1 );
	assert.deepEqual( absent.matrix, visible.matrix );
	assert.equal( visible.draws.length, 1 );
	assert.equal( absent.draws.length, 0 );
	assert.equal( prepare( 450, .2 ).draws.length, 1 );
	world.invalidate();
	assert.equal( prepare( 450, .3 ).draws.length, 1 );
	world.dispose( geometry, textures );
});

test("pending indexes match full readiness scans through asset arrival, replacement, cancellation and device loss", () => {
	fc.assert(
		fc.property( fc.array( fc.integer( { min: 0, max: 9 } ), { minLength: 30, maxLength: 100 } ), steps => {
			const world = createWorldRenderer(), images = new Set();
			/** @type {any} */ let pending = null;
			let current = null, prune = false, serial = 0;
			const geometry = { upload: () => ({}), release() {} }, textures = { upload: () => ({}), release() {} };
			const needed = () =>
				new Set( (pending?.groups ?? []).flatMap( g => g.paths ).filter( p => !images.has( p ) ) );
			const check = () => {
				const stats = world.stats();
				assert.equal( stats.sceneId, current?.id ?? null );
				assert.equal( stats.pendingGroups, pending?.groups.filter( g => !g.done ).length ?? 0 );
				assert.equal( stats.pendingTextures, needed().size );
				assert.deepEqual( new Set( world.neededTextures() ), needed() );
				const snapshot = world.neededTextures();
				snapshot.push( "external mutation" );
				assert.deepEqual( new Set( world.neededTextures() ), needed() );
			};
			try {
				for ( const step of steps ) {
					if ( step < 3 ) {
						const id = `scene-${++serial}`, count = step === 0 ? 0 : step === 1 ? 3 : 19;
						const value = {
							id,
							originRegion: 1,
							warnings: [],
							groups: Array.from( { length: count }, ( _, i ) => {
								const g = scene( `${id}-${i}`, `t${i % 4}` ).groups[0];
								g.material.frames = [ `t${i % 4}`, `t${(i + 1) % 4}`, `t${i % 4}` ];
								return g;
							} )
						};
						world.scene( value );
						pending = { id, groups: value.groups.map( g => ({ paths: g.material.frames, done: false }) ) };
					} else if ( step < 7 ) {
						const path = `t${step - 3}`;
						world.texture( path, { width: 1, height: 1, close() {} } );
						images.add( path );
					} else if ( step === 7 ) {
						world.cancelPending();
						pending = null;
						prune = true;
					} else if ( step === 8 ) {
						world.invalidate();
						pending = pending ?? current;
						current = null;
						if ( pending ) {
							pending = { ...pending, groups: pending.groups.map( g => ({ ...g, done: false }) ) };
						}
					} else {
						let budget = 8;
						for (
							const g of pending?.groups ?? []
						) {
							if ( !g.done && g.paths.every( p => images.has( p ) ) && budget > 0 ) {
								g.done = true;
								budget--;
							}
						}
						if ( pending && pending.groups.every( g => g.done ) ) {
							current = pending;
							pending = null;
							prune = true;
						}
						if ( prune ) {
							const used = new Set(
								[ ...(pending?.groups ?? []), ...(current?.groups ?? []) ].flatMap( g => g.paths )
							);
							for ( const p of images ) if ( !used.has( p ) ) images.delete( p );
							prune = false;
						}
						world.prepare( geometry, textures, 1 );
					}
					check();
				}
			} finally {
				world.dispose( geometry, textures );
				assert.equal( world.stats().pendingGroups, 0 );
				assert.equal( world.stats().pendingTextures, 0 );
			}
		} ),
		{ seed: 9091915, numRuns: 100 }
	);
});
test("live stars flicker on reuse frames and retain their sequence across streaming and GPU loss", () => {
	const world = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) ),
		control = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) );
	const geometry = { upload: () => ({}), release() {} }, textures = { upload: () => ({}), release() {} };
	const value = scene( "stars" );
	value.groups[0].material.sky = 2;
	value.environment = { startTimeOfDay: 0, ratePerSecond: 0, tracks: { starAlpha: [ { t: 0, value: 1 } ] } };
	const render = ( owner, time ) => [ ...owner.prepare( geometry, textures, 1, time ).environment.slice( 60, 70 ) ];
	world.scene( value );
	control.scene( value );
	assert.deepEqual( render( world, 10 ), Array( 10 ).fill( 0 ) );
	render( control, 10 );
	const flickered = render( world, 10.125 );
	assert.notDeepEqual( flickered, Array( 10 ).fill( 0 ) );
	assert.deepEqual( flickered, render( control, 10.125 ) );
	world.scene( { ...value, id: "adjacent region" } );
	assert.deepEqual( render( world, 10.25 ), render( control, 10.25 ) );
	world.invalidate();
	assert.deepEqual( render( world, 10.375 ), render( control, 10.375 ) );
	world.scene( null );
	control.scene( null );
	render( world, 11 );
	render( control, 11 );
	world.scene( value );
	control.scene( value );
	assert.deepEqual( render( world, 12 ), render( control, 12 ) );
	assert.notDeepEqual( render( world, 12.125 ), Array( 10 ).fill( 0 ) );
	world.dispose( geometry, textures );
	control.dispose( geometry, textures );
});
test("live star admission cannot replace the startup RNG with a published asset seed", () => {
	const world = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) ),
		geometry = { upload: () => ({}), release() {} },
		textures = { upload: () => ({}), release() {} };
	const value = scene( "continued" );
	value.groups[0].material.sky = 2;
	value.starRandomState = 412358345;
	value.environment = { startTimeOfDay: 0, ratePerSecond: 0, tracks: { starAlpha: [ { t: 0, value: 1 } ] } };
	world.scene( value );
	world.prepare( geometry, textures, 1, 0 );
	const at100 = [ ...world.prepare( geometry, textures, 1, .1 ).environment.slice( 60, 70 ) ];
	const alternate = createWorldRenderer( undefined, undefined, createPresentationRandom( 412358345 ) );
	alternate.scene( value );
	alternate.prepare( geometry, textures, 1, 0 );
	assert.notDeepEqual( at100, [ ...alternate.prepare( geometry, textures, 1, .1 ).environment.slice( 60, 70 ) ] );
	alternate.dispose( geometry, textures );
	const control = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) );
	control.scene( value );
	control.prepare( geometry, textures, 1, 0 );
	control.prepare( geometry, textures, 1, .1 );
	world.scene( { ...value, id: "different sky asset", starRandomState: 1 } );
	assert.deepEqual( [ ...world.prepare( geometry, textures, 1, .2 ).environment.slice( 60, 70 ) ], [
		...control.prepare( geometry, textures, 1, .2 ).environment.slice( 60, 70 )
	] );
	world.dispose( geometry, textures );
	control.dispose( geometry, textures );
});
/*
================
fixture
================
*/
function fixture() {
	const released = new Set(), uploads = [];
	const geometry = {
		/*
		================
		upload
		================
		*/
		upload( data ) {
			const draw = { data };
			uploads.push( draw );
			return draw;
		},
		/*
		================
		release
		================
		*/
		release( draw ) {
			assert.ok( !released.has( draw ), "draw released once" );
			released.add( draw );
		}
	};
	const images = {
		/*
		================
		upload
		================
		*/
		upload( source ) {
			return { source };
		},
		release() {}
	};
	const world = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) );
	return { world, released, uploads, prepare: () => world.prepare( geometry, images, 1 ) };
}
test("clearing a visible scene never returns released draws", () => {
	const f = fixture();
	f.world.scene( scene( "A" ) );
	assert.equal( f.prepare().draws.length, 1 );
	f.world.scene( null );
	assert.deepEqual( f.prepare().draws, [] );
	assert.equal( f.released.size, 1 );
	assert.deepEqual( f.prepare().draws, [] );
});
test("prepared origin follows the committed scene independently of its display ID", () => {
	const f = fixture(), first = scene( "arbitrary display name" );
	first.originRegion = 123;
	f.world.scene( first );
	assert.equal( f.prepare().originRegion, 123 );
	assert.equal( f.prepare().originRegion, 123 );
	const next = scene( "unrelated:name", "pending" );
	next.originRegion = 456;
	f.world.scene( next );
	assert.equal( f.prepare().originRegion, 123 );
	f.world.texture( "pending", { width: 1, height: 1, close() {} } );
	assert.equal( f.prepare().originRegion, 456 );
	f.world.invalidate();
	assert.equal( f.prepare().originRegion, 456 );
	f.world.scene( null );
	assert.equal( f.prepare().originRegion, 0 );
});
test("aborting a partially uploaded replacement releases only its resources", () => {
	const world = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) ),
		released = [],
		closed = [];
	const geometry = {
		/*
		================
		upload
		================
		*/
		upload( data, image ) {
			return { data, image };
		},
		/*
		================
		release
		================
		*/
		release( draw ) {
			released.push( draw );
		}
	};
	const textures = {
		/*
		================
		upload
		================
		*/
		upload( source ) {
			return { source };
		},
		/*
		================
		release
		================
		*/
		release( draw ) {
			released.push( draw );
		}
	};
	world.scene( scene( "A", "a" ) );
	world.texture( "a", {
		width: 1,
		height: 1,
		/*
		================
		close
		================
		*/
		close() {
			closed.push( "a" );
		}
	} );
	const current = world.prepare( geometry, textures, 1 ).draws[0];
	const next = scene( "B", "b" );
	next.groups.push( {
		...next.groups[0],
		id: "waiting",
		material: { ...next.groups[0].material, texture: "missing" }
	} );
	world.scene( next );
	world.texture( "b", {
		width: 1,
		height: 1,
		/*
		================
		close
		================
		*/
		close() {
			closed.push( "b" );
		}
	} );
	world.prepare( geometry, textures, 1 );
	assert.equal( world.stats().residentGroups, 2 );
	world.cancelPending();
	assert.equal( world.prepare( geometry, textures, 1 ).draws[0], current );
	assert.deepEqual( closed, [ "b" ] );
	assert.ok( !released.includes( current ) );
	assert.equal( released.length, 2 );
	assert.equal( world.stats().residentGroups, 1 );
	world.invalidate();
	assert.equal( world.prepare( geometry, textures, 1 ).draws.length, 1 );
	assert.equal( world.stats().sceneId, "A" );
	world.dispose( geometry, textures );
	assert.deepEqual( closed, [ "b", "a" ] );
});
test("GPU image identity includes ordered animation frames and survives loss", () => {
	const world = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) ),
		uploads = [],
		released = [];
	const geometry = {
		/*
		================
		upload
		================
		*/
		upload( data, image ) {
			return { image };
		},
		release() {}
	};
	const images = {
		/*
		================
		upload
		================
		*/
		upload( source, frames ) {
			const draw = { frames: frames.map( f => f.name ) };
			uploads.push( draw );
			return draw;
		},
		/*
		================
		release
		================
		*/
		release( draw ) {
			released.push( draw );
		}
	};
	for ( const name of [ "a", "b", "c" ] ) world.texture( name, { name, width: 1, height: 1, close() {} } );
	const show = frames => {
		const value = scene( "A", "a" );
		value.groups[0].material.frames = frames;
		world.scene( value );
		return world.prepare( geometry, images, 1 ).draws[0].image;
	};
	const animated = show( [ "a", "b", "c" ] );
	assert.deepEqual( animated.frames, [ "a", "b", "c" ] );
	assert.equal( show( [ "a", "b", "c" ] ), animated );
	const reordered = show( [ "a", "c", "b" ] );
	assert.notEqual( reordered, animated );
	assert.deepEqual( reordered.frames, [ "a", "c", "b" ] );
	assert.ok( released.includes( animated ) );
	world.invalidate();
	world.prepare( geometry, images, 1 );
	assert.equal( uploads.length, 3 );
	const single = show( undefined );
	assert.deepEqual( single.frames, [ "a" ] );
	world.texture( "b", { name: "b", width: 1, height: 1, close() {} } );
	assert.deepEqual( show( [ "a", "b" ] ).frames, [ "a", "b" ] );
	world.scene( null );
	world.prepare( geometry, images, 1 );
	assert.equal( new Set( released ).size, released.length );
	world.dispose( geometry, images );
});
test("replacement with the same logical ID uses new GPU handles", () => {
	const f = fixture();
	f.world.scene( scene( "A" ) );
	const old = f.prepare().draws[0];
	f.world.scene( scene( "A" ) );
	const next = f.prepare().draws[0];
	assert.notEqual( next, old );
	assert.ok( f.released.has( old ) );
	assert.ok( !f.released.has( next ) );
});
test("device loss before the first upload preserves the requested scene", () => {
	const f = fixture();
	f.world.scene( scene( "first" ) );
	f.world.invalidate();
	assert.equal( f.prepare().draws.length, 1 );
});
test("device loss preserves a pending replacement instead of restoring old content", () => {
	const f = fixture();
	f.world.scene( scene( "A" ) );
	f.prepare();
	const replacement = scene( "B" );
	replacement.groups[0].geometry.positions[0] = 42;
	f.world.scene( replacement );
	f.world.invalidate();
	assert.equal( f.prepare().draws[0].data.positions[0], 42 );
});
test("device loss does not undo a pending scene clear", () => {
	const f = fixture();
	f.world.scene( scene( "A" ) );
	f.prepare();
	f.world.scene( null );
	f.world.invalidate();
	assert.deepEqual( f.prepare().draws, [] );
});
test("A to B to A requests evicted textures again and retains shared textures", () => {
	const f = fixture();
	let closed = 0;
	f.world.scene( scene( "A", "a" ) );
	assert.deepEqual( f.world.neededTextures(), [ "a" ] );
	f.world.texture( "a", {
		width: 1,
		height: 1,
		/*
		================
		close
		================
		*/
		close() {
			closed++;
		}
	} );
	f.prepare();
	f.world.scene( scene( "A2", "a" ) );
	assert.deepEqual( f.world.neededTextures(), [] );
	f.prepare();
	f.world.scene( scene( "B", "b" ) );
	assert.deepEqual( f.world.neededTextures(), [ "b" ] );
	f.world.texture( "b", {
		width: 1,
		height: 1,
		/*
		================
		close
		================
		*/
		close() {
			closed++;
		}
	} );
	f.prepare();
	assert.equal( closed, 1 );
	f.world.scene( scene( "A", "a" ) );
	assert.deepEqual( f.world.neededTextures(), [ "a" ] );
	f.world.texture( "a", {
		width: 1,
		height: 1,
		/*
		================
		close
		================
		*/
		close() {
			closed++;
		}
	} );
	assert.equal( f.prepare().draws.length, 1 );
	assert.equal( closed, 2 );
});
test("admission copies caller metadata and validates optional geometry before mutation", () => {
	const f = fixture(), source = scene( "A" );
	f.world.scene( source );
	source.groups[0].material.color[0] = 0;
	source.groups[0].center[0] = Infinity;
	assert.equal( f.prepare().draws[0].data.material.color[0], 1 );
	const invalid = scene( "B" );
	invalid.groups[0].geometry.maskUVs = new Float32Array( 1 );
	assert.throws( () => f.world.scene( invalid ), /attributes/ );
	assert.equal( f.prepare().draws.length, 1 );
});
test("residency budget includes both current and pending scenes", async () => {
	const { worldSceneBytes } = await import(
		sourceFileUrl( path.join( root, "src/engine/foundation/rendering/world-scene.ts" ) ).href
	);
	const a = scene( "A" ), b = scene( "B" ), limit = worldSceneBytes( a ) + worldSceneBytes( b ) - 1;
	assert.ok( worldSceneBytes( a ) < limit && worldSceneBytes( b ) < limit, "each scene fits independently" );
	const world = createWorldRenderer( limit ),
		geometry = {
			/*
			================
			upload
			================
			*/
			upload( data ) {
				return { data };
			},
			release() {}
		},
		images = { upload() {}, release() {} };
	world.scene( a );
	world.prepare( geometry, images, 1 );
	assert.throws( () => world.scene( b ), /budget/ );
	assert.equal( world.prepare( geometry, images, 1 ).draws.length, 1 );
});

test("LOD selection keeps GPU resources and restores stitched edge heights", async () => {
	const { createWorldDecoder } = await import(
		sourceFileUrl( path.join( root, "src/engine/runtime/assets/worker/world/world.ts" ) ).href
	);
	const heights = Array( 289 ).fill( 0 );
	heights[17 + 16] = 10;
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [ { blockX: 0, blockZ: 0, heights, textureData: Array( 289 ).fill( 0 ) } ] },
		terrainTextures: { tileCatalog: { referencedTiles: [ { textureId: 0, imagePublicPath: "tile" } ] } },
		objects: { placements: [], resources: { meshes: [], bsr: [], materialSets: [] } }
	};
	const world = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) );
	world.scene( createWorldDecoder().decode( new TextEncoder().encode( JSON.stringify( bundle ) ) ) );
	world.texture( "tile", { width: 1, height: 1, close() {} } );
	let uploads = 0, indexWrites = 0, positionWrites = 0, lastIndexCount = 0;
	let fullBytes = 0, sparseBytes = 0;
	const geometry = {
		/*
		================
		upload
		================
		*/
		upload( data ) {
			uploads++;
			return { data, positions: data.positions.slice() };
		},
		release() {},
		/*
		================
		updateIndices
		================
		*/
		updateIndices( draw, indices ) {
			indexWrites++;
			lastIndexCount = indices.length;
		},
		/*
		================
		updatePositions
		================
		*/
		updatePositions( draw, positions, colors, uvs, ranges ) {
			positionWrites++;
			assert.ok( ranges?.length );
			fullBytes += positions.length / 3 * 56;
			for ( const [start, count] of ranges ) {
				draw.positions.set( positions.subarray( start * 3, (start + count) * 3 ), start * 3 );
				sparseBytes += count * 56;
			}
			assert.deepEqual( draw.positions, positions, "every changed seam vertex reaches the GPU mirror" );
		}
	};
	const images = {
		/*
		================
		upload
		================
		*/
		upload() {
			return {};
		},
		release() {}
	};
	const prepare = x => {
		world.camera( { eye: [ x, 500, 10 ], target: [ 160, 0, 160 ], fov: Math.PI / 3, near: 1, far: 5000 } );
		return world.prepare( geometry, images, 1 ).draws[0];
	};
	const draw = prepare( -1280 ), positions = draw.data.positions;
	const edge = [];
	for ( let i = 0; i < positions.length; i += 3 ) {
		if ( positions[i] === 320 && positions[i + 2] === 20 ) edge.push( i + 1 );
	}
	assert.ok( edge.length );
	assert.ok( edge.every( i => positions[i] === 0 ) );
	assert.equal( lastIndexCount, 1536 );
	// Cross the LOD boundary while looking away. Terrain is chosen by eye cell,
	// not by view (the GPU clips what is behind), so the crossing stitches the
	// edge at once and turning back changes nothing.
	world.camera( { eye: [ -960, 500, 10 ], target: [ -2000, 500, 10 ], fov: Math.PI / 3, near: 1, far: 5000 } );
	assert.equal( world.prepare( geometry, images, 1 ).draws[0], draw );
	assert.ok( edge.every( i => positions[i] === 10 ) );
	assert.equal( prepare( -960 ), draw );
	assert.ok( edge.every( i => positions[i] === 10 ) );
	const writes = [ indexWrites, positionWrites ];
	assert.equal( prepare( -960 ), draw );
	assert.deepEqual( [ indexWrites, positionWrites ], writes );
	assert.equal( prepare( -1600 ), draw );
	assert.equal( lastIndexCount, 384 );
	assert.equal( uploads, 1 );
	// A third cell evicts an older candidate plan. Returning must rebuild the
	// right LOD instead of reusing another cell's retained indices.
	for ( const x of [ -960, -1280, -1600, -960, -1280 ] ) {
		prepare( x );
		assert.equal( lastIndexCount, x === -1600 ? 384 : 1536 );
	}
	for ( const [x, height] of [ [ -960, 10 ], [ -1280, 0 ], [ -960, 10 ] ] ) {
		world.camera( { eye: [ x, 500, 10 ], target: [ x - 2000, 500, 10 ], fov: Math.PI / 3, near: 1, far: 5000 } );
		assert.equal( world.prepare( geometry, images, 1 ).draws[0], draw );
		assert.equal( prepare( x ), draw );
		assert.ok( edge.every( i => positions[i] === height ) );
	}
	assert.ok( sparseBytes < fullBytes / 4, `${sparseBytes} sparse bytes versus ${fullBytes} full bytes` );
	world.invalidate();
	const recovered = prepare( -1280 );
	assert.notEqual( recovered, draw );
	assert.ok( edge.every( i => recovered.data.positions[i] === 0 ) );
	prepare( -960 );
	assert.ok( edge.every( i => recovered.data.positions[i] === 10 ) );
	world.dispose( geometry, images );
});

test("object material traversal is owned by the admitted scene, independently of mesh names", () => {
	const f = fixture(), value = scene( "material-order" );
	value.groups = [ "z-wall", "a-window" ].map( ( id, index ) => ({
		...value.groups[0],
		id,
		material: { ...value.groups[0].material, objectFade: true },
		materialOrder: { set: "house.bmt", index }
	}) );
	f.world.scene( value );
	value.groups[0].materialOrder.index = 99;
	assert.equal( f.prepare().draws[0], f.uploads[0] );
	assert.equal( f.prepare().draws[1], f.uploads[1] );
	const invalid = scene( "invalid" );
	invalid.groups[0].materialOrder = { set: "house.bmt", index: NaN };
	assert.throws( () => f.world.scene( invalid ), /material order/ );
	assert.equal( f.prepare().draws[0], f.uploads[0] );
});

test("title terrain stays detailed through movement and GPU loss; gameplay restores distance LOD", async () => {
	const { createWorldDecoder } = await import(
		sourceFileUrl( path.join( root, "src/engine/runtime/assets/worker/world/world.ts" ) ).href
	);
	const heights = Array( 289 ).fill( 0 );
	heights[17 + 16] = 10;
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: { blocks: [ { blockX: 0, blockZ: 0, heights, textureData: Array( 289 ).fill( 0 ) } ] },
		terrainTextures: { tileCatalog: { referencedTiles: [ { textureId: 0, imagePublicPath: "tile" } ] } },
		objects: { placements: [], resources: { meshes: [], bsr: [], materialSets: [] } }
	};
	const decoder = createWorldDecoder(),
		title = { ...decoder.decode( bundle, true ), terrainDetail: "full" },
		mission = decoder.decode( bundle );
	assert.equal( mission.terrainDetail, undefined );
	const world = createWorldRenderer( undefined, undefined, createPresentationRandom( 1 ) );
	let count = 0;
	const geometry = {
		/*
		================
		upload
		================
		*/
		upload( data ) {
			return { data };
		},
		release() {},
		/*
		================
		updateIndices
		================
		*/
		updateIndices( draw, indices ) {
			count = indices.length;
		},
		updatePositions() {}
	};
	const images = {
		/*
		================
		upload
		================
		*/
		upload() {
			return {};
		},
		release() {}
	};
	const prepare = x => {
		world.camera( { eye: [ x, 500, 10 ], target: [ 160, 0, 160 ], fov: Math.PI / 3, near: 1, far: 5000 } );
		return world.prepare( geometry, images, 1 ).draws[0];
	};
	world.scene( title );
	title.terrainDetail = "distance";
	world.texture( "tile", { width: 1, height: 1, close() {} } );
	for ( const x of [ -1280, -1600, -3200 ] ) {
		const draw = prepare( x );
		assert.equal( count, 1536 );
		for ( let i = 0; i < draw.data.positions.length; i += 3 ) {
			if ( draw.data.positions[i] === 320 && draw.data.positions[i + 2] === 20 ) {
				assert.equal( draw.data.positions[i + 1], 10 );
			}
		}
	}
	world.invalidate();
	prepare( -3200 );
	assert.equal( count, 1536 );
	assert.throws( () => world.scene( { ...mission, terrainDetail: "invalid" } ), /terrain detail/ );
	prepare( -3200 );
	assert.equal( count, 1536 );
	world.scene( mission );
	prepare( -1600 );
	assert.equal( count, 384 );
	world.dispose( geometry, images );
});

test("weather emission consumes shared RNG before star trials on the real world owner path", () => {
	const random = createPresentationRandom( 5, 1, 128 ), world = createWorldRenderer( undefined, undefined, random );
	const geometry = { upload: () => ({}), release() {}, updatePositions() {} },
		textures = { upload: () => ({}), release() {} };
	const value = scene( "ordered-stars" );
	value.groups[0].material.sky = 2;
	value.environment = { startTimeOfDay: 0, ratePerSecond: 0, tracks: { starAlpha: [ { t: 0, value: 1 } ] } };
	world.scene( value );
	world.prepare( geometry, textures, 1, 0 );
	world.weather( { mode: 3, amount: 80 } );
	world.prepare( geometry, textures, 1, .2 );
	world.prepare( geometry, textures, 1, .4 );
	random.takeTrace();
	const frame = world.prepare( geometry, textures, 1, .6 ),
		events = random.takeTrace(),
		flicker = events.findIndex( e => e.operation === "flicker" );
	assert.ok( frame.environment[51] > 0, "stars remain visible during the weather transition" );
	assert.ok( flicker >= 4, "snow admission makes four random calls before star rendering" );
	assert.ok( events.slice( 0, flicker ).every( e => e.operation === "range" ) );
	assert.notEqual( events[flicker].stateBefore, events[flicker].stateAfter );
	world.dispose( geometry, textures );
});

test("decal texture bindings are rebuilt after terrain retirement and reused while resident", () => {
	const world = createWorldRenderer(),
		draws = [],
		released = new Set(),
		images = [],
		geometry = {
			/*
			================
			upload
			================
			*/
			upload( data, image ) {
				const draw = { data, image };
				draws.push( draw );
				return draw;
			},
			updatePositions() {},
			/*
			================
			updateIndices
			================
			*/
			updateIndices( draw ) {
				return draw;
			},
			/*
			================
			release
			================
			*/
			release( draw ) {
				released.add( draw );
			}
		},
		textures = {
			/*
			================
			upload
			================
			*/
			upload( source, frames, mips ) {
				const image = { source, mips };
				images.push( image );
				return image;
			},
			release() {}
		};
	const terrain = scene( "terrain" );
	terrain.groups[0].material.terrain = true;
	terrain.groups[0].ranges = [ {
		cell: [ 0, 0 ],
		lod: 0,
		indexStart: 0,
		indexCount: 3,
		vertexStart: 0,
		vertexCount: 3,
		center: [ 160, 0, 160 ],
		radius: 230,
		heights: Array( 289 ).fill( 0 )
	} ];
	const prepare = () => world.prepare( geometry, textures, 1 );
	world.scene( terrain );
	for ( const path of world.neededTextures() ) world.texture( path, { width: 4, height: 4, close() {} } );
	world.selectionDecal( { pose: { regionId: 1, x: 30, y: 0, z: 30, angle: 0 }, slot: 0 } );
	const first = prepare().decalDraws[0];
	assert.ok( first );
	assert.equal( first.image.mips, false );
	assert.equal( prepare().decalDraws[0], first );
	world.scene( null );
	prepare();
	world.scene( terrain );
	for ( const path of world.neededTextures() ) world.texture( path, { width: 4, height: 4, close() {} } );
	const second = prepare().decalDraws[0];
	assert.ok( second );
	assert.notEqual( second, first );
	assert.notEqual( second.image, first.image );
	assert.ok( released.has( first ) );
	world.dispose( geometry, textures );
});

test("toe decals reuse geometry, fade at twenty seconds and survive device recreation without stale draws", () => {
	const world = createWorldRenderer(),
		released = new Set(),
		geometry = {
			/*
			================
			upload
			================
			*/
			upload( data, image ) {
				return { data, image };
			},
			updatePositions() {},
			/*
			================
			updateIndices
			================
			*/
			updateIndices( d ) {
				return d;
			},
			/*
			================
			release
			================
			*/
			release( d ) {
				released.add( d );
			},
			/*
			================
			updateMaterialColors
			================
			*/
			updateMaterialColors( d, c ) {
				d.tint = [ ...c ];
			},
			/*
			================
			updateInstances
			================
			*/
			updateInstances( d, m, a ) {
				d.alpha = a[0];
				return d;
			}
		},
		textures = { upload: () => ({}), release() {} };
	const terrain = scene( "feet" );
	terrain.groups[0].material.terrain = true;
	terrain.groups[0].ranges = [ {
		cell: [ 0, 0 ],
		lod: 0,
		indexStart: 0,
		indexCount: 3,
		vertexStart: 0,
		vertexCount: 3,
		center: [ 160, 0, 160 ],
		radius: 230,
		heights: Array( 289 ).fill( 0 )
	} ];
	world.scene( terrain );
	for ( const path of world.neededTextures() ) world.texture( path, { width: 4, height: 4, close() {} } );
	world.footprints( [ {
		id: 1,
		pose: { regionId: 1, x: 30, y: 0, z: 30 },
		yaw: 0,
		right: false,
		surface: "SNOW",
		started: 0
	} ] );
	const prepare = t => world.prepare( geometry, textures, 1, t ).groundDecalDraws;
	const first = prepare( 0 )[0];
	assert.ok( first );
	assert.equal( first.data.material.groundDecal, true );
	assert.equal( first.alpha, 1 );
	assert.equal( prepare( 10 )[0], first );
	assert.equal( prepare( 19.49 )[0], first );
	assert.ok( Math.abs( first.alpha - 127 / 255 ) < 1e-6 );
	world.invalidate();
	const restored = prepare( 19.6 )[0];
	assert.ok( restored );
	assert.notEqual( restored, first );
	assert.deepEqual( prepare( 20 ), [] );
	assert.ok( released.has( restored ) );
	world.footprints( [ {
		id: 2,
		pose: { regionId: 1, x: 30, y: 0, z: 30 },
		yaw: 0,
		right: true,
		surface: "SAND",
		started: 20
	} ] );
	const second = prepare( 20 )[0];
	assert.ok( second );
	world.footprints( [] );
	assert.deepEqual( prepare( 20.1 ), [] );
	assert.ok( released.has( second ) );
	world.dispose( geometry, textures );
});

test("selection texture demand follows scene admission, cancellation, replacement and GPU recovery", () => {
	const world = createWorldRenderer(),
		geometry = { upload: () => ({}), release() {}, updateIndices() {}, updatePositions() {} },
		textures = { upload: () => ({}), release() {} };
	const terrain = scene( "demand" );
	terrain.groups[0].material.terrain = true;
	terrain.groups[0].ranges = [ {
		cell: [ 0, 0 ],
		lod: 0,
		indexStart: 0,
		indexCount: 3,
		vertexStart: 0,
		vertexCount: 3,
		center: [ 160, 0, 160 ],
		radius: 230,
		heights: Array( 289 ).fill( 0 )
	} ];
	const expected = [ 1, 2, 3, 4 ].map( i => `/assets/images/Media_extracted/effect/select_0${i}.png` );
	const demand = () => world.neededTextures().filter( path => path.includes( "/effect/select_" ) );
	const prepare = () => world.prepare( geometry, textures, 1 );
	try {
		assert.deepEqual( demand(), [] );
		world.scene( terrain );
		assert.deepEqual( demand(), expected );
		world.cancelPending();
		assert.deepEqual( demand(), [] );
		world.scene( { ...terrain, residency: "frontend" } );
		assert.deepEqual( demand(), [] );
		prepare();
		world.scene( terrain );
		assert.deepEqual( demand(), expected );
		world.invalidate();
		assert.deepEqual( demand(), expected );
		for ( const path of expected ) world.texture( path, { width: 4, height: 4, close() {} } );
		prepare();
		assert.deepEqual( demand(), [] );
		world.invalidate();
		assert.deepEqual( demand(), [] );
		prepare();
		world.scene( null );
		prepare();
		assert.deepEqual( demand(), [] );
		world.scene( terrain );
		assert.deepEqual( demand(), expected );
	} finally {
		world.dispose( geometry, textures );
	}
	assert.deepEqual( demand(), [] );
});

test("sixteen material pieces share one fade tick; an outdoor scene switch keeps the placement fade", () => {
	const world = createWorldRenderer(), textures = { upload: () => ({}), release() {} };
	const geometry = {
		upload: () => ({}),
		release() {},
		/*
		================
		updateInstances
		================
		*/
		updateInstances( draw, m, a ) {
			draw.alpha = a?.[0];
			return draw;
		}
	};
	const value = scene( "many-pieces" ), base = value.groups[0];
	base.instanceRadius = 2;
	base.material.objectFade = true;
	base.geometry.instances[14] = 450;
	base.visibility = [ { id: "building", radius: 0, range: 480, cells: [ [ 0, 1 ] ], cellRadius: 7 } ];
	value.groups = Array.from( { length: 16 }, ( _, i ) => ({ ...base, id: "piece-" + i }) );
	world.scene( value );
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, 450 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	const check = ( time, alpha ) => {
		let frame;
		for ( let attempt = 0; attempt < 16; attempt++ ) {
			frame = world.prepare( geometry, textures, 1, time );
			if ( !world.stats().pendingGroups ) break;
		}
		assert.equal( frame.draws.length, 16 );
		for ( const draw of frame.draws ) {
			assert.ok( Math.abs( draw.alpha - alpha ) < 1e-6, `time ${time}: ${draw.alpha} != ${alpha}` );
		}
	};
	check( 0, 0 );
	check( .125, 64 / 255 );
	world.invalidate();
	check( .125, 64 / 255 );
	check( .25, 128 / 255 );
	check( .5, 1 );
	// Retained stationary frames must materialize their stamp before movement.
	// Otherwise the >50-frame out-of-range branch wrongly hides this building.
	for ( let i = 1; i <= 100; i++ ) check( .5 + i / 240, 1 );
	world.camera( { eye: [ 0, 0, -80 ], target: [ 0, 0, 450 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	check( 1, 1 );
	check( 1.125, 191 / 255 );
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, 450 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	check( 1.25, 191 / 255 );
	check( 1.375, 1 );
	// A region crossing replaces the scene but keeps this placement: it must not
	// fade in again. A placement the new scene does not have starts from zero.
	world.scene( { ...value, id: "crossed" } );
	check( 1.5, 1 );
	const moved = value.groups.map( group => ({
		...group,
		visibility: [ { ...group.visibility[0], id: "another-building" } ]
	}) );
	world.scene( { ...value, id: "elsewhere", groups: moved } );
	check( 1.625, 0 );
	check( 1.75, 64 / 255 );
	world.dispose( geometry, textures );
});

test("scenery follows resident placement admission while retaining offscreen and stationary clocks", () => {
	const world = createWorldRenderer(),
		geometry = { upload: () => ({}), release() {}, updateInstances: draw => draw },
		textures = { upload: () => ({}), release() {} };
	const value = scene( "emitter" ), group = value.groups[0];
	group.instanceRadius = 2;
	group.geometry.instances[14] = 450;
	group.visibility = [ { id: "placement", radius: 0, range: 1000, cells: [ [ 0, 1 ] ], cellRadius: 7 } ];
	value.scenery = [ {
		id: "fire",
		placement: "placement",
		model: "/assets/effects/programs.json#map/fire.efp",
		pose: { regionId: 1, x: 0, y: 0, z: 450, yaw: 0 },
		basis: [ 1, 0, 0, 0, 1, 0, 0, 0, 1 ],
		nightOnly: false,
		renderPriority: 0
	} ];
	world.scene( value );
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, -1 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	world.prepare( geometry, textures, 1, 0 );
	assert.equal( world.scenery().emitters.length, 1, "offscreen is still resident" );
	for ( let i = 1; i < 4; i++ ) {
		world.prepare( geometry, textures, 1, i / 60 );
		assert.equal( world.scenery().emitters.length, 1, "stationary selection retains admission" );
	}
	world.camera( { eye: [ 0, 0, 0 ], target: [ 3200, 0, 0 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	world.prepare( geometry, textures, 1, .1 );
	assert.equal( world.scenery().emitters.length, 0, "stale alpha outside associated cells cannot retain emitter" );
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	world.prepare( geometry, textures, 1, .2 );
	assert.equal( world.scenery().emitters.length, 1 );
	world.invalidate();
	world.prepare( geometry, textures, 1, .3 );
	assert.equal( world.scenery().emitters.length, 1 );
	world.scene( null );
	world.prepare( geometry, textures, 1, .4 );
	assert.equal( world.scenery(), null );
	world.dispose( geometry, textures );
});

test("background distance changes the live culling frustum and restores distant draws without moving the camera", () => {
	const world = createWorldRenderer(),
		geometry = { upload: () => ({}), release() {}, updateInstances: draw => draw },
		textures = { upload: () => ({}), release() {} };
	const value = scene( "distance" );
	value.groups[0].geometry.instances[14] = 3000;
	value.groups[0].instanceRadius = 1;
	world.scene( value );
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], near: 1, far: 3500, fov: Math.PI / 3 } );
	assert.equal( world.prepare( geometry, textures, 1, 0, 100, 100, 1500 ).draws.length, 0 );
	assert.equal( world.prepare( geometry, textures, 1, 1, 100, 100, 3500 ).draws.length, 1 );
	assert.equal( world.prepare( geometry, textures, 1, 2, 100, 100, 1500 ).draws.length, 0 );
	world.invalidate();
	assert.equal( world.prepare( geometry, textures, 1, 3, 100, 100, 5500 ).draws.length, 1 );
	world.dispose( geometry, textures );
});

test("frontend residency uses the background draw distance as the camera far plane", () => {
	const world = createWorldRenderer(),
		geometry = { upload: () => ({}), release() {}, updateInstances: draw => draw },
		textures = { upload: () => ({}), release() {} };
	const value = scene( "frontend-distance" );
	value.residency = "frontend";
	world.scene( value );
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, 1 ], near: 1, far: 3500, fov: Math.PI / 3 } );
	assert.equal( world.prepare( geometry, textures, 1, 0, 100, 100, 1500 ).camera.far, 1500 );
	assert.equal( world.prepare( geometry, textures, 1, 1, 100, 100, 5500 ).camera.far, 5500 );
	world.dispose( geometry, textures );
});
