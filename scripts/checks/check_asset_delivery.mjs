/*
===========================================================================

check_asset_delivery.mjs - the published asset packs deliver what they claim

Validates the pack index's delivery metadata (version 2: no transport copy
of any member, every world animation catalog indexed), the packed font
atlases and the runtime effect catalogs (every per-effect program and mesh
file among them), and proves each animation index
matches the catalog it was derived from.

Stored members are proven lossless by check_asset_pack_integrity.mjs, which
decodes every member and checks its length and SHA-256.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../lib/generatedRoot.mjs";
import { isAnimationCatalog, validateAssetDelivery } from "../build/assetDelivery.mjs";
import { validatePackedFontAtlases } from "../build/assetPackPublication.mjs";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { gunzipSync } from "node:zlib";
import { readPublishedAssetBytesSync } from "../lib/publishedAsset.mjs";
import {
	EFFECT_PROGRAM_INDEX,
	effectProgramIndexFiles
} from "../../apps/client-next/src/engine/foundation/assets/effect-programs.ts";

const publicRoot = CLIENT_PUBLIC_ROOT;
const RUNTIME_EFFECT_CATALOGS = [
	"/assets/skill/effectRecords.json",
	"/assets/skill/namedEffectRecords.json",
	EFFECT_PROGRAM_INDEX
];

const index = JSON.parse( await readFile( path.join( publicRoot, "assets/packs/manifest.json" ), "utf8" ) );
validateAssetDelivery( index );
await validatePackedFontAtlases( index, publicRoot );
// Effect records are runtime inputs, including the named table loaded on the
// first item/cure event. Loose files are not evidence of worker delivery.
for ( const catalog of RUNTIME_EFFECT_CATALOGS ) {
	if ( !index.assets.some( e => e.path === catalog || e.path === catalog + ".gz" ) ) {
		throw Error( "Missing runtime effect catalog in asset publication: " + catalog );
	}
}
// The worker reads one effect file and its meshes per effect (#3951): every
// file the effect index names must be a published member.
const published = new Set( index.assets.map( e => e.path.toLowerCase() ) );
const effectIndex = JSON.parse( readPublishedAssetBytesSync( EFFECT_PROGRAM_INDEX, publicRoot ).toString( "utf8" ) );
for ( const file of effectProgramIndexFiles( effectIndex ) ) {
	if ( !published.has( file.toLowerCase() ) ) {
		throw Error( "Missing effect program file in asset publication: " + file );
	}
}

let animationManifests = 0, storedMembers = 0, identityBytes = 0, storedBytes = 0;
for ( const e of index.assets ) {
	identityBytes += e.length;
	storedBytes += e.stored ? e.stored.length : e.length;
	if ( e.stored ) storedMembers++;
	if ( isAnimationCatalog( e.path ) ) {
		let bytes = readPublishedAssetBytesSync( e.path, publicRoot );
		if ( e.path.endsWith( ".gz" ) ) bytes = gunzipSync( bytes );
		const actual = Object.keys( JSON.parse( bytes.toString( "utf8" ) ).objects ).sort();
		if ( e.animationDigest !== e.sha256 || JSON.stringify( actual ) !== JSON.stringify( e.animationSources ) ) {
			throw Error( "Animation index drift: " + e.path );
		}
		animationManifests++;
	}
}

console.log(
	JSON.stringify( {
		assets: index.assets.length,
		storedMembers,
		identityBytes,
		storedBytes,
		animationManifests
	} )
);
