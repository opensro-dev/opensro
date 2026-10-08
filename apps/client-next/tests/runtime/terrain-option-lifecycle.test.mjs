/*
===========================================================================

terrain-option-lifecycle.test.mjs - relief preferences across asset and scene owners

The option must reach the worker and invalidate active and prefetched scenes.
These tests inspect transmitted requests and admitted leases, not source text.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
/*
================
load
================
*/
async function load( file ) {
	return import( new URL( "../../src/engine/" + file, import.meta.url ).href );
}
const { createAssets } = await load( "runtime/assets/assets.ts" );
const { createFrontendStage } = await load( "runtime/frontend/stage/stage.ts" );
const { createWorldDecoder } = await load( "runtime/assets/worker/world/world.ts" );

test("asset requests serialize terrain relief without changing native-off requests", t => {
	const messages = [];
	const descriptor = Object.getOwnPropertyDescriptor( globalThis, "Worker" );
	/*
	================
	Worker
	================
	*/
	class Worker {
		/*
		================
		postMessage
		================
		*/
		postMessage( message ) {
			messages.push( message );
		}
		/*
		================
		terminate
		================
		*/
		terminate() {}
	}
	Object.defineProperty( globalThis, "Worker", { configurable: true, value: Worker } );
	t.after( () => {
		if ( descriptor ) Object.defineProperty( globalThis, "Worker", descriptor );
		else Reflect.deleteProperty( globalThis, "Worker" );
	} );
	const assets = createAssets();
	try {
		for ( const enabled of [ undefined, true, false ] ) {
			const id = assets.request( "https://test.invalid/world.json", 128 << 20, "world", {
				terrainNormals: enabled
			} );
			const sent = messages.find( row => row.id === id );
			assert.deepEqual( sent, {
				kind: "load",
				id,
				url: "https://test.invalid/world.json",
				limit: 128 << 20,
				decode: "world",
				...(enabled ? { terrainNormals: true } : {})
			} );
		}
	} finally {
		assets.dispose();
	}
});

test("frontend relief replaces the current scene and retires prefetched old-mode leases", () => {
	let serial = 0, closed = 0, displayed = "previous";
	const requests = [], results = new Map(), cancelled = [], admitted = [];
	const assets = {
		available: () => 4,
		request( url, limit, decode, options ) {
			requests.push( { id: ++serial, url, decode, options } );
			return serial;
		},
		take( id ) {
			const row = results.get( id );
			results.delete( id );
			return row ?? null;
		},
		cancel( id ) {
			cancelled.push( id );
			results.delete( id );
		}
	};
	const renderer = {
		cancelWorldUpdate() {},
		adoptWorld( lease ) {
			displayed = lease.sceneId;
			admitted.push( lease );
		},
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({ sceneId: displayed, pendingGroups: 0, pendingTextures: 0 })
	};
	const stage = createFrontendStage( assets, renderer, "https://test.invalid" );
	/*
	================
	completeManifest
	================
	*/
	function completeManifest() {
		results.set( serial, {
			kind: "bytes",
			buffer:
				new TextEncoder().encode( JSON.stringify( { regionBundlePublicPath: "/assets/world.json" } ) ).buffer
		} );
	}
	/*
	================
	completeWorld
	================
	*/
	function completeWorld( name, images = [] ) {
		results.set( serial, { kind: "world", world: { sceneId: name }, images } );
	}
	stage.install( "/assets/current.json" );
	stage.step();
	completeManifest();
	stage.step();
	completeWorld( "flat" );
	stage.step();
	assert.equal( stage.ready(), true );
	stage.preload( "/assets/next.json" );
	completeManifest();
	stage.preload( "/assets/next.json" );
	completeWorld( "stale-prefetch", [ {
		image: {
			close() {
				closed++;
			}
		}
	} ] );
	stage.preload( "/assets/next.json" );
	stage.setTerrainNormals( true );
	assert.equal( closed, 1 );
	assert.equal( displayed, "flat", "keep the old scene until replacement admission" );
	assert.equal( stage.ready(), false );
	stage.step();
	completeManifest();
	stage.step();
	assert.equal( requests.at( -1 ).options.terrainNormals, true );
	completeWorld( "relief" );
	stage.step();
	assert.equal( stage.ready(), true );
	const count = requests.length;
	stage.setTerrainNormals( true );
	stage.step();
	assert.equal( requests.length, count );
	stage.preload( "/assets/next.json" );
	completeManifest();
	stage.preload( "/assets/next.json" );
	assert.equal( requests.at( -1 ).options.terrainNormals, true );
	const pending = serial;
	stage.setTerrainNormals( false );
	assert.ok( cancelled.includes( pending ) );
	stage.step();
	completeManifest();
	stage.step();
	assert.equal( requests.at( -1 ).options, undefined );
	completeWorld( "flat-again" );
	stage.step();
	assert.deepEqual( admitted.map( row => row.sceneId ), [ "flat", "relief", "flat-again" ] );
	stage.dispose();
});

test("worker terrain decoding restores authored flat normals after relief at every LOD", () => {
	const bundle = {
		source: { sectorX: 1, sectorY: 1 },
		terrain: {
			blocks: [ {
				blockX: 0,
				blockZ: 0,
				heights: Array.from( { length: 289 }, ( _, i ) => i % 17 * 10 ),
				textureData: Array( 289 ).fill( 0 )
			} ]
		},
		terrainTextures: {
			tileCatalog: { referencedTiles: [ { textureId: 0, imagePublicPath: "/assets/ground.png" } ] }
		},
		objects: { placements: [], resources: { meshes: [], bsr: [], materialSets: [] } }
	};
	const decoder = createWorldDecoder();
	const scenes = [ false, true, false ].map( terrainNormals => decoder.decode( bundle, false, { terrainNormals } ) );
	for ( const [index, scene] of scenes.entries() ) {
		assert.ok( scene.groups.length > 0 );
		const lods = new Set();
		for ( const group of scene.groups ) {
			for ( const range of group.ranges ?? [] ) lods.add( range.lod );
			const normals = group.geometry.normals;
			for ( let i = 0; i < normals.length; i += 3 ) {
				if ( index === 1 ) {
					assert.ok( Math.abs( normals[i] + .5 / Math.hypot( .5, 1 ) ) < 1e-6 );
					assert.ok( Math.abs( normals[i + 1] - 1 / Math.hypot( .5, 1 ) ) < 1e-6 );
				} else assert.deepEqual( [ ...normals.slice( i, i + 3 ) ], [ 0, 1, 0 ] );
			}
		}
		assert.equal( lods.size, 4 );
	}
	assert.deepEqual( scenes[0], scenes[2] );
});
