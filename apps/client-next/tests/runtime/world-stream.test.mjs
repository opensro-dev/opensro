/*
===========================================================================

world-stream.test.mjs - tests for the client modules it imports

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
/*
================
load
================
*/
async function load( file ) {
	return import( sourceFileUrl( path.join( root, file ) ).href );
}
const { createWorldStream } = await load( "src/engine/runtime/world/world.ts" );
const { prepareWorldScene, worldSceneTransfers } = await load( "src/engine/foundation/rendering/world-scene.ts" );
const { createWorldLease } = await load( "src/engine/runtime/assets/world-lease.ts" );
const { createWorldRenderer } = await load( "src/engine/runtime/renderer/world/world.ts" );
const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );
/*
================
scene

True when request r asks for the scene of bundle (its objects; the
terrain of each region is a separate part request).
================
*/
function scene( r, bundle ) {
	const url = new URL( r.url );
	return url.pathname === "/assets/" + bundle &&
		new URLSearchParams( url.hash.slice( 1 ) ).get( "part" ) === "objects";
}
/*
================
fixture
================
*/
function fixture( options = {} ) {
	let id = 0;
	const ready = new Map(), requests = [], cancelled = [], textures = [];
	const world = createWorldRenderer();
	let camera;
	const assets = {
		available: () => 4 - ready.size,
		/*
		================
		request
		================
		*/
		request( url, limit, decode ) {
			const key = ++id;
			requests.push( { key, url, decode } );
			if ( decode === "world" ) {
				// The stream asks for an outdoor scene as its objects plus one terrain
				// part per region, all in the anchor region's coordinates.
				const region = url.includes( "/a.json" ) ? 1 : url.includes( "/c.json" ) ? 3 : 2,
					hash = new URLSearchParams( new URL( url ).hash.slice( 1 ) ),
					part = hash.get( "part" ) ?? "all",
					anchor = hash.get( "anchor" ),
					origin = anchor === null ? region : Number.parseInt( anchor, 16 );
				ready.set( key, {
					kind: "world",
					id: key,
					scene: {
						id: `${region}:${part}`,
						originRegion: origin,
						warnings: [],
						groups: [ {
							id: `${part}:${region}`,
							...(part === "terrain" ? { terrainSector: region } : {}),
							center: [ 0, 0, 0 ],
							radius: 10000,
							material: {
								texture: `/assets/${region}${options.blockTextures ? ".texture" : ".png"}`,
								color: [ 1, 1, 1, 1 ],
								alphaCutoff: 0,
								blend: false,
								doubleSided: true
							},
							geometry: {
								positions: new Float32Array( [ 0, 0, 0, 1, 0, 0, 0, 1, 0 ] ),
								normals: new Float32Array( 9 ),
								uvs: new Float32Array( 6 ),
								indices: new Uint32Array( [ 0, 1, 2 ] ),
								instances: identity(),
								transform: identity()
							}
						} ]
					}
				} );
			} else if ( decode === "png" ) {
				ready.set( key, {
					kind: "image",
					id: key,
					image: { width: 1, height: 1, close() {} }
				} );
			} else if ( String( url ).endsWith( ".texture" ) ) {
				// A published NTX1 container: opaque DXT1 red, one block per mip level.
				const block = new Uint8Array( 8 );
				new DataView( block.buffer ).setUint16( 0, 0xf800, true );
				const bytes = new Uint8Array( 20 + 3 * block.length ), view = new DataView( bytes.buffer );
				[ 0x3158544e, 4, 4, 0x31545844, 3 ].forEach( ( value, index ) =>
					view.setUint32( index * 4, value, true )
				);
				for ( let level = 0; level < 3; level++ ) bytes.set( block, 20 + level * block.length );
				ready.set( key, { kind: "bytes", id: key, buffer: bytes.buffer } );
			} else {ready.set( key, {
					kind: "bytes",
					id: key,
					buffer: new TextEncoder().encode(
						JSON.stringify( {
							regionsById: {
								"0x0001": [ { bundlePublicPath: "/assets/a.json" } ],
								"0x0002": [ { bundlePublicPath: "/assets/b.json" } ]
							}
						} )
					).buffer
				} );}
			return key;
		},
		/*
		================
		take
		================
		*/
		take( key ) {
			const value = ready.get( key );
			ready.delete( key );
			if ( value?.kind === "world" ) {
				const prepared = prepareWorldScene( value.scene );
				return {
					kind: "world",
					id: key,
					world: createWorldLease(
						structuredClone( prepared, { transfer: worldSceneTransfers( prepared.scene ) } )
					)
				};
			}
			return value ?? null;
		},
		/*
		================
		cancel
		================
		*/
		cancel( key ) {
			cancelled.push( key );
			ready.delete( key );
		}
	};
	const renderer = {
		cancelWorldUpdate: () => world.cancelPending(),
		setWorld: scene => world.scene( scene ),
		adoptWorld: lease => world.adopt( lease ),
		setWorldTexture( path, image ) {
			textures.push( { path, image } );
			world.texture( path, image );
		},
		neededWorldTextures: () => world.neededTextures(),
		worldStats: () => world.stats(),
		/*
		================
		setWorldCamera
		================
		*/
		setWorldCamera( value ) {
			camera = value;
			world.camera( value );
		}
	};
	const stream = createWorldStream( assets, renderer, "https://assets.test" );
	/*
	================
	step
	================
	*/
	function step( region, controls ) {
		stream.step( { regionId: region, x: 0, y: 0, z: 0, angle: 0 }, controls );
		world.prepare( { upload: data => ({ data }), release() {} }, { upload: () => ({}), release() {} }, 1 );
		assert.equal( stream.error(), null );
	}
	return { stream, world, step, requests, cancelled, ready, textures, camera: () => camera };
}
test("stream revisits an evicted region and finishes its texture transaction", () => {
	const f = fixture();
	for ( const region of [ 1, 2, 1 ] ) for ( let i = 0; i < 5; i++ ) f.step( region );
	assert.equal( f.requests.filter( r => r.url.endsWith( "/1.png" ) ).length, 2 );
	assert.equal( f.world.stats().pendingTextures, 0 );
	assert.equal( f.world.stats().visibleGroups, 1 );
});
test("published block textures decode to native GPU resources through the stream", () => {
	const f = fixture( { blockTextures: true } );
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	const set = f.textures.find( t => t.path === "/assets/1.texture" );
	assert.ok( set, "the block container is requested and admitted" );
	assert.equal( set.image.format, "bc1-rgba-unorm" );
	assert.equal( set.image.levels.length, 3 );
	assert.equal( f.world.stats().pendingTextures, 0 );
});
test("reset cancels outstanding jobs and permits a fresh world transaction", () => {
	const f = fixture();
	f.step( 1 );
	f.stream.reset();
	assert.equal( f.cancelled.length, 1 );
	for ( let i = 0; i < 5; i++ ) f.step( 2 );
	assert.equal( f.world.stats().pendingTextures, 0 );
});
test("committed camera controls change the actual world view", () => {
	const f = fixture();
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	const before = f.camera();
	f.step( 1, { yaw: 0.5, pitch: 0.7, distance: 300 } );
	assert.notDeepEqual( f.camera().eye, before.eye );
	assert.deepEqual( f.camera().target, before.target );
});

