/*
===========================================================================

buildSkillStageModelAssets.mjs - models owned by native skill effects

Collects stage resources, persistent skill-object resources and copied weapon
models. All three use the shared BSR compiler so animation, materials and
resource identity survive the same publication contract.

===========================================================================
*/

import fs from "node:fs";
import { listTextDataShardNamesSync, readTextDataLinesSync, splitTextDataRow } from "../shared/textDataIo.mjs";
import path from "node:path";

import { buildEffectRecordTable, parseSkillAniSet, parseSkillEffectSet } from "./parseSkillEffect.mjs";
import { compileBsrVisualToGlb } from "./compileBsrVisual.mjs";
import { dataAssetPath } from "../shared/jmxAssetIO.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { publicRoot, retailTextdataRoot } from "../world/paths.mjs";
import {
	claimResourceOutput,
	finalizeResourceGlbManifest,
	readPreviousResourceGlbPaths,
	resourceGlbOutput
} from "./resourceGlbOutput.mjs";

const skillEffectPath = path.join( retailTextdataRoot, "skilleffect.txt" );
const outputRoot = path.join( publicRoot, "assets", "skillfx" );

/*
================
collectSkillObjectResources

Keep stationary-object admission independent of the full cast-effect catalog.
The shared native join still owns aliases and version-local skill identities.
================
*/
export function collectSkillObjectResources( textdataDir = retailTextdataRoot, sourcePath = skillEffectPath ) {
	const { table } = buildEffectRecordTable( textdataDir, sourcePath );
	const objects = {};
	for ( const [id, record] of Object.entries( table ) ) {
		if ( record.objectResource ) objects[id] = record.objectResource;
	}
	return objects;
}

/*
================
collectSkillStageBsrPaths

86C440 loads the animation set's separate object resource. Looking only at
cast stages loses stationary objects even when their casting effect exists.
================
*/
export function collectSkillStageBsrPaths( sourcePath = skillEffectPath ) {
	const paths = new Set();
	for ( const rows of parseSkillEffectSet( sourcePath ).values() ) {
		for ( const row of rows ) {
			const value = row.objectResourcePath;
			if ( value?.endsWith( ".bsr" ) ) paths.add( value );
		}
	}
	for ( const row of parseSkillAniSet( sourcePath ).values() ) {
		if ( row.objectResource?.kind === "model" ) paths.add( row.objectResource.path );
	}
	return [ ...paths ].sort();
}

/*
================
collectThrownWeaponResources

8E0C60 kind 5 copies slot 6's equipped resource. Publish its own bones;
avatar-bound attachment models do not preserve that independent identity.
================
*/
export function collectThrownWeaponResources( textdataDir = retailTextdataRoot ) {
	const resources = {};
	for ( const file of listTextDataShardNamesSync( textdataDir, /^itemdata.*\.txt$/i ) ) {
		for ( const line of readTextDataLinesSync( path.join( textdataDir, file ) ) ) {
			const c = splitTextDataRow( line );
			// All shipped kind-5 stages are CH spear-shoot actions, accepting spear
			// and glaive (TypeID4 4/5). Include every rarity and degree, not crowd rolls.
			if (
				c[0] !== "1" || c[9] !== "3" || c[10] !== "1" || c[11] !== "6" ||
				![ "4", "5" ].includes( c[12] ) || !c[52]?.endsWith( ".bsr" )
			) {
				continue;
			}
			resources[c[1]] = "res/" + c[52].replaceAll( "\\", "/" ).toLowerCase();
		}
	}
	return resources;
}

