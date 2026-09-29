/*
===========================================================================

characterMaterialBindings.mjs - native material sources for published actors

Every character GLB the roster references records the native BSR it was
converted from: a model its `source`, an attachment one `sources` entry per
part (buildItemSetGlb and the private-skeleton builders). This module turns
that provenance into the native material flags and environment modifiers the
oracle test compares the published GLB against. It never re-derives which
items exist, so the published catalog and its oracle cannot drift apart.

===========================================================================
*/
import { parseJmxResourceBsr } from "../world/objects/formats.mjs";
import { loadDataAsset, loadMaterialTextures } from "../shared/jmxAssetIO.mjs";
import { selectItemMaterialPaths } from "./itemAttachments.mjs";

/*
================
publishedCharacterAssets

Every GLB the roster references, with its recorded provenance. Walks the
whole dress section, so a catalog added later is covered without a change
here. A referenced GLB without provenance, or one GLB recorded with two
different sources, is an error.
================
*/
export function publishedCharacterAssets( roster ) {
	const assets = new Map();
	function record( glb, sources ) {
		const previous = assets.get( glb );
		if ( previous && JSON.stringify( previous ) !== JSON.stringify( sources ) ) {
			throw Error( "Conflicting native sources for " + glb );
		}
		assets.set( glb, sources );
	}
	for ( const model of roster.models ) {
		if ( typeof model.source !== "string" ) throw Error( "Unbound published model " + model.glb );
		const sources = { "*": { bsr: model.source } };
		record( model.glb, sources );
		if ( model.previewGlb ) record( model.previewGlb, sources );
	}
	const pending = [ roster.dress ];
	while ( pending.length ) {
		const node = pending.pop();
		if ( !node || typeof node !== "object" ) continue;
		if ( typeof node.glb === "string" ) {
			if ( !node.sources || typeof node.sources !== "object" ) {
				throw Error( "Unbound published item " + node.glb );
			}
			const sources = {};
			for ( const [part, source] of Object.entries( node.sources ) ) sources["part:" + part] = source;
			record( node.glb, sources );
			continue;
		}
		for ( const value of Object.values( node ) ) pending.push( value );
	}
	return assets;
}

/*
================
characterMaterialBindings

Resolves each published asset's recorded sources to native materials:
Map<glb, Map<meshKey, { bsrPath, materials, environmentModifiers }>>.
================
*/
export async function characterMaterialBindings( roster ) {
	const result = new Map(), cache = new Map();
	async function load( source ) {
		const key = source.bsr + "#" + (source.materialSetId ?? "");
		if ( cache.has( key ) ) return cache.get( key );
		const bsr = parseJmxResourceBsr( await loadDataAsset( source.bsr ), source.bsr );
		const materials = await loadMaterialTextures(
			selectItemMaterialPaths( bsr, source.materialSetId, source.bsr )
		);
		const value = {
			bsrPath: source.bsr,
			materials,
			environmentModifiers: bsr.modifiers?.environmentModifiers ?? []
		};
		cache.set( key, value );
		return value;
	}
	for ( const [glb, sources] of publishedCharacterAssets( roster ) ) {
		const binding = new Map();
		for ( const [meshKey, source] of Object.entries( sources ) ) binding.set( meshKey, await load( source ) );
		result.set( glb, binding );
	}
	return result;
}

/*
================
resolveCharacterMaterialBinding

The native material of one published primitive: its mesh's part source, or
the whole-model source, matched by material name.
================
*/
export function resolveCharacterMaterialBinding( bindings, meshName, materialName ) {
	const source = bindings.get( meshName ) ?? bindings.get( "*" );
	if ( !source ) throw Error( "Missing native part " + meshName );
	const entry = [ ...source.materials ].find( ( [name] ) => name.toLowerCase() === materialName.toLowerCase() );
	if ( !entry ) throw Error( "Missing native material " + source.bsrPath + ":" + materialName );
	return { flags: entry[1].flags, environmentModifiers: source.environmentModifiers, bsrPath: source.bsrPath };
}