test("returning from a failed neighbor reuses the still-displayed region", () => {
	const f = fixture();
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	f.step( 2 );
	const job = f.requests.slice().reverse().find( r => scene( r, "b.json" ) );
	f.ready.set( job.key, { kind: "error", id: job.key, error: "neighbor unavailable" } );
	f.stream.step( { regionId: 2, x: 0, y: 0, z: 0, angle: 0 } );
	assert.match( f.stream.error(), /neighbor unavailable/ );
	const count = f.requests.length;
	for ( let i = 0; i < 8; i++ ) f.step( 1 );
	assert.equal( f.requests.length, count );
	assert.equal( f.world.stats().sceneId, "1:objects" );
	assert.equal( f.world.stats().pendingGroups, 0 );
	f.stream.reset();
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	assert.ok( f.requests.length > count, "reset invalidates displayed-region reuse" );
});
test("failed scene transaction preserves current scene and camera, then retries with fresh handles", () => {
	const f = fixture();
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	f.step( 2 );
	const worldJob = f.requests.slice().reverse().find( r => scene( r, "b.json" ) ),
		replacement = f.ready.get( worldJob.key ).scene;
	replacement.groups.push( {
		...replacement.groups[0],
		id: "extra",
		material: { ...replacement.groups[0].material, texture: "/assets/extra.png" }
	} );
	f.step( 2 );
	const textures = f.requests.filter( r => r.decode === "png" && f.ready.has( r.key ) );
	assert.equal( textures.length, 2 );
	f.ready.set( textures[0].key, { kind: "error", id: textures[0].key, error: "HTTP 503" } );
	const pose = { regionId: 2, x: 100, y: 0, z: 0, angle: 0 };
	f.stream.step( pose );
	assert.match( f.stream.error(), /503/ );
	assert.ok( f.cancelled.includes( textures[1].key ) );
	assert.equal( f.ready.size, 0 );
	assert.equal( f.world.stats().sceneId, "1:objects" );
	assert.equal( f.world.stats().pendingGroups, 0 );
	const requests = f.requests.length;
	f.stream.step( { ...pose, x: 200 } );
	assert.equal( f.camera().target[0], 200 );
	assert.equal( f.requests.length, requests );
	f.stream.retry();
	for ( let i = 0; i < 5; i++ ) f.step( 2 );
	assert.equal( f.world.stats().sceneId, "2:objects" );
	assert.equal( f.stream.error(), null );
	assert.ok( f.requests.slice( requests ).some( r => scene( r, "b.json" ) ), "the retry requests fresh handles" );
	const retried = f.requests.length;
	f.stream.dispose();
	f.stream.retry();
	f.stream.step( pose );
	assert.equal( f.requests.length, retried, "a disposed stream requests nothing" );
});

