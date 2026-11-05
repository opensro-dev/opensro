/*
===========================================================================

entry-fixture.mjs - a frontend over instant fixture assets

Shared entry-flow fixture: a real createFrontend stepped 100 ms at a
time against a session the caller sets. A failing fixture answers every
asset request with an error, so the title stage fails and the frontend
fails with it.

===========================================================================
*/
import "./native-source-loader.mjs";
import assert from "node:assert/strict";
const { createFrontend } = await import( "../../src/engine/runtime/frontend/frontend.ts" );

/*
================
entryFixture

The fixture: frontend, the stepped clock, the session it is driven
with, and the roster/dock/phase helpers the entry tests drive it by.
================
*/
/**
 * @param {boolean} [failing] answer every asset request with an error
 * @param {import("../../src/engine/contracts/session").SessionCommand[]} [sent] collects the frontend's commands
 */
export function entryFixture( failing = false, sent = undefined ) {
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
	// Only the four methods the entry flow calls; the stage reads kind,
	// error, buffer and world off a result and nothing else, so the
	// fake's results carry just those (compare navigation-view.test.mjs).
	const assets = /** @type {import("../../src/engine/contracts/assets").AssetOwner} */ (/** @type {unknown} */ ({
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
	}));
	// Only the members the entry flow calls; the rest of Renderer is
	// the live page's surface and stays unmodelled here.
	const renderer = /** @type {import("../../src/engine/contracts/runtime").Renderer} */ (/** @type {unknown} */ ({
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
	}));
	const frontend = createFrontend(
		assets,
		renderer,
		"http://fixture.invalid",
		sent ? command => sent.push( command ) : undefined
	);
	/** @type {import("../../src/engine/contracts/session").SessionState} */
	const signedOut = { phase: "signed-out", revision: 1, characters: [] };
	const fixture = {
		frontend,
		now: 0,
		session: signedOut,
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
				characters: [ {
					id: 1,
					name: "fixture",
					level: 1,
					raceIndex: 0,
					gender: 0,
					figureIndex: 0,
					heightIndex: 0,
					volumeIndex: 0,
					weaponIndex: 0,
					protectorIndex: 0,
					armorSelected: true,
					weaponSelected: true,
					deletePending: false,
					maxHp: 100,
					maxMp: 100,
					visualLoadout: {
						modelCodename: "CHAR_TEST",
						items: [],
						avatars: [],
						animationSetName: "test",
						heightScale: 1,
						volumeScale: 1
					}
				} ]
			};
		},
		/** @param {import("../../src/engine/contracts/session").SessionState["phase"]} phase */
		bind( phase ) {
			fixture.session = { ...fixture.session, phase, revision: fixture.session.revision + 1 };
			return fixture.step();
		}
	};
	for ( let i = 0; i < 20 && ![ "intro", "failed" ].includes( frontend.snapshot().phase ); i++ ) fixture.step();
	return fixture;
}
