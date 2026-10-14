/*
===========================================================================

frontend-settle.test.mjs - a drawn frontend scene gives up its CPU geometry

A prepared scene's streams share one packed arena; a settled title scene
must leave no view of it (renderer world/settle.ts). The world renderer
settles only the drawn frontend scene at full terrain detail and reports a
settled scene a device loss dropped; the stage settles the title once it is
ready and requests the scene again when it is lost (frontend stage.ts).

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
const { settleReady, settleScene } = await load( "src/engine/runtime/renderer/world/settle.ts" );
const { createWorldRenderer } = await load( "src/engine/runtime/renderer/world/world.ts" );
const { createFrontendStage } = await load( "src/engine/runtime/frontend/stage/stage.ts" );

const identity = () => new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] );

/*
================
arenaScene

A terrain group and an object group whose streams are views of one
arena, as prepareWorldScene packs them.
================
*/
function arenaScene() {
	const arena = new ArrayBuffer( 4096 );
	let offset = 0;
	const view = ( Type, values ) => {
		const out = new Type( arena, offset, values.length );
		out.set( values );
		offset += Math.ceil( out.byteLength / 16 ) * 16;
		return out;
	};
	const range = {
		indexStart: 0,
		indexCount: 3,
		lod: 0,
		cell: [ 0, 0 ],
		heights: [],
		seamVertices: view( Uint32Array, [ 0, 0 ] )
	};
	const terrain = {
		id: "terrain",
		ranges: [ range ],
		material: { terrain: true },
		geometry: {
			positions: view( Float32Array, [ 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
			indices: view( Uint32Array, [ 0, 1, 2 ] ),
			vertices: view( Float32Array, new Array( 42 ).fill( 1 ) ),
			dynamicVertices: true,
			transform: view( Float32Array, identity() ),
			instances: view( Float32Array, identity() )
		}
	};
	const object = {
		id: "object",
		material: {},
		geometry: {
			positions: view( Float32Array, [ 0, 0, 0, 2, 0, 0, 0, 2, 0 ] ),
			normals: view( Float32Array, new Array( 9 ).fill( 0 ) ),
			indices: view( Uint32Array, [ 0, 1, 2 ] ),
			transform: view( Float32Array, identity() ),
			instances: view( Float32Array, identity() )
		}
	};
	return { arena, scene: { id: "title", originRegion: 1, groups: [ terrain, object ] }, terrain, object, range };
}

/*
================
views

Every typed view a group or its ranges hold.
================
*/
function views( scene ) {
	const out = [];
	for ( const group of scene.groups ) {
		for ( const value of Object.values( group.geometry ) ) if ( ArrayBuffer.isView( value ) ) out.push( value );
		for ( const range of group.ranges ?? [] ) if ( range.seamVertices ) out.push( range.seamVertices );
	}
	return out;
}

test("a settled scene views no byte of its packed arena and keeps what its frame reads", () => {
	const { arena, scene, terrain, object } = arenaScene();
	const dropped = [], terrainDraw = {}, objectDraw = {};
	/** @type {Map<object, any>} */
	const selections = new Map( [ [ terrain, { indices: new Uint32Array( 3 ) } ] ] );
	/** @type {WeakMap<Float32Array, any>} */
	const pickBounds = new WeakMap();
	pickBounds.set( object.geometry.positions, { tag: "bounds" } );
	const state = {
		geometry: { dropMirror: draw => dropped.push( draw ) },
		draws: new Map( /** @type {[object, object][]} */ ([ [ terrain, terrainDraw ], [ object, objectDraw ] ]) ),
		selections,
		pickBounds
	};
	assert.equal( settleReady( scene, state ), false, "terrain that has not chosen its ranges cannot settle" );
	selections.get( terrain ).chosen = terrain.ranges;
	assert.equal( settleReady( scene, state ), true );
	const objectPositions = [ ...object.geometry.positions ];
	settleScene( scene, state );
	assert.deepEqual( views( scene ).filter( view => view.buffer === arena ), [], "nothing views the arena" );
	assert.deepEqual( dropped, [ terrainDraw ], "the terrain draw drops its CPU mirror" );
	assert.equal( terrain.geometry.positions.length, 0 );
	assert.equal( selections.get( terrain ).indices.length, 0 );
	assert.deepEqual( [ ...object.geometry.positions ], objectPositions, "objects keep their own copies" );
	assert.deepEqual( [ ...object.geometry.instances ], [ ...identity() ] );
	assert.deepEqual( pickBounds.get( object.geometry.positions ), { tag: "bounds" }, "pick bounds follow the copy" );
});

/*
================
frontendScene
================
*/
function frontendScene( id, terrainDetail = "full" ) {
	return {
		id,
		originRegion: 1,
		warnings: [],
		residency: "frontend",
		terrainDetail,
		groups: [ {
			id,
			center: [ 960, 0, 960 ],
			radius: 10000,
			material: { color: [ 1, 1, 1, 1 ], alphaCutoff: 0, blend: false, doubleSided: true },
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

test("the renderer settles only the drawn frontend scene at full detail, and a loss reports it", () => {
	const geometry = {
		upload: () => ({}),
		release() {},
		updateInstances: draw => draw,
		updateIndices() {},
		streamVertices() {},
		dropMirror() {}
	};
	const textures = { upload: () => ({}), release() {} };
	const world = createWorldRenderer();
	world.camera( { eye: [ 0, 0, 0 ], target: [ 0, 0, -1 ], near: 1, far: 1000, fov: Math.PI / 3 } );
	world.scene( { ...frontendScene( "distance" ), terrainDetail: "distance" } );
	world.prepare( geometry, textures, 1, 0 );
	assert.equal( world.settle( "distance" ), false, "a distance-detail scene re-chooses its terrain" );
	// Terrain drawn through a layer is re-streamed from its own vertices on a
	// rebuild, so it keeps them.
	const layered = frontendScene( "layered" );
	layered.groups[0] = {
		...layered.groups[0],
		terrainSector: 1,
		ranges: [],
		material: { ...layered.groups[0].material, terrain: true },
		geometry: { ...layered.groups[0].geometry, vertices: new Float32Array( 42 ), dynamicVertices: true }
	};
	world.scene( layered );
	world.prepare( geometry, textures, 1, 0 );
	assert.equal( world.stats().sceneId, "layered" );
	assert.equal( world.settle( "layered" ), false, "a layered scene keeps its terrain streams" );
	world.scene( frontendScene( "title" ) );
	assert.equal( world.settle( "title" ), false, "a scene still loading is not drawn yet" );
	world.prepare( geometry, textures, 1, 0 );
	assert.equal( world.settle( "title" ), true );
	world.prepare( geometry, textures, 1, 0 );
	world.prepare( geometry, textures, 1, 0 );
	world.invalidate();
	assert.equal( world.stats().lostSceneId, "title", "a settled scene cannot be uploaded again" );
	world.prepare( geometry, textures, 1, 0 );
	assert.equal( world.stats().sceneId, null );
	world.scene( frontendScene( "title" ) );
	assert.equal( world.stats().lostSceneId, null, "admitting it again clears the report" );
	// An unsettled scene still survives a loss from its CPU copy.
	world.prepare( geometry, textures, 1, 0 );
	world.invalidate();
	world.prepare( geometry, textures, 1, 0 );
	assert.equal( world.stats().sceneId, "title" );
	assert.equal( world.stats().lostSceneId, null );
});

test("the title stage settles once ready and loads its scene again after a loss", () => {
	let serial = 0, sceneId = null, lostSceneId = null;
	const results = new Map(), requests = [], settles = [];
	const assets = {
		available: () => 4,
		request: ( url, size, kind ) => {
			requests.push( kind );
			return ++serial;
		},
		take: id => {
			const result = results.get( id );
			results.delete( id );
			return result;
		},
		cancel: id => results.delete( id )
	};
	const renderer = {
		cancelWorldUpdate() {},
		adoptWorld: world => {
			sceneId = world.sceneId;
			lostSceneId = null;
		},
		settleWorld: id => {
			settles.push( id );
			return id === sceneId;
		},
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({ sceneId, lostSceneId, pendingGroups: 0, pendingTextures: 0 })
	};
	const stage = createFrontendStage( assets, renderer, "http://localhost" );
	const manifest = { regionBundlePublicPath: "/assets/title.json", camera: [], cameraControllerTargetTimeSeconds: 0 };
	stage.install( "/assets/title/manifest.json", "full", "settle" );
	stage.step();
	results.set( serial, { kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer } );
	stage.step();
	results.set( serial, { kind: "world", world: { sceneId: "title" } } );
	stage.step();
	stage.step();
	assert.deepEqual( settles, [ "title" ], "settled once, when ready" );
	stage.step();
	assert.deepEqual( settles, [ "title" ], "and only once" );
	// A device loss drops the settled scene: the stage loads it again.
	sceneId = null;
	lostSceneId = "title";
	stage.step();
	assert.deepEqual( requests, [ undefined, "frontend-world", "frontend-world" ] );
	assert.equal( stage.ready(), false );
	results.set( serial, { kind: "world", world: { sceneId: "title" } } );
	stage.step();
	stage.step();
	assert.equal( stage.ready(), true );
	assert.deepEqual( settles, [ "title", "title" ], "the reloaded scene settles again" );
});

test("a stage installed to keep never settles", () => {
	let serial = 0, sceneId = null;
	const results = new Map(), settles = [];
	const assets = {
		available: () => 4,
		request: () => ++serial,
		take: id => {
			const result = results.get( id );
			results.delete( id );
			return result;
		},
		cancel: id => results.delete( id )
	};
	const renderer = {
		cancelWorldUpdate() {},
		adoptWorld: world => {
			sceneId = world.sceneId;
		},
		settleWorld: id => settles.push( id ),
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({ sceneId, pendingGroups: 0, pendingTextures: 0 })
	};
	const stage = createFrontendStage( assets, renderer, "http://localhost" );
	stage.install( "/assets/dock/manifest.json", "distance" );
	stage.step();
	results.set( serial, {
		kind: "bytes",
		buffer: new TextEncoder().encode( JSON.stringify( { regionBundlePublicPath: "/assets/dock.json" } ) ).buffer
	} );
	stage.step();
	results.set( serial, { kind: "world", world: { sceneId: "dock" } } );
	stage.step();
	stage.step();
	assert.equal( stage.ready(), true );
	assert.deepEqual( settles, [] );
});
