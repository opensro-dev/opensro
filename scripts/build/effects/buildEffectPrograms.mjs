/*
===========================================================================

buildEffectPrograms.mjs - the particle effect programs catalog

Publishes assets/effects/programs.json: every EFP the native client can
reach (enabled skill-effect records, character actions, equipment, model,
scenery and entity BSR particle modifiers, and the EFP literals in
SRO_Client.exe), with their meshes and textures. The retail Particles.pk2
must match the committed archive evidence
(scripts/build/reference/particle-archive.json).

===========================================================================
*/
import { loadCharacterActionEffectRows } from "../data/buildSkillDataAsset.mjs";
import { equipmentParticleCatalog } from "../char/equipmentParticles.mjs";
import { readPublishedAssetBytesSync } from "../../lib/publishedAsset.mjs";
/*
 * Build the authored v1.150 EasyFX data plane used by the browser renderer.
 *
 * This is deliberately resource-driven. skilleffect.txt selects EFP files;
 * JMXVEFF selects its own BMS geometry, DDJ textures, blend state, emission
 * counts and per-frame curves. No effect is recreated with hand-picked CSS or
 * Babylon constants. The binary reader follows the folded sub_b29e30/sub_b298f0
 * persistence order in parseJmxVisualEffect.mjs; the static mesh reader is shared with the
 * world-resource pipeline.
 */

import fs from "node:fs";
import { createHash } from "node:crypto";
import path from "node:path";
import { isMainScript } from "../shared/fsUtils.mjs";

import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { dataAssetPath } from "../shared/jmxAssetIO.mjs";
import { parseCharacterBsr } from "../char/formats.mjs";
import { parseJmxBmsStaticMesh } from "../world/objects/formats.mjs";
import {
	extractedRoot,
	gameRoot,
	imageSourceRoot,
	normalizeAssetPath,
	publicRoot,
	rebuildRoot,
	retailTextdataRoot
} from "../world/paths.mjs";
import { collectJmxEffectTexturePaths, parseJmxVisualEffect } from "./parseJmxVisualEffect.mjs";
import { buildEffectRecordTable } from "../char/parseSkillEffect.mjs";
import { loadCharacterDataRows } from "../char/resolveCharRoster.mjs";
import { parseStructureEffects } from "../char/structureEffects.mjs";

const particlesRoot = path.join( extractedRoot, "Particles_extracted" );
const skillEffectPath = path.join(
	retailTextdataRoot,
	"skilleffect.txt"
);
const clientBinaryPath = path.join( gameRoot, "SRO_Client.exe" );
const particleImageRoot = path.join( imageSourceRoot, "Particles_extracted" );
const outputRoot = path.join( publicRoot, "assets", "effects" );
const publicParticleImageRoot = path.join(
	publicRoot,
	"assets",
	"images",
	"Particles_extracted"
);

/*
================
fileIndex

Every file under root with the extension, keyed by normalized relative path.
================
*/
function fileIndex( root, extension ) {
	const out = new Map();
	if ( !fs.existsSync( root ) ) return out;
	const pending = [ root ];
	while ( pending.length ) {
		const dir = pending.pop();
		for ( const entry of fs.readdirSync( dir, { withFileTypes: true } ) ) {
			const absolute = path.join( dir, entry.name );
			if ( entry.isDirectory() ) pending.push( absolute );
			else if ( entry.isFile() && entry.name.toLowerCase().endsWith( extension ) ) {
				out.set( normalizeAssetPath( path.relative( root, absolute ) ), absolute );
			}
		}
	}
	return out;
}

/*
================
resolveIndexedFile

The file for a resource name: an exact path, else a unique basename match.
Two basename matches are ambiguous and refused.
================
*/
function resolveIndexedFile( index, resourceName, label ) {
	const exact = index.get( resourceName );
	if ( exact ) return exact;
	if ( resourceName.includes( "/" ) ) return null;
	const suffix = `/${resourceName}`;
	const matches = [ ...index ].filter( ( [key] ) => key.endsWith( suffix ) );
	if ( matches.length > 1 ) {
		throw new Error(
			`[effect-programs] ambiguous ${label} ${resourceName}: ` +
				matches.map( ( [key] ) => key ).join( ", " )
		);
	}
	return matches[0]?.[1] ?? null;
}

/*
================
resolveEffectFiles

Every EFP a resource name can mean: the exact path, else each basename match.
================
*/
function resolveEffectFiles( index, resourceName ) {
	const exact = index.get( resourceName );
	if ( exact ) return [ [ resourceName, exact ] ];
	if ( resourceName.includes( "/" ) ) return [];
	const suffix = `/${resourceName}`;
	return [ ...index ]
		.filter( ( [key] ) => key.endsWith( suffix ) )
		.sort( ( [a], [b] ) => a.localeCompare( b ) );
}

