/*
===========================================================================

resourceBuild.mjs - the full browser asset build's orchestration

buildSroResources turns the extracted retail client into
.generated/client-public. Shared prerequisites (converted source images)
come first; independent lanes then run concurrently; the pack tail
(packPublicTree.mjs) runs strictly in order at the end. Every producer is a
member of one plain steps struct, so tests run this exact orchestration
with recording stubs. scripts/build_sro_resources.mjs is the CLI: the
generated-assets lock, the fingerprint gate, the Python preflight and the
summary it prints from formatResourceBuildSummary.

===========================================================================
*/

import { buildAudioResources } from "./audio.mjs";
import { buildCifResources } from "./cif.mjs";
import { buildConfigResources } from "./config.mjs";
import { buildFontResources } from "./fonts.mjs";
import { buildLauncherResources, copyLauncherAssets } from "./launcher.mjs";
import { buildTextResources } from "./text.mjs";
import { buildTitleResources } from "./title.mjs";
import { buildUiImagePreloadManifest } from "./uiImagePreload.mjs";
import { buildBackgroundInstallAsset } from "./data/buildBackgroundInstallAsset.mjs";
import { buildCrowdVatAssets } from "./char/buildCrowdVatAssets.mjs";
import { buildDropModelAssets } from "./char/buildDropModelAssets.mjs";
import { buildLocomotionBanAssets } from "./char/buildLocomotionBanAssets.mjs";
import { buildNpcModelAssets } from "./char/buildNpcModelAssets.mjs";
import { buildNpcVatAssets } from "./char/buildNpcVatAssets.mjs";
import { buildRoster } from "./char/buildRoster.mjs";
import { buildSkillEffectRecordsAsset } from "./char/buildSkillEffectRecords.mjs";
import { buildSkillStageModelAssets } from "./char/buildSkillStageModelAssets.mjs";
import { buildEffectProgramsAsset } from "./effects/buildEffectPrograms.mjs";
import { buildLevelDataAsset } from "./char/buildLevelDataAsset.mjs";
import { buildActionWndDataAsset } from "./data/buildActionWndDataAsset.mjs";
import { buildTeleportDataAsset } from "./data/buildTeleportDataAsset.mjs";
import { buildSkillMasteryDataAsset } from "./data/buildSkillMasteryDataAsset.mjs";
import { buildSkillDataAsset } from "./data/buildSkillDataAsset.mjs";
import { buildQuestDataAsset } from "./data/buildQuestDataAsset.mjs";
import { buildCharacterDataCountryAsset } from "./data/buildCharacterDataCountryAsset.mjs";
import { buildCosPresentationAsset } from "./data/buildCosPresentationAsset.mjs";
import { buildGInterfaceSectionsAsset } from "./data/buildGInterfaceSectionsAsset.mjs";
import { buildSiegeFortressDataAsset } from "./data/buildSiegeFortressDataAsset.mjs";
import { buildMissionPresentationAsset } from "./data/buildMissionPresentationAsset.mjs";
import { buildNameFilterAsset } from "./data/buildNameFilterAsset.mjs";
import { buildStallNetworkAssets } from "./data/buildStallNetworkAssets.mjs";
import { runPython } from "./shared/pythonRun.mjs";
import { runConvertImages } from "./shared/convertImagesRunner.mjs";
import { buildNativeLensResources } from "./shared/nativeLensResources.mjs";
import { buildNativeCharacterTextures } from "./shared/nativeCharacterTextures.mjs";
import { mapWithConcurrency } from "./shared/asyncUtils.mjs";
import {
	buildCharacterSelectInterfaceModels,
	buildCharacterSelectLizard,
	buildAuthoredWorldRegionResources,
	buildCharacterCreateWorldRegionResources,
	buildCharacterSelectWorldRegionResources,
	copyMissionMinimapTileImages,
	buildDungeonResourceManifest,
	buildSharedWorldEnvironmentCatalog,
	buildTitleWorldRegionResources,
	buildWorldAnimatedObjects,
	buildWorldRegionCatalog,
	loadOutdoorWorldRegionResourceGroup
} from "./world/index.mjs";
import { rebuildRoot } from "./world/paths.mjs";

import { formatOptimizationSummary } from "./jsonAssetCompression.mjs";
import { packPublicTree } from "./packPublicTree.mjs";
import { buildJobs } from "./shared/buildParallelism.mjs";

