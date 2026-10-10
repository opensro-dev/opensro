/*
===========================================================================

assetPackGroups.mjs - the one definition of the browser asset pack groups

Which groups exist, their load modes and target sizes, and every sweep and
exclusion rule that keeps an asset family out of the generic game-data and
game-images groups.

Both pack-building entry points route through collectAssetPackGroups():
scripts/build_sro_resources.mjs (the full pipeline tail) and
scripts/rebuild_asset_packs_from_public.mjs (the standalone rebuild). Each
once carried its own copy of the group list, and they drifted: the
mission-npc-vat group was added to the pipeline only, so standalone rebuilds
produced packs without it. assetPackGroupParity.test.mjs pins the group
semantics against a fixture tree.

The only per-caller differences are the inputs: the ui-preload image list,
the mission minimap tile list, and whether the outdoor streaming world was
built (the pipeline skips the outdoor listing when its world lane produced
no outdoor region group; the standalone rebuild packs what is on disk).

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import path from "node:path";
import { listPublicAssetFiles } from "./assetPacks.mjs";
import {
	collectDedicatedModelGroups,
	IMAGE_ASSET_EXTENSIONS,
	imagePackGroup,
	isImageAsset,
	REQUIRED_RUNTIME_TEXT_ASSETS
} from "./assetPackOwnership.mjs";
import { rebuildRoot } from "./world/paths.mjs";

export { IMAGE_ASSET_EXTENSIONS, isImageAsset } from "./assetPackOwnership.mjs";

export const OUTDOOR_WORLD_PACK_TARGET_BYTES = 8 * 1024 * 1024;

// VAT bins are lazy-loaded: the first NPC (or title crowd body) that needs one
// used to pay a ~50MiB default-target pack download. 8MiB chunks (the
// outdoor-world precedent) keep that first hit small while the model count grows.
export const VAT_PACK_TARGET_BYTES = 8 * 1024 * 1024;

const TITLE_CROWD_VAT_PREFIX = "/assets/char/vat/";
const MISSION_NPC_VAT_PREFIX = "/assets/npc/vat/";
const OUTDOOR_WORLD_PREFIX = "/assets/world/outdoor/";
const DEVELOPER_LAB_ANIMATION_CATALOG = "/assets/npc/animation-catalog.json";
const DEVELOPER_LAB_ANIMATION_CATALOG_GZIP = `${DEVELOPER_LAB_ANIMATION_CATALOG}.gz`;

/*
================
isGeneratedWebAssetManifestSidecar
================
*/
function isGeneratedWebAssetManifestSidecar( publicPath ) {
	return /^\/assets\/manifest\.json\.(?:br|gz|zst)$/i.test( publicPath );
}

/*
================
isOutdoorWorldAsset
================
*/
function isOutdoorWorldAsset( publicPath ) {
	return publicPath.toLowerCase().startsWith( OUTDOOR_WORLD_PREFIX );
}

/*
================
collectOutdoorWorldFiles

The outdoor-world group's members: each .json.gz, a raw .json only when no
.json.gz stands for it, and every image under /assets/world/outdoor. The
full build and the outdoor pack refresh both list the group through this.
================
*/
export async function collectOutdoorWorldFiles( publicRoot ) {
	const files = await listPublicAssetFiles( {
		publicRoot,
		roots: [ "/assets/world/outdoor" ],
		extensions: [ ".json", ".gz", ...IMAGE_ASSET_EXTENSIONS ]
	} );
	const compressedJson = files.filter( ( publicPath ) => publicPath.endsWith( ".json.gz" ) );
	const compressedSources = new Set( compressedJson.map( ( publicPath ) => publicPath.slice( 0, -".gz".length ) ) );
	const rawJson = files.filter( ( publicPath ) =>
		publicPath.endsWith( ".json" ) && !compressedSources.has( publicPath )
	);
	const images = files.filter( isImageAsset );
	return { compressedJson, rawJson, images };
}

