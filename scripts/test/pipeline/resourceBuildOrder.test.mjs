/*
===========================================================================

resourceBuildOrder.test.mjs - the full build's dependency order, run

Runs buildSroResources with recording stub steps at one lane and at eight.
Primary producers publish packs before families read them on a cold build.
The final pack tail includes the families' output, audits complete claims
and retires sidecars only after every producer has finished.

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { buildSroResources, RESOURCE_BUILD_STEPS } from "../../build/resourceBuild.mjs";

// Prerequisites that run before the source-image conversion.
const PRE_CONVERSION = new Set( [ "buildNativeLensResources", "buildNativeCharacterTextures" ] );
const STEP_DELAY_MS = 2;
const EARLY_PASS = 0;
const FINAL_PASS = 1;
const PACK_PASS_COUNT = 2;
const PRIMARY_PACKED_SOURCE = "/assets/data/current.json";
const BASE_UI_IMAGE = "/assets/images/ui.png";
const FAMILY_UI_IMAGE = "/assets/images/family.png";

// The minimal results the orchestration itself reads; everything else is {}.
const RESULTS = {
	runConvertImages: () => ({ status: 0 }),
	buildTitleResources: () => ({ primary: { area: "constantinople" }, manifests: [ { area: "constantinople" } ] }),
	buildTitleWorldRegionResources: ( manifest ) => ({ bundle: { source: { area: manifest.area } } }),
	buildTextResources: () => ({ textCatalog: {} }),
	buildUiImagePreloadManifest: () => ({ images: [ { path: BASE_UI_IMAGE } ] }),
	copyMissionMinimapTileImages: () => [ { publicPath: "/assets/images/minimap.png" } ],
	loadOutdoorWorldRegionResourceGroup: () => null
};

/*
================
recordingSteps

Every producer in RESOURCE_BUILD_STEPS, recording "start"/"end" events. An
async producer yields between them so concurrent lanes interleave; a
synchronous one (the data planes) finishes before it returns, as it does in
the build, which calls it without await.
================
*/
function recordingSteps( overrides = {} ) {
	const events = [];
	const calls = {};
	const steps = {};
	for ( const [name, producer] of Object.entries( RESOURCE_BUILD_STEPS ) ) {
		if ( producer.constructor.name !== "AsyncFunction" ) {
			steps[name] = ( ...args ) => {
				events.push( `start:${name}` );
				(calls[name] ??= []).push( args );
				events.push( `end:${name}` );
				const result = overrides[name] ?? RESULTS[name];
				return result ? result( ...args ) : {};
			};
			continue;
		}
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
function position( events, event, occurrence = EARLY_PASS ) {
	let index = -1;
	for ( let count = 0; count <= occurrence; count++ ) {
		index = events.indexOf( event, index + 1 );
		assert.ok( index >= 0, `${event} occurrence ${occurrence} never happened` );
	}
	return index;
}

/*
================
coldStoreSteps

The family can only read published packs. Its new art remains loose until
the build lists and publishes the completed tree on the final pass.
================
*/
function coldStoreSteps() {
	const loose = new Map( [ [ BASE_UI_IMAGE, "primary UI image" ] ] );
	let packIndex = null;
	const publications = [];
	const recorded = recordingSteps( {
		buildMissionPresentationAsset: () => {
			loose.set( PRIMARY_PACKED_SOURCE, "primary producer output" );
			return {};
		},
		buildUiImagePreloadManifest: () => ({
			images: [ ...loose.keys() ].filter( ( path ) => path.startsWith( "/assets/images/" ) ).map( ( path ) => ({
				path
			}) )
		}),
		packTree: ( request ) => {
			packIndex = new Map( loose );
			publications.push( {
				packIndex,
				preloadPaths: [ ...request.groupInputs.uiImagePreloadPaths ]
			} );
			return {};
		},
		produceAllFamilies: () => {
			assert.equal(
				packIndex?.get( PRIMARY_PACKED_SOURCE ),
				"primary producer output",
				"the family must read its input through an already published pack index"
			);
			loose.set( FAMILY_UI_IMAGE, "family UI image" );
			return [];
		}
	} );
	return { ...recorded, publications };
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
		const tail = position( events, "start:packTree", FINAL_PASS );
		assert.equal( calls.packTree.length, PACK_PASS_COUNT );
		assert.equal( calls.buildUiImagePreloadManifest.length, PACK_PASS_COUNT );
		assert.equal( events.filter( ( event ) => event.startsWith( "start:" ) ).length, events.length / 2 );
		assert.ok(
			events.slice( 0, tail ).every( ( event, i, all ) =>
				event.startsWith( "end:" ) || all.includes( `end:${event.slice( 6 )}` )
			),
			"every producer finished before the pack tail began"
		);
		assert.deepEqual( events.slice( -2 ), [ "start:packTree", "end:packTree" ], "the final pack tail runs last" );
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

test("both pack passes get the caller's lists and only the final pass audits and retires", async () => {
	const withoutOutdoor = recordingSteps();
	await buildSroResources( withoutOutdoor.steps, { laneCount: 2, log: () => {} } );
	assert.equal( withoutOutdoor.calls.packTree.length, PACK_PASS_COUNT );
	for ( const [index, [request]] of withoutOutdoor.calls.packTree.entries() ) {
		assert.equal( request.auditClaims, index === FINAL_PASS ? true : undefined );
		assert.equal( request.retireSidecars, index === FINAL_PASS ? true : undefined );
		assert.deepEqual( request.groupInputs, {
			uiImagePreloadPaths: [ BASE_UI_IMAGE ],
			missionMinimapTilePaths: [ "/assets/images/minimap.png" ],
			includeOutdoorWorld: false
		} );
	}

	const withOutdoor = recordingSteps( { loadOutdoorWorldRegionResourceGroup: () => ({ incompleteBundleCount: 0 }) } );
	await buildSroResources( withOutdoor.steps, { laneCount: 2, log: () => {} } );
	for ( const [request] of withOutdoor.calls.packTree ) {
		assert.equal( request.groupInputs.includeOutdoorWorld, true );
	}
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

// Steps that consume the families' output, or run after the tree is complete.
// The texture variants re-encode every native texture, the families' included.
const AFTER_FAMILIES = [
	"buildTextureVariants",
	"buildUiIconAtlas",
	"buildUiImagePreloadManifest",
	"buildBackgroundInstallAsset",
	"packTree"
];

for ( const laneCount of [ 1, 8 ] ) {
	test(`the families run after every producer and before the image sweep and background install (${laneCount} lane(s))`, async () => {
		const { events, steps } = recordingSteps();
		await buildSroResources( steps, { laneCount, log: () => {} } );
		const earlyPackStart = position( events, "start:packTree" );
		const earlyPackEnd = position( events, "end:packTree" );
		const familiesStart = position( events, "start:produceAllFamilies" );
		const familiesEnd = position( events, "end:produceAllFamilies" );
		// The families read the data planes (effect records and programs, skill
		// and mastery data): a clean tree has none of them before their producer
		// ran. Every producer, not a chosen few, finishes first.
		for ( const [index, event] of events.entries() ) {
			const name = event.slice( event.indexOf( ":" ) + 1 );
			if ( !event.startsWith( "end:" ) || name === "produceAllFamilies" || AFTER_FAMILIES.includes( name ) ) {
				continue;
			}
			assert.ok( index < earlyPackStart, `${name} finishes before the early pack pass` );
		}
		assert.ok( position( events, "end:buildUiImagePreloadManifest" ) < earlyPackStart );
		assert.ok( earlyPackEnd < familiesStart, "the families start after their input packs are published" );
		// Their code-selected art must be in the native-interface preload sweep,
		// and their sounds in the background-install list.
		for ( const name of AFTER_FAMILIES ) {
			if ( events.includes( `start:${name}` ) ) {
				const occurrence = name === "packTree" || name === "buildUiImagePreloadManifest" ?
					FINAL_PASS :
					EARLY_PASS;
				assert.ok(
					familiesEnd < position( events, `start:${name}`, occurrence ),
					`${name} starts after the families`
				);
			}
		}
	});

	test(`a cold pack store is published before families and repacked with their new art (${laneCount} lane(s))`, async () => {
		const { steps, publications } = coldStoreSteps();
		const results = await buildSroResources( steps, { laneCount, log: () => {} } );
		assert.equal( publications.length, PACK_PASS_COUNT );
		assert.equal( publications[EARLY_PASS].packIndex.has( PRIMARY_PACKED_SOURCE ), true );
		assert.equal( publications[EARLY_PASS].packIndex.has( FAMILY_UI_IMAGE ), false );
		assert.deepEqual( publications[EARLY_PASS].preloadPaths, [ BASE_UI_IMAGE ] );
		assert.equal( publications[FINAL_PASS].packIndex.get( FAMILY_UI_IMAGE ), "family UI image" );
		assert.deepEqual( publications[FINAL_PASS].preloadPaths, [ BASE_UI_IMAGE, FAMILY_UI_IMAGE ] );
		assert.deepEqual( results.uiImagePreload.images.map( ( image ) => image.path ), [
			BASE_UI_IMAGE,
			FAMILY_UI_IMAGE
		] );
	});
}

test("a failed early pack publication stops the families and final tree consumers", async () => {
	const failure = new Error( "initial pack publication failed" );
	const { calls, steps } = recordingSteps( {
		packTree: () => {
			throw failure;
		}
	} );
	await assert.rejects( buildSroResources( steps, { laneCount: 2, log: () => {} } ), failure );
	assert.equal( calls.packTree.length, 1 );
	assert.equal( calls.buildUiImagePreloadManifest.length, 1 );
	assert.equal( calls.produceAllFamilies, undefined );
	assert.equal( calls.buildTextureVariants, undefined );
	assert.equal( calls.buildBackgroundInstallAsset, undefined );
});