/*
================
collectNativeExecutableEffectReferences

EFP paths embedded directly in the retail executable: the second native
resource-owner domain beside the active CIDecoSkillRecord table. In v1.150
this finds the six berserk-orb paths CICPlayer uses and the direct
critical-hit literal. Reading the PE keeps the build closed over the actual
retail program instead of a hand-maintained list that could drift.
================
*/
export function collectNativeExecutableEffectReferences( binaryPath = clientBinaryPath ) {
	if ( !fs.existsSync( binaryPath ) ) {
		throw new Error( `[effect-programs] native client binary missing: ${binaryPath}` );
	}
	const bytes = fs.readFileSync( binaryPath );
	const references = new Set();
	const collect = ( text ) => {
		const pattern = /(?<![a-z0-9_.\\/-])(?:[a-z0-9_.-]+[\\/])*[a-z0-9_.-]+\.efp/gi;
		for ( const match of text.matchAll( pattern ) ) {
			references.add( normalizeAssetPath( match[0] ) );
		}
	};
	collect( bytes.toString( "latin1" ) );
	collect( bytes.toString( "utf16le" ) );
	return [ ...references ].sort();
}

/*
================
collectEffectReferences

Every reachable EFP, grouped by the native owner that references it.
================
*/
export function collectEffectReferences() {
	const { table, namedTable } = buildEffectRecordTable( retailTextdataRoot, skillEffectPath );
	const recordReferences = new Set();
	const add = ( value ) => {
		const normalized = normalizeAssetPath( value ?? "" );
		if ( normalized.endsWith( ".efp" ) ) recordReferences.add( normalized );
	};

	// Follow the same reachability join as native: sub_920020 first filters
	// skillaniset2 rows by Service, sub_91dae0 constructs enabled records, and
	// sub_9169d0 joins only resolvable SkillData/built-in names.  A raw scan of
	// every skilleffectset row crosses that ownership boundary and incorrectly
	// promotes disabled/future authoring residue into runtime asset debt.
	for ( const record of [ ...Object.values( table ), ...Object.values( namedTable ) ] ) {
		add( record.defenseEffectPath );
		add( record.damageEffectPath );
		add( record.criticalDamageEffectPath );
		add( record.arrowTrailEffectPath );
		add( record.arrowForceEffectPath );
		for ( const stage of record.authoredStages ?? [] ) {
			add( stage.objectResourcePath );
			add( stage.secondaryObjectPath );
		}
		// 86C440: a stationary skill object (Fire Trap) may be an EFP itself,
		// published through the skillfx manifest's objects table.
		if ( record.objectResource?.kind === "effect" ) add( record.objectResource.path );
	}

	for ( const row of loadCharacterActionEffectRows() ) for ( const resource of row.bloodEffects ) add( resource );
	for ( const rows of Object.values( equipmentParticleCatalog() ) ) for ( const row of rows ) add( row.effectPath );
	const nativeExecutableReferences = collectNativeExecutableEffectReferences();
	const modelParticleReferences = collectModelParticleReferences( { ...table, ...namedTable } );
	const sceneryParticleReferences = collectSceneryParticleReferences();
	const entityParticleReferences = collectEntityParticleReferences();
	const structureEffectReferences = collectStructureEffectReferences();
	return {
		references: [
			...new Set( [
				...recordReferences,
				...modelParticleReferences.map( row => row.effectPath ),
				...sceneryParticleReferences.map( row => row.effectPath ),
				...entityParticleReferences.map( row => row.effectPath ),
				...structureEffectReferences.map( row => row.effectPath ),
				...nativeExecutableReferences
			] )
		].sort(),
		recordReferences: [ ...recordReferences ].sort(),
		nativeExecutableReferences,
		modelParticleReferences,
		sceneryParticleReferences,
		entityParticleReferences,
		structureEffectReferences,
		effectRecordCount: Object.keys( table ).length
	};
}