/*
================
collectAssetPackGroups

Lists the public asset tree and assembles the canonical pack-group array for
buildAssetPacks(). Returns the individual listings alongside `groups` so the
pipeline's build summary can report per-family counts.

options.uiImagePreloadPaths and options.missionMinimapTilePaths are public
paths. options.includeOutdoorWorld (default true) lists /assets/world/outdoor;
the generic sweeps exclude that prefix regardless, so a skipped outdoor lane
never leaks stale outdoor files into game-images or game-data.
options.publicRoot overrides .generated/client-public for fixture trees.
================
*/
export async function collectAssetPackGroups( {
	uiImagePreloadPaths,
	missionMinimapTilePaths,
	includeOutdoorWorld = true,
	publicRoot
} ) {
	const titleCrowdVat = await listPublicAssetFiles( {
		publicRoot,
		roots: [ "/assets/char/vat" ],
		extensions: [ ".bin", ".json" ]
	} );
	const titleCrowdVatPaths = new Set( titleCrowdVat.map( ( publicPath ) => publicPath.toLowerCase() ) );

	// Mission NPC VAT artifacts (buildNpcVatAssets): their own lazy group, kept out of
	// the generic json/data groups exactly like the title crowd VATs.
	const missionNpcVatAll = await listPublicAssetFiles( {
		publicRoot,
		roots: [ "/assets/npc/vat" ],
		extensions: [ ".bin", ".json" ]
	} );
	// Every NPC VAT (including COS VATs owned by mission-cos-models) stays out of the json sweeps.
	const missionNpcVatPaths = new Set( missionNpcVatAll.map( ( publicPath ) => publicPath.toLowerCase() ) );

	// Outdoor streaming world assets pack into their own manual-load group; sweeping
	// them into game-data/game-images breaks the generated-asset membership contract.
	const outdoor = includeOutdoorWorld ?
		await collectOutdoorWorldFiles( publicRoot ) :
		{ compressedJson: [], rawJson: [], images: [] };
	const outdoorCompressedJson = outdoor.compressedJson;
	const outdoorRawJson = outdoor.rawJson;
	const outdoorImages = outdoor.images;

	const allGameImages = (
		await listPublicAssetFiles( {
			publicRoot,
			roots: [ "/assets" ],
			extensions: IMAGE_ASSET_EXTENSIONS,
			exclude: [ ...uiImagePreloadPaths, ...missionMinimapTilePaths ]
		} )
	).filter( ( publicPath ) => !isOutdoorWorldAsset( publicPath ) );
	// These families remain readable on demand, including during title loading.
	// Startup membership preserves cached entries; group byte totals alone
	// do not measure startup network traffic (issue #273).
	// The collector's outdoor exclusion and the classifier's must agree:
	// an outdoor-classified file reaching this partition would silently
	// land in no group otherwise.
	const divergent = allGameImages.filter( file => imagePackGroup( file ) === "outdoor-world" );
	if ( divergent.length ) {
		throw new Error( `Outdoor/classifier exclusion diverged for ${divergent[0]}` );
	}
	const gameImages = allGameImages.filter( file => imagePackGroup( file ) === "game-images" );
	const worldTextureImages = allGameImages.filter( file => imagePackGroup( file ) === "world-textures" );
	const mapTileImages = allGameImages.filter( file => imagePackGroup( file ) === "map-tiles" );
	const uiIconImages = allGameImages.filter( file => imagePackGroup( file ) === "ui-icons" );
	const particleTextureImages = allGameImages.filter( file => imagePackGroup( file ) === "particle-textures" );

	const compressedJson = (
		await listPublicAssetFiles( {
			publicRoot,
			roots: [ "/assets" ],
			extensions: [ ".gz" ]
		} )
	).filter(
		( publicPath ) =>
			publicPath.endsWith( ".json.gz" ) &&
			!publicPath.toLowerCase().startsWith( TITLE_CROWD_VAT_PREFIX ) &&
			!publicPath.toLowerCase().startsWith( MISSION_NPC_VAT_PREFIX ) &&
			!isOutdoorWorldAsset( publicPath ) &&
			publicPath.toLowerCase() !== DEVELOPER_LAB_ANIMATION_CATALOG_GZIP &&
			!isGeneratedWebAssetManifestSidecar( publicPath )
	);
	const compressedJsonSourcePaths = new Set(
		compressedJson.map( ( publicPath ) => publicPath.slice( 0, -".gz".length ) )
	);

	const rawJson = (
		await listPublicAssetFiles( {
			publicRoot,
			roots: [ "/assets" ],
			extensions: [ ".json" ]
		} )
	).filter(
		( publicPath ) =>
			!compressedJsonSourcePaths.has( publicPath ) &&
			!titleCrowdVatPaths.has( publicPath.toLowerCase() ) &&
			!missionNpcVatPaths.has( publicPath.toLowerCase() ) &&
			!isOutdoorWorldAsset( publicPath ) &&
			publicPath.toLowerCase() !== DEVELOPER_LAB_ANIMATION_CATALOG &&
			// The web asset manifest regenerates with a fresh timestamp every build and must be
			// fetched loose anyway (it is how clients discover the packs); packing it forced one
			// game-data pack to rebuild + re-compress on every run.
			publicPath.toLowerCase() !== "/assets/manifest.json"
	);

	const animationData = await listPublicAssetFiles( {
		publicRoot,
		roots: [ "/assets/anim" ],
		extensions: [ ".ban", ".bin" ]
	} );
	const runtimeTextData = (await listPublicAssetFiles( { publicRoot, roots: [ "/assets" ], extensions: [ ".txt" ] } ))
		.filter(
			publicPath => REQUIRED_RUNTIME_TEXT_ASSETS.includes( publicPath.toLowerCase() )
		);

	// Dev-only character labs consume this catalog on demand. Keep it out of
	// startup game-data so retail animation metadata has zero mission boot tax.
	const developerLabData = (
		await listPublicAssetFiles( {
			publicRoot,
			roots: [ "/assets/npc" ],
			extensions: [ ".gz" ]
		} )
	).filter( ( publicPath ) => publicPath.toLowerCase() === DEVELOPER_LAB_ANIMATION_CATALOG_GZIP );

	const allModels = await listPublicAssetFiles( {
		publicRoot,
		roots: [ "/assets" ],
		extensions: [ ".glb" ]
	} );

	// Equipment, Hwan and COS models belong to their dedicated lazy groups
	// (assetPackOwnership.mjs). They must never also land in game-models or
	// mission-npc-vat.
	const dedicated = await collectDedicatedModelGroups(
		path.resolve( publicRoot ?? CLIENT_PUBLIC_ROOT ),
		new Set( [ ...allModels, ...missionNpcVatAll ].map( ( publicPath ) => publicPath.toLowerCase() ) )
	);
	const gameModels = allModels.filter( ( publicPath ) => !dedicated.claimed.has( publicPath.toLowerCase() ) );
	const missionNpcVat = missionNpcVatAll.filter( ( publicPath ) =>
		!dedicated.claimed.has( publicPath.toLowerCase() )
	);

	const gameAudio = await listPublicAssetFiles( {
		publicRoot,
		roots: [ "/assets/audio" ],
		extensions: [ ".mp3", ".wav" ]
	} );

	const groups = [
		{ name: "native-ui", load: "startup", files: [ ...uiImagePreloadPaths ] },
		{ name: "game-images", load: "startup", files: gameImages },
		// Preserve game-images' cache protection for every image family.
		// Startup controls eviction pinning; reads remain on demand.
		{ name: "world-textures", load: "startup", files: worldTextureImages },
		{ name: "map-tiles", load: "startup", files: mapTileImages },
		{ name: "ui-icons", load: "startup", files: uiIconImages },
		{ name: "particle-textures", load: "startup", files: particleTextureImages },
		{
			name: "game-data",
			load: "startup",
			files: [ ...compressedJson, ...rawJson, ...animationData, ...runtimeTextData ]
		},
		{ name: "developer-labs", load: "manual", files: developerLabData },
		{ name: "game-audio", load: "manual", files: gameAudio },
		{ name: "title-crowd-vat", load: "lazy", targetBytes: VAT_PACK_TARGET_BYTES, files: titleCrowdVat },
		{ name: "mission-npc-vat", load: "lazy", targetBytes: VAT_PACK_TARGET_BYTES, files: missionNpcVat },
		{ name: "mission-minimap", load: "lazy", files: [ ...missionMinimapTilePaths ] },
		{ name: "game-models", load: "lazy", files: gameModels },
		...dedicated.groups,
		{
			name: "outdoor-world",
			load: "manual",
			targetBytes: OUTDOOR_WORLD_PACK_TARGET_BYTES,
			files: [ ...outdoorCompressedJson, ...outdoorRawJson, ...outdoorImages ]
		}
	];

	return {
		groups,
		titleCrowdVat,
		missionNpcVat,
		outdoorCompressedJson,
		outdoorRawJson,
		outdoorImages,
		gameImages,
		compressedJson,
		rawJson,
		animationData,
		developerLabData,
		gameModels,
		dedicatedModelGroups: dedicated.groups,
		gameAudio
	};
}
