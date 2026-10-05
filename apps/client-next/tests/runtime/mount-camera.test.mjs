/*
===========================================================================

mount-camera.test.mjs - tests for characters.ts, random.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { createCharacterPresentation } = await import(
	sourceFileUrl( "src/engine/runtime/characters/characters.ts" ).href
);

const { createPresentationRandom } = await import( sourceFileUrl( "src/engine/runtime/random/random.ts" ).href );
test("camera anchor rides the locally driven mount, falls back to the rider, and dismounts", () => {
	let id = 0;
	const jobs = new Map();
	const encode = value => new TextEncoder().encode( JSON.stringify( value ) ).buffer;
	const assets = {
		available: () => 4,
		/*
		================
		request
		================
		*/
		request( url, limit, decode ) {
			jobs.set( ++id, { url, decode } );
			return id;
		},
		/*
		================
		cancel
		================
		*/
		cancel( id ) {
			jobs.delete( id );
		},
		/*
		================
		take
		================
		*/
		take( id ) {
			const job = jobs.get( id );
			if ( !job ) return null;
			jobs.delete( id );
			if ( job.decode === "effects" ) return { kind: "effects", catalog: {} };
			if ( job.decode === "character" ) {
				return {
					kind: "character",
					model: {
						nodes: [],
						primitives: [],
						images: [],
						clips: [ { name: "stand", duration: 1, channels: [] } ]
					},
					images: []
				};
			}
			let value = {};
			if ( job.url.endsWith( "/roster.json" ) ) {
				value = {
					models: [ { codename: "rider", refObjId: 1, glb: "/assets/rider.glb", clips: [ "stand" ] }, {
						codename: "mount",
						refObjId: 2,
						glb: "/assets/mount.glb",
						clips: [ "stand" ]
					} ]
				};
			}
			if ( job.url.endsWith( "/characterActionData.json" ) ) {
				value = {
					characterActionEffectRows: [ { codename: "rider", soundProfileName: "rider", heightFactor: 1 }, {
						codename: "mount",
						soundProfileName: "mount",
						heightFactor: 2
					} ]
				};
			}
			if ( job.url.endsWith( "/skillfx/manifest.json" ) ) {
				value = { format: "sro-skill-stage-models", models: {} };
			}
			if ( job.url.endsWith( "/itemdrop/manifest.json" ) ) {
				value = {
					format: "sro-mission-itemdrop-models",
					models: {}
				};
			}
			return { kind: "bytes", buffer: encode( value ) };
		}
	};
	const presenter = createCharacterPresentation(
		assets,
		{ setCharacterModel() {}, setCharacterAssembly() {}, retainCharacterModels() {}, setCharacterActors() {} },
		"http://localhost",
		() => {},
		createPresentationRandom( 1 )
	);
	const rider = { gid: 1, refObjId: 1, regionId: 257, x: 0, y: 0, z: 0, heading: 0, mountedOn: 2 },
		mount = { ...rider, gid: 2, refObjId: 2, x: 10, y: 8, z: 20, mountedOn: undefined };
	const gameplay = {
		localGid: 1,
		pose: { regionId: 257, x: 1, y: 2, z: 3, angle: 0 },
		inventory: [],
		casts: [],
		vitals: []
	};
	for ( let i = 0; i < 30; i++ ) presenter.step( [ rider, mount ], gameplay, i / 60 );
	assert.equal( presenter.error(), null );
	// CCharactor_GetActiveMoverEntity (0x85E000): the local movement owner
	// drives the mount, so the anchor is the local pose raised by the mount's
	// authored height (2 + 40 - 13), not the mount row's stale spawn point.
	assert.deepEqual( presenter.cameraTarget(), {
		mounted: true,
		height: 20,
		pose: { regionId: 257, x: 1, y: 29, z: 3, angle: 0 }
	} );
	presenter.step( [ rider ], gameplay, 1 );
	assert.deepEqual(
		presenter.cameraTarget(),
		{ mounted: false, height: 20, pose: gameplay.pose },
		"a rider whose mount is absent is followed on foot, never lost"
	);
	presenter.step( [ { ...rider, mountedOn: undefined } ], gameplay, 2 );
	assert.deepEqual( presenter.cameraTarget(), { mounted: false, height: 20, pose: gameplay.pose } );
	presenter.reset();
	assert.equal( presenter.cameraTarget(), null );
	presenter.dispose();
});
