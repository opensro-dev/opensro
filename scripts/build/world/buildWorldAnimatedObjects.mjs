// Build animated world-object GLBs + per-area manifests.
//
// Native placed .o2 objects whose BSR carries a skeleton (.bsk) + clips (.ban)
// animate automatically in the client: the compound object resolves its
// "default" animation set and loops it (CRTBranch_FindAnimationSetEntry has an
// explicit "default" fallback). The browser's static thin-instance pipeline
// cannot represent that, so this step bakes each animated BSR into a skinned
// GLB (reusing the character avatar pipeline, like buildCharacterSelectLizard)
// and emits a per-area manifest the runtime uses to route those placements to
// the AnimatedWorldObjectController instead of StaticObjectRenderer.
//
// Outputs:
//   .generated/client-public/assets/world/animated-objects/<slug>.glb   (shared)
//   .generated/client-public/assets/world/<area>/animated-objects.json  (per area)

import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import fs from "node:fs";
import path from "node:path";
import { runConvertImages } from "../shared/convertImagesRunner.mjs";
import { writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { fileURLToPath } from "node:url";
import { assembleAvatar } from "../char/buildAvatar.mjs";
import { avatarToGlb } from "../char/exportGlb.mjs";
import { parseBan, parseCharacterBsr } from "../char/formats.mjs";
import { parseJmxResourceBsr } from "./objects/formats.mjs";
import { dataAssetPath, loadDataAsset, loadMaterialTextures } from "../shared/jmxAssetIO.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";

const isCli = isMainScript( import.meta.url );

const scriptDir = path.dirname( fileURLToPath( import.meta.url ) );
const rebuildRoot = path.resolve( scriptDir, "..", "..", ".." );
const worldAssetsRoot = path.join( CLIENT_PUBLIC_ROOT, "assets", "world" );
const glbOutputDir = path.join( worldAssetsRoot, "animated-objects" );
const GLB_PUBLIC_PREFIX = "/assets/world/animated-objects";

export const ANIMATED_WORLD_OBJECTS_MANIFEST_FORMAT = "sro-animated-world-objects";

/** res/nature/china/hawk.bsr -> nature-china-hawk */
function glbSlugForBsrPath( bsrGamePath ) {
	return bsrGamePath
		.replace( /^res\//i, "" )
		.replace( /\.bsr$/i, "" )
		.replaceAll( "/", "-" )
		.replace( /[^a-z0-9_-]+/gi, "_" )
		.toLowerCase();
}

/**
 * Probe a JMXVBMS 0110 buffer for a plausible skin section at ptr[1]
 * (u32 boneCount, counted bone names). Static world meshes reuse ptr[1] for a
 * different section, so validate the bone-name strings before trusting it.
 */
export function bmsMeshIsSkinned( buffer ) {
	if ( buffer.length < 0x40 ) return false;
	const ptr1 = buffer.readUInt32LE( 0x0c + 4 );
	if ( !ptr1 || ptr1 + 4 > buffer.length ) return false;
	const boneCount = buffer.readUInt32LE( ptr1 );
	if ( boneCount === 0 || boneCount > 96 ) return false;
	let p = ptr1 + 4;
	for ( let i = 0; i < boneCount; i += 1 ) {
		if ( p + 4 > buffer.length ) return false;
		const len = buffer.readUInt32LE( p );
		if ( len === 0 || len > 64 || p + 4 + len > buffer.length ) return false;
		const name = buffer.subarray( p + 4, p + 4 + len ).toString( "latin1" );
		if ( !/^[\x20-\x7e]+$/.test( name ) ) return false;
		p += 4 + len;
	}
	return true;
}

/**
 * Native default-state clip: the "default" animation set's stateId-0 record.
 * Fall back to the set's first state, then filename heuristics (_basic/_stand),
 * then the first clip in the BSR animation list.
 */
export function pickDefaultWorldObjectClip( bsr ) {
	const defaultSet = bsr.animationSets?.find( ( set ) => set.name.toLowerCase() === "default" );
	const state0 = defaultSet?.states.find( ( state ) => state.stateId === 0 && state.animationPath );
	if ( state0 ) return state0.animationPath;
	const firstState = defaultSet?.states.find( ( state ) => state.animationPath );
	if ( firstState ) return firstState.animationPath;
	const names = bsr.animationPaths.map( ( p ) => p.toLowerCase().split( /[\\/]/ ).pop() ?? "" );
	for ( const re of [ /_basic/, /_stand/, /_wait/ ] ) {
		const i = names.findIndex( ( n ) => re.test( n ) );
		if ( i >= 0 ) return bsr.animationPaths[i];
	}
	return bsr.animationPaths[0] ?? null;
}

async function classifyBsr( bsrGamePath ) {
	const buffer = await loadDataAsset( bsrGamePath );
	const bsr = parseCharacterBsr( buffer, bsrGamePath );
	if ( !bsr.skeletonPath || bsr.animationPaths.length === 0 ) return null;

	const resource = parseJmxResourceBsr( buffer, bsrGamePath );
	const skinnedMeshPaths = [];
	const staticMeshPaths = [];
	for ( const meshPath of resource.meshPaths ) {
		const meshBuffer = await loadDataAsset( meshPath );
		(bmsMeshIsSkinned( meshBuffer ) ? skinnedMeshPaths : staticMeshPaths).push( meshPath );
	}
	if ( skinnedMeshPaths.length === 0 ) return null;

	const clipPath = pickDefaultWorldObjectClip( bsr );
	if ( !clipPath ) return null;

	return { bsr, resource, skinnedMeshPaths, staticMeshPaths, clipPath };
}

function listAreaRegionBundles() {
	const areas = new Map();
	if ( !fs.existsSync( worldAssetsRoot ) ) return areas;
	for ( const entry of fs.readdirSync( worldAssetsRoot, { withFileTypes: true } ) ) {
		if ( !entry.isDirectory() || entry.name === "animated-objects" ) continue;
		const areaDir = path.join( worldAssetsRoot, entry.name );
		const bundles = fs
			.readdirSync( areaDir )
			.filter( ( name ) => /^region-[0-9a-f]+\.json$/i.test( name ) )
			.map( ( name ) => path.join( areaDir, name ) );
		if ( bundles.length > 0 ) areas.set( entry.name, bundles );
	}
	return areas;
}

function collectBundleBsrPaths( bundlePath ) {
	const bundle = JSON.parse( fs.readFileSync( bundlePath, "utf8" ) );
	const paths = new Set();
	for ( const resource of bundle.objects?.resources?.bsr ?? [] ) {
		for ( const branch of resource.branches ?? [ resource ] ) {
			if ( branch?.sourcePath ) paths.add( branch.sourcePath );
		}
	}
	return paths;
}

async function convertMissingTextures( missingTexturePaths ) {
	if ( missingTexturePaths.length === 0 ) return { converted: 0, skipped: true };
	// convert_images.py filters are case-insensitive substrings of the
	// Data_extracted-relative path; pass each missing texture path directly.
	const filters = [ ...new Set( missingTexturePaths ) ];
	console.log( `[animated-objects] converting ${filters.length} missing texture(s) ...` );
	const result = await runConvertImages( filters );
	if ( result.status !== 0 ) {
		console.warn( `[animated-objects] texture conversion exited ${result.status}; GLBs may be untextured` );
	}
	return { converted: filters.length, skipped: false };
}

export async function buildWorldAnimatedObjects( { skipTextures = false } = {} ) {
	const areas = listAreaRegionBundles();
	const bsrPathsByArea = new Map();
	const allBsrPaths = new Set();
	for ( const [area, bundlePaths] of areas ) {
		const areaPaths = new Set();
		for ( const bundlePath of bundlePaths ) {
			for ( const bsrPath of collectBundleBsrPaths( bundlePath ) ) areaPaths.add( bsrPath );
		}
		bsrPathsByArea.set( area, areaPaths );
		for ( const bsrPath of areaPaths ) allBsrPaths.add( bsrPath );
	}

	// Classify every referenced BSR once (shared across areas).
	const animatedByBsrPath = new Map();
	for ( const bsrPath of [ ...allBsrPaths ].sort() ) {
		if ( !fs.existsSync( dataAssetPath( bsrPath ) ) ) continue;
		try {
			const classified = await classifyBsr( bsrPath );
			if ( classified ) animatedByBsrPath.set( bsrPath, classified );
		} catch ( error ) {
			console.warn( `[animated-objects] ${bsrPath}: classification failed: ${error?.message ?? error}` );
		}
	}

	// Texture pre-pass: convert only PNGs that are missing on disk.
	const textureSkip = skipTextures || process.env.SRO_SKIP_TEXTURE_CONVERT === "1";
	if ( !textureSkip ) {
		const missing = new Set();
		for ( const { resource } of animatedByBsrPath.values() ) {
			const materials = await loadMaterialTextures( resource.materialPaths, { onWarning: () => {} } );
			for ( const material of materials.values() ) {
				if ( material.texturePath && !fs.existsSync( material.pngPath ) ) missing.add( material.texturePath );
			}
		}
		await convertMissingTextures( [ ...missing ] );
	}

	// Emit one GLB per animated BSR (deduped across areas).
	fs.mkdirSync( glbOutputDir, { recursive: true } );
	const manifestEntryByBsrPath = new Map();
	let glbBytes = 0;
	for ( const [bsrPath, classified] of animatedByBsrPath ) {
		const { bsr, skinnedMeshPaths, staticMeshPaths, clipPath } = classified;
		const slug = glbSlugForBsrPath( bsrPath );
		const glbPath = path.join( glbOutputDir, `${slug}.glb` );
		try {
			const avatar = await assembleAvatar( bsrPath, {
				meshPaths: skinnedMeshPaths,
				noClips: true,
				skipUnboundMeshes: true
			} );
			const clip = parseBan( await loadDataAsset( clipPath ), clipPath );
			avatar.clips = [ { role: "default", path: clipPath, clip } ];
			avatar.clip = clip;
			const glb = avatarToGlb( avatar );
			fs.writeFileSync( glbPath, glb );
			glbBytes += glb.length;
			manifestEntryByBsrPath.set( bsrPath, {
				glbPublicPath: `${GLB_PUBLIC_PREFIX}/${slug}.glb`,
				clipName: "default",
				clipSourcePath: clipPath.replaceAll( "\\", "/" ),
				clipDurationMs: clip.durationMs,
				boneCount: avatar.skeleton.boneCount,
				skinnedMeshPaths,
				staticMeshPaths
			} );
			if ( avatar.skippedMeshes.length > 0 ) {
				// A skinned mesh whose bones the skeleton lacks falls back to static.
				const entry = manifestEntryByBsrPath.get( bsrPath );
				entry.staticMeshPaths = [ ...staticMeshPaths, ...avatar.skippedMeshes.map( ( m ) => m.meshPath ) ];
				entry.skinnedMeshPaths = skinnedMeshPaths.filter(
					( meshPath ) => !avatar.skippedMeshes.some( ( m ) => m.meshPath === meshPath )
				);
				console.warn(
					`[animated-objects] ${bsrPath}: ${avatar.skippedMeshes.length} mesh(es) skipped (unbound bones), kept static`
				);
			}
		} catch ( error ) {
			console.warn(
				`[animated-objects] ${bsrPath}: GLB build failed, placements stay static: ${error?.message ?? error}`
			);
		}
	}

	// Per-area manifests (only entries the area's bundles actually reference).
	const areaSummaries = [];
	for ( const [area, areaPaths] of bsrPathsByArea ) {
		const objects = {};
		for ( const bsrPath of [ ...areaPaths ].sort() ) {
			const entry = manifestEntryByBsrPath.get( bsrPath );
			if ( entry ) objects[bsrPath] = entry;
		}
		const manifestPath = path.join( worldAssetsRoot, area, "animated-objects.json" );
		const manifest = {
			format: ANIMATED_WORLD_OBJECTS_MANIFEST_FORMAT,
			version: 1,
			objects
		};
		writeJsonIfChangedSync( manifestPath, manifest );
		areaSummaries.push( { area, animatedResourceCount: Object.keys( objects ).length } );
	}

	const summary = {
		animatedResourceCount: manifestEntryByBsrPath.size,
		glbBytes,
		areas: areaSummaries
	};
	if ( isCli ) {
		console.log(
			`[animated-objects] OK ${summary.animatedResourceCount} GLB(s), ${glbBytes} B total; ` +
				areaSummaries.map( ( s ) => `${s.area}: ${s.animatedResourceCount}` ).join( ", " )
		);
	}
	return summary;
}

if ( isCli ) {
	await buildWorldAnimatedObjects( { skipTextures: process.argv.slice( 2 ).includes( "--skip-textures" ) } );
}