const RETAIL_CURSOR_IDS = [ "0x95", "0x96", "0x97", "0x98", "0x99", "0x9a", "0xa0", "0xa1", "0xa3" ];
/*
================
extractRetailCursors

Extract cursor resources through the shared retail source paths.
================
*/
async function extractRetailCursors() {
	const result = await runPython(
		[ "scripts/extract_client_cursors.py", ...RETAIL_CURSOR_IDS ],
		{
			task: "[cursors] embedded retail cursor extraction",
			cwd: rebuildRoot,
			context: [
				"Browser cursor paths are client presentation resources and must be published before pack indexing."
			]
		}
	);
	const output = result.stdout.trim();
	if ( output ) console.log( output );
	return { count: RETAIL_CURSOR_IDS.length };
}

export const RESOURCE_BUILD_STEPS = Object.freeze( {
	buildAudioResources,
	buildCifResources,
	buildConfigResources,
	buildFontResources,
	buildLauncherResources,
	copyLauncherAssets,
	buildTextResources,
	buildTitleResources,
	buildUiImagePreloadManifest,
	buildBackgroundInstallAsset,
	buildCrowdVatAssets,
	buildDropModelAssets,
	buildLocomotionBanAssets,
	buildNpcModelAssets,
	buildNpcVatAssets,
	buildRoster,
	buildSkillEffectRecordsAsset,
	buildSkillStageModelAssets,
	buildEffectProgramsAsset,
	buildLevelDataAsset,
	buildActionWndDataAsset,
	buildTeleportDataAsset,
	buildSkillMasteryDataAsset,
	buildSkillDataAsset,
	buildQuestDataAsset,
	buildCharacterDataCountryAsset,
	buildCosPresentationAsset,
	buildGInterfaceSectionsAsset,
	buildSiegeFortressDataAsset,
	buildMissionPresentationAsset,
	buildNameFilterAsset,
	buildStallNetworkAssets,
	runConvertImages,
	buildNativeLensResources,
	buildNativeCharacterTextures,
	buildCharacterSelectInterfaceModels,
	buildCharacterSelectLizard,
	buildAuthoredWorldRegionResources,
	buildCharacterCreateWorldRegionResources,
	buildCharacterSelectWorldRegionResources,
	copyMissionMinimapTileImages,
	buildDungeonResourceManifest,
	buildSharedWorldEnvironmentCatalog,
	buildTitleWorldRegionResources,
	buildWorldAnimatedObjects,
	buildWorldRegionCatalog,
	loadOutdoorWorldRegionResourceGroup,
	extractRetailCursors,
	packTree: packPublicTree
} );

