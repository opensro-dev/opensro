// Build-time VAT payloads for mission NPC/monster models (phase 1 of the
// mission-NPC VAT port).
//
// Shares the bake core with buildCrowdVatAssets.mjs but with mission-NPC
// settings: the runtime state-id clip whitelist (locomotion + combat/death)
// ciCharactorDrawVisualSystem selects), NO stand frame cap (mission NPCs idle at
// close range; the title crowd's 2-frame stand cap is a startup-CPU
// trade this build-time bake does not need), and native-object-preview
// material metadata (the mission import contract; world-unlit and
// native-object-preview artifacts are not interchangeable).
//
// Artifacts land beside the models:
//   /assets/npc/vat/<model>.vat.json
//   /assets/npc/vat/<model>.vat.bin
// and each baked model gains a `vat` block in
// public/assets/npc/manifest.json (the runtime unlock in AvatarVatLoader /
// ciCharactorDrawVisualSystem is phase 2 and reads that block; until then the
// artifacts are inert).
//
// Usage (from rebuild/):
//   node scripts/build/char/buildNpcVatAssets.mjs

import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { isMainScript } from "../shared/fsUtils.mjs";
import {
	bakeVatFromGlb,
	createVatPrecisionCensus,
	formatVatPrecisionCensus,
	isExistingVatFresh,
	normalizePublicPath,
	publicPathToDisk,
	recordVatPrecision,
	reportVatPrecision,
	CROWD_VAT_FORMAT,
	CROWD_VAT_VERSION,
	CROWD_VAT_COMPILER_VERSION
} from "./buildCrowdVatAssets.mjs";
import { runVatPipeline } from "./vatPipeline.mjs";

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", "..", ".." );
const npcManifestPath = path.join( CLIENT_PUBLIC_ROOT, "assets", "npc", "manifest.json" );

// Mirrors CICHARACTOR_DRAW_CLIP_ROLE_WHITELIST (ciCharactorDrawVisualSystem.ts):
// the only roles the mission renderer will ever play, so the only roles worth baking.
export const NPC_VAT_CLIP_ROLES = [
	"stand",
	"walk",
	"attack1",
	"hit1",
	"death",
	"attack2",
	"run",
	"hit2",
	"stand02",
	"attack3",
	"attack4",
	"deathLoop",
	"down",
	"downwait",
	"downdamage",
	"wakeup",
	"downdie",
	"emote0"
];

const NPC_VAT_SETTINGS = {
	format: CROWD_VAT_FORMAT,
	version: CROWD_VAT_VERSION,
	compilerVersion: CROWD_VAT_COMPILER_VERSION,
	clipRoles: NPC_VAT_CLIP_ROLES,
	standFrameCap: null,
	// A model whose baked clips are all outside the whitelist gets no VAT at
	// all (skipped loudly below) instead of an unrelated clip nothing plays.
	materialMode: "native-object-preview"
};

function npcVatPublicPaths( glbPublicPath ) {
	const normalized = normalizePublicPath( glbPublicPath );
	const rel = normalized.replace( /^\/assets\/npc\//, "" ).replace( /\.glb$/i, "" );
	return {
		manifest: `/assets/npc/vat/${rel}.vat.json`,
		bin: `/assets/npc/vat/${rel}.vat.bin`
	};
}

export async function buildNpcVatAssets( options = {} ) {
	const manifestPath = options.manifestPath ?? npcManifestPath;
	return runVatPipeline( {
		manifestPath,
		missingResult: { built: 0, reused: 0, shared: 0, skipped: 0, failed: 0, assetCount: 0 },
		settings: NPC_VAT_SETTINGS,
		logTag: "npc-vat",
		// models is a Record keyed by codename (buildNpcModelAssets.mjs), not an array.
		getModels: ( npcManifest ) => Object.values( npcManifest.models ?? {} ),
		classifyModel: ( model ) => {
			if ( !model?.glb ) return "ignore";
			return Array.isArray( model.clips ) &&
					model.clips.some( ( clip ) => NPC_VAT_CLIP_ROLES.includes( clip ) ) ?
				"include" :
				"skip";
		},
		vatPublicPathsForGlb: npcVatPublicPaths,
		publicPathToDisk,
		bakeVatFromGlb,
		isExistingVatFresh,
		createVatPrecisionCensus,
		reportVatPrecision,
		recordVatPrecision,
		createVatReference: ( vatManifest, vatPublic ) => ({
			manifest: vatPublic.manifest,
			bin: vatPublic.bin,
			bytes: vatManifest.bin.byteLength,
			frames: vatManifest.texture.frameCount,
			clips: Object.keys( vatManifest.clips ),
			compilerVersion: vatManifest.compilerVersion,
			materialMode: vatManifest.settings.materialMode
		}),
		updateManifest: ( npcManifest ) => {
			npcManifest.vat = {
				format: NPC_VAT_SETTINGS.format,
				version: NPC_VAT_SETTINGS.version,
				compilerVersion: NPC_VAT_SETTINGS.compilerVersion,
				clipRoles: NPC_VAT_CLIP_ROLES,
				materialMode: NPC_VAT_SETTINGS.materialMode
			};
		},
		shareByGlb: true,
		skipMissingGlb: true,
		skipEmptyBake: true,
		refreshSidecars: true,
		cleanupRoot: path.join( path.dirname( manifestPath ), "vat" )
	} );
}

if ( isMainScript( import.meta.url ) ) {
	const result = await buildNpcVatAssets();
	console.log(
		`[npc-vat] built ${result.built}, reused ${result.reused}, shared ${result.shared}, ` +
			`skipped ${result.skipped}, ` +
			`failed ${result.failed}, indexed ${result.assetCount}`
	);
	console.log( `[npc-vat] precision census: ${formatVatPrecisionCensus( result.precision )}` );
}
