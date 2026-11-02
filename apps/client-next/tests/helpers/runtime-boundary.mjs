/*
===========================================================================

runtime-boundary.mjs - the synthetic runtime build boundary

Bundles the real runtime with every owner replaced by a recording fake,
so a test drives the composition across the same wiring production
uses. Shared by the runtime failure/teardown suites and the own
displacement adoption suite (#1569).

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { build } from "esbuild";
export const factories = {
	frontend: "createFrontend",
	navigation: "createNavigationStream",
	ui: "createUi",
	audio: "createAudio",
	characters: "createCharacterPresentation",
	world: "createWorldStream",
	presentation: "createPresentation",
	assets: "createAssets",
	input: "createInput",
	platform: "createPlatform",
	renderer: "createRenderer",
	simulation: "createSimulationHost",
	release: "createReleaseWatch",
	"bug-report": "createBugReport",
	"build-info": "createBuildInfo"
};
const compiled = await build( {
	entryPoints: [ "src/engine/runtime/runtime.ts" ],
	bundle: true,
	platform: "node",
	format: "esm",
	write: false,
	define: {
		"import.meta.env.DEV": "false",
		// A real build always defines MODE (frame-probes.ts reads it).
		"import.meta.env.MODE": JSON.stringify( "production" ),
		// tools/build-metadata.mjs stamps these in a real build; the fixture has none.
		"import.meta.env.SRO_CLIENT_REVISION": "undefined",
		"import.meta.env.SRO_CLIENT_SUBJECT": "undefined",
		"import.meta.env.VITE_AGENT_API_BASE": "undefined",
		"import.meta.url": JSON.stringify( "http://localhost/runtime.ts" )
	},
	plugins: [ {
		name: "runtime-owners",
		/*
================
setup
================
		*/
		setup( build ) {
			build.onResolve( { filter: /^\.\// }, args => {
				const owner = args.path.split( "/" )[1];
				if ( factories[owner] ) return { path: owner, namespace: "owner" };
			} );
			build.onLoad(
				{ filter: /.*/, namespace: "owner" },
				args => ({
					contents: `export function ${factories[args.path]}( ...args ) {
( globalThis.__runtimeCalls ??= [] ).push( { owner: ${JSON.stringify( args.path )}, args } );
return globalThis.__runtimeOwners[${JSON.stringify( args.path )}];}`
				})
			);
		}
	} ]
} );
export const { startRuntime } = await import(
	"data:text/javascript;base64," + Buffer.from( compiled.outputFiles[0].contents ).toString( "base64" )
);
/*
================
noTextureFeatures

Every fake owner's texture-features hook: the renderer's resolves empty
(no variant), the asset owner's ignores what it is given.
================
*/
export function noTextureFeatures() {
	return Promise.resolve( [] );
}

/*
================
frameOwners

Owners whose first frame reaches characters.step; failure selects which
owner fails it ("assets" dies idle, "characters" throws from its step).
================
*/
export function frameOwners( failure, closed, reports, sounds ) {
	// The fakes grow owner-specific members below and in each suite;
	// the map is deliberately untyped beyond the shared first-frame shape.
	/** @type {{ [owner: string]: any }} */
	const owners = Object.fromEntries(
		Object.keys( factories ).map( key => [ key, {
			textureFeatures: noTextureFeatures,
			/*
================
step
================
			*/
			step() {},
			/*
================
dispose
================
			*/
			dispose() {
				closed.push( key );
			}
		} ] )
	);
	Object.assign( owners.assets, {
		health: () => failure === "assets" ? { phase: "failed", error: "worker died" } : { phase: "running" }
	} );
	owners.renderer.setCharacterPreview = () => {};
	owners.renderer.setSelectionDecal = () => {};
	// The frame hands the free-fly arm its keys before each world step.
	owners.world.setFreeCameraKeys = () => {};
	Object.assign( owners.characters, { frameWork() {} } );
	Object.assign( owners.platform, {
		report: value => reports.push( value ),
		runningEntry: () => null,
		/*
================
setMovementDump
================
		*/
		setMovementDump() {},
		setAssetDump() {},
		setKeepAwake() {},
		visibilityReturned: () => false,
		connectionReturned: () => false,
		/*
================
presentUpdate
================
		*/
		presentUpdate() {}
	} );
	owners.release.newerAvailable = () => false;
	Object.assign( owners.input, {
		drain: () => null,
		error: () => null,
		camera: () => ({ yaw: 0, pitch: 0, distance: 80 })
	} );
	Object.assign( owners.simulation, {
		// Never settles: these frames are driven by the fake RAF alone.
		delivery: () => new Promise( () => {} ),
		pollSession: () => null,
		pollWorld: () => null,
		poll: () => ({ timeMs: 1000, sequence: 1, acceptedInputSequence: 0 }),
		error: () => null,
		camera: () => null
	} );
	owners.audio.world = () => {};
	owners.audio.nativeUi = ( handle, at ) => sounds.push( { handle, at } );
	owners.audio.nativeItem = ( cue, at ) => sounds.push( { ...cue, at } );
	Object.assign( owners.presentation, {
		gameplay: () => null,
		entities: () => [],
		takeFeedback: () => [],
		takeSounds: () => [ { kind: "ui-sound", handle: "SND_POTION", at: 950 }, {
			kind: "item-sound",
			cue: { handle: "SND_EQUIP", typeFlags: 0x132c },
			at: 900
		} ]
	} );
	Object.assign( owners.world, {
		error: () => null,
		reconnecting: () => false,
		terminalFailure: () => undefined,
		step: () => {},
		pumpCameraScripts: () => {}
	} );
	owners.renderer.setWorldClock = () => {};
	owners.renderer.setWeather = () => {};
	owners.frontend.step = () => ({ phase: "loading-title" });
	Object.assign( owners.navigation, {
		error: () => null,
		reconnecting: () => false,
		terminalFailure: () => undefined,
		step: () => {}
	} );
	owners.characters.previewReady = () => false;
	owners.characters.dockReady = () => false;
	owners.characters.entryReady = () => false;
	owners.ui.entryReady = () => false;
	owners.characters.profile = () => {};
	owners.characters.eventRain = () => false;
	owners.characters.orbGauge = () => ({ authoritative: 0, displayed: 0, pending: 0 });
	owners.characters.receiveFeedback = () => {};
	// The main-thread mover's per-frame state (#1440).
	owners.characters.ownMover = () => {};
	owners.characters.drawnPose = () => undefined;
	owners.characters.step = () => {
		throw new Error( "injected presentation failure" );
	};
	Object.assign( owners.characters, { mallOutfit: () => {}, mallPreviewState: () => ({ wearable: [] }) } );
	Object.assign( owners.ui, {
		mallPreview: () => null,
		mallPreviewState: () => {},
		debugClockSeed: seed => seed
	} );
	return owners;
}
