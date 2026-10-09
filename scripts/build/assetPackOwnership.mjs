/*
===========================================================================

assetPackOwnership.mjs - image and dedicated model pack ownership

The full pack build (assetPackGroups.mjs) and the generated-asset tests
derive the dedicated model groups from this module alone. Membership comes
from the published authority documents (roster.json, the NPC manifest),
never from directory sweeps: a stale GLB no document references stays in
the generic game-models sweep.

Image paths share one classifier between full and focused builds. Explicit
preload/minimap owners are resolved before this default classification.

Every path has exactly one owning group. The client rejects a whole pack
index that names a path twice; that happened on 2026-09-23 when the full
build swept every .glb into game-models while a focused publisher appended
its own group over the same paths.

===========================================================================
*/
import { readFile } from "node:fs/promises";
import path from "node:path";
import { npcManifestModels } from "./shared/npcManifest.mjs";

// Native texture containers preserve authored mip levels alongside ordinary images.
export const IMAGE_ASSET_EXTENSIONS = [ ".png", ".jpg", ".jpeg", ".dds", ".webp", ".cur", ".texture" ];

/*
================
isImageAsset
================
*/
export function isImageAsset( publicPath ) {
	return IMAGE_ASSET_EXTENSIONS.some( ( extension ) => publicPath.toLowerCase().endsWith( extension ) );
}

/*
================
imagePackGroup

Default owner for an image without an explicit preload or existing owner.
Load mode controls persistent-cache pinning; all groups are read on demand.
================
*/
export function imagePackGroup( publicPath ) {
	const lower = publicPath.toLowerCase();
	if ( lower.startsWith( "/assets/world/outdoor/" ) ) return "outdoor-world";
	if ( lower.startsWith( "/assets/world/" ) ) return "world-textures";
	if ( lower.startsWith( "/assets/images/map_extracted/tile2d/" ) ) return "map-tiles";
	if ( lower.startsWith( "/assets/images/media_extracted/icon/" ) ) return "ui-icons";
	if ( lower.startsWith( "/assets/images/particles_extracted/textures/" ) ) return "particle-textures";
	return "game-images";
}

/*
================
unique
================
*/
function unique( paths ) {
	return [ ...new Set( paths.filter( ( value ) => typeof value === "string" && value ) ) ];
}

/*
================
equipmentModelFiles

Per-item equipment and avatar bodies, default wear and avatar auxiliaries:
everything the item catalog publishes under /assets/char/equipment/.
================
*/
export function equipmentModelFiles( dress ) {
	const entries = [
		...Object.values( dress?.equipment ?? {} ).flatMap( ( row ) =>
			Object.values( row?.bodies ?? {} ).filter( Boolean )
		),
		...Object.values( dress?.defaultWear ?? {} ),
		...Object.values( dress?.avatarAuxiliary ?? {} )
	];
	return unique( entries.map( ( entry ) => entry?.glb ) ).filter( ( glb ) => glb.includes( "/equipment/" ) );
}

/*
================
hwanModelFiles
================
*/
export function hwanModelFiles( dress ) {
	return unique( Object.values( dress?.hwan ?? {} ).map( ( entry ) => entry?.glb ) );
}

/*
================
cosModelFiles

COS (pet and transport) models and their VAT artifacts from the NPC manifest.
================
*/
export function cosModelFiles( npcManifest ) {
	const rows = Object.values( npcManifestModels( npcManifest ) ).filter( ( row ) => row?.kind === "cos" );
	return unique( rows.flatMap( ( row ) => [ row.glb, ...(row.vat ? [ row.vat.manifest, row.vat.bin ] : []) ] ) );
}

// Group name -> member derivation. Order is precedence if two documents
// ever overlap.
export const DEDICATED_MODEL_GROUPS = [
	{ name: "equipment-models", source: "roster", files: ( docs ) => equipmentModelFiles( docs.roster?.dress ) },
	{ name: "hwan-models", source: "roster", files: ( docs ) => hwanModelFiles( docs.roster?.dress ) },
	{ name: "mission-cos-models", source: "npc", files: ( docs ) => cosModelFiles( docs.npc ) }
];

/*
================
readJsonIfPresent
================
*/
async function readJsonIfPresent( filename ) {
	try {
		return JSON.parse( await readFile( filename, "utf8" ) );
	} catch ( error ) {
		if ( error?.code === "ENOENT" ) return undefined;
		throw error;
	}
}

/*
================
collectDedicatedModelGroups

The dedicated groups for a full pack build. `available` is the case-folded
set of files that exist in the listing; a member the documents name but the
tree lacks is left out here and caught by the membership tests.
================
*/
export async function collectDedicatedModelGroups( publicRoot, available ) {
	const docs = {
		roster: await readJsonIfPresent( path.join( publicRoot, "assets", "char", "roster.json" ) ),
		npc: await readJsonIfPresent( path.join( publicRoot, "assets", "npc", "manifest.json" ) )
	};
	const claimed = new Set();
	const groups = [];
	for ( const declaration of DEDICATED_MODEL_GROUPS ) {
		const files = declaration
			.files( docs )
			.filter( ( file ) => available.has( file.toLowerCase() ) && !claimed.has( file.toLowerCase() ) );
		for ( const file of files ) claimed.add( file.toLowerCase() );
		if ( files.length ) groups.push( { name: declaration.name, load: "lazy", files } );
	}
	return { groups, claimed };
}
