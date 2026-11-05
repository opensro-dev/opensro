/*
===========================================================================

frontend-entry-report.test.mjs - what a refused world entry files

A refused entry whose session carries the mint's typed MAINTENANCE
refusal files as world_entry_maintenance, never as the rejection
incident a real failure files (#2557): a release restart must not
drown real entry failures. Any other refusal (or none) keeps
world_entry_rejected.

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
async function load( file ) {
	return import( sourceFileUrl( path.resolve( root, file ) ).href );
}
const { createFrontend } = await load( "src/engine/runtime/frontend/frontend.ts" );

/*
================
entryFixture

A frontend over instant fixture assets, stepped 100 ms at a time against a
session the caller sets. A failing fixture answers every asset request with
an error, so the title stage fails and the frontend fails with it.
================
*/
/** @param {boolean} [failing] @param {( command: { kind: string; code?: string; } ) => void} [send] */
function entryFixture( failing = false, send = undefined ) {
	let serial = 0, sceneId = "";
	const jobs = new Map();
	const manifest = {
		camera: [ {
			timeSeconds: 0,
			sectorX: 81,
			sectorY: 105,
			position: { x: 0, y: 0, z: 0 },
			rotation: { x: 0, y: 0, z: 0 },
			mode: 30
		} ],
		cameraControllerTargetTimeSeconds: 0,
		regionBundlePublicPath: "/assets/fixture-world.json"
	};
	const assets = {
		available: () => 8,
		request: ( url, size, kind ) => {
			const id = ++serial;
			jobs.set(
				id,
				failing ?
					{ kind: "error", error: "fixture failure" } :
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
		worldStats: () => ({ sceneId, pendingGroups: 0, pendingTextures: 0 }),
		setWorldCamera() {},
		characterStats: () => ({ actors: 1 })
	};
	const frontend = createFrontend( assets, renderer, "http://fixture.invalid", send );
	const fixture = {
		frontend,
		now: 0,
		session: { phase: "signed-out", revision: 1, characters: /** @type {any[]} */ ([]) },
		step: () => frontend.step( fixture.session, fixture.now += 100 ),
		/*
		================
		toDock

		Signs in to a one-character roster and waits for the dock to settle.
		================
		*/
		toDock() {
			fixture.selectRoster();
			for ( let i = 0; i < 30 && frontend.snapshot().phase !== "dock"; i++ ) fixture.step();
			assert.equal( frontend.snapshot().phase, "dock" );
		},
		selectRoster() {
			fixture.session = {
				phase: "character-select",
				revision: 2,
				characters: [ { id: 1, name: "fixture", deletePending: false, visualLoadout: { heightScale: 1 } } ]
			};
		},
		bind( phase ) {
			fixture.session = { ...fixture.session, phase, revision: fixture.session.revision + 1 };
			return fixture.step();
		}
	};
	for ( let i = 0; i < 20 && ![ "intro", "failed" ].includes( frontend.snapshot().phase ); i++ ) fixture.step();
	return fixture;
}

/*
================
maintenanceEntryFilesApart

The dock bounces a refused entry back to character select; the one
client-report it files carries the maintenance code only when the
session's entry failure carries the typed refusal.
================
*/
test("a maintenance refusal files as world_entry_maintenance, a real refusal as world_entry_rejected", () => {
	for (
		const [refusal, expected] of [
			[ "MAINTENANCE", "world_entry_maintenance" ],
			[ "UNKNOWN_CHARACTER", "world_entry_rejected" ],
			[ undefined, "world_entry_rejected" ]
		]
	) {
		const sent = [];
		const f = entryFixture( false, command => sent.push( command ) );
		f.toDock();
		f.frontend.select( "fixture" );
		for ( let i = 0; i < 30; i++ ) f.step();
		assert.equal( f.frontend.start(), true );
		f.session = { ...f.session, phase: "connecting", revision: 3 };
		f.step();
		f.session = {
			...f.session,
			phase: "character-select",
			revision: 4,
			error: "The server is under maintenance. Please try again later.",
			entryFailure: {
				stage: "connecting",
				bytesReceived: 0,
				framesReceived: 0,
				...(refusal ? { refusal } : {})
			}
		};
		f.step();
		const reports = sent.filter( command => command.kind === "client-report" );
		assert.equal( reports.length, 1, refusal ?? "no refusal" );
		assert.equal( reports[0].code, expected, refusal ?? "no refusal" );
		f.frontend.dispose();
	}
});