/*
================
collectEntityParticleReferences

Particle references of item-drop, NPC and skill-effect models. Entity
resources are independent producers, not skill-stage aliases; the original
BSR is read even when a stale model manifest omitted its modifier payload.
================
*/
export function collectEntityParticleReferences(
	readManifest = domain => JSON.parse( readPublishedAssetBytesSync( `/assets/${domain}/manifest.json`, publicRoot ) ),
	readModel = resourcePath => parseCharacterBsr( fs.readFileSync( dataAssetPath( resourcePath ) ), resourcePath )
) {
	const references = [];
	for ( const domain of [ "itemdrop", "npc", "skillfx" ] ) {
		const models = new Set(
			Object.entries( readManifest( domain ).models ).map( ( [key, row] ) =>
				row.bsr ?? (key.startsWith( "res/" ) ? key : `res/${key}`)
			)
		);
		for ( const modelPath of [ ...models ].sort() ) {
			const modifiers = readModel( modelPath ).particleModifiers;
			for (
				const effectPath of [
					...new Set(
						modifiers.flatMap( modifier =>
							modifier.entries.map( entry => normalizeAssetPath( entry.effectPath ) )
						)
					)
				].sort()
			) {
				if ( !effectPath.endsWith( ".efp" ) ) {
					throw Error( `Invalid ${domain} particle reference: ${modelPath}` );
				}
				const selectors = modifiers.filter( modifier =>
					modifier.entries.some( entry => normalizeAssetPath( entry.effectPath ) === effectPath )
				).map( modifier => ({
					kind: modifier.kind,
					stateId: modifier.stateId,
					animationSetName: modifier.animationSetName
				}) );
				references.push( { domain, modelPath, effectPath, selectors } );
			}
		}
	}
	return references;
}

/*
================
collectStructureEffectReferences

atstructeffect.txt, loaded whole at startup (Client_LoadGameDataTables
722E20): each resolvable target's damage-level effects and the particle
modifiers of its stage models (CICATStruct_SetVisualStage loads them).
================
*/
export function collectStructureEffectReferences(
	text = fs.readFileSync( path.join( retailTextdataRoot, "atstructeffect.txt" ), "utf16le" ),
	known = codename => characterCodenames().has( codename ),
	readModel = resourcePath => parseCharacterBsr( fs.readFileSync( dataAssetPath( resourcePath ) ), resourcePath )
) {
	const references = new Map();
	for ( const [codename, target] of parseStructureEffects( text, known ) ) {
		for ( const effects of Object.values( target.levels ) ) {
			for ( const effect of effects ) {
				references.set( codename + " " + effect.effectPath, { codename, effectPath: effect.effectPath } );
			}
		}
		for ( const modelPath of Object.values( target.stages ) ) {
			for ( const modifier of readModel( modelPath ).particleModifiers ) {
				for ( const entry of modifier.entries ) {
					const effectPath = normalizeAssetPath( entry.effectPath );
					if ( !effectPath.endsWith( ".efp" ) ) {
						throw Error( `Invalid structure stage particle: ${modelPath}` );
					}
					references.set( codename + " " + effectPath, { codename, modelPath, effectPath } );
				}
			}
		}
	}
	return [ ...references.values() ].sort( ( a, b ) =>
		a.codename.localeCompare( b.codename ) || a.effectPath.localeCompare( b.effectPath )
	);
}

/*
================
characterCodenames
================
*/
function characterCodenames() {
	return new Set( loadCharacterDataRows( retailTextdataRoot, { codenamePattern: /./ } ).keys() );
}

/*
================
collectSceneryParticleReferences

Particle references of outdoor placement resources, a separate native
producer from skills. Every compound branch is walked; effect files are
never selected by scenery filename.
================
*/
export function collectSceneryParticleReferences(
	index = JSON.parse( readPublishedAssetBytesSync( "/assets/world/outdoor/object-resources.json", publicRoot ) )
) {
	const references = new Map();
	for ( const root of index.bsr ) {
		for ( const branch of root.branches ?? [ root ] ) {
			const modifiers = branch.modifiers ??
				parseCharacterBsr( fs.readFileSync( dataAssetPath( branch.sourcePath ) ), branch.sourcePath );
			for ( const modifier of modifiers.particleModifiers ?? [] ) {
				for ( const entry of modifier.entries ) {
					const effectPath = normalizeAssetPath( entry.effectPath );
					if ( !effectPath.endsWith( ".efp" ) ) throw Error( "Invalid scenery effect reference" );
					references.set( branch.sourcePath + "\0" + effectPath, {
						modelPath: branch.sourcePath,
						effectPath
					} );
				}
			}
		}
	}
	return [ ...references.values() ].sort( ( a, b ) =>
		a.modelPath.localeCompare( b.modelPath ) || a.effectPath.localeCompare( b.effectPath )
	);
}

