/*
===========================================================================

background-install.test.mjs - tests for assets/worker/install.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

const { backgroundInstallPaths, createBackgroundInstaller, BACKGROUND_INSTALL_FORMAT, BACKGROUND_INSTALL_VERSION } =
	await import( sourceFileUrl( "src/engine/runtime/assets/worker/install.ts" ).href );

const ORIGIN = "https://opensro.test";

/*
================
installList
================
*/
function installList( tiers ) {
	return { format: BACKGROUND_INSTALL_FORMAT, version: BACKGROUND_INSTALL_VERSION, tiers };
}

/*
================
fakeSource

A pack reader that serves the list and records installs. `installed` paths
report as already local; `broken` paths throw.
================
*/
/**
 * @param {unknown} list
 * @param {{ installed?: string[], broken?: string[] }} [options]
 */
function fakeSource( list, { installed = [], broken = [] } = {} ) {
	const calls = [];
	return {
		calls,
		async read( url, _limit, _signal, report ) {
			calls.push( [ "read", new URL( url ).pathname, report ] );
			return new TextEncoder().encode( JSON.stringify( list ) );
		},
		async install( url ) {
			const path = new URL( url ).pathname;
			calls.push( [ "install", path ] );
			if ( broken.includes( path ) ) throw new Error( "unavailable" );
			return !installed.includes( path );
		}
	};
}

/*
================
settle

Runs the installer's async loop to completion under a fake clock.
================
*/
async function settle( installer ) {
	for ( let i = 0; i < 200 && !installer.stats().done; i++ ) {
		await new Promise( ( resolve ) => setImmediate( resolve ) );
	}
}

test("the list is read as tiers in order and rejects anything outside /assets/", () => {
	assert.deepEqual(
		backgroundInstallPaths( installList( [
			{ name: "combat", paths: [ "/assets/audio/sfx/prim/snd/player/a.wav", "/assets/skillfx/b.glb" ] },
			{ name: "world-sounds", paths: [ "/assets/audio/sfx/prim/snd/monster/c.wav" ] }
		] ) ),
		[
			"/assets/audio/sfx/prim/snd/player/a.wav",
			"/assets/skillfx/b.glb",
			"/assets/audio/sfx/prim/snd/monster/c.wav"
		]
	);
	assert.throws(
		() => backgroundInstallPaths( { format: "other", version: 1, tiers: [] } ),
		/Invalid background install list/
	);
	assert.throws( () => backgroundInstallPaths( installList( [ { name: "x", paths: [ "/api/secret" ] } ] ) ), /path/ );
	assert.throws(
		() => backgroundInstallPaths( installList( [ { name: "x", paths: [ "/assets/../x" ] } ] ) ),
		/path/
	);
	assert.throws( () => backgroundInstallPaths( installList( [ { name: "x", paths: [ "/assets\\x" ] } ] ) ), /path/ );
});

test("installs every listed file in order, silently, counting fetched, skipped and failed", async () => {
	const list = installList( [
		{ name: "combat", paths: [ "/assets/a.wav", "/assets/b.wav", "/assets/c.wav" ] },
		{ name: "world-sounds", paths: [ "/assets/d.wav" ] }
	] );
	const source = fakeSource( list, { installed: [ "/assets/b.wav" ], broken: [ "/assets/c.wav" ] } );
	const installer = createBackgroundInstaller( source, async () => {} );
	installer.start( ORIGIN + "/assets/delivery/background-install.json" );
	await settle( installer );

	assert.deepEqual( source.calls, [
		[ "read", "/assets/delivery/background-install.json", false ],
		[ "install", "/assets/a.wav" ],
		[ "install", "/assets/b.wav" ],
		[ "install", "/assets/c.wav" ],
		[ "install", "/assets/d.wav" ]
	] );
	assert.deepEqual( installer.stats(), { started: true, done: true, fetched: 2, skipped: 1, failed: 1 } );
});

test("waits for the foreground to go idle before each file and starts only once", async () => {
	const source = fakeSource( installList( [ { name: "combat", paths: [ "/assets/a.wav", "/assets/b.wav" ] } ] ) );
	/** @type {( value?: unknown ) => void} */
	let release = () => {};
	let idleCalls = 0;
	// The foreground stays busy until the test releases it.
	const idle = () => {
		idleCalls++;
		return new Promise( ( resolve ) => release = resolve );
	};
	const installer = createBackgroundInstaller( source, idle );
	installer.start( ORIGIN + "/assets/delivery/background-install.json" );
	installer.start( ORIGIN + "/assets/delivery/other.json" );
	for ( let i = 0; i < 20; i++ ) await new Promise( ( resolve ) => setImmediate( resolve ) );

	assert.deepEqual(
		source.calls.map( ( call ) => call[0] ),
		[ "read" ],
		"nothing installs while the foreground is busy"
	);
	assert.equal( source.calls[0][1], "/assets/delivery/background-install.json", "a second start is ignored" );
	release();
	for ( let i = 0; i < 20; i++ ) await new Promise( ( resolve ) => setImmediate( resolve ) );
	release();
	await settle( installer );

	assert.equal( idleCalls, 2, "the installer waits for idle before every file" );
	assert.deepEqual( source.calls.map( ( call ) => call[0] ), [ "read", "install", "install" ] );
});

test("disposal stops the install between files", async () => {
	const source = fakeSource( installList( [ { name: "combat", paths: [ "/assets/a.wav", "/assets/b.wav" ] } ] ) );
	let installer;
	source.install = async ( url ) => {
		source.calls.push( [ "install", new URL( url ).pathname ] );
		installer.dispose();
		return true;
	};
	installer = createBackgroundInstaller( source, async () => {} );
	installer.start( ORIGIN + "/assets/delivery/background-install.json" );
	for ( let i = 0; i < 50; i++ ) await new Promise( ( resolve ) => setImmediate( resolve ) );

	assert.deepEqual( source.calls.filter( ( call ) => call[0] === "install" ), [ [ "install", "/assets/a.wav" ] ] );
	assert.equal( installer.stats().done, false );
});

test("failed install-list delivery can be retried without restarting the asset owner", async () => {
	const source = fakeSource( installList( [ { name: "combat", paths: [ "/assets/a.wav" ] } ] ) );
	const read = source.read;
	let attempts = 0;
	source.read = async ( ...args ) => {
		if ( ++attempts === 1 ) throw Error( "temporary outage" );
		return read( ...args );
	};
	const installer = createBackgroundInstaller( source, async () => {} );
	installer.start( ORIGIN + "/assets/delivery/background-install.json" );
	await settle( installer );
	assert.equal( installer.stats().started, false );
	installer.start( ORIGIN + "/assets/delivery/background-install.json" );
	await settle( installer );
	assert.equal( installer.stats().done, true );
	assert.equal( installer.stats().fetched, 1 );
	assert.equal( attempts, 2 );
	installer.dispose();
});

test("the install document lists only actual release routes and omits compressed sidecars", async () => {
	const { backgroundInstallDocument } = await import(
		"../../../../scripts/build/data/buildBackgroundInstallAsset.mjs"
	);
	const sound = "/assets/audio/sfx/prim/snd/player/swing.wav",
		monster = "/assets/audio/sfx/prim/snd/monster/hit.wav",
		model = "/assets/skillfx/hit.glb";
	const document = backgroundInstallDocument( [
		monster,
		sound,
		model,
		sound,
		sound + ".gz",
		model + ".br",
		"/assets/music/theme.ogg"
	] );
	assert.deepEqual( backgroundInstallPaths( document ), [ sound, model, monster ] );
});