/*
================
buildSroResources

Runs every producer in dependency order and returns their results.
options.laneCount overrides SRO_BUILD_JOBS (shared/buildParallelism.mjs); options.log receives
progress lines (console.log by default).
================
*/
export async function buildSroResources( steps = RESOURCE_BUILD_STEPS, options = {} ) {
	const log = options.log ?? console.log;
	const stepTimings = [];
	const timed = async ( label, task ) => {
		const startedAt = performance.now();
		log( `[resource-build] ${label}: start` );
		try {
			const result = await task();
			const seconds = (performance.now() - startedAt) / 1000;
			stepTimings.push( [ label, seconds ] );
			log( `[resource-build] ${label}: done (${seconds.toFixed( 1 )}s)` );
			return result;
		} catch ( error ) {
			const seconds = (performance.now() - startedAt) / 1000;
			console.error( `[resource-build] ${label}: failed after ${seconds.toFixed( 1 )}s` );
			throw error;
		}
	};
	const resourceLaneCount = options.laneCount ?? buildJobs();
	const runResourceTasks = ( tasks ) => mapWithConcurrency( tasks, resourceLaneCount, ( task ) => task() );
	log( `[resource-build] worker lanes: ${resourceLaneCount}` );

	// `pnpm assets compact` deliberately removes rebuild/assets,
	// including every converted DDJ/TGA staging image. A full resource build must
	// therefore recreate that cache from the extracted PK2 inputs before any lane
	// tries to publish terrain, UI, model or effect textures. The converter is
	// incremental, so an ordinary non-compacted rebuild only scans and skips fresh
	// outputs; convertImagesRunner suppresses the narrower duplicate passes below.
	await timed( "nativeLensResources", () => steps.buildNativeLensResources() );
	await timed( "nativeCharacterTextures", () => steps.buildNativeCharacterTextures() );
	const sourceImages = await timed( "sourceImages", () => steps.runConvertImages( [] ) );
	if ( sourceImages.status !== 0 ) {
		throw new Error( `Source image conversion failed with exit status ${sourceImages.status}.` );
	}

	// The early build fans out into parallel lanes. Each lane is strictly ordered
	// internally; two lanes may only overlap because their filesystem inputs and
	// outputs are provably disjoint:
	//   - interface-images lane: assets/cif/** + all of the pipeline's writers into
	//     assets/images/Media_extracted/** (cif layout images -> .../interface,
	//     launcher skins -> .../launcher{,_europe}, minimap tiles -> .../minimap{,_d}),
	//     serialized in one lane so no two of them can ever race a shared target.
	//   - text lane: assets/text/**, the text-derived assets/data/*.json files,
	//     assets/launcher/manifest.json (consumes the in-memory textCatalog) and
	//     assets/config/**.
	//   - fonts lane: assets/fonts/** only (async Python subprocesses).
	//   - audio lane: assets/audio/** only.
	//   - title-character lane: the title/local-player roster GLBs + dress/weapon
	//     GLBs, raw BANs + exact manifest control-clip data, pose/skin fixtures, item-drop
	//     GLBs, and title VATs. Roster publication is the dependency root: VATs
	//     read its GLBs/manifest, while the other producers may fan out after it.
	//   - world lane: assets/title/**, assets/world/**, assets/character-select/**,
	//     assets/images/Map_extracted/** and the converted-texture source tree
	//     rebuild/assets/images/Data_extracted/** (convert_images.py; disjoint from
	//     the Media_extracted subtree the interface-images lane reads). Kept strictly
	//     sequential internally: the region bundles share Map_extracted image
	//     targets, and the animated world objects step scans every
	//     assets/world/<area>/region-*.json bundle written above it.
	// The shared file-hash cache (.state/file-hash-cache.json) is only opened by
	// buildAssetPacks and buildWebAssetManifest, which stay strictly sequential in
	// the tail below, so its whole-file save() can never interleave.
	const interfaceImagesLane = async () => {
		const cifResult = await timed( "cif", () => steps.buildCifResources() );
		const copiedLauncherImages = await timed( "launcherAssets", () => steps.copyLauncherAssets() );
		const missionMinimapTiles = await timed( "minimapTiles", () => steps.copyMissionMinimapTileImages() );
		return { cifResult, copiedLauncherImages, missionMinimapTiles };
	};

	const textLane = async () => {
		const textResources = await timed( "text", () => steps.buildTextResources() );
		const launcherManifest = await timed(
			"launcher",
			() => steps.buildLauncherResources( textResources.textCatalog )
		);
		await timed( "config", () => steps.buildConfigResources() );
		return { ...textResources, launcherManifest };
	};

	const fontsLane = () => timed( "fonts", () => steps.buildFontResources() );
	const audioLane = () => timed( "audio", () => steps.buildAudioResources() );
	const cursorAssetsLane = () => timed( "cursors", () => steps.extractRetailCursors() );
	const titleCharacterAssetsLane = async () => {
		// This is the one broad prim/mtrl image conversion. NPC and item-drop model
		// builders run with texture conversion disabled after it to avoid rewriting
		// image-manifest.csv or repeating the same Python scan.
		const roster = await timed( "titleRoster", () => steps.buildRoster() );
		const [vat, locomotion, itemDrop] = await runResourceTasks( [
			() => timed( "crowdVat", () => steps.buildCrowdVatAssets() ),
			() => timed( "locomotionBan", () => steps.buildLocomotionBanAssets() ),
			() => timed( "itemDropModels", () => steps.buildDropModelAssets( { skipTextures: true } ) )
		] );
		return { roster, vat, locomotion, itemDrop };
	};
	// Mission NPC models and their VAT payloads are one dependency chain. The
	// model compiler owns the native state-id whitelist and BSR sound cursors;
	// VAT must consume the manifest it just wrote. Keeping only the VAT step in
	// this graph previously let builder changes leave stale stand/walk/run-only
	// GLBs in a nominally successful full resource build.
	const npcAssetsLane = async () => {
		const models = await timed( "npcModels", () => steps.buildNpcModelAssets( { skipTextures: true } ) );
		const vat = await timed( "npcVat", () => steps.buildNpcVatAssets() );
		return { models, vat };
	};

	const worldLane = async () => {
		const titleResources = await timed( "title", () => steps.buildTitleResources() );
		const titleManifest = titleResources.primary;
		const worldEnvironmentCatalog = await timed(
			"worldEnvironment",
			() => steps.buildSharedWorldEnvironmentCatalog()
		);
		const titleWorldRegions = [];
		for ( const manifest of titleResources.manifests ) {
			titleWorldRegions.push(
				await timed(
					`titleWorldRegion:${manifest.area}`,
					() => steps.buildTitleWorldRegionResources( manifest )
				)
			);
		}
		const titleWorldRegion =
			titleWorldRegions.find( ( region ) => region.bundle.source.area === titleManifest.area ) ??
				titleWorldRegions[0];
		if ( !titleWorldRegion ) {
			throw new Error( "No title world regions were built" );
		}
		const characterSelectWorldRegion = await timed(
			"charSelectWorldRegion",
			() => steps.buildCharacterSelectWorldRegionResources()
		);
		const characterCreateWorldRegions = await timed(
			"charCreateWorldRegions",
			() => steps.buildCharacterCreateWorldRegionResources()
		);
		const authoredWorldRegion = await timed(
			"authoredWorldRegion",
			() => steps.buildAuthoredWorldRegionResources()
		);
		const allowDevOutdoorRouting = process.env.SRO_ALLOW_DEV_OUTDOOR_ROUTING === "1";
		const outdoorWorldRegion = await timed( "outdoorWorldRegion", () =>
			steps.loadOutdoorWorldRegionResourceGroup( {
				allowDevOnDemand: allowDevOutdoorRouting
			} ) );
		if ( outdoorWorldRegion?.incompleteBundleCount > 0 ) {
			console.warn(
				`[world:outdoor] packaging a dev-on-demand routing index with ` +
					`${outdoorWorldRegion.incompleteBundleCount} missing bundle(s); production builds must not set ` +
					`SRO_ALLOW_DEV_OUTDOOR_ROUTING.`
			);
		}
		const worldRegionCatalog = await timed( "worldRegionCatalog", () =>
			steps.buildWorldRegionCatalog( [
				...titleWorldRegions.map( ( region ) => ({
					...region,
					sourceName: region.bundle.source.area === titleManifest.area ?
						"title" :
						`title-${region.bundle.source.area}`
				}) ),
				{ ...characterSelectWorldRegion, sourceName: "character-select" },
				...Object.entries( characterCreateWorldRegions ).map( ( [race, result] ) => ({
					...result,
					sourceName: `character-create-${race}`
				}) ),
				authoredWorldRegion,
				...(outdoorWorldRegion ? [ outdoorWorldRegion ] : [])
			] ) );
		const dungeonResources = await timed( "dungeonResources", () => steps.buildDungeonResourceManifest() );
		const characterSelectInterfaceModels = await timed(
			"charSelectInterfaceModels",
			() => steps.buildCharacterSelectInterfaceModels()
		);
		const characterSelectLizard = await timed( "charSelectLizard", () => steps.buildCharacterSelectLizard() );
		// Animated placed world objects (hawk/fish/boats/trees/scenery people): must run
		// AFTER the region bundles above (it scans their BSR resource lists) and BEFORE
		// JSON optimization + asset packs (GLBs sweep into game-models, manifests into
		// game-data).
		const animatedWorldObjects = await timed( "animatedWorldObjects", () => steps.buildWorldAnimatedObjects() );
		return {
			titleResources,
			titleManifest,
			worldEnvironmentCatalog,
			titleWorldRegions,
			titleWorldRegion,
			characterSelectWorldRegion,
			characterCreateWorldRegions,
			authoredWorldRegion,
			outdoorWorldRegion,
			worldRegionCatalog,
			dungeonResources,
			characterSelectInterfaceModels,
			characterSelectLizard,
			animatedWorldObjects
		};
	};

	const [
		interfaceImagesResults,
		textResults,
		fontCatalog,
		audioResult,
		cursorAssets,
		titleCharacterAssets,
		npcAssets,
		worldResults
	] = await runResourceTasks( [
		interfaceImagesLane,
		textLane,
		fontsLane,
		audioLane,
		cursorAssetsLane,
		titleCharacterAssetsLane,
		npcAssetsLane,
		worldLane
	] );
	const titleRosterAssets = titleCharacterAssets.roster;
	const crowdVatAssets = titleCharacterAssets.vat;
	const locomotionAssets = titleCharacterAssets.locomotion;
	const itemDropAssets = titleCharacterAssets.itemDrop;
	const npcModelAssets = npcAssets.models;
	const npcVatAssets = npcAssets.vat;
	const { cifResult, copiedLauncherImages, missionMinimapTiles } = interfaceImagesResults;
	const { textCatalog, zoneNameCatalog, regionCodeCatalog, launcherManifest } = textResults;
	const {
		titleResources,
		titleManifest,
		worldEnvironmentCatalog,
		titleWorldRegions,
		titleWorldRegion,
		characterSelectWorldRegion,
		characterCreateWorldRegions,
		outdoorWorldRegion,
		worldRegionCatalog,
		dungeonResources,
		characterSelectInterfaceModels,
		characterSelectLizard,
		animatedWorldObjects
	} = worldResults;

	// Every lane above has finished publishing before this sweep over
	// assets/images/** runs (native-interface preload membership must see the
	// final image tree).
	const uiImagePreload = await timed( "uiImagePreload", () => steps.buildUiImagePreloadManifest() );
	// WIP skill-effect data plane (f0902c effect-record table -> effectRecords.json,
	// read by the bridge's loadWipSkillEffectRecords at mission init): must run
	// BEFORE JSON optimization + asset packs so the JSON gets compression sidecars
	// and sweeps into the game-data pack. Skips itself when Media_extracted is absent.
	const skillEffectRecords = await timed( "skillEffectRecords", () => steps.buildSkillEffectRecordsAsset() );
	// Every BSR referenced by the folded skilleffectset data plane. This is one
	// generic compiler pass (player and monster projectiles), not a weapon list.
	const skillStageModels = await timed( "skillStageModels", () => steps.buildSkillStageModelAssets() );
	// Authored EasyFX programs selected by those records (plus the native
	// progression-orb family). This must precede JSON optimization and packs.
	const effectPrograms = await steps.buildEffectProgramsAsset();
	// WIP level-data plane (leveldata.txt -> levelData.json, read by the
	// bridge's loadWipLevelData for the 0x30D2 exp/level-up fold's REAL 7e0f20
	// record getter). Same placement rules as the effect records above.
	const levelData = steps.buildLevelDataAsset();
	// WIP action-record data plane (actionwnddata.txt raw rows ->
	// actionwnddata.json, seeded through the REAL sub_80cb00 parse fold into the
	// data_cec870 +0x258 map the CIFAction_OnCreate populate iterates). Same
	// placement rules as the level data above.
	const actionWndData = steps.buildActionWndDataAsset();
	// CIFNPCTalk teleport destination data plane: teleportdata + teleportlink
	// retain their native media-table keys (NPC RefObj id, never runtime gid).
	const teleportData = steps.buildTeleportDataAsset();
	// WIP skill-pane mastery/group data plane (skillmasterydata.txt +
	// skillgroup.txt raw rows -> skillMasteryData.json, decoded by
	// bridge/ui/panes/skillPanePlane.ts for the hand-mirrored CIFSkill pane's
	// mastery tabs, board header and group rows). Same placement rules.
	const skillMasteryData = steps.buildSkillMasteryDataAsset();
	// WIP skill-pane skilldata plane (skilldata_*.txt projected columns ->
	// skillData.json, decoded by bridge/ui/panes/skillPanePlane.ts for the CIFSkill
	// pane's per-row skill slots - the sub_586b40/sub_589050 fill legs). Same
	// placement rules.
	const skillData = await steps.buildSkillDataAsset();
	// WIP quest-pane questdata plane (questdata.txt projected columns + the
	// referenced textquest.txt title entries -> questData.json, decoded by
	// bridge/ui/panes/questPlane.ts for the CIFQuestSlotMain live-row title
	// "%s (%d/%d)" and the sub_5c3840 content-button level art). Same
	// placement rules.
	const questData = steps.buildQuestDataAsset();
	// WIP party-pane chardata country plane (characterdata_*.txt id -> country
	// byte -> characterDataCountry.json, decoded by bridge/ui/panes/partyPanePlane.ts
	// for the sub_81d5b0 kindred race-mark resolve over sub_7efeb0's
	// recordMap1f0 lookup). Same placement rules.
	const characterDataCountry = steps.buildCharacterDataCountryAsset();
	// COS HUD reference fields (icon, max HP, rideable) for the status icon,
	// command bar and info page.
	const cosPresentation = await steps.buildCosPresentationAsset();
	// WIP Toggle*Window plane (ginterface.txt raw section lines ->
	// ginterface-sections.json, decoded once in sectionWindowPlane.ts and driven
	// through the REAL sub_783f80 CreateControlsFromSection deserializer).
	const ginterfaceSections = await steps.buildGInterfaceSectionsAsset();
	// WIP fortress-emblem data plane (siegefortress.txt decoded rows ->
	// siegeFortressData.json: fortress id -> code name + resolved public emblem
	// image, the sub_914ae0 CrestPath128 sprite map twin). Same placement rules.
	const siegeFortressData = steps.buildSiegeFortressDataAsset();
	// Native character-name filter (textdata/abusefilter.txt, copied byte for
	// byte). The game-data pack group claims it, so it must exist before packing.
	const nameFilter = await steps.buildNameFilterAsset();
	// The stall network's category tables, claimed by the same pack group.
	await steps.buildStallNetworkAssets();
	// EnterWorld v2 sends semantic ids only. This client projection is generated
	// after NPC/item builders settle and owns every presentation resource path.
	const missionPresentation = steps.buildMissionPresentationAsset();
	// Every producing lane has finished: list what the client installs in the
	// background after world entry, so it is packed with the other game data.
	const backgroundInstall = await timed( "backgroundInstall", () => steps.buildBackgroundInstallAsset() );
	// The ordered pack tail lives in packPublicTree.mjs, shared with the
	// standalone repack. Only the outdoor gate is per-caller: when the world lane
	// produced no outdoor region group there is nothing to list. A full build
	// owns every sidecar it publishes, so it retires the unowned ones.
	const packed = await steps.packTree( {
		timed,
		retireSidecars: true,
		auditClaims: true,
		groupInputs: {
			uiImagePreloadPaths: uiImagePreload.images.map( ( image ) => image.path ),
			missionMinimapTilePaths: missionMinimapTiles.map( ( tile ) => tile.publicPath ),
			includeOutdoorWorld: Boolean( outdoorWorldRegion )
		}
	} );
	const { jsonOptimization, packGroups, claimAudit, assetPacks, sidecarRetirement, manifest, finalJsonOptimization } =
		packed;
	return {
		stepTimings,
		fontCatalog,
		cifResult,
		textCatalog,
		zoneNameCatalog,
		regionCodeCatalog,
		titleResources,
		titleManifest,
		launcherManifest,
		titleWorldRegion,
		titleWorldRegions,
		characterSelectWorldRegion,
		characterCreateWorldRegions,
		characterSelectInterfaceModels,
		characterSelectLizard,
		animatedWorldObjects,
		worldEnvironmentCatalog,
		worldRegionCatalog,
		outdoorWorldRegion,
		missionMinimapTiles,
		dungeonResources,
		audioResult,
		cursorAssets,
		copiedLauncherImages,
		uiImagePreload,
		titleRosterAssets,
		crowdVatAssets,
		locomotionAssets,
		itemDropAssets,
		npcModelAssets,
		npcVatAssets,
		skillEffectRecords,
		effectPrograms,
		levelData,
		actionWndData,
		teleportData,
		skillMasteryData,
		skillData,
		questData,
		characterDataCountry,
		cosPresentation,
		ginterfaceSections,
		siegeFortressData,
		nameFilter,
		missionPresentation,
		skillStageModels,
		backgroundInstall,
		packGroups,
		claimAudit,
		jsonOptimization,
		assetPacks,
		sidecarRetirement,
		manifest,
		finalJsonOptimization
	};
}