/*
================
collectModelParticleReferences

Particle references of the BSR stages enabled skill records name. A stage
BSR can hold only a skeleton and ModDataParticle, with no mesh and no direct
EFP in skilleffect.txt (SYSTEM_HELPERMARK is one). References are followed
from enabled records, not from every BSR in the extracted tree.
================
*/
export function collectModelParticleReferences(
	table,
	readModel = resourcePath => parseCharacterBsr( fs.readFileSync( dataAssetPath( resourcePath ) ), resourcePath )
) {
	const models = new Set();
	for ( const record of Object.values( table ) ) {
		for ( const stage of record.authoredStages ?? [] ) {
			for ( const value of [ stage.objectResourcePath, stage.secondaryObjectPath ] ) {
				const resourcePath = normalizeAssetPath( value ?? "" );
				if ( resourcePath.endsWith( ".bsr" ) ) models.add( resourcePath );
			}
		}
	}
	const references = [];
	for ( const modelPath of [ ...models ].sort() ) {
		const effects = new Set();
		for ( const modifier of readModel( modelPath ).particleModifiers ) {
			for ( const entry of modifier.entries ) {
				const effectPath = normalizeAssetPath( entry.effectPath );
				if ( effectPath.endsWith( ".efp" ) ) effects.add( effectPath );
			}
		}
		for ( const effectPath of [ ...effects ].sort() ) references.push( { modelPath, effectPath } );
	}
	return references;
}

/*
================
collectMeshPaths

The BMS meshes an effect's object tree and controllers reference.
================
*/
function collectMeshPaths( effect ) {
	const paths = new Set();
	const visitResource = ( resource ) => {
		for ( const mesh of resource?.meshes ?? [] ) {
			const normalized = normalizeAssetPath( mesh.path ?? "" );
			if ( normalized.endsWith( ".bms" ) ) paths.add( normalized );
		}
	};
	const visit = ( object ) => {
		visitResource( object.resource );
		for ( const controller of object.controllers ?? [] ) visitResource( controller.resource );
		for ( const child of object.children ?? [] ) visit( child );
	};
	visit( effect.root );
	return paths;
}

/*
================
effectTexturePngPath

The converted PNG path of a DDJ texture reference.
================
*/
function effectTexturePngPath( texturePath ) {
	return normalizeAssetPath( texturePath ).replace( /\.ddj$/i, ".png" );
}

/*
================
copyIfDifferent

Copy unless the target is already the same size and not older.
================
*/
function copyIfDifferent( source, target ) {
	fs.mkdirSync( path.dirname( target ), { recursive: true } );
	if ( fs.existsSync( target ) ) {
		const a = fs.statSync( source );
		const b = fs.statSync( target );
		if ( a.size === b.size && a.mtimeMs <= b.mtimeMs ) return false;
	}
	fs.copyFileSync( source, target );
	return true;
}

/*
================
compactMesh

The published fields of one decoded mesh.
================
*/
function compactMesh( mesh, key ) {
	return {
		sourcePath: key,
		positions: mesh.positions,
		normals: mesh.normals,
		uvs: mesh.uvs,
		indices: mesh.indices,
		bounds: mesh.bounds
	};
}