/*
================
buildSkillStageModelAssets

Build every admitted dependency before replacing the resource manifest.
Missing source models remain explicit errors in that manifest.
================
*/
export async function buildSkillStageModelAssets() {
	if ( !fs.existsSync( skillEffectPath ) ) {
		console.warn( `[skill-stage-models] source missing (${skillEffectPath}) - skipping` );
		return { written: false, built: 0, referenced: 0 };
	}

	const weapons = collectThrownWeaponResources();
	const movers = new Set();
	for ( const rows of parseSkillEffectSet( skillEffectPath ).values() ) {
		for ( const row of rows ) {
			if ( row.scripts?.[0] === "SCT_MOVER" && row.objectResourcePath ) {
				movers.add( row.objectResourcePath );
			}
		}
	}
	const references = [ ...new Set( [ ...collectSkillStageBsrPaths(), ...Object.values( weapons ) ] ) ].sort();
	const manifestPath = path.join( outputRoot, "manifest.json" );
	const previousGlbPaths = readPreviousResourceGlbPaths( manifestPath );
	const models = {};
	const outputOwners = new Map();
	let built = 0;
	let absent = 0;
	fs.mkdirSync( outputRoot, { recursive: true } );

	for ( const resourcePath of references ) {
		const source = dataAssetPath( resourcePath );
		const output = resourceGlbOutput( resourcePath, {
			namespace: "skillfx",
			publicAssetsRoot: path.join( publicRoot, "assets" )
		} );
		claimResourceOutput( outputOwners, resourcePath, output.publicPath );
		const { publicPath, diskPath } = output;
		const entry = { bsr: resourcePath, glb: publicPath };
		if ( !fs.existsSync( source ) ) {
			entry.error = "not present in Data_extracted";
			models[resourcePath] = entry;
			absent += 1;
			console.warn( `[skill-stage-models] SKIP ${resourcePath} (${entry.error})` );
			continue;
		}
		try {
			const {
				glb,
				clips,
				clipLoop,
				particleModifiers,
				modifierSets,
				animationBindings,
				materialModifiers,
				textureModifiers,
				states
			} = await compileBsrVisualToGlb( resourcePath, {
				stateIds: movers.has( resourcePath ) ? [ 0, 2, 7 ] : [],
				allStates: true
			} );
			fs.mkdirSync( path.dirname( diskPath ), { recursive: true } );
			fs.writeFileSync( diskPath, glb );
			entry.bytes = glb.length;
			entry.clips = clips.map( ( clip ) => clip.role );
			entry.clipLoop = clipLoop;
			entry.particleModifiers = particleModifiers;
			entry.modifierSets = modifierSets;
			entry.animationBindings = animationBindings;
			entry.materialModifiers = materialModifiers;
			entry.textureModifiers = textureModifiers;
			if ( movers.has( resourcePath ) ) entry.states = states;
			built += 1;
			console.log( `[skill-stage-models] OK ${resourcePath} -> ${publicPath} (${glb.length} B)` );
		} catch ( error ) {
			entry.error = String( error?.message ?? error );
			console.warn( `[skill-stage-models] FAIL ${resourcePath}: ${entry.error}` );
		}
		models[resourcePath] = entry;
	}

	const manifest = {
		format: "sro-skill-stage-models",
		version: 2,
		source: "textdata/skilleffect.txt stage objects (91E720) and animation-set objects (91DAE0)",
		count: references.length,
		builtCount: built,
		absentCount: absent,
		models,
		objects: collectSkillObjectResources(),
		weapons
	};
	const removed = await finalizeResourceGlbManifest( {
		manifestPath,
		manifest,
		previousPublicPaths: previousGlbPaths,
		currentPublicPaths: Object.values( models ).map( ( model ) => model.glb ).filter( Boolean ),
		namespace: "skillfx",
		publicAssetsRoot: path.join( publicRoot, "assets" )
	} );
	if ( removed.length > 0 ) {
		console.log( `[skill-stage-models] removed ${removed.length} superseded GLB output(s)` );
	}
	console.log( `[skill-stage-models] manifest -> ${manifestPath} (${built}/${references.length})` );
	return { written: true, built, referenced: references.length, absent, manifestPath };
}

if ( isMainScript( import.meta.url ) ) {
	await buildSkillStageModelAssets();
}