for ( const failedPart of [ "catalog", "objects", "terrain", "texture" ] ) {
	test(`world ${failedPart} recovers a transient outage without manual retry`, () => {
		const f = fixture(), pose = { regionId: 1, x: 0, y: 0, z: 0, angle: 0 };
		f.step( 1 );
		if ( failedPart !== "catalog" ) f.step( 1 );
		if ( failedPart === "texture" ) f.step( 1 );
		const job = f.requests.find( r =>
			f.ready.has( r.key ) && (
				failedPart === "catalog" ?
					r.decode === undefined :
					failedPart === "texture" ?
					r.decode === "png" :
					new URLSearchParams( new URL( r.url ).hash.slice( 1 ) ).get( "part" ) === failedPart
			)
		);
		assert.ok( job, `pending ${failedPart}` );
		f.ready.set( job.key, { kind: "error", id: job.key, error: "Load failed", transient: true } );
		f.stream.step( pose, undefined, undefined, undefined, 100 );
		assert.match( f.stream.error(), /Load failed/ );
		assert.equal( f.stream.reconnecting(), true );
		const requests = f.requests.length;
		f.stream.step( pose, undefined, undefined, undefined, 2099 );
		assert.equal( f.requests.length, requests );
		f.stream.step( pose, undefined, undefined, undefined, 2100 );
		assert.ok( f.requests.length > requests );
		for ( let i = 0; i < 8; i++ ) f.step( 1 );
		assert.equal( f.stream.error(), null );
		assert.equal( f.stream.reconnecting(), false );
		assert.equal( f.world.stats().sceneId, "1:objects" );
		f.stream.dispose();
		f.stream.retryTransient();
		const completed = f.requests.length;
		f.stream.step( pose, undefined, undefined, undefined, 100000 );
		assert.equal( f.requests.length, completed );
	});
}

test("world permanent errors wait for manual retry even after an online event", () => {
	const f = fixture(), pose = { regionId: 1, x: 0, y: 0, z: 0, angle: 0 };
	f.step( 1 );
	const job = f.requests[0];
	f.ready.set( job.key, { kind: "error", id: job.key, error: "Asset HTTP 404" } );
	f.stream.step( pose );
	f.stream.retryTransient();
	f.stream.step( pose, undefined, undefined, undefined, 1000000 );
	assert.match( f.stream.error(), /404/ );
	assert.equal( f.stream.reconnecting(), false );
	assert.equal( f.requests.length, 1 );
	f.stream.retry();
	for ( let i = 0; i < 8; i++ ) f.step( 1 );
	assert.equal( f.stream.error(), null );
});

test("one neighbouring preload transfers across the boundary and reversal cancels it", () => {
	for ( const completed of [ false, true ] ) {
		const f = fixture();
		for ( let i = 0; i < 5; i++ ) f.step( 1 );
		const pose = x => ({ regionId: 1, x, y: 0, z: 960, angle: 0 });
		f.stream.step( pose( 1000 ) );
		f.stream.step( pose( 1600 ) );
		const request = f.requests.at( -1 );
		assert.ok( scene( request, "b.json" ) );
		if ( completed ) f.stream.step( pose( 1700 ) );
		f.step( 2 );
		for ( let i = 0; i < 5; i++ ) f.step( 2 );
		assert.equal( f.requests.filter( r => scene( r, "b.json" ) ).length, 1 );
		assert.equal( f.world.stats().sceneId, "2:objects" );
		f.stream.dispose();
	}
	const f = fixture();
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	const pose = x => ({ regionId: 1, x, y: 0, z: 960, angle: 0 });
	f.stream.step( pose( 1000 ) );
	f.stream.step( pose( 1600 ) );
	const request = f.requests.at( -1 );
	f.ready.delete( request.key );
	f.stream.step( pose( 1550 ) );
	assert.ok( f.cancelled.includes( request.key ) );
	f.stream.dispose();
});
test("reset cancels a pending future scene as well as active work", () => {
	const f = fixture();
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	f.stream.step( { regionId: 1, x: 1000, y: 0, z: 960, angle: 0 } );
	f.stream.step( { regionId: 1, x: 1600, y: 0, z: 960, angle: 0 } );
	const request = f.requests.at( -1 );
	f.stream.reset();
	assert.ok( f.cancelled.includes( request.key ) );
});