/*
================
buildEffectProgramsAsset

Build and publish the catalog; see the file banner.
================
*/
export async function buildEffectProgramsAsset() {
	if ( !fs.existsSync( particlesRoot ) || !fs.existsSync( skillEffectPath ) ) {
		console.warn( "[effect-programs] source missing - skipping" );
		return { written: false, effects: 0, meshes: 0, textures: 0 };
	}

	const efpIndex = fileIndex( particlesRoot, ".efp" );
	const bmsIndex = fileIndex( particlesRoot, ".bms" );
	const pngIndex = fileIndex( particleImageRoot, ".png" );
	const effects = {};
	const ambiguousEffectAliases = {};
	const missingEffects = [];
	const nativeUnavailableEffects = [];
	// The evidence is committed with this checkout; the archive belongs to the game root.
	const archiveEvidence = JSON.parse(
		fs.readFileSync( path.join( rebuildRoot, "scripts/build/reference/particle-archive.json" ), "utf8" )
	);
	if ( archiveEvidence.format !== "sro-particle-archive-v1" ) {
		throw Error( "Particle archive evidence has an unknown format" );
	}
	if (
		createHash( "sha256" ).update( fs.readFileSync( path.join( gameRoot, "Particles.pk2" ) ) ).digest( "hex" ) !==
			archiveEvidence.sha256
	) {
		throw Error(
			`${
				path.join( gameRoot, "Particles.pk2" )
			} is not the v1.150 archive the effect evidence was recorded from (sha256 ${archiveEvidence.sha256}); use the retail v1.150 client`
		);
	}
	const meshNames = new Set();
	const textureNames = new Set();

	const reachability = collectEffectReferences();
	for ( const effectName of reachability.references ) {
		const resolved = resolveEffectFiles( efpIndex, effectName );
		if ( !resolved.length ) {
			// AFF730 keeps a reset stored object after B1F270 fails its header read.
			// Only archive-proven absence has this contract. Extraction/build misses
			// still fail below; no substitute asset or filename alias is permitted.
			if ( archiveEvidence.absent.includes( effectName ) ) {
				nativeUnavailableEffects.push( effectName );
				continue;
			}
			missingEffects.push( effectName );
			continue;
		}
		if ( resolved.length > 1 ) {
			ambiguousEffectAliases[effectName] = resolved.map( ( [key] ) => key );
		}
		for ( const [resolvedName, source] of resolved ) {
			const parsed = parseJmxVisualEffect( fs.readFileSync( source ), source );
			// A unique basename keeps its authored lookup key. Ambiguous basenames
			// remain explicit full-path entries and are published in the alias
			// diagnostics table; choosing one silently would invent archive order.
			const key = resolved.length === 1 ? effectName : resolvedName;
			effects[key] = parsed;
			for ( const mesh of collectMeshPaths( parsed ) ) meshNames.add( mesh );
			for ( const texture of collectJmxEffectTexturePaths( parsed ) ) {
				textureNames.add( effectTexturePngPath( texture ) );
			}
		}
	}

	if ( missingEffects.length > 0 ) {
		throw new Error(
			"[effect-programs] runtime-reachable EFP source closure is incomplete:\n" +
				missingEffects.map( ( effectName ) => `  - ${effectName}` ).join( "\n" )
		);
	}

	const meshes = {};
	for ( const meshName of [ ...meshNames ].sort() ) {
		const source = resolveIndexedFile( bmsIndex, meshName, "BMS" );
		if ( !source ) throw new Error( `[effect-programs] missing BMS ${meshName}` );
		meshes[meshName] = compactMesh(
			parseJmxBmsStaticMesh( fs.readFileSync( source ), source ),
			meshName
		);
	}

	let copiedTextures = 0;
	const textures = {};
	for ( const textureName of [ ...textureNames ].sort() ) {
		const source = pngIndex.get( textureName );
		if ( !source ) throw new Error( `[effect-programs] missing converted texture ${textureName}` );
		const target = path.join( publicParticleImageRoot, ...textureName.split( "/" ) );
		if ( copyIfDifferent( source, target ) ) copiedTextures += 1;
		textures[textureName] = `/assets/images/Particles_extracted/${textureName}`;
	}

	fs.mkdirSync( outputRoot, { recursive: true } );
	const outPath = path.join( outputRoot, "programs.json" );
	writeJsonIfChangedSync( outPath, {
		format: "sro-jmx-easyfx-programs",
		version: 3,
		source:
			"native reachable CIDecoSkillRecord EFP closure including BSR ModDataParticle plus SRO_Client.exe direct EFP literals",
		// Native ProcessManager publishes one EasyFX step for each crossed 50 ms
		// quantum (data_f0c90c), so EFP timeline frames are 20 Hz.
		framesPerSecond: 20,
		reachability: {
			effectRecordCount: reachability.effectRecordCount,
			effectRecordReferences: reachability.recordReferences.length,
			modelParticleReferences: reachability.modelParticleReferences,
			sceneryParticleReferences: reachability.sceneryParticleReferences,
			entityParticleReferences: reachability.entityParticleReferences,
			nativeExecutableReferences: reachability.nativeExecutableReferences
		},
		effects,
		ambiguousEffectAliases,
		missingEffects,
		nativeUnavailable: { archiveSha256: archiveEvidence.sha256, effects: nativeUnavailableEffects },
		meshes,
		textures
	} );
	await refreshPrecompressedSidecars( [ outPath ], { onlyWhenStale: true } );
	console.log(
		`[effect-programs] wrote ${Object.keys( effects ).length} EFP program(s), ` +
			`${Object.keys( meshes ).length} BMS mesh(es), ${Object.keys( textures ).length} texture(s) ` +
			`(${copiedTextures} copied), ${reachability.recordReferences.length} reachable record EFP(s), ` +
			`${reachability.nativeExecutableReferences.length} native literal EFP(s) -> ${
				path.relative( publicRoot, outPath )
			}`
	);
	return {
		written: true,
		effects: Object.keys( effects ).length,
		meshes: Object.keys( meshes ).length,
		textures: Object.keys( textures ).length,
		missingEffects: missingEffects.length,
		copiedTextures,
		outPath
	};
}

if ( isMainScript( import.meta.url ) ) {
	await buildEffectProgramsAsset();
}
