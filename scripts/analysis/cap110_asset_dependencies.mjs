/*
===========================================================================

cap110_asset_dependencies.mjs - equipment closure through shared parsers

Reads JSON resources from stdin and returns their direct dependencies. Binary
payloads are base64 data, never code. The importer resolves each dependency
against native assets first and then the owner's later archives. This checks
resource framing and existence, not browser rendering or combat behavior.

===========================================================================
*/
import { readFileSync } from "node:fs";
import { parseCharacterBsr } from "../build/char/formats.mjs";
import { equipmentSources } from "../build/char/equipmentParticles.mjs";
import { collectMeshPaths } from "../build/effects/buildEffectPrograms.mjs";
import { collectJmxEffectTexturePaths, parseJmxVisualEffect } from "../build/effects/parseJmxVisualEffect.mjs";
import { parseCompound } from "../build/world/objects/compound.mjs";
import { parseJmxBmtMaterialSet, parseJmxResourceBsr, resolveBmtTexturePath } from "../build/world/objects/formats.mjs";

/*
================
dependencies
================
*/
function dependencies( resource ) {
	if ( resource.weapon ) {
		return [
			...new Set( [
				...equipmentSources( resource.weapon, false ),
				...equipmentSources( resource.weapon, true )
			] )
		];
	}
	const bytes = Buffer.from( resource.bytes, "base64" );
	const paths = new Set();
	const particles = new Set();
	if ( resource.path.endsWith( ".bsr" ) ) {
		const object = parseJmxResourceBsr( bytes, resource.path );
		const character = parseCharacterBsr( bytes, resource.path );
		for (
			const name of [
				...object.meshPaths,
				...object.materialPaths,
				character.skeletonPath,
				...character.animationPaths
			]
		) {
			if ( name ) paths.add( name );
		}
		// Native drop resources keep their unchanged particle/audio owners.
		// A new later resource cannot silently acquire that exemption: its
		// importer declares a posture instead. "data" walks the sound
		// tracks' files as Data resources (the Sky Temple import, whose
		// monsters keep their authored sounds); "all" walks every particle
		// set's effects, not only the equipment selector kind.
		if ( !resource.native && character.soundModifiers.length ) {
			if ( resource.sounds !== "data" ) {
				throw Error( `${resource.path}: sound closure requires an explicit importer` );
			}
			for ( const set of character.soundModifiers ) {
				for ( const entry of set.entries ) {
					for ( const track of entry.tracks ) {
						if ( track.sourcePath ) paths.add( track.sourcePath );
					}
				}
			}
		}
		if ( !resource.native ) {
			for ( const modifier of character.particleModifiers ) {
				if ( modifier.kind !== 2 && resource.particles !== "all" ) {
					throw Error( `${resource.path}: unsupported equipment particle selector` );
				}
				for ( const entry of modifier.entries ) particles.add( entry.effectPath );
			}
		}
	} else if ( resource.path.endsWith( ".cpd" ) ) {
		const compound = parseCompound( bytes, resource.path );
		for ( const name of compound.branches ) paths.add( name );
	} else if ( resource.path.endsWith( ".bmt" ) ) {
		const materialSet = parseJmxBmtMaterialSet( bytes, resource.path );
		for ( const material of materialSet.materials ) {
			const texture = resolveBmtTexturePath( resource.path, material.textureName );
			if ( texture ) paths.add( texture );
		}
	} else if ( resource.path.endsWith( ".efp" ) ) {
		const effect = parseJmxVisualEffect( bytes, resource.path );
		for ( const name of [ ...collectMeshPaths( effect ), ...collectJmxEffectTexturePaths( effect ) ] ) {
			particles.add( name );
		}
	}
	return [ ...paths ].sort().concat( [ ...particles ].sort().map( path => ({ archive: "Particles", path }) ) );
}

const resources = JSON.parse( readFileSync( 0, "utf8" ) );
process.stdout.write( JSON.stringify( resources.map( dependencies ) ) );
