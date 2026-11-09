/*
===========================================================================

frontend-loading-gate.test.mjs - when a frontend load may end

The loading gate opens a stage once its world is resident and its own
owner (dock roster, creation preview) answers ready. These tests pin
what may hold the gate shut - an unready preview may, a world emitter
may not (#3011) - and that a gate which stops changing fails with a
report instead of loading forever (#3010).

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
	return import( sourceFileUrl( path.resolve( root, file ) ).href );
}
const { createFrontend } = await load( "src/engine/runtime/frontend/frontend.ts" );
/*
================
creationGateEmitters

BR-261108-1908-A644 (#3011): the creation world's bar filled and never
ended. The renderer's actor list carries the lone preview character and
every visible world emitter (the create worlds hold EFP particle
objects), so the gate's old "renderer draws exactly one actor" test
held the load shut whenever an emitter was ready in view. The preview's
own readiness (one preview actor, every wardrobe path resident) is the
creation fact; the world's emitters never hold it back, and an
unready preview still does.
================
*/
test("world emitters beside the creation preview never hold the creation load", () => {
	let serial = 0, sceneId = "";
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
		createCamera: [ key ],
		createCameraControllerTargetTimeSeconds: 0,
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
	// The preview character plus two of the create world's EFP emitters.
	const renderer = {
		cancelWorldUpdate() {},
		settleWorld: () => false,
		adoptWorld( world ) {
			sceneId = world.sceneId;
		},
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({ sceneId, pendingGroups: 0, pendingTextures: 0 }),
		setWorldCamera() {},
		pickFrontendRace: () => null,
		frontendRaceCenters: () => [],
		characterStats: () => ({ actors: 3 })
	};
	const frontend = createFrontend( assets, renderer, "http://fixture.invalid", () => {} );
	let now = 0, creationReady = false, session = { phase: "signed-out", revision: 1, characters: [] };
	const step = () => frontend.step( session, now += 100, true, creationReady );
	for ( let i = 0; i < 20 && frontend.snapshot().phase !== "intro"; i++ ) step();
	frontend.reveal();
	for ( let i = 0; i < 6; i++ ) step();
	session = { phase: "character-select", revision: 2, characters: [] };
	for ( let i = 0; i < 30 && frontend.snapshot().phase !== "dock"; i++ ) step();
	assert.equal( frontend.snapshot().phase, "dock" );
	frontend.create();
	for ( let i = 0; i < 30 && frontend.snapshot().phase !== "create"; i++ ) step();
	assert.equal( frontend.snapshot().phase, "create" );
	frontend.race( 0, 0 );
	for ( let i = 0; i < 30 && frontend.snapshot().phase !== "loading-create"; i++ ) step();
	assert.equal( frontend.snapshot().phase, "loading-create" );
	for ( let i = 0; i < 30; i++ ) step();
	assert.equal( frontend.snapshot().phase, "loading-create", "an unready preview still holds the load" );
	creationReady = true;
	for ( let i = 0; i < 5 && frontend.snapshot().phase !== "customize"; i++ ) step();
	assert.equal( frontend.snapshot().phase, "customize" );
	frontend.dispose();
});
/*
================
loadingGateStall

BR-261108-1908-A644: after character creation the loading bar filled
and never ended - every asset had arrived, the session was still at
character-select, and no owner reported an error. The loading gate
(stage ready, dock/creation ready) stalls exactly like that when a
polled owner stops answering without failing. A full stall bound with
no gate fact changing must fail the load and report the stall, so the
player gets the failed screen's way back; inside the bound the load
is still just waiting.
================
*/
test("a loading gate that stops changing fails with a report instead of loading forever", () => {
	let serial = 0, sceneId = "", match = true;
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
		settleWorld: () => false,
		setWorld() {},
		adoptWorld( world ) {
			sceneId = world.sceneId;
		},
		setWorldTexture() {},
		neededWorldTextures: () => [],
		worldStats: () => ({
			sceneId: match ? sceneId : "another-scene",
			residentGroups: 1,
			pendingGroups: 0,
			pendingTextures: 0
		}),
		setWorldCamera() {},
		characterStats: () => ({ actors: 1 })
	};
	const sent = [];
	const frontend = createFrontend( assets, renderer, "http://fixture.invalid", command => sent.push( command ) );
	let now = 0, session = { phase: "signed-out", revision: 1, characters: [] };
	const step = () => frontend.step( session, now += 100 );
	for ( let i = 0; i < 20 && frontend.snapshot().phase !== "intro"; i++ ) step();
	frontend.reveal();
	for ( let i = 0; i < 6; i++ ) step();
	// From here the renderer never promotes the stage's scene: every
	// asset arrives (the bar reads full) and the gate never opens.
	match = false;
	session = {
		phase: "character-select",
		revision: 2,
		characters: [ { id: 1, name: "fixture", deletePending: false, visualLoadout: { heightScale: 1 } } ]
	};
	let state = step();
	for ( let i = 0; i < 30 && state.phase !== "loading-dock"; i++ ) state = step();
	assert.equal( state.phase, "loading-dock" );
	for ( let i = 0; i < 60 && state.loadingProgress !== 1; i++ ) state = step();
	assert.equal( state.loadingProgress, 1, "the bar fills completely" );
	for ( let i = 0; i < 500; i++ ) state = step();
	assert.equal( state.phase, "loading-dock", "inside the bound the load is still waiting, not failed" );
	assert.equal( sent.filter( command => command.kind === "client-report" ).length, 0 );
	for ( let i = 0; i < 200 && state.phase !== "failed"; i++ ) state = step();
	assert.equal( state.phase, "failed", "a full stall bound ends the load instead of loading forever" );
	assert.match( state.error, /Loading stalled/ );
	const reports = sent.filter( command => command.kind === "client-report" );
	assert.equal( reports.length, 1 );
	assert.equal( reports[0].code, "frontend_loading_stalled" );
	assert.match( reports[0].message, /loading-dock/ );
	frontend.dispose();
});
