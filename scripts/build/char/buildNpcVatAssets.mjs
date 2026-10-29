/*
===========================================================================

buildNpcVatAssets.mjs - VAT payloads for the mission NPC and monster models

The NPC VAT owner: it bakes the vertex-animation textures and decides
which prior VAT reference a model rebuild may keep.

Shares the bake core with buildCrowdVatAssets.mjs but with mission-NPC
settings: the runtime state-id clip whitelist (locomotion + combat/death)
ciCharactorDrawVisualSystem selects), NO stand frame cap (mission NPCs idle at
close range; the title crowd's 2-frame stand cap is a startup-CPU
trade this build-time bake does not need), and native-object-preview
material metadata (the mission import contract; world-unlit and
native-object-preview artifacts are not interchangeable).

Artifacts land beside the models:
  /assets/npc/vat/<model>.vat.json
  /assets/npc/vat/<model>.vat.bin
and each baked model gains a `vat` block in
public/assets/npc/manifest.json (the runtime unlock in AvatarVatLoader /
ciCharactorDrawVisualSystem is phase 2 and reads that block; until then the
artifacts are inert).

Usage (from rebuild/):
  node scripts/build/char/buildNpcVatAssets.mjs

===========================================================================
*/

import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { containedPublicFile } from "../shared/assetPaths.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { sha256Hex } from "../shared/hash.mjs";
import { readJsonOrNullSync } from "../shared/jsonOut.mjs";
import { npcManifestModels } from "../shared/npcManifest.mjs";
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

