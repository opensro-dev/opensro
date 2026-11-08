/*
===========================================================================

buildHdTexturePacks.mjs - the HD set's own packs and index

Port-only, not native (#2061). buildHdTextureVariants.mjs publishes the
x2 variants loose under CLIENT_PUBLIC_ROOT/hd/texture-variants; this
packs them behind a separate pack index at hd/packs/manifest.json, using
the one pack builder (assetPacks.mjs) with a second output root.

The separation is the product rule: the default pack index
(assets/packs/manifest.json), the web asset manifest and the service
worker precache are built only from /assets and never name an HD byte,
so a client with the Experimental "HD textures" row off downloads
nothing of this set. A client with the row on learns of the set only
from this index, fetched when the row is switched on. The loose files
stay on disk as the pack reader's loose fallback, exactly as the main
tree's members do.

An HD build that produced no variants leaves no index: absence is the
off state, and the client treats a missing index as an empty set.

===========================================================================
*/
import path from "node:path";
import { CLIENT_PUBLIC_ROOT } from "../../lib/generatedRoot.mjs";
import { buildAssetPacks, listPublicAssetFiles } from "../assetPacks.mjs";

export const HD_TEXTURES_FLAG = "SRO_HD_TEXTURES";
export const HD_PACK_INDEX_PATH = "/hd/packs/manifest.json";
export const HD_PACK_GROUPS = Object.freeze( [
	Object.freeze( { name: "texture-variants-hd-bc7", format: "bc7" } ),
	Object.freeze( { name: "texture-variants-hd-astc", format: "astc" } )
] );

/*
================
hdTexturesEnabled

The HD lane is opt-in (SRO_HD_TEXTURES=1): its first encode of the full
set runs for hours on the GPU, so ordinary builds skip it and ship no HD
tree, while a build that wants the channel asks for it. The default is
the native texture set alone.
================
*/
export function hdTexturesEnabled( env = process.env ) {
	return env[HD_TEXTURES_FLAG] === "1";
}

/*
================
buildHdTexturePacks

Packs the loose HD variants under publicRoot/hd into hd/packs and
returns the pack summary, or { skipped: true } when the tree holds no
variants. publicRoot is the buildAssetPacks fixture override; output
then lands in that tree's hd/packs.
================
*/
export async function buildHdTexturePacks( flags = {} ) {
	const publicRoot = flags.publicRoot ?? CLIENT_PUBLIC_ROOT;
	const groups = [];
	for ( const group of HD_PACK_GROUPS ) {
		const files = await listPublicAssetFiles( {
			publicRoot,
			root: "/hd",
			roots: [ `/hd/texture-variants/${group.format}` ],
			extensions: [ ".texture" ]
		} ).catch( error => {
			// No variants tree at all: the off state, not a broken build.
			if ( error?.code === "ENOENT" ) return [];
			throw error;
		} );
		if ( files.length ) groups.push( { name: group.name, load: "manual", files } );
	}
	if ( !groups.length ) {
		console.log( "HD texture packs: skipped (no HD variants published)" );
		return { skipped: true };
	}
	const summary = await buildAssetPacks( {
		publicRoot,
		outputRoot: path.join( publicRoot, "hd", "packs" ),
		groups,
		hashCachePath: flags.hashCachePath,
		memberCacheRoot: flags.memberCacheRoot
	} );
	console.log(
		`HD texture packs: ${summary.assetCount} variants in ${summary.packCount} packs ` +
			`(${(summary.totalBytes / 1048576).toFixed( 1 )} MB) at ${HD_PACK_INDEX_PATH}`
	);
	return summary;
}
