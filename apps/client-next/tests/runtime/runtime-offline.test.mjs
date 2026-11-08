/*
===========================================================================

runtime-offline.test.mjs - the asset_offline filing discipline (#2869)

A fetch burst across sources arms an asset_offline that only failures
still arriving a full OFFLINE_REPORT_GAP_MS later file: a drop the retry
owners recover inside their backoff files nothing, and a failure that
arrives more than a full gap after the previous one buries the arm, so a
later blink starts a fresh streak instead of filing the old one's report.

Split from runtime-failures.test.mjs on the responsibility boundary: that
file owns failure classification and teardown, this one owns the offline
arm/file/bury state machine against its own runtime and clock.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { defined } from "../helpers/defined.mjs";
import { frameOwners, startRuntime } from "../helpers/runtime-boundary.mjs";

test("a fetch burst arms asset_offline, a sustained streak files it, silence buries the arm", async t => {
	const keys = [
		"__runtimeOwners",
		"requestAnimationFrame",
		"cancelAnimationFrame",
		"location",
		"document",
		"performance"
	];
	const previous = new Map( keys.map( key => [ key, Object.getOwnPropertyDescriptor( globalThis, key ) ] ) );
	t.after( () => {
		for ( const [key, value] of previous ) {
			if ( value ) Object.defineProperty( globalThis, key, value );
			else delete globalThis[key];
		}
	} );
	const closed = [], reports = [], sounds = [], commands = [];
	const owners = frameOwners( "characters", closed, reports, sounds );
	// The frame clock and the deliveries are the test's own (the
	// runtime-failures pattern): one frame advances the clock one
	// second, and a frame runs when a delivery settles.
	let now = 1000;
	const deliveries = [];
	globalThis.performance = { now: () => now };
	globalThis.document = { visibilityState: "visible" };
	globalThis.location = { search: "", origin: "http://localhost" };
	owners.simulation.delivery = () => new Promise( resolve => deliveries.push( resolve ) );
	globalThis.__runtimeOwners = owners;
	globalThis.requestAnimationFrame = () => 1;
	globalThis.cancelAnimationFrame = () => {};
	// The owner surface the frame reads on its way to the report block.
	let characterError = null, rendererError = null;
	owners.simulation.session = command => {
		commands.push( command );
	};
	Object.assign( owners.world, {
		error: () => null,
		reconnecting: () => false,
		ready: () => false,
		progress: () => 0,
		loadingRegion: () => undefined,
		terminalFailure: () => undefined
	} );
	Object.assign( owners.navigation, {
		error: () => null,
		reconnecting: () => false,
		phase: () => "idle",
		terminalFailure: () => undefined
	} );
	owners.characters.step = () => {};
	owners.characters.cameraTarget = () => null;
	owners.characters.drawnPose = () => null;
	owners.characters.ownMover = () => {};
	owners.characters.ready = () => false;
	owners.characters.damageText = () => [];
	owners.characters.takeCameraScripts = () => [];
	owners.characters.error = () => characterError;
	owners.characters.failure = () => null;
	owners.ui.event = () => {};
	owners.ui.cursor = () => null;
	owners.renderer.experimentalVideo = () => {};
	owners.renderer.setDamageText = () => {};
	owners.renderer.setTeleportGates = () => {};
	owners.renderer.pickEntity = () => null;
	owners.renderer.audioListener = () => null;
	owners.world.retryTransient = () => {};
	owners.navigation.retryTransient = () => {};
	owners.platform.readViewport = () => ({ width: 800, height: 600 });
	owners.platform.presentWorldCursor = () => {};
	owners.platform.presentTelemetry = () => {};
	owners.platform.setKeepAwake = () => {};
	owners.presentation.travel = () => null;
	owners.presentation.read = () => undefined;
	owners.input.altHeld = () => false;
	Object.assign( owners.renderer, {
		worldStats: () => ({ visibleGroups: 0, pendingGroups: 0, pendingTextures: 0, residentGroups: 0 }),
		phase: () => rendererError ? "failed" : "ready",
		error: () => rendererError,
		adapterFacts: () => ({
			vendor: "nvidia",
			architecture: "ampere",
			features: [ "texture-compression-bc" ],
			limits: { maxBufferSize: 0, maxStorageBufferBindingSize: 0, maxTextureDimension2D: 0 }
		}),
		characterStats: () => ({ actors: 0, draws: 0 }),
		gpuTiming: () => undefined
	} );
	Object.assign( owners.platform, {
		presentUi() {},
		presentLoading() {},
		readUiViewport: () => ({ width: 800, height: 600 }),
		diagnosticsActive: () => false
	} );
	owners.assets.progress = () => null;
	Object.assign( owners.audio, {
		enqueue() {},
		ready: () => true,
		error: () => null,
		music() {},
		musicStatus: () => "stopped",
		prepareUi() {},
		prepareCombat() {}
	} );
	Object.assign( owners.ui, {
		resourceError: () => null,
		stats: () => ({ pending: 0, failed: [], error: undefined }),
		runtimeError() {}
	} );
	owners["bug-report"].note = () => {};
	owners["build-info"].readout = () => "";
	owners.input.dropNamesHeld = () => false;
	owners.input.blindHeld = () => false;
	owners.presentation.count = () => 0;
	const runtime = startRuntime( {}, {} );
	// A failed assertion must not leak a running runtime: it keeps node alive.
	try {
		const frame = async () => {
			now += 1000;
			for ( let i = 0; i < 64 && deliveries.length === 0; i++ ) await Promise.resolve();
			defined( deliveries.shift() )();
			for ( let i = 0; i < 32; i++ ) await Promise.resolve();
		};
		const assetReports = () => commands.filter( c => c.kind === "client-report" );
		const offlineCount = () => assetReports().filter( report => report.code === "asset_offline" ).length;
		await frame();
		assert.deepEqual( assetReports(), [], "no failure, no incident" );
		// A fetch burst across sources in one poll is a dropped
		// connection, not three broken assets (#2559): the first two
		// file as asset failures, and the third completes the burst -
		// but a burst that recovers inside the retry owners' backoff
		// files no asset_offline (#2869): the burst only arms it.
		characterError = "TypeError: Failed to fetch";
		owners.audio.error = () => "TypeError: Failed to fetch";
		owners.audio.failure = () => ({ path: "/assets/audio/burst.ogg", message: "TypeError: Failed to fetch" });
		owners.ui.resourceError = () =>
			"UI image unavailable; retrying: /assets/ui/burst.ddj: TypeError: Failed to fetch";
		owners.ui.resourceFailure = () => ({
			path: "/assets/ui/burst.ddj",
			attempts: 1,
			transient: true,
			message: "UI image unavailable; retrying: /assets/ui/burst.ddj: TypeError: Failed to fetch"
		});
		const burstStart = assetReports().length;
		await frame();
		assert.deepEqual( assetReports().slice( burstStart ).map( report => report.code ), [
			"asset_failed",
			"asset_failed"
		] );
		// The outage's later fetch failures are the same outage: they
		// file nothing more, never an asset_failed, while the armed
		// report waits out its gap.
		const tailStart = assetReports().length;
		characterError = "TypeError: Failed to fetch (burst tail)";
		await frame();
		assert.deepEqual( assetReports().slice( tailStart ), [] );
		// Silence past the gap buries the armed report: had the
		// connection recovered here, nothing would ever file. This
		// outage instead resumes after five silent frames, so its
		// first failure arrives more than a full gap after the tail
		// and buries the first burst's arm: the resumed burst arms
		// fresh and files no asset_offline of its own.
		for ( let i = 0; i < 5; i++ ) await frame();
		assert.equal( offlineCount(), 0 );
		characterError = "TypeError: Failed to fetch (sustained)";
		owners.audio.error = () => "TypeError: Failed to fetch (sustained)";
		owners.audio.failure = () => ({
			path: "/assets/audio/burst2.ogg",
			message: "TypeError: Failed to fetch (sustained)"
		});
		owners.ui.resourceError = () =>
			"UI image unavailable; retrying: /assets/ui/burst2.ddj: TypeError: Failed to fetch (sustained)";
		owners.ui.resourceFailure = () => ({
			path: "/assets/ui/burst2.ddj",
			attempts: 4,
			transient: true,
			message: "UI image unavailable; retrying: /assets/ui/burst2.ddj: TypeError: Failed to fetch (sustained)"
		});
		const sustainedStart = assetReports().length;
		await frame();
		assert.deepEqual( assetReports().slice( sustainedStart ).map( report => report.code ), [
			"asset_failed",
			"asset_failed"
		] );
		// The resumed outage keeps failing: each frame's fresh burst
		// (new texts, so the poll reports them) lands inside the gap
		// of the one before, nothing files, until a failure arrives a
		// full gap after the fresh arm - that one files it.
		for ( let i = 2; i <= 5; i++ ) {
			characterError = `TypeError: Failed to fetch (sustained ${i})`;
			owners.audio.error = () => `TypeError: Failed to fetch (sustained ${i})`;
			owners.ui.resourceError = () =>
				`UI image unavailable; retrying: /assets/ui/burst2.ddj: TypeError: Failed to fetch (sustained ${i})`;
			const keepAliveStart = assetReports().length;
			await frame();
			assert.deepEqual( assetReports().slice( keepAliveStart ), [], `sustained ${i} files nothing` );
		}
		characterError = "TypeError: Failed to fetch (sustained 6)";
		owners.audio.error = () => "TypeError: Failed to fetch (sustained 6)";
		owners.ui.resourceError = () =>
			"UI image unavailable; retrying: /assets/ui/burst2.ddj: TypeError: Failed to fetch (sustained 6)";
		const filingStart = assetReports().length;
		await frame();
		assert.deepEqual( assetReports().slice( filingStart ).map( report => report.code ), [
			"asset_offline"
		] );
		assert.equal( offlineCount(), 1 );
		// A later blink is a new streak, not this outage's tail: its
		// first failure arrives more than a full gap after the last
		// one above, so its burst arms fresh and files nothing - the
		// arm an earlier streak left must not survive the silence.
		characterError = null;
		owners.audio.error = () => null;
		owners.ui.resourceError = () => null;
		for ( let i = 0; i < 6; i++ ) await frame();
		characterError = "TypeError: Failed to fetch (blink)";
		owners.audio.error = () => "TypeError: Failed to fetch (blink)";
		owners.ui.resourceError = () =>
			"UI image unavailable; retrying: /assets/ui/burst3.ddj: TypeError: Failed to fetch (blink)";
		const blinkStart = assetReports().length;
		await frame();
		assert.deepEqual( assetReports().slice( blinkStart ).map( report => report.code ), [
			"asset_failed",
			"asset_failed"
		] );
		assert.equal( offlineCount(), 1 );
	} finally {
		runtime.dispose();
	}
});