/*
================
npcVatPublicPaths
================
*/
function npcVatPublicPaths( glbPublicPath ) {
	const normalized = normalizePublicPath( glbPublicPath );
	const rel = normalized.replace( /^\/assets\/npc\//, "" ).replace( /\.glb$/i, "" );
	return {
		manifest: `/assets/npc/vat/${rel}.vat.json`,
		bin: `/assets/npc/vat/${rel}.vat.bin`
	};
}

/*
================
publicFileOrNull

containedPublicFile for a reference read back from a previous manifest:
a path outside /assets (or escaping it) is not an error here, just a
reference that cannot be preserved.
================
*/
function publicFileOrNull( publicRoot, publicPath ) {
	try {
		return containedPublicFile( publicRoot, String( publicPath ?? "" ) );
	} catch {
		return null;
	}
}

/*
================
preserveFreshNpcVatReferences

This file owns VAT generation, while buildNpcModelAssets owns the primary
NPC manifest. A model-only incremental rebuild used to replace that manifest
with a VAT-free base document even when every GLB was byte-identical to the
already baked payload. Runtime then silently changed from the pooled VAT path
to per-entity skeletons until the full resource graph happened to run again.

Preserve a prior enrichment only after proving the complete chain still
matches: public paths, compiler contract, GLB SHA-256, VAT byte metadata and
clip index. The contract half is the bake's own freshness test
(isExistingVatFresh, against the previous manifest's VAT contract); the
rest pins the bytes and the manifest reference. A changed GLB or policy
deliberately drops its stale reference; buildNpcVatAssets is the only
owner allowed to regenerate it.
================
*/
export function preserveFreshNpcVatReferences( models, previousManifest, options = {} ) {
	const publicRoot = options.publicRoot ?? CLIENT_PUBLIC_ROOT;
	const previousVatContract = previousManifest?.vat;
	// A v8 manifest holds joined entries already; the join leaves them as is.
	const previousJoined = npcManifestModels( previousManifest );
	const previousModels = Object.values( previousJoined );
	if ( !previousVatContract || previousModels.length === 0 ) {
		return { contract: null, preserved: 0, stale: 0 };
	}

	const previousByGlb = new Map();
	const ambiguousGlbs = new Set();
	for ( const previous of previousModels ) {
		if ( !previous?.glb || !previous?.vat ) continue;
		const prior = previousByGlb.get( previous.glb );
		if ( prior && JSON.stringify( prior.vat ) !== JSON.stringify( previous.vat ) ) {
			ambiguousGlbs.add( previous.glb );
			continue;
		}
		previousByGlb.set( previous.glb, previous );
	}

	const jsonByPath = new Map();
	const statByPath = new Map();
	const sha256ByPath = new Map();
	const readJson = ( filePath ) => {
		if ( !jsonByPath.has( filePath ) ) jsonByPath.set( filePath, readJsonOrNullSync( filePath ) );
		return jsonByPath.get( filePath );
	};
	const readStat = ( filePath ) => {
		if ( !statByPath.has( filePath ) ) {
			statByPath.set( filePath, fs.existsSync( filePath ) ? fs.statSync( filePath ) : null );
		}
		return statByPath.get( filePath );
	};
	const readSha256 = ( filePath ) => {
		if ( !sha256ByPath.has( filePath ) ) {
			sha256ByPath.set( filePath, sha256Hex( fs.readFileSync( filePath ) ) );
		}
		return sha256ByPath.get( filePath );
	};

	let preserved = 0;
	let stale = 0;
	for ( const model of models ) {
		if ( !model?.glb ) continue;
		if ( ambiguousGlbs.has( model.glb ) ) {
			stale += 1;
			continue;
		}
		const previous = previousJoined[model.codename]?.glb === model.glb ?
			previousJoined[model.codename] :
			previousByGlb.get( model.glb );
		const reference = previous?.vat;
		if ( !reference ) continue;

		const glbPath = publicFileOrNull( publicRoot, model.glb );
		const vatManifestPath = publicFileOrNull( publicRoot, reference.manifest );
		const vatBinPath = publicFileOrNull( publicRoot, reference.bin );
		const glbStat = glbPath ? readStat( glbPath ) : null;
		const vatBinStat = vatBinPath ? readStat( vatBinPath ) : null;
		const vat = vatManifestPath ? readJson( vatManifestPath ) : null;
		const valid = Boolean(
			glbPath && vatBinPath && vat &&
				glbStat?.isFile() && vatBinStat?.isFile() &&
				isExistingVatFresh( vat, vatBinPath, { glbSha256: readSha256( glbPath ) }, previousVatContract ) &&
				vat.source?.glb === model.glb &&
				vat.source?.byteLength === glbStat.size &&
				vat.bin?.path === reference.bin &&
				vat.bin?.sha256 === readSha256( vatBinPath ) &&
				reference.bytes === vat.bin.byteLength &&
				reference.frames === vat.texture?.frameCount &&
				reference.compilerVersion === vat.compilerVersion &&
				reference.materialMode === vat.settings.materialMode &&
				JSON.stringify( reference.clips ?? [] ) === JSON.stringify( Object.keys( vat.clips ?? {} ) )
		);
		if ( !valid ) {
			stale += 1;
			continue;
		}
		model.vat = { ...reference };
		preserved += 1;
	}

	return {
		contract: preserved > 0 ? { ...previousVatContract } : null,
		preserved,
		stale
	};
}

/*
================
buildNpcVatAssets
================
*/
export async function buildNpcVatAssets( options = {} ) {
	const manifestPath = options.manifestPath ?? npcManifestPath;
	// No missingResult: the resource lane's model step writes the manifest
	// immediately before this runs, so a missing file is a failure to raise,
	// not an empty build to report.
	return runVatPipeline( {
		manifestPath,
		settings: NPC_VAT_SETTINGS,
		logTag: "npc-vat",
		// One VAT per baked model: the resources keyed by BSR (npcManifest.mjs)
		// hold each GLB once, and every reference of it joins the VAT. Stall
		// booth monsters (boothModels, #490) animate their stand loop like any
		// other animated model, so they get theirs too.
		getModels: ( npcManifest ) => [
			...Object.values( npcManifest.resources ?? {} ),
			...Object.values( npcManifest.boothModels ?? {} )
		],
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