test("ordinary sector crossings remain visible while discontinuous travel requests a loading screen", () => {
	const f = fixture();
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	f.step( 2 );
	assert.equal( f.stream.loadingRegion(), undefined );
	for ( let i = 0; i < 5; i++ ) f.step( 2 );
	f.step( 0x8001 );
	assert.equal( f.stream.loadingRegion(), 0x8001 );
	for ( let i = 0; i < 5; i++ ) f.step( 0x8001 );
	assert.equal( f.stream.loadingRegion(), undefined );
	f.stream.dispose();
});

test("an old displayed scene cannot satisfy readiness for a newly requested region", () => {
	const f = fixture();
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	assert.equal( f.stream.ready(), true );
	f.step( 2 );
	assert.equal( f.stream.ready(), false );
	assert.ok( f.stream.progress() < 1 );
	for ( let i = 0; i < 5; i++ ) f.step( 2 );
	assert.equal( f.stream.ready(), true );
	f.stream.reset();
	assert.equal( f.stream.ready(), false );
});

test("failed speculation waits for a new intent; actual crossing can retry once as required work", () => {
	const f = fixture();
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	const pose = x => ({ regionId: 1, x, y: 0, z: 960, angle: 0 });
	f.stream.step( pose( 1000 ) );
	f.stream.step( pose( 1500 ) );
	const request = f.requests.at( -1 );
	f.ready.set( request.key, { kind: "error", id: request.key, error: "503" } );
	for ( let x = 1550; x < 1800; x += 50 ) f.stream.step( pose( x ) );
	assert.equal( f.requests.filter( r => scene( r, "b.json" ) ).length, 1 );
	assert.equal( f.stream.error(), null );
	for ( let i = 0; i < 5; i++ ) f.step( 2 );
	assert.equal( f.requests.filter( r => scene( r, "b.json" ) ).length, 2 );
	assert.equal( f.stream.ready(), true );
	f.stream.dispose();
});

test("mission selection ignores frontend catalog order on entry and edge prefetch", () => {
	const f = fixture();
	f.step( 1 );
	const request = f.requests.at( -1 );
	f.ready.set( request.key, {
		kind: "bytes",
		buffer: new TextEncoder().encode( JSON.stringify( {
			regionsById: {
				"0x0001": [ { source: "title", bundlePublicPath: "/assets/wrong-title.json" }, {
					source: "mission-outdoor-global",
					area: "outdoor",
					bundlePublicPath: "/assets/a.json"
				} ],
				"0x0002": [ { source: "character-create-europe", bundlePublicPath: "/assets/wrong-create.json" }, {
					source: "mission-outdoor-global",
					area: "outdoor",
					bundlePublicPath: "/assets/b.json"
				} ]
			}
		} ) ).buffer
	} );
	for ( let i = 0; i < 5; i++ ) f.step( 1 );
	f.stream.step( { regionId: 1, x: 1500, y: 0, z: 960, angle: 0 } );
	assert.ok( f.requests.some( r => scene( r, "a.json" ) ) );
	assert.ok( f.requests.some( r => scene( r, "b.json" ) ) );
	assert.ok( f.requests.every( r => !r.url.includes( "wrong-" ) ) );
	f.stream.dispose();
	f.world.dispose();
});

test("a crossing requests terrain only for the regions it adds, in one anchor", () => {
	const f = fixture();
	f.step( 1 );
	const catalog = f.requests.at( -1 ), row = name => [ { area: "outdoor", bundlePublicPath: "/assets/" + name } ];
	f.ready.set( catalog.key, {
		kind: "bytes",
		buffer: new TextEncoder().encode( JSON.stringify( {
			regionsById: { "0x0001": row( "a.json" ), "0x0002": row( "b.json" ), "0x0003": row( "c.json" ) }
		} ) ).buffer
	} );
	for ( const region of [ 1, 2, 3 ] ) for ( let i = 0; i < 6; i++ ) f.step( region );
	const terrain = f.requests.filter( r => new URL( r.url ).hash.includes( "part=terrain" ) ).map( r =>
		new URL( r.url ).pathname
	);
	// 0x0001 covers a and b; 0x0002 adds c; 0x0003 adds nothing new.
	assert.deepEqual( terrain, [ "/assets/a.json", "/assets/b.json", "/assets/c.json" ] );
	const anchors = f.requests.filter( r => new URL( r.url ).hash.includes( "part=" ) ).map( r =>
		new URLSearchParams( new URL( r.url ).hash.slice( 1 ) ).get( "anchor" )
	);
	assert.ok( anchors.every( anchor => anchor === "0001" ), "every part shares the first scene's anchor" );
	assert.equal( f.world.stats().sceneId, "3:objects" );
	assert.equal( f.world.stats().pendingGroups, 0 );
	f.stream.dispose();
	f.world.dispose();
});
