/*
===========================================================================

frontend.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { root } from "../../tools/project.mjs";
import { readPublishedAssetJsonSync } from "../../../..//scripts/lib/publishedAsset.mjs";
async function load( file ) {
	return import( sourceFileUrl( path.resolve( root, file ) ).href );
}
const { createFrontendFlow } = await load( "src/engine/runtime/frontend/flow/flow.ts" );

test("world restoration supersedes title loading and stays idempotent through admission", () => {
	const flow = createFrontendFlow(), old = flow.snapshot().generation;
	flow.resumeWorld();
	const current = flow.snapshot().generation;
	assert.equal( flow.snapshot().phase, "loading-world" );
	flow.ready( old );
	flow.authenticated();
	flow.resumeWorld();
	assert.equal( flow.snapshot().generation, current );
	flow.worldReady();
	flow.resumeWorld();
	assert.equal( flow.snapshot().phase, "world" );
	flow.reset();
	flow.resumeWorld();
	flow.entryRejected();
	assert.equal( flow.snapshot().phase, "loading-dock" );
});
const { createFrontendCamera } = await load( "src/engine/runtime/frontend/camera/camera.ts" );
test("server-completed restart returns the world flow to the native dock loading and arrival sequence", () => {
	const flow = createFrontendFlow();
	flow.resumeWorld();
	flow.worldReady();
	assert.equal( flow.snapshot().phase, "world" );
	flow.returnedToDock();
	assert.equal( flow.snapshot().phase, "loading-dock" );
	const generation = flow.snapshot().generation;
	flow.returnedToDock();
	assert.equal( flow.snapshot().generation, generation );
	flow.ready( generation );
	assert.equal( flow.snapshot().phase, "dock-arrival" );
	flow.advance( 0, true );
	assert.equal( flow.snapshot().phase, "dock" );
});
const { sampleFrontendCamera } = await load( "src/engine/foundation/rendering/frontend-camera.ts" );
const { sub_4dc920_4dcab0_EvaluateTitleCameraTracks: reference } = await load(
	"tests/oracles/legacy/apps/client/src/domains/world/babylon/camera/native/sub_4dc920_4dcab0_CameraTrack_Evaluate.ts"
);

test("restored authentication enters the dock without waiting for a title reveal", () => {
	for ( const intro of [ false, true ] ) {
		const flow = createFrontendFlow();
		if ( intro ) flow.ready( flow.snapshot().generation );
		const old = flow.snapshot().generation;
		flow.authenticated();
		assert.equal( flow.snapshot().phase, "loading-dock" );
		const admitted = flow.snapshot().generation;
		flow.ready( old );
		assert.equal( flow.snapshot().phase, "loading-dock" );
		flow.authenticated();
		assert.equal( flow.snapshot().generation, admitted, "repeated session snapshots cannot restart admission" );
		flow.ready( admitted );
		assert.equal( flow.snapshot().phase, "dock-arrival" );
		flow.advance( 0, true );
		assert.equal( flow.snapshot().phase, "dock" );
	}
});

test("published camera tracks match native-backed reference samples", () => {
	for (
		const asset of [ "/assets/title/constantinople/manifest.json", "/assets/character-select/world-manifest.json" ]
	) {
		const manifest = readPublishedAssetJsonSync( asset, CLIENT_PUBLIC_ROOT );
		for ( const keys of [ manifest.camera, manifest.createCamera ].filter( Boolean ) ) {
			for ( let time = 0; time <= keys.at( -1 ).timeSeconds; time += .03125 ) {
				const actual = sampleFrontendCamera( keys, time ), expected = reference( keys, time );
				assert.deepEqual( actual.position, expected.position );
				assert.deepEqual( actual.rotation, expected.rotation );
				assert.equal( actual.mode, expected.cameraScalar );
			}
		}
	}
});
test("creation cancellation returns from the current camera pose over two seconds", () => {
	const key = ( timeSeconds, x ) => ({
		timeSeconds,
		sectorX: 81,
		sectorY: 105,
		position: { x, y: 0, z: 0 },
		rotation: { x: 0, y: 0, z: 0 },
		mode: 30
	});
	const camera = createFrontendCamera();
	camera.install( { keys: [ key( 0, 0 ), key( 5, 100 ) ], target: 5, mode: "transition" } );
	const before = camera.step( 1 ).camera;
	camera.returnTo( key( 0, 20 ) );
	assert.deepEqual( camera.step( 0 ).camera, before );
	assert.equal( camera.step( 1 ).complete, false );
	const end = camera.step( 1 );
	assert.equal( end.complete, true );
	assert.equal( end.camera.target[0], 20 );
});
test("frontend readiness is scoped to its generation and departure waits for its fade", () => {
	const flow = createFrontendFlow(), stale = flow.snapshot().generation;
	flow.reset();
	flow.ready( stale );
	assert.equal( flow.snapshot().phase, "loading-title" );
	flow.ready( flow.snapshot().generation );
	flow.reveal();
	flow.advance( .5 );
	flow.authenticated();
	flow.advance( .49 );
	assert.equal( flow.snapshot().phase, "login-accepted" );
	flow.advance( .01 );
	assert.equal( flow.snapshot().phase, "loading-dock" );
	flow.ready( stale );
	assert.equal( flow.snapshot().phase, "loading-dock" );
	flow.ready( flow.snapshot().generation );
	flow.advance( 0, true );
	flow.start();
	flow.worldReady();
	assert.equal( flow.snapshot().phase, "departing" );
	flow.advance( .5 );
	assert.equal( flow.snapshot().phase, "loading-world" );
	flow.worldReady();
	assert.equal( flow.snapshot().phase, "world" );
});
test("repeated stage failures preserve the first failure and transition identity", () => {
	const flow = createFrontendFlow();
	flow.fail( "first" );
	const state = flow.snapshot();
	flow.fail( "second" );
	assert.deepEqual( flow.snapshot(), state );
});
const { createFrontendStage } = await load( "src/engine/runtime/frontend/stage/stage.ts" );
const { createStagePreload } = await load( "src/engine/runtime/frontend/stage/preload.ts" );
test("preloaded scenes transfer their image lease once and close abandoned images", () => {
	let serial = 0, closed = 0;
	const results = new Map(), cancelled = [];
	const assets = {
		available: () => 4,
		request: () => ++serial,
		take: id => {
			const result = results.get( id );
			results.delete( id );
			return result;
		},
		cancel: id => cancelled.push( id )
	};
	const preload = createStagePreload( assets, "http://localhost" );
	function ready( path ) {
		preload.step( path );
		results.set( serial, {
			kind: "bytes",
			buffer:
				new TextEncoder().encode( JSON.stringify( { regionBundlePublicPath: "/assets/world.json" } ) ).buffer
		} );
		preload.step( path );
		results.set( serial, {
			kind: "world",
			scene: { id: "dock" },
			images: [ {
				path: "/assets/a.png",
				image: {
					close() {
						closed++;
					}
				}
			} ]
		} );
		preload.step( path );
	}
	ready( "/assets/dock.json" );
	const lease = preload.take( "/assets/dock.json" );
	assert.ok( lease );
	assert.equal( preload.take( "/assets/dock.json" ), null );
	preload.clear();
	assert.equal( closed, 0 );
	lease.world.images[0].image.close();
	assert.equal( closed, 1 );
	ready( "/assets/dock.json" );
	preload.clear();
	preload.clear();
	assert.equal( closed, 2 );
	preload.step( "/assets/dock.json" );
	preload.take( "/assets/other.json" );
	assert.equal( cancelled.length, 1 );
	preload.dispose();
	const count = serial;
	preload.step( "/assets/dock.json" );
	assert.equal( serial, count );
});
const { selectedDockCamera } = await load( "src/engine/foundation/rendering/dock-camera.ts" );
const { previewIdle } = await load( "src/engine/foundation/animation/preview-idle.ts" );
test("all published preview models resolve native weapon idle inheritance and seated state", () => {
	const roster = readPublishedAssetJsonSync(
		"/assets/char/roster.json",
		CLIENT_PUBLIC_ROOT
	);
	const sets = [
		"default",
		"sword",
		"spear",
		"bow",
		"onehand_staff",
		"onehand_sword",
		"twohand_sword",
		"dagger",
		"dual_axe",
		"harf",
		"twohand_staff"
	];
	for ( const model of roster.models.filter( row => row.previewGlb ) ) {
		for ( const set of sets ) {
			assert.ok(
				model.previewClips.includes( previewIdle( model.previewClips, set, false ) ),
				model.codename + " " + set
			);
		}
		assert.equal( previewIdle( model.previewClips, "default", true ), "charselect-state14" );
	}
	assert.equal( previewIdle( [ "stand", "preview-state0-spear" ], "spear", false ), "preview-state0-spear" );
	assert.throws( () => previewIdle( [], "sword", false ), /Missing native preview/ );
});
test("selected camera follows native per-slot focus and deletion height, not mesh yaw", () => {
	const row = { deletePending: false, visualLoadout: { heightScale: .94 } };
	const key = selectedDockCamera( row, 1, 3 );
	assert.equal( key.position.x, 64 );
	assert.equal( key.position.z, 651 );
	assert.equal( key.mode, 8 );
	assert.equal( key.position.y, .94 * 16 - 28.779172897338867 );
	assert.ok(
		Math.abs( key.rotation.y - (.39250001311302185 - (3.200000047683716 + .14999985694885254 * .5)) ) < 1e-6
	);
	assert.equal( selectedDockCamera( { ...row, deletePending: true }, 1, 3 ).position.y, .94 * 9 - 29 );
});
test("stage readiness requires its own committed scene, independently of camera visibility", () => {
	let serial = 0;
	const requests = new Map(), results = new Map();
	const assets = {
		available: () => 4,
		request: ( url ) => {
			requests.set( ++serial, url );
			return serial;
		},
		take: id => {
			const r = results.get( id );
			results.delete( id );
			return r;
		},
		cancel: id => {
			requests.delete( id );
			results.delete( id );
		}
	};
	let stats = { sceneId: "previous", pendingGroups: 0, pendingTextures: 0, visibleGroups: 100 };
	const renderer = {
		cancelWorldUpdate() {},
		adoptWorld() {},
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => stats
	};
	const stage = createFrontendStage( assets, renderer, "http://localhost" );
	stage.install( "/assets/title/manifest.json" );
	stage.step();
	results.set( 1, {
		kind: "bytes",
		buffer:
			new TextEncoder().encode( JSON.stringify( { regionBundlePublicPath: "/assets/title/world.json" } ) ).buffer
	} );
	stage.step();
	results.set( 2, { kind: "world", world: { sceneId: "title" } } );
	stage.step();
	assert.equal( stage.ready(), false );
	stats = { ...stats, sceneId: "title", visibleGroups: 0 };
	assert.equal( stage.ready(), true );
	stage.install( "/assets/dock/manifest.json" );
	assert.equal( stage.ready(), false );
	stage.dispose();
});
test("title logo fades from the alpha actually reached when login is opened", () => {
	const flow = createFrontendFlow();
	flow.ready( flow.snapshot().generation );
	for ( let i = 0; i < 5; i++ ) flow.advance( 3 );
	flow.advance( 1.5 );
	assert.equal( flow.snapshot().logoAlpha, .5 );
	flow.reveal();
	assert.equal( flow.snapshot().logoAlpha, .5 );
	flow.advance( .25 );
	assert.equal( flow.snapshot().logoAlpha, .25 );
	flow.advance( .25 );
	assert.equal( flow.snapshot().logoAlpha, 0 );
	assert.equal( flow.snapshot().phase, "login" );
});

test("stage terrain policy changes with title/dock ownership, without mutating decoded scenes", () => {
	let serial = 0;
	const results = new Map(), admitted = [];
	const assets = {
		available: () => 4,
		request: () => ++serial,
		take: id => {
			const r = results.get( id );
			results.delete( id );
			return r;
		},
		cancel: id => results.delete( id )
	};
	const renderer = {
		cancelWorldUpdate() {},
		adoptWorld: ( world, detail ) => admitted.push( { id: world.sceneId, terrainDetail: detail } ),
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({})
	};
	const stage = createFrontendStage( assets, renderer, "http://localhost" ), source = { id: "same-world" };
	for ( const detail of [ "full", "distance", "full" ] ) {
		stage.install( "/assets/stage.json", detail );
		stage.step();
		results.set( serial, {
			kind: "bytes",
			buffer:
				new TextEncoder().encode( JSON.stringify( { regionBundlePublicPath: "/assets/world.json" } ) ).buffer
		} );
		stage.step();
		results.set( serial, { kind: "world", world: { sceneId: source.id } } );
		stage.step();
		assert.equal( admitted.at( -1 ).terrainDetail, detail );
		assert.equal( source.terrainDetail, undefined );
	}
	assert.deepEqual( admitted.map( s => s.terrainDetail ), [ "full", "distance", "full" ] );
	stage.dispose();
});

const { createFrontend } = await load( "src/engine/runtime/frontend/frontend.ts" );
test("dock Cancel emits logout once per departure while waiting for the session result", () => {
	let serial = 0, sceneId = "", now = 0;
	const jobs = new Map(), sent = [];
	const key = {
		timeSeconds: 0,
		sectorX: 81,
		sectorY: 105,
		position: { x: 0, y: 0, z: 0 },
		rotation: { x: 0, y: 0, z: 0 },
		mode: 30
	};
	const manifest = {
		camera: [ key ],
		cameraControllerTargetTimeSeconds: 0,
		regionBundlePublicPath: "/assets/fixture-world.json"
	};
	const assets = {
		available: () => 8,
		request: ( url, size, kind ) => {
			const id = ++serial;
			jobs.set(
				id,
				kind === "frontend-world" ?
					{ kind: "world", world: { sceneId: "fixture" } } :
					{ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer }
			);
			return id;
		},
		take: id => {
			const value = jobs.get( id );
			jobs.delete( id );
			return value;
		},
		cancel: id => jobs.delete( id )
	};
	const renderer = {
		cancelWorldUpdate() {},
		adoptWorld( world ) {
			sceneId = world.sceneId;
		},
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({ sceneId, pendingGroups: 0, pendingTextures: 0 }),
		setWorldCamera() {},
		characterStats: () => ({ actors: 0 })
	};
	const frontend = createFrontend( assets, renderer, "http://fixture.invalid", command => sent.push( command ) );
	let session = { phase: "character-select", revision: 1, characters: [] };
	const step = () => frontend.step( session, now += 1000 );
	for ( let cycle = 1; cycle <= 2; cycle++ ) {
		session = { phase: "character-select", revision: cycle * 3, characters: [] };
		for ( let i = 0; i < 15 && frontend.snapshot().phase !== "dock"; i++ ) step();
		assert.equal( frontend.snapshot().phase, "dock" );
		frontend.leave();
		for ( let i = 0; i < 10 && sent.length < cycle; i++ ) step();
		assert.equal( frontend.snapshot().phase, "title-logout" );
		assert.equal( sent.length, cycle );
		session = { phase: "authenticating", revision: cycle * 3 + 1, characters: [] };
		for ( let i = 0; i < 60; i++ ) {
			frontend.leave();
			step();
		}
		assert.equal( sent.length, cycle );
		assert.deepEqual( sent.at( -1 ), { kind: "logout" } );
		session = { phase: "signed-out", revision: cycle * 3 + 2, characters: [] };
		step();
		assert.notEqual( frontend.snapshot().phase, "title-logout" );
	}
	frontend.dispose();
});
test("full roster Create emits a timed native refusal and permits another attempt after the roster changes", () => {
	let serial = 0, sceneId = "", sounds = 0;
	const jobs = new Map();
	const key = {
		timeSeconds: 0,
		sectorX: 81,
		sectorY: 105,
		position: { x: 0, y: 0, z: 0 },
		rotation: { x: 0, y: 0, z: 0 },
		mode: 30
	};
	const manifest = {
		camera: [ key ],
		cameraControllerTargetTimeSeconds: 0,
		regionBundlePublicPath: "/assets/fixture-world.json"
	};
	const assets = {
		available: () => 8,
		request: ( url, size, kind ) => {
			const id = ++serial;
			jobs.set(
				id,
				kind === "frontend-world" ?
					{ kind: "world", world: { sceneId: "fixture" } } :
					{ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer }
			);
			return id;
		},
		take: id => {
			const result = jobs.get( id );
			jobs.delete( id );
			return result;
		},
		cancel: id => jobs.delete( id )
	};
	const renderer = {
		cancelWorldUpdate() {},
		adoptWorld( world ) {
			sceneId = world.sceneId;
		},
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({ sceneId, pendingGroups: 0, pendingTextures: 0 }),
		setWorldCamera() {},
		characterStats: () => ({ actors: 4 })
	};
	const frontend = createFrontend( assets, renderer, "http://fixture.invalid", () => {}, () => sounds++ );
	let now = 0, session = { phase: "signed-out", revision: 1, characters: [] };
	const step = () => frontend.step( session, now += 1000 );
	for ( let i = 0; i < 10 && frontend.snapshot().phase !== "intro"; i++ ) step();
	frontend.reveal();
	step();
	session = {
		phase: "character-select",
		revision: 2,
		characters: Array.from(
			{ length: 4 },
			( _, id ) => ({ id, name: `fixture${id}`, deletePending: false, visualLoadout: { heightScale: 1 } })
		)
	};
	for ( let i = 0; i < 15 && frontend.snapshot().phase !== "dock"; i++ ) step();
	assert.equal( frontend.snapshot().phase, "dock" );
	frontend.create();
	let state = step();
	assert.equal( state.phase, "dock" );
	assert.deepEqual( state.status, { key: "UIO_MSG_ERROR_CHARACTER_OVER_3", suffix: "", args: [ 4 ] } );
	assert.equal( sounds, 1 );
	frontend.create();
	state = step();
	assert.equal( sounds, 2 );
	assert.ok( state.status );
	now += 15000;
	assert.equal( step().status, undefined );
	session = { ...session, revision: 3, characters: session.characters.slice( 0, 3 ) };
	step();
	frontend.create();
	assert.equal( frontend.snapshot().phase, "create-arrival" );
	frontend.dispose();
});

test("transition takes over an in-flight preload without cancellation or duplicate requests", () => {
	for ( const phase of [ "manifest", "world" ] ) {
		let serial = 0;
		const results = new Map(), cancelled = [];
		const assets = {
			available: () => 4,
			request: () => ++serial,
			take( id ) {
				const value = results.get( id );
				results.delete( id );
				return value;
			},
			cancel: id => cancelled.push( id )
		};
		const preload = createStagePreload( assets, "http://localhost" );
		preload.step( "/assets/dock.json" );
		if ( phase === "world" ) {
			results.set( serial, {
				kind: "bytes",
				buffer: new TextEncoder().encode( JSON.stringify( { regionBundlePublicPath: "/assets/world.json" } ) )
					.buffer
			} );
			preload.step( "/assets/dock.json" );
		}
		const lease = preload.take( "/assets/dock.json" );
		assert.equal( lease.kind, phase );
		assert.equal( lease.job, serial );
		assert.deepEqual( cancelled, [] );
		preload.dispose();
		assert.deepEqual( cancelled, [] );
	}
});

test("startup texture status describes queued work, not the changing request concurrency", () => {
	const pending = new Map(), results = new Map();
	let sequence = 0, capacity = 1, needed = [];
	const assets = {
		available: () => Math.max( 0, capacity - pending.size ),
		request( url ) {
			const id = ++sequence;
			pending.set( id, url );
			return id;
		},
		take( id ) {
			const result = results.get( id );
			if ( result ) {
				results.delete( id );
				pending.delete( id );
			}
			return result;
		},
		cancel( id ) {
			pending.delete( id );
		}
	};
	const renderer = {
		cancelWorldUpdate() {},
		adoptWorld() {
			needed = [ "/assets/a.png", "/assets/b.png", "/assets/c.png", "/assets/d.png" ];
		},
		setWorldTexture( path ) {
			needed = needed.filter( p => p !== path );
		},
		neededWorldTextures: () => needed
	};
	const stage = createFrontendStage( assets, renderer, "http://fixture.invalid" );
	assert.equal( stage.status(), undefined, "no scene is wanted before install" );
	stage.install( "/assets/title.json" );
	assert.equal( stage.status(), "Loading scene description" );
	stage.step();
	results.set( 1, {
		kind: "bytes",
		buffer: new TextEncoder().encode( JSON.stringify( { regionBundlePublicPath: "/assets/world.json" } ) ).buffer
	} );
	stage.step();
	assert.equal( stage.status(), "Loading scene models" );
	capacity = 0;
	results.set( 2, { kind: "world", world: { sceneId: "title" }, images: [] } );
	stage.step();
	assert.equal( pending.size, 0 );
	assert.equal(
		stage.status(),
		"Preparing textures - 4 remaining",
		"queued textures still count as loading with no request slots"
	);
	capacity = 1;
	stage.step();
	assert.equal( pending.size, 1 );
	assert.equal( stage.status(), "Preparing textures - 4 remaining" );
	capacity = 3;
	stage.step();
	assert.equal( pending.size, 3 );
	assert.equal(
		stage.status(),
		"Preparing textures - 4 remaining",
		"opening slots must not turn concurrency into a countdown"
	);
	while ( pending.size ) {
		for ( const [id, url] of pending ) {
			results.set( id, { kind: "image", image: {}, path: new URL( url ).pathname } );
		}
		stage.step();
	}
	assert.equal( stage.status(), "Preparing scene graphics" );
	// World entry clears the stage: nothing loads, so nothing is reported
	// (#527). A return to the title wants the manifest again.
	stage.clear();
	assert.equal( stage.status(), undefined );
	stage.install( "/assets/title.json" );
	assert.equal( stage.status(), "Loading scene description" );
	stage.dispose();
});

test("entry admission retains the dock through connecting and rejection, and leaves only after a bound world", () => {
	let serial = 0, sceneId = "", clears = 0;
	const jobs = new Map();
	const key = {
		timeSeconds: 0,
		sectorX: 81,
		sectorY: 105,
		position: { x: 0, y: 0, z: 0 },
		rotation: { x: 0, y: 0, z: 0 },
		mode: 30
	};
	const manifest = {
		camera: [ key ],
		cameraControllerTargetTimeSeconds: 0,
		regionBundlePublicPath: "/assets/fixture-world.json"
	};
	const assets = {
		available: () => 8,
		request: ( url, size, kind ) => {
			const id = ++serial;
			jobs.set(
				id,
				kind === "frontend-world" ?
					{ kind: "world", world: { sceneId: "fixture" } } :
					{ kind: "bytes", buffer: new TextEncoder().encode( JSON.stringify( manifest ) ).buffer }
			);
			return id;
		},
		take: id => {
			const result = jobs.get( id );
			jobs.delete( id );
			return result;
		},
		cancel: id => jobs.delete( id )
	};
	const renderer = {
		cancelWorldUpdate() {
			clears++;
		},
		setWorld() {},
		adoptWorld( world ) {
			sceneId = world.sceneId;
		},
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({ sceneId, pendingGroups: 0, pendingTextures: 0 }),
		setWorldCamera() {},
		characterStats: () => ({ actors: 1 })
	};
	const frontend = createFrontend( assets, renderer, "http://fixture.invalid" );
	let now = 0, session = { phase: "signed-out", revision: 1, characters: [] };
	const step = () => frontend.step( session, now += 100 );
	for ( let i = 0; i < 20 && frontend.snapshot().phase !== "intro"; i++ ) step();
	frontend.reveal();
	for ( let i = 0; i < 6; i++ ) step();
	session = {
		phase: "character-select",
		revision: 2,
		characters: [ { id: 1, name: "fixture", deletePending: false, visualLoadout: { heightScale: 1 } } ]
	};
	for ( let i = 0; i < 30 && frontend.snapshot().phase !== "dock"; i++ ) step();
	frontend.select( "fixture" );
	for ( let i = 0; i < 30; i++ ) step();
	assert.equal( frontend.start(), true );
	assert.equal( frontend.start(), false );
	const before = clears;
	session = { ...session, phase: "connecting", revision: 3 };
	assert.equal( step().phase, "dock" );
	assert.equal( step().entryPending, true );
	assert.equal( clears, before );
	session = { ...session, phase: "character-select", revision: 4, error: "offline" };
	const refused = step();
	assert.equal( refused.phase, "dock" );
	assert.equal( refused.entryPending, false );
	assert.ok( refused.status );
	assert.equal( clears, before );
	assert.equal( frontend.start(), true );
	session = { ...session, phase: "entering-world", revision: 5, error: undefined };
	assert.equal( step().phase, "dock" );
	session = { ...session, phase: "world", revision: 6 };
	assert.equal( step().phase, "departing" );
	for ( let i = 0; i < 5; i++ ) step();
	assert.equal( frontend.snapshot().phase, "loading-world" );
	frontend.worldReady();
	step();
	assert.equal( frontend.snapshot().phase, "world" );
	session = { ...session, phase: "character-select", revision: 7 };
	let returned = step();
	assert.equal( returned.selectedCharacter, "" );
	assert.equal( returned.entryPending, false );
	for ( let i = 0; i < 30 && frontend.snapshot().phase !== "dock"; i++ ) returned = step();
	assert.equal( returned.phase, "dock" );
	assert.equal( returned.selectedCharacter, "" );
	frontend.select( "fixture" );
	for ( let i = 0; i < 30; i++ ) step();
	assert.equal( frontend.start(), true, "returned dock must support ordinary re-entry" );
	frontend.dispose();
});
