/*
===========================================================================

assets.mjs - asset pipeline tasks

`pnpm assets <verb> [<family>]`: prepare and check a licensed client
(prepare, doctor), build (build, build full, build world-outdoor), publish
and refresh pack families, and the compaction and maintenance tools. A
first build is `assets prepare`, `assets doctor`, then `assets build full`.

===========================================================================
*/
import { commandTask, seriesTask } from "./define.mjs";

/*
================
familyTask

Re-publish one pack family into an existing generated tree.
================
*/
/**
 * @param {"refresh"|"publish"} kind @param {string} family @param {string[]} args @param {string} description
 */
function familyTask( kind, family, args, description ) {
	return commandTask( {
		name: `assets:${kind}:${family}`,
		description,
		kind: "assets",
		ci: false,
		requires: [ "licensed-client-extraction", "generated-assets" ],
		timeoutClass: "long",
		command: "node",
		args
	} );
}

// The rows of scripts/build/families/looseFamilies.mjs, run by
// scripts/refresh_asset_family.mjs (assetFamilyTasks.test.mjs keeps the lists equal).
export const REFRESH_FAMILIES = [
	"character-info",
	"effect",
	"entity-bsr",
	"footprint",
	"guide",
	"item-mall",
	"native-audio",
	"native-window",
	"overlay",
	"quick-status",
	"quickslot",
	"restriction-text",
	"return-scroll",
	"slot-effect",
	"world-map-markers"
];

// The publish rows of the same table (kind: "publish"), run by the same runner.
// Monster material variants are published by the full build (group game-models).
export const PUBLISH_FAMILIES = [
	"dungeon-worlds",
	"flares",
	"minimap-coverage",
	"skill-ui",
	"star-rng",
	"weather-assets"
];

export const ASSET_TASKS = [
	...REFRESH_FAMILIES.map( ( family ) =>
		familyTask(
			"refresh",
			family,
			[ "scripts/refresh_asset_family.mjs", family ],
			`Refresh ${family} asset packs`
		)
	),
	...PUBLISH_FAMILIES.map( ( family ) =>
		familyTask(
			"publish",
			family,
			[ "scripts/refresh_asset_family.mjs", family ],
			`Publish ${family} assets outside the full build`
		)
	),
	commandTask( {
		name: "assets:refresh:delivery",
		description: "Regenerate the web manifest and stale precompressed sidecars from the installed packs",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "medium",
		command: "node",
		args: [ "scripts/refresh_asset_delivery.mjs" ]
	} ),
	commandTask( {
		name: "assets:repack",
		description: "Rebuild every asset pack from the published loose tree",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/rebuild_asset_packs_from_public.mjs" ]
	} ),
	commandTask( {
		name: "assets:prepare",
		description: "Extract your licensed v1.150 client (PK2 archives and music) into extracted/",
		kind: "assets",
		ci: false,
		requires: [ "licensed-client" ],
		timeoutClass: "long",
		command: "python",
		args: [ "-B", "scripts/prepare_client_resources.py" ]
	} ),
	commandTask( {
		name: "assets:doctor",
		description: "Check the game root, the extraction and the build tools before building",
		kind: "assets",
		ci: false,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/assets_doctor.mjs" ]
	} ),
	commandTask( {
		name: "assets:build",
		description: "Build the browser projection of licensed SRO resources",
		kind: "assets",
		ci: true,
		requires: [ "licensed-client-extraction" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/build_sro_resources.mjs" ]
	} ),
	commandTask( {
		name: "assets:refresh:fonts",
		description: "Refresh native font asset packs",
		kind: "assets",
		ci: false,
		requires: [ "licensed-client-extraction" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/refresh_native_font_asset_packs.mjs" ]
	} ),
	commandTask( {
		name: "assets:refresh:title-crowd",
		description: "Refresh title-crowd VAT and reconcile startup game-data asset packs",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/refresh_title_crowd_asset_packs.mjs" ]
	} ),
	commandTask( {
		name: "assets:compact",
		description: "Compact SRO assets and drop the generated cache",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/compact_sro_assets.mjs", "--drop-generated-cache" ]
	} ),
	commandTask( {
		name: "assets:build:world-outdoor",
		description: "Build outdoor world resources",
		kind: "assets",
		ci: false,
		requires: [ "licensed-client-extraction" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/build_outdoor_world_resources.mjs" ]
	} ),
	seriesTask( {
		name: "assets:build:full",
		description: "Build the complete browser resource projection, including every outdoor region",
		kind: "assets",
		ci: true,
		requires: [ "licensed-client-extraction" ],
		timeoutClass: "long",
		tasks: [ "assets:build:world-outdoor", "assets:build" ]
	} ),
	commandTask( {
		name: "assets:refresh:outdoor",
		description: "Refresh outdoor world asset packs",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/refresh_outdoor_asset_packs.mjs" ]
	} ),
	commandTask( {
		name: "assets:refresh:world-map",
		description: "Refresh world-map asset packs",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "long",
		command: "node",
		args: [ "scripts/refresh_world_map_asset_packs.mjs" ]
	} ),
	commandTask( {
		name: "assets:gc",
		description: "Soft-archive asset-pack outputs the published index no longer uses (--apply)",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/gc_asset_packs.mjs" ]
	} ),
	commandTask( {
		name: "assets:ledger",
		description: "Report packed assets no build owner claims (read-only; see docs/ASSET_DELIVERY.md)",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/report_publication_ledger.mjs" ]
	} ),
	commandTask( {
		name: "assets:lock",
		description: "Show the generated-asset rebuild lock owner",
		kind: "assets",
		ci: false,
		requires: [],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/rebuildLock.mjs", "--status", "--name", "generated-assets" ]
	} ),
	commandTask( {
		name: "assets:optimize:cif-layouts",
		description: "Compress generated CIF layout data",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "medium",
		command: "node",
		args: [ "scripts/build/cifLayoutCompression.mjs" ]
	} ),
	commandTask( {
		name: "assets:optimize:json",
		description: "Compress generated JSON assets",
		kind: "assets",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "medium",
		command: "node",
		args: [ "scripts/build/jsonAssetCompression.mjs" ]
	} ),
	commandTask( {
		name: "assets:check:compact",
		description: "Validate compacted asset products",
		kind: "check",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "medium",
		command: "node",
		args: [ "scripts/checks/check_compact_assets.mjs" ]
	} ),
	commandTask( {
		name: "assets:check:served",
		description: "Measure the served packs against the 80%-of-PK2 ceiling",
		kind: "check",
		ci: false,
		requires: [ "generated-assets" ],
		timeoutClass: "short",
		command: "node",
		args: [ "scripts/checks/check_served_size.mjs" ]
	} ),
	commandTask( {
		name: "assets:check:integrity",
		description: "Validate generated asset-pack manifests and artifacts",
		kind: "check",
		ci: true,
		requires: [ "generated-assets" ],
		timeoutClass: "medium",
		command: "node",
		args: [ "scripts/checks/check_asset_pack_integrity.mjs" ]
	} )
];
