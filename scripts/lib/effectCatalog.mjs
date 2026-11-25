/*
===========================================================================

effectCatalog.mjs - the published EasyFX effect programs, read from Node

Since #3951 the build publishes one file per effect and one per BMS mesh
(scripts/build/effects/buildEffectPrograms.mjs), named by the client's
foundation/assets/effect-programs.ts, plus programs/index.json listing the
closure. The runtime reads one effect at a time; audits, tests and build
steps that walk the whole closure read it here, through the published-asset
reader (loose file, else its pack member), so no caller spells the layout.

effectProgramFromCatalog is the one adapter from the whole-catalogue shape
(what readEffectCatalogSync returns, and what small test fixtures write by
hand) to the single-effect input the worker's compiler takes.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "./generatedRoot.mjs";
import { readPublishedAssetBytesSync, readPublishedAssetJsonSync } from "./publishedAsset.mjs";
import {
	effectProgramHeader,
	EFFECT_PROGRAM_INDEX,
	effectMeshFile,
	effectProgramFile,
	effectProgramKey
} from "../../apps/client-next/src/engine/foundation/assets/effect-programs.ts";

const catalogCache = new Map();

/*
================
readEffectIndexSync

The closure record: effect keys, mesh keys, textures, reachability,
aliases, missing and archive-absent effects.
================
*/
export function readEffectIndexSync( publicRoot = CLIENT_PUBLIC_ROOT ) {
	const index = readPublishedAssetJsonSync( EFFECT_PROGRAM_INDEX, publicRoot );
	if ( index.format !== effectProgramHeader().format || index.version !== effectProgramHeader().version ) {
		throw new Error( "Unsupported effect program index" );
	}
	return index;
}

/*
================
readEffectProgramSync

One effect's published file (its bytes, for the worker's own parser, and
its value) and the meshes it names, as the worker reads them.
================
*/
export function readEffectProgramSync( path, publicRoot = CLIENT_PUBLIC_ROOT ) {
	const bytes = readPublishedAssetBytesSync( effectProgramFile( path ), publicRoot );
	const file = JSON.parse( bytes.toString( "utf8" ) );
	const meshes = {};
	for ( const key of file.meshes ?? [] ) {
		meshes[key] = readPublishedAssetJsonSync( effectMeshFile( key ), publicRoot );
	}
	return { bytes, file, meshes };
}

/*
================
readEffectCatalogSync

The whole closure in the catalogue shape: framesPerSecond, effects by key,
meshes by key, textures, nativeUnavailable, missingEffects,
ambiguousEffectAliases and reachability. Read once per public root.
================
*/
export function readEffectCatalogSync( publicRoot = CLIENT_PUBLIC_ROOT ) {
	const cached = catalogCache.get( publicRoot );
	if ( cached ) return cached;
	const index = readEffectIndexSync( publicRoot );
	const effects = {};
	for ( const key of index.effects ) {
		effects[key] = readPublishedAssetJsonSync( effectProgramFile( key ), publicRoot ).effect;
	}
	const meshes = {};
	for ( const key of index.meshes ) meshes[key] = readPublishedAssetJsonSync( effectMeshFile( key ), publicRoot );
	const catalog = {
		framesPerSecond: index.framesPerSecond,
		reachability: index.reachability,
		effects,
		ambiguousEffectAliases: index.ambiguousEffectAliases,
		missingEffects: index.missingEffects,
		nativeUnavailable: index.nativeUnavailable,
		meshes,
		textures: index.textures
	};
	catalogCache.set( publicRoot, catalog );
	return catalog;
}

/*
================
effectProgramFromCatalog

One effect of a catalogue-shaped value as the compiler's input: { file,
meshes }, with file in the published per-effect layout (bytes encodes it
for the worker's parser). An archive-absent key gives the unavailable file;
an unknown key gives a file without an effect, which the compiler refuses.
================
*/
export function effectProgramFromCatalog( catalog, path ) {
	const key = effectProgramKey( path );
	const header = {
		...effectProgramHeader(),
		framesPerSecond: catalog.framesPerSecond ?? effectProgramHeader().framesPerSecond
	};
	const unavailable = catalog.nativeUnavailable;
	if ( !catalog.effects?.[key] && unavailable?.effects?.includes( key ) ) {
		const file = { ...header, unavailable: { archiveSha256: unavailable.archiveSha256 } };
		return { file, meshes: {}, bytes: encode( file ) };
	}
	const file = {
		...header,
		...(catalog.effects?.[key] ? { effect: catalog.effects[key] } : {}),
		meshes: Object.keys( catalog.meshes ?? {} ),
		textures: catalog.textures ?? {}
	};
	return { file, meshes: catalog.meshes ?? {}, bytes: encode( file ) };
}

/*
================
encode
================
*/
function encode( value ) {
	return new TextEncoder().encode( JSON.stringify( value ) );
}