/*
================
formatResourceBuildSummary

The lines the CLI prints after a build, from buildSroResources' results.
================
*/
export function formatResourceBuildSummary( results ) {
	const {
		stepTimings,
		fontCatalog,
		cifResult,
		textCatalog,
		zoneNameCatalog,
		regionCodeCatalog,
		titleResources,
		titleManifest,
		launcherManifest,
		titleWorldRegion,
		titleWorldRegions,
		characterSelectWorldRegion,
		characterCreateWorldRegions,
		characterSelectInterfaceModels,
		characterSelectLizard,
		animatedWorldObjects,
		worldEnvironmentCatalog,
		worldRegionCatalog,
		outdoorWorldRegion,
		missionMinimapTiles,
		dungeonResources,
		audioResult,
		cursorAssets,
		copiedLauncherImages,
		uiImagePreload,
		titleRosterAssets,
		crowdVatAssets,
		locomotionAssets,
		itemDropAssets,
		npcModelAssets,
		npcVatAssets,
		skillEffectRecords,
		effectPrograms,
		levelData,
		actionWndData,
		teleportData,
		skillMasteryData,
		skillData,
		questData,
		characterDataCountry,
		cosPresentation,
		ginterfaceSections,
		siegeFortressData,
		nameFilter,
		backgroundInstall,
		packGroups,
		jsonOptimization,
		assetPacks,
		sidecarRetirement,
		manifest,
		finalJsonOptimization
	} = results;
	const out = [];
	const copiedFontFiles = Object.keys( fontCatalog.packagedFonts ?? fontCatalog.fontsByIndex ).length;

	out.push(
		`Built ${cifResult.layoutsBuilt} CIF layouts, ${Object.keys( textCatalog.entries ).length} text entries, ` +
			`${Object.keys( zoneNameCatalog.entries ).length} zone-name entries, ` +
			`${Object.keys( regionCodeCatalog.entries ).length} region-code entries, ` +
			`${titleResources.manifests.length} title scene manifest(s), ${titleManifest.camera.length} active title camera keys, ${
				Object.keys( launcherManifest.rectsById ).length
			} launcher rects, ` +
			`${titleWorldRegion.terrainSectorCount} title terrain sectors, ` +
			`${titleWorldRegion.placementCount} title world placements, ` +
			`title region index (seed ${titleWorldRegion.seedRegionId}, ${titleWorldRegion.regionCount} regions), ` +
			`title variants [` +
			titleWorldRegions
				.map( ( region ) =>
					`${region.bundle.source.area}: seed ${region.seedRegionId}, ${region.placementCount} placements`
				)
				.join( "; " ) +
			`], ` +
			`${characterSelectWorldRegion.placementCount} char-select stage placements (${characterSelectWorldRegion.terrainSectorCount} sectors), ` +
			`char-select region index (seed ${characterSelectWorldRegion.seedRegionId}, ${characterSelectWorldRegion.regionCount} regions), ` +
			`char-create regions [` +
			Object.entries( characterCreateWorldRegions )
				.map( ( [race, result] ) =>
					`${race}: seed ${result.seedRegionId}, ${result.placementCount} placements`
				)
				.join( "; " ) +
			`], ` +
			`${characterSelectInterfaceModels.propCount} char-select interface props (${characterSelectInterfaceModels.meshCount} meshes, ${characterSelectInterfaceModels.textureCount} textures), ` +
			`animated lizard GLB (${characterSelectLizard.bones} bones, clips=[${characterSelectLizard.clips}], ${characterSelectLizard.bytes} B), ` +
			`${animatedWorldObjects.animatedResourceCount} animated world-object GLB(s) (${animatedWorldObjects.glbBytes} B; ` +
			animatedWorldObjects.areas.map( ( area ) => `${area.area}: ${area.animatedResourceCount}` ).join( ", " ) +
			`), ` +
			`${worldEnvironmentCatalog.environmentCount} sky environments (${worldEnvironmentCatalog.regionNodeCount} region nodes, login env ${worldEnvironmentCatalog.loginEnvKey}), ` +
			`cataloged ${worldRegionCatalog.regionCount} world regions (${worldRegionCatalog.entryCount} coverage entries), ` +
			`${
				outdoorWorldRegion ?
					`${outdoorWorldRegion.regionIndexDescriptor.regions.length} outdoor streaming regions, ` :
					""
			}` +
			`copied/indexed ${missionMinimapTiles.length} mission minimap tile(s), ` +
			`published ${dungeonResources.dungeonCount} dungeon info row(s) / ${dungeonResources.resourceCount} dof payload(s), ` +
			`copied ${cifResult.copiedImages.size} images, ` +
			`copied ${copiedFontFiles} packaged fonts, ` +
			`copied ${Object.keys( audioResult.audioCatalog.musicByName ).length} music tracks, ` +
			`extracted ${cursorAssets.count} embedded cursors, ` +
			`copied ${copiedLauncherImages} launcher images, ` +
			`indexed ${uiImagePreload.imageCount} native UI images for preload, ` +
			`built ${titleRosterAssets.built}/${titleRosterAssets.modelCount} title/local-player roster GLB(s), ` +
			`built/reused ${crowdVatAssets.built}/${crowdVatAssets.reused} title-crowd VAT artifact(s) ` +
			`(${crowdVatAssets.failed} failed, ${crowdVatAssets.assetCount} indexed), ` +
			`published ${locomotionAssets.published} raw BAN file(s) / ${locomotionAssets.clipCount} model-clip link(s), ` +
			`built ${itemDropAssets.built}/${itemDropAssets.modelCount} item-drop GLB(s) ` +
			`(${itemDropAssets.absent} absent, ${itemDropAssets.removed} retired), ` +
			`covered ${npcModelAssets.covered}/${npcModelAssets.modelCount} mission NPC/monster entries ` +
			`with ${npcModelAssets.built} unique GLB artifact(s) (${npcModelAssets.reused} shared), ` +
			`built/reused/shared ${npcVatAssets.built}/${npcVatAssets.reused}/${npcVatAssets.shared} ` +
			`mission-NPC VAT artifact(s) ` +
			`(${npcVatAssets.skipped} skipped, ${npcVatAssets.failed} failed, ${npcVatAssets.assetCount} indexed), ` +
			(skillEffectRecords.written ?
				`published ${skillEffectRecords.records} skill effect record(s), ` :
				`skipped skill effect records (no Media_extracted source), `) +
			(effectPrograms.written ?
				`published ${effectPrograms.effects} EasyFX program(s) / ${effectPrograms.meshes} effect mesh(es), ` :
				`skipped EasyFX programs (no extracted source), `) +
			(levelData.written ?
				`published ${levelData.records} level-data row(s), ` :
				`skipped level data (no Media_extracted source), `) +
			(actionWndData.written ?
				`published ${actionWndData.records} action-record row(s), ` :
				`skipped action-record rows (no Media_extracted source), `) +
			(teleportData.written ?
				`published ${teleportData.teleportRows} teleport + ${teleportData.linkRows} teleport-link row(s), ` :
				`skipped teleport data (no Media_extracted source), `) +
			(skillMasteryData.written ?
				`published ${skillMasteryData.masteryRows} mastery + ${skillMasteryData.groupRows} skill-group row(s), ` :
				`skipped skill-mastery rows (no Media_extracted source), `) +
			(skillData.written ?
				`published ${skillData.rows} projected skilldata row(s), ` :
				`skipped skilldata rows (no Media_extracted source), `) +
			(questData.written ?
				`published ${questData.rows} projected questdata row(s), ` :
				`skipped questdata rows (no Media_extracted source), `) +
			(characterDataCountry.written ?
				`published ${characterDataCountry.rows} chardata country row(s), ` :
				`skipped chardata country rows (no Media_extracted source), `) +
			(ginterfaceSections.written ?
				`published ${ginterfaceSections.sections} ginterface window section(s), ` :
				`skipped ginterface window sections (no Media_extracted source), `) +
			(siegeFortressData.written ?
				`published ${siegeFortressData.rows} siege-fortress emblem row(s), ` :
				`skipped siege-fortress emblem rows (no Media_extracted source), `) +
			(nameFilter.written ?
				`published the ${nameFilter.bytes}-byte name filter, ` :
				`skipped the name filter (no Media_extracted source), `) +
			`indexed ${packGroups.titleCrowdVat.length} title-crowd VAT files for packs, ` +
			`indexed ${packGroups.gameImages.length} non-UI game images for packs, ` +
			`indexed ${packGroups.compressedJson.length} compressed JSON sidecars + ${packGroups.rawJson.length} small raw JSON files for packs, ` +
			`indexed ${
				packGroups.outdoorCompressedJson.length + packGroups.outdoorRawJson.length +
				packGroups.outdoorImages.length
			} on-demand outdoor files for packs, ` +
			`indexed ${packGroups.gameModels.length} GLB models for packs, ` +
			`indexed ${packGroups.gameAudio.length} audio files for packs, ` +
			`listed ${backgroundInstall.combat} combat + ${
				backgroundInstall["world-sounds"]
			} world-sound file(s) for background install, ` +
			`retired ${sidecarRetirement.retired.length} unowned precompressed sidecar(s), ` +
			`and packed ${assetPacks.assetCount} assets into ${assetPacks.packCount} browser asset pack(s) ` +
			`with ${assetPacks.zstdSidecarCount} zstd19/w23 sidecar(s).`
	);
	if ( results.claimAudit ) {
		const audit = results.claimAudit, mib = bytes => (bytes / 1048576).toFixed( 1 );
		out.push(
			`Publication ledger: ${audit.files} packed file(s), ${mib( audit.bytes )} MiB, claimed by no build owner ` +
				`(report only; see .generated/unclaimed-assets.json)` +
				audit.folders.slice( 0, 8 ).map( f => `
  ${f.folder}: ${f.files} file(s), ${mib( f.bytes )} MiB` ).join( "" )
		);
	}
	out.push( formatOptimizationSummary( jsonOptimization ) );
	if ( finalJsonOptimization.minifiedFiles > 0 || finalJsonOptimization.compressionJobs > 0 ) {
		out.push( formatOptimizationSummary( finalJsonOptimization ) );
	}
	out.push(
		`Built browser asset manifest with ${manifest.files.length} files (${manifest.manifestHash.slice( 0, 12 )}).`
	);
	out.push(
		`Step timings (slowest first): ${
			stepTimings
				.sort( ( left, right ) => right[1] - left[1] )
				.filter( ( [, seconds] ) => seconds >= 0.05 )
				.map( ( [label, seconds] ) => `${label} ${seconds.toFixed( 1 )}s` )
				.join( ", " )
		}`
	);
	return out;
}
