/*
===========================================================================

resourceBuildOrder.test.mjs - the full build's dependency order, run

Runs buildSroResources with recording stub steps at one lane and at eight.
Every producer runs, converted source images exist before any lane starts,
the in-lane chains keep their order, and the pack tail runs once, last,
with the outdoor gate and sidecar retirement a full build owns.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { buildSroResources, RESOURCE_BUILD_STEPS } from "../../build/resourceBuild.mjs";

// Prerequisites that run before the source-image conversion.
const PRE_CONVERSION = new Set( [ "buildNativeLensResources", "buildNativeCharacterTextures" ] );
const STEP_DELAY_MS = 2;

// The minimal results the orchestration itself reads; everything else is {}.
const RESULTS = {
	runConvertImages: () => ({ status: 0 }),
	buildTitleResources: () => ({ primary: { area: "constantinople" }, manifests: [ { area: "constantinople" } ] }),
	buildTitleWorldRegionResources: ( manifest ) => ({ bundle: { source: { area: manifest.area } } }),
	buildTextResources: () => ({ textCatalog: {} }),
	buildUiImagePreloadManifest: () => ({ images: [ { path: "/assets/images/ui.png" } ] }),
	copyMissionMinimapTileImages: () => [ { publicPath: "/assets/images/minimap.png" } ],
	loadOutdoorWorldRegionResourceGroup: () => null
};

/*
================
recordingSteps

Every producer in RESOURCE_BUILD_STEPS, recording "start"/"end" events and
yielding between them so concurrent lanes interleave.
================
*/
function recordingSteps( overrides = {} ) {
	const events = [];
	const calls = {};
	const steps = {};
	for ( const name of Object.keys( RESOURCE_BUILD_STEPS ) ) {
		steps[name] = async ( ...args ) => {
			events.push( `start:${name}` );
			(calls[name] ??= []).push( args );
			await new Promise( ( resolve ) => setTimeout( resolve, STEP_DELAY_MS ) );
			events.push( `end:${name}` );
			const result = overrides[name] ?? RESULTS[name];
			return result ? result( ...args ) : {};
		};
	}
	return { events, calls, steps };
}

/*
================
position
================
*/
function position( events, event ) {
	const index = events.indexOf( event );
	assert.ok( index >= 0, `${event} never happened` );
	return index;
}

for ( const laneCount of [ 1, 8 ] ) {
	test(`every producer runs, after source images and before the pack tail (${laneCount} lane(s))`, async () => {
		const { events, calls, steps } = recordingSteps();
		await buildSroResources( steps, { laneCount, log: () => {} } );

		for ( const name of Object.keys( RESOURCE_BUILD_STEPS ) ) {
			assert.ok( calls[name], `${name} never ran` );
		}
		assert.deepEqual( calls.runConvertImages[0], [ [] ], "the full build owns the unfiltered conversion" );
		const converted = position( events, "end:runConvertImages" );
		for ( const [index, event] of events.entries() ) {
			const name = event.slice( event.indexOf( ":" ) + 1 );
			if ( event.startsWith( "start:" ) && name !== "runConvertImages" && !PRE_CONVERSION.has( name ) ) {
				assert.ok( index > converted, `${name} started before the source images were converted` );
			}
		}
		const tail = position( events, "start:packTree" );
		assert.equal( calls.packTree.length, 1 );
		assert.equal( events.filter( ( event ) => event.startsWith( "start:" ) ).length, events.length / 2 );
		assert.ok(
			events.slice( 0, tail ).every( ( event, i, all ) =>
				event.startsWith( "end:" ) || all.includes( `end:${event.slice( 6 )}` )
			),
			"every producer finished before the pack tail began"
		);
	});

	test(`in-lane chains keep their order (${laneCount} lane(s))`, async () => {
		const { events, steps } = recordingSteps();
		await buildSroResources( steps, { laneCount, log: () => {} } );
		// Roster publication is the character lane's dependency root.
		for ( const follower of [ "buildCrowdVatAssets", "buildLocomotionBanAssets", "buildDropModelAssets" ] ) {
			assert.ok( position( events, "end:buildRoster" ) < position( events, `start:${follower}` ), follower );
		}
		// NPC VAT consumes the manifest the model build just wrote.
		assert.ok( position( events, "end:buildNpcModelAssets" ) < position( events, "start:buildNpcVatAssets" ) );
		// Animated world objects scan the region bundles the world lane wrote.
		assert.ok(
			position( events, "end:buildTitleWorldRegionResources" ) <
				position( events, "start:buildWorldAnimatedObjects" )
		);
		// Text feeds the launcher its catalog.
		assert.ok( position( events, "end:buildTextResources" ) < position( events, "start:buildLauncherResources" ) );
	});
}

test("the pack tail gets the caller's lists, the outdoor gate and sidecar retirement", async () => {
	const withoutOutdoor = recordingSteps();
	await buildSroResources( withoutOutdoor.steps, { laneCount: 2, log: () => {} } );
	const [request] = withoutOutdoor.calls.packTree[0];
	assert.equal( request.retireSidecars, true );
	assert.deepEqual( request.groupInputs, {
		uiImagePreloadPaths: [ "/assets/images/ui.png" ],
		missionMinimapTilePaths: [ "/assets/images/minimap.png" ],
		includeOutdoorWorld: false
	} );

	const withOutdoor = recordingSteps( { loadOutdoorWorldRegionResourceGroup: () => ({ incompleteBundleCount: 0 }) } );
	await buildSroResources( withOutdoor.steps, { laneCount: 2, log: () => {} } );
	assert.equal( withOutdoor.calls.packTree[0][0].groupInputs.includeOutdoorWorld, true );
});

test("a failed source-image conversion stops the build before any lane", async () => {
	const { calls, steps } = recordingSteps( { runConvertImages: () => ({ status: 1 }) } );
	await assert.rejects(
		buildSroResources( steps, { laneCount: 2, log: () => {} } ),
		/Source image conversion failed/
	);
	assert.equal( calls.buildCifResources, undefined );
	assert.equal( calls.packTree, undefined );
});

for ( const laneCount of [ 1, 8 ] ) {
	test(`the families run after every builder and before the image sweep and background install (${laneCount} lane(s))`, async () => {
		const { events, steps } = recordingSteps();
		await buildSroResources( steps, { laneCount, log: () => {} } );
		const familiesStart = position( events, "start:produceAllFamilies" );
		const familiesEnd = position( events, "end:produceAllFamilies" );
		// Their code-selected art must be in the native-interface preload sweep,
		// and their sounds in the background-install list.
		assert.ok( familiesEnd < position( events, "start:buildUiImagePreloadManifest" ) );
		assert.ok( familiesEnd < position( events, "start:buildBackgroundInstallAsset" ) );
		for ( const builder of [ "buildCifResources", "buildNpcModelAssets", "buildTextResources" ] ) {
			if ( !events.includes( `end:${builder}` ) ) continue;
			assert.ok(
				position( events, `end:${builder}` ) < familiesStart,
				`${builder} finishes before the families`
			);
		}
	});
}
