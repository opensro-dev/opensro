/*
===========================================================================

buildNpcModelAssets.mjs - bake the mission NPC, monster and COS models

Bake the mission NPC roster's models to GLB, mirroring buildRoster.mjs for
the title-crowd chars: one GLB per NPC model + a manifest keyed by
codename. The runtime (ciCharactorDrawVisualSystem.ts, the remote-entity
slice) publishes only these authored models; the WIP shadow world stays
the data authority (position/yaw/lifecycle), the GLB is twin-side visuals.

The monster roster is DERIVED at bake time from the server's spawnable
join (sro-evidence creatable-monsters; see npcModelRoster.mjs) - the same set the
bootstrap refObjSnapshot seeds per session, so bake coverage tracks the
server mechanically. NPCs come from sro-evidence spawnable-npcs, which exports
the unique RefObj types in mission.LoadNpcWorldRoster instead of requiring a
second hand-maintained list. Position duplicates remain runtime instances,
not duplicate model builds.

NPC resources are media-driven rather than assumed to share one animation
shape. Smith and Advice expose state 0 idle, which is exported as "stand";
the fixed gacha machine has no animation set and is emitted with an explicit
staticPose contract. Mission NPCs remain stationary (movement plan rev. 62).

MOB_* .bsr files are DIFFERENT: they carry a full locomotion/combat set
(mangnyang.bsr: 15 clips over one "default" animation set — stand01 stateId
0, walk stateId 1, run stateId 7, attacks/damage/die/down; MONSTER-LIVE Q3
finding, probe temp/asset-probe-bsr-anim.mjs). Monsters assemble with the
native default animation-set state ids now pinned by the complete spawnable
BSR census: stand=0, walk=1, run=7. Those exact records select the GLB
movement clips and carry their BSR ModDataSound cursor metadata.

Output:
  .generated/client-public/assets/npc/<native path below res/>.glb
  .generated/client-public/assets/npc/manifest.json (codename -> glb + meta)
  .generated/client-public/assets/npc/animation-catalog.json (BSR-authored lab data)

Textures: .ddj under prim/mtrl are converted by scripts/convert_images.py
(same pass buildRoster runs); reuse --skip-textures when they're in.

===========================================================================
*/

import { writeIntoPublicTreeSync } from "../shared/publicWrite.mjs";
import { loadBoothModelRoster } from "./boothModelRoster.mjs";
import { bakeNpcSecondaryResources } from "./npcSecondaryResources.mjs";
import { authoredAnimationBindings } from "./authoredAnimationBindings.mjs";
import { pickAttachedMotionClips } from "./attachedMotionClips.mjs";

import fs from "node:fs";
import { characterMaterialVariants } from "./materialVariants.mjs";
import { parseWeatherEvents } from "./weatherEvents.mjs";
import path from "node:path";
import {
	findDefaultAnimationSet,
	findDefaultAnimationState,
	pickDefaultSetSoundEvents,
	pickSetSoundEvents,
	pickSetTrackEvents,
	pickDefaultSetStateTableMetadata,
	pickAnimationStateTableMetadata
} from "./animationUtils.mjs";
import { assembleAvatar, primSlot, NATIVE_IDLE_STATE_CLIPS, NATIVE_EMOTE_STATE_CLIPS } from "./buildAvatar.mjs";
import { assembleStaticBsrModel } from "./compileBsrVisual.mjs";
import { avatarToGlb } from "./exportGlb.mjs";
import { loadCharacterDataRows } from "./resolveCharRoster.mjs";
import {
	enabledCosReferences,
	loadFortressStructureRoster,
	loadModelledMobRoster,
	loadSpawnableNpcRoster
} from "./npcModelRoster.mjs";
import { parseStructureEffects } from "./structureEffects.mjs";
import { parseBan, parseCharacterBsr } from "./formats.mjs";
import { SKILL_EFFECT_ANIMATION_ID_BY_NAME } from "./native/skillEffectAnimationRegistry.ts";
import { loadDataAsset, loadMaterialTextures } from "../shared/jmxAssetIO.mjs";
import { normalizeAssetPath } from "../shared/assetPaths.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { convertTextureTrees } from "../shared/convertImagesRunner.mjs";
import { sha256Hex } from "../shared/hash.mjs";
import { readJsonOrNullSync, writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { loadOptionalDataAsset } from "../shared/optionalDataAsset.mjs";
import { splitTextDataRow } from "../shared/textDataIo.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import {
	claimResourceOutput,
	readPreviousResourceGlbPaths,
	removeSupersededResourceOutputs,
	resourceGlbOutput
} from "./resourceGlbOutput.mjs";
import { gameRoot, publicAssetsRoot, retailTextdataRoot } from "../world/paths.mjs";
import {
	NPC_MANIFEST_FORMAT,
	NPC_MANIFEST_VERSION,
	npcManifestModels,
	splitNpcManifestModels
} from "../shared/npcManifest.mjs";

const textdataDir = retailTextdataRoot;
const publicAssets = publicAssetsRoot;

/*
================
publicAssetPathToDisk

Resolve one generated /assets/ URL without accepting a path outside the
generated public-assets owner. This builder only uses it to validate VAT
artifacts already referenced by its previous manifest.
================
*/
function publicAssetPathToDisk( publicPath, publicAssetsRoot = publicAssets ) {
	const normalized = String( publicPath ?? "" ).replaceAll( "\\", "/" );
	if ( !normalized.startsWith( "/assets/" ) || normalized.includes( ".." ) ) {
		return null;
	}
	return path.join( publicAssetsRoot, normalized.slice( "/assets/".length ) );
}

/*
================
preserveFreshNpcVatReferences

buildNpcVatAssets owns VAT generation, while this builder owns the primary
NPC manifest. A model-only incremental rebuild used to replace that manifest
with a VAT-free base document even when every GLB was byte-identical to the
already baked payload. Runtime then silently changed from the pooled VAT path
to per-entity skeletons until the full resource graph happened to run again.

Preserve a prior enrichment only after proving the complete chain still
matches: public paths, compiler contract, GLB SHA-256, VAT byte metadata and
clip index. A changed GLB or policy deliberately drops its stale reference;
the VAT builder is the only owner allowed to regenerate it.
================
*/
export function preserveFreshNpcVatReferences( models, previousManifest, options = {} ) {
	const publicAssetsRoot = options.publicAssetsRoot ?? publicAssets;
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

		const glbPath = publicAssetPathToDisk( model.glb, publicAssetsRoot );
		const vatManifestPath = publicAssetPathToDisk( reference.manifest, publicAssetsRoot );
		const vatBinPath = publicAssetPathToDisk( reference.bin, publicAssetsRoot );
		const glbStat = glbPath ? readStat( glbPath ) : null;
		const vatBinStat = vatBinPath ? readStat( vatBinPath ) : null;
		const vat = vatManifestPath ? readJson( vatManifestPath ) : null;
		const valid = Boolean(
			glbPath && vatBinPath && vat &&
				glbStat?.isFile() && vatBinStat?.isFile() &&
				vat.format === previousVatContract.format &&
				vat.version === previousVatContract.version &&
				vat.compilerVersion === previousVatContract.compilerVersion &&
				vat.settings?.materialMode === previousVatContract.materialMode &&
				JSON.stringify( vat.settings?.clipRoles ?? [] ) ===
					JSON.stringify( previousVatContract.clipRoles ?? [] ) &&
				vat.source?.glb === model.glb &&
				vat.source?.byteLength === glbStat.size &&
				vat.source?.sha256 === readSha256( glbPath ) &&
				vat.bin?.path === reference.bin &&
				vat.bin?.byteLength === vatBinStat.size &&
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

/**
 * Native sub_920020 #section characterInfo. This is one record, not separate
 * sound and mount catalogs: sub_856480 binds it at CICharactor+0x710;
 * ResourceTypeName feeds the sound callbacks, Ride Type lands at +0x18, and
 * the optional second BSR lands at +0x24 for sub_861b00's CICRide branch.
 */
/*
================
expandCharacterInfoCodenames

Mirror SkillEffectCharacterInfo_ParseRow (sub_91b830): a characterInfo key
whose final seven characters are NNN~NNN denotes an inclusive family.  The
native loader registers one action-effect context for every expanded RefObj
name.  Keep this expansion at the data boundary; consumers must never need to
know whether a record was authored directly or through the compact form.
================
*/
export { expandCharacterInfoCodenames } from "../shared/characterInfo.mjs";
import { expandCharacterInfoCodenames } from "../shared/characterInfo.mjs";

/*
================
characterInfoResolver

CharacterInfo_FindByRefThenOriginalThenDefault (9171B0): a character's own
characterInfo row, else its original reference's, else the default, which is
the first row whose codename names a known reference
(CharacterInfo_RegisterExistingRefAndSeedDefault, 91B7E0). Every character
resolves, so none is left without a sound and effect profile. Native reads the
original reference from the character data (+0x5C); the clone base link in
characterdata column 4, the chain resolveNpcModel follows for the model, is
taken to be that reference. `records` keeps file order (a Map).
================
*/
function characterInfoResolver( records, rows ) {
	const BASE_COLUMN = 4;
	const defaultCodename = [ ...records.keys() ].find( codename => rows.has( codename ) );
	const fallback = defaultCodename === undefined ? undefined : records.get( defaultCodename );
	return codename => records.get( codename ) ?? records.get( rows.get( codename )?.[BASE_COLUMN] ?? "" ) ?? fallback;
}

/*
================
loadCharacterInfo
================
*/
function loadCharacterInfo() {
	const records = new Map();
	const sourceLines = new Map();
	const text = fs
		.readFileSync( path.join( textdataDir, "skilleffect.txt" ), "utf16le" )
		.replace( /^\uFEFF/, "" );
	let inCharacterInfo = false;
	for ( const [lineIndex, line] of text.split( /\r?\n/ ).entries() ) {
		if ( line.startsWith( "#section" ) ) {
			inCharacterInfo = /^#section\s+characterInfo\b/i.test( line );
			continue;
		}
		if ( !inCharacterInfo || !line || line.startsWith( "//" ) ) continue;
		const cols = splitTextDataRow( line );
		const codename = cols[0];
		const soundProfileName = cols[1];
		if ( !codename ) continue;
		const rideTypeName = String( cols[3] ?? "none" ).trim().toUpperCase();
		const rideModelPath = normalizeBsrPath( cols[4] );
		// 91B830 stores column 5 at record +0x28: the mesh 8E64F0 loads on death.
		const deathModelPath = normalizeBsrPath( cols[5] );
		const riderTransformMode = rideTypeName === "RT_FIXED" ? 1 : rideTypeName === "RT_DUMMY" ? 2 : 0;
		const record = {
			soundProfileName: soundProfileName && soundProfileName.toLowerCase() !== "none" ? soundProfileName : null,
			rideModelPath,
			deathModelPath,
			riderTransformMode
		};
		for ( const expandedCodename of expandCharacterInfoCodenames( codename ) ) {
			const priorLine = sourceLines.get( expandedCodename );
			if ( priorLine !== undefined ) {
				throw new Error(
					`Duplicate skilleffect characterInfo codename ${expandedCodename}: ` +
						`lines ${priorLine + 1} and ${lineIndex + 1}`
				);
			}
			records.set( expandedCodename, record );
			sourceLines.set( expandedCodename, lineIndex );
		}
	}
	return records;
}

/*
================
normalizeBsrPath
================
*/
function normalizeBsrPath( value ) {
	const normalized = normalizeAssetPath( value );
	if ( !normalized || normalized === "none" ) return null;
	return normalized.startsWith( "res/" ) ? normalized : `res/${normalized}`;
}

/*
================
buildRetailAnimationCatalog

Publish the complete retail animation surface separately from the runtime
behavior whitelist. One BAN can be reached by more than one native state
(Mangyang stand01: 0/79, stand02: 8/122), so aliases remain attached to the
unique exported GLB clip instead of becoming duplicate AnimationGroups.
================
*/
export function buildRetailAnimationCatalog( bsr, clips ) {
	const defaultSet = bsr.animationSets
		?.find( ( set ) => set.name.toLowerCase() === "default" );
	const states = defaultSet?.states ?? [];
	const normalizePath = normalizeAssetPath;

	return clips.map( ( { role, path: clipPath, clip } ) => {
		const normalizedClipPath = normalizePath( clipPath );
		return {
			role,
			durationMs: clip.durationMs,
			looping: clip.field2 === 1,
			stateIds: states
				.filter( ( state ) => normalizePath( state.animationPath ) === normalizedClipPath )
				.map( ( state ) => state.stateId )
		};
	} );
}

/*
================
resolveNpcModel
================
*/
function resolveNpcModel( codename, rows ) {
	const cols = rows.get( codename );
	if ( !cols ) return null;
	let bsrCols = cols;
	let baseCodename = null;
	const visited = new Set( [ codename ] );
	while ( bsrCols.findIndex( ( value ) => /\.bsr$/i.test( value ) ) < 0 ) {
		// _CLON rows carry a base codename at col[4]. Follow the identity chain
		// defensively: current retail data is one hop, but cycles and missing
		// links must fail rather than turn into order-dependent unresolved pegs.
		const link = bsrCols[4];
		if ( !link || visited.has( link ) ) {
			throw new Error(
				`[npc] ${codename}: invalid characterdata base chain at ${link || "<empty>"}`
			);
		}
		const baseRow = rows.get( link );
		if ( !baseRow ) {
			throw new Error( `[npc] ${codename}: characterdata base ${link} is missing` );
		}
		visited.add( link );
		bsrCols = baseRow;
		baseCodename = link;
	}
	const bsrIndex = bsrCols.findIndex( ( v ) => /\.bsr$/i.test( v ) );
	if ( bsrIndex < 0 ) return null;
	const scalePercent = Number( cols[48] );
	return {
		codename,
		refObjId: Number( cols[1] ),
		baseCodename,
		materialKind: Number( cols[109] ),
		scalePercent: Number.isFinite( scalePercent ) && scalePercent > 0 ? scalePercent : 100,
		bsrPath: `res/${bsrCols[bsrIndex].replaceAll( "\\", "/" ).toLowerCase()}`
	};
}

// The default-set motions bakeCharacterResource requires of a monster resource.
const BODY_REQUIRED_STATES = [ 0 ];
const DEATH_REQUIRED_STATES = [ 4, 36 ];

/*
================
bakeCharacterResource

Bake one character BSR resource. Primary RefObj models and secondary
CICRide models use the same native resource loader, so they must share one
compiler path as well; only the manifest identity differs. requiredStates
are the default-set motions a monster resource cannot lack: stand (0) for a
body, death and deathLoop (4, 36) for a characterInfo death model, which
8E64F0 only ever plays those two motions on.
================
*/
export async function bakeCharacterResource( bsrPath, output, isMob, requiredStates = BODY_REQUIRED_STATES ) {
	const { publicPath, diskPath } = output;
	const source = await loadDataAsset( bsrPath );
	const bsr = parseCharacterBsr( source, bsrPath );
	const materialSets = characterMaterialVariants( source, bsrPath );
	const avatar = bsr.skeletonPath ?
		await assembleAvatar( bsrPath, {
			noClips: true,
			materialSetPaths: materialSets.has( 0 ) ? [ materialSets.get( 0 ) ] : [],
			rigidUnboundMeshes: true,
			slotForMesh: ( _mp, i ) => primSlot( i )
		} ) :
		await assembleStaticBsrModel( bsrPath );
	const clips = [];
	const movementRules = isMob ?
		[
			[ "stand", 0 ],
			[ "walk", 1 ],
			[ "attack1", 2 ],
			[ "hit1", 3 ],
			[ "death", 4 ],
			[ "attack2", 5 ],
			[ "run", 7 ],
			[ "stand02", 8 ],
			[ "hit2", 9 ],
			[ "attack3", 16 ],
			[ "attack4", 17 ],
			[ "deathLoop", 36 ],
			[ "down", 62 ],
			[ "downwait", 63 ],
			[ "downdamage", 64 ],
			[ "wakeup", 65 ],
			[ "downdie", 66 ]
		] :
		[ [ "stand", 0 ] ];
	const animationStates = {};
	const missingOptionalAnimationPaths = [];
	for (
		const [role, stateId] of [
			...movementRules,
			...NATIVE_IDLE_STATE_CLIPS.map( ( { role, stateId } ) => [ role, stateId ] ),
			...NATIVE_EMOTE_STATE_CLIPS.filter( row => row.stateId === 50 ).map( (
				{ role, stateId }
			) => [ role, stateId ] )
		]
	) {
		const state = findDefaultAnimationState( bsr, stateId );
		if ( !state?.animationPath ) {
			if ( isMob && requiredStates.includes( stateId ) ) {
				throw new Error( `${bsrPath} has no authored default animation state ${stateId} (${role})` );
			}
			continue;
		}
		let clip;
		if ( !isMob && stateId === 0 ) {
			// Native CPrimAnimation opens BAN resources lazily. A town NPC's BSR
			// and mesh remain valid when its optional idle archive is absent or
			// malformed; only that animation lane is unavailable. The previous
			// build collapsed this leaf failure into a missing world object.
			const animationBytes = await loadOptionalDataAsset( state.animationPath );
			try {
				clip = animationBytes === null ?
					null :
					parseBan( animationBytes, state.animationPath );
			} catch ( error ) {
				console.warn(
					`[npc] optional NPC stand animation unreadable: ${state.animationPath} ` +
						`(model ${bsrPath} remains valid): ${error instanceof Error ? error.message : String( error )}`
				);
				clip = null;
			}
			if ( clip === null ) {
				missingOptionalAnimationPaths.push( state.animationPath );
				continue;
			}
		} else {
			clip = parseBan( await loadDataAsset( state.animationPath ), state.animationPath );
		}
		clips.push( { role, path: state.animationPath, clip } );
		animationStates[role] = {
			stateId,
			durationMs: clip.durationMs,
			soundEvents: pickDefaultSetSoundEvents( bsr, stateId ),
			...pickDefaultSetStateTableMetadata( bsr, stateId )
		};
	}
	for ( const motion of pickAttachedMotionClips( bsr ) ) {
		const clip = parseBan( await loadDataAsset( motion.path ), motion.path );
		clips.push( { role: motion.role, path: motion.path, clip } );
		animationStates[motion.role] = {
			stateId: motion.id,
			durationMs: clip.durationMs,
			loop: clip.field2 !== 0,
			soundEvents: pickSetSoundEvents( bsr, motion.set, motion.id ),
			...pickAnimationStateTableMetadata( motion.state ),
			trackEvents: pickSetTrackEvents( bsr, motion.state, motion.id )
		};
	}
	if ( isMob ) {
		// A monster skill names its motion by ANI_* state (skilleffect.txt animation
		// rows, data_ccd620): ANI_ATTACK5..9 are 183..190, beyond the movement table
		// above. CICharactor_PlayAnimationByMotionId (85ED80) plays any state the
		// BSR's default set authors, so publish each one a skill can name under its
		// native role; the client resolves native:default:<id> directly
		// (skill-motion-resolve.ts). Captain Ivy's ATTACK05..08 had no timeline.
		const published = new Set( Object.values( animationStates ).map( state => state.stateId ) );
		const reachable = new Set( SKILL_EFFECT_ANIMATION_ID_BY_NAME.values() );
		for ( const state of findDefaultAnimationSet( bsr )?.states ?? [] ) {
			if ( !state.animationPath || published.has( state.stateId ) || !reachable.has( state.stateId ) ) continue;
			published.add( state.stateId );
			const role = `native:default:${state.stateId}`;
			// A state that reuses another state's BAN gets its own clip: the runtime
			// keys a state's metadata by clip name.
			const shared = clips.find( clip => clip.path === state.animationPath );
			const clip = shared?.clip ?? parseBan( await loadDataAsset( state.animationPath ), state.animationPath );
			clips.push( { role, path: state.animationPath, clip } );
			animationStates[role] = {
				stateId: state.stateId,
				durationMs: clip.durationMs,
				soundEvents: pickDefaultSetSoundEvents( bsr, state.stateId ),
				...pickDefaultSetStateTableMetadata( bsr, state.stateId )
			};
		}
	}
	const allowedClips = Object.keys( animationStates );

	if ( isMob ) {
		// Ship the authored family once. Runtime selection remains constrained to
		// the state-id-derived allowedClips above; filenames never confer behavior.
		const usedPaths = new Set( clips.map( ( clip ) => clip.path ) );
		for ( const animPath of avatar.animationPaths ) {
			if ( usedPaths.has( animPath ) ) continue;
			usedPaths.add( animPath );
			const base = path.basename( animPath, ".ban" ).toLowerCase();
			const role = base.startsWith( `${path.basename( bsrPath, ".bsr" )}_` ) ?
				base.slice( path.basename( bsrPath, ".bsr" ).length + 1 ) :
				base;
			const roleOwner = clips.find( ( clip ) => clip.role === role );
			if ( roleOwner ) {
				throw new Error(
					`${bsrPath} maps animation role "${role}" to both ${roleOwner.path} and ${animPath}`
				);
			}
			// Native CPrimAnimation stores every BSR-authored path but opens each
			// BAN lazily (sub_a6c500 -> sub_a6c2d0). An archive miss returns 0 for
			// that clip; it does not invalidate the model or its other states. Keep
			// the required stand/walk/run lane strict above, while preserving that
			// per-optional-clip failure boundary here. This matters for shipped data
			// such as bluetiger state 79's authored "stnad01" typo.
			const animationBytes = await loadOptionalDataAsset( animPath );
			if ( animationBytes === null ) {
				missingOptionalAnimationPaths.push( animPath );
				console.warn(
					`[npc] optional animation absent in archive: ${animPath} (model ${bsrPath} remains valid)`
				);
				continue;
			}
			try {
				clips.push( { role, path: animPath, clip: parseBan( animationBytes, animPath ) } );
			} catch ( error ) {
				missingOptionalAnimationPaths.push( animPath );
				console.warn(
					`[npc] optional animation unreadable: ${animPath} (model ${bsrPath} remains valid): ` +
						`${error instanceof Error ? error.message : String( error )}`
				);
			}
		}
	}

	const animationBindings = await authoredAnimationBindings( bsr, clips, loadOptionalDataAsset );
	for ( const binding of animationBindings ) {
		if ( binding.clip === null && binding.path && !missingOptionalAnimationPaths.includes( binding.path ) ) {
			missingOptionalAnimationPaths.push( binding.path );
		}
	}

	// Mission PathCtl owns NPC world displacement. Export locomotion as an
	// in-place pose so the skeleton cannot apply the same travel a second time.
	// Attack, hit, death, and other authored root motion remain untouched.
	const glb = avatarToGlb( {
		...avatar,
		clips,
		inPlaceHorizontalRootMotionRoles: [ "walk", "run" ]
	} );
	writeIntoPublicTreeSync( diskPath, glb );
	const materialVariants = {};
	if ( isMob ) {
		for ( const [slot, materialPath] of materialSets ) {
			if ( slot === 0 ) continue;
			const materials = await loadMaterialTextures( [ materialPath ], {
				onWarning: message => {
					throw Error( message );
				}
			} );
			const variant = avatarToGlb( {
				...avatar,
				materials,
				clips,
				inPlaceHorizontalRootMotionRoles: [ "walk", "run" ]
			} );
			const suffix = `.material-${slot}.glb`;
			writeIntoPublicTreeSync( diskPath.replace( /\.glb$/, suffix ), variant );
			materialVariants[slot] = publicPath.replace( /\.glb$/, suffix );
		}
	}
	return {
		glb: publicPath,
		bytes: glb.length,
		bones: avatar.skeleton.boneCount,
		clips: clips.map( ( clip ) => clip.role ),
		materials: avatar.materials.size,
		allowedClips,
		animationStates,
		animationBindings,
		modifierSets: bsr.modifierSets,
		particleModifiers: bsr.particleModifiers,
		materialModifiers: bsr.materialModifiers,
		textureModifiers: bsr.textureModifiers,
		...(Object.keys( materialVariants ).length ? { materialVariants } : {}),
		...(!isMob && allowedClips.length === 0 ? { staticPose: true } : {}),
		retailAnimationCatalog: buildRetailAnimationCatalog( bsr, clips ),
		...(missingOptionalAnimationPaths.length > 0 ? { missingOptionalAnimationPaths } : {})
	};
}

/*
================
buildNpcModelAssets
================
*/
export async function buildNpcModelAssets( options = {} ) {
	// A scratch output root lets a branch verify its bake without rewriting
	// the shared tree's manifest; the default is the published tree.
	const publicAssets = options.publicAssetsRoot ?? publicAssetsRoot;
	const eventRain = parseWeatherEvents( fs.readFileSync( path.join( textdataDir, "skilleffect.txt" ), "utf16le" ) );
	const skipTextures = options.skipTextures ?? false;
	if ( !skipTextures ) await convertTextureTrees( "npc", [ "prim/mtrl" ] );

	// Runtime rosters, not codename prefixes, decide what is built. Load the
	// complete RefObjChar identity table so native NPC-band structure rows and
	// clone base links remain resolvable without a second classifier.
	const rows = loadCharacterDataRows( textdataDir, { codenamePattern: /./ } );
	const characterInfo = loadCharacterInfo();
	const resolveCharacterInfo = characterInfoResolver( characterInfo, rows );
	const npcRoster = loadSpawnableNpcRoster();
	const mobRoster = loadModelledMobRoster();
	const mobRosterByCodename = new Map( mobRoster.map( ( ref ) => [ ref.codename, ref ] ) );
	// 582110: growth pets and hidden transports route action 1 to state 50.
	// Publish every enabled reference, sharing the native BSR bake across levels.
	const cosRoster = enabledCosReferences( rows ).map( ( { codename, refObjId } ) => ({
		codename,
		refObjId,
		rideModelPath: resolveCharacterInfo( codename )?.rideModelPath,
		riderTransformMode: resolveCharacterInfo( codename )?.riderTransformMode
	}) );
	const cosNames = new Set( cosRoster.map( row => row.codename ) );
	const structureRoster = loadFortressStructureRoster();
	const structureNames = new Set( structureRoster.map( row => row.codename ) );
	const structureEffects = parseStructureEffects(
		fs.readFileSync( path.join( textdataDir, "atstructeffect.txt" ), "utf16le" ),
		codename => structureNames.has( codename )
	);
	const roster = [ ...npcRoster, ...mobRoster, ...cosRoster, ...structureRoster ];
	if ( new Set( roster.map( ( ref ) => ref.codename ) ).size !== roster.length ) {
		throw new Error( "[npc] server NPC and monster rosters contain an overlapping codename" );
	}
	if ( new Set( roster.map( ( ref ) => ref.refObjId ) ).size !== roster.length ) {
		throw new Error( "[npc] server NPC and monster rosters contain an overlapping RefObj identity" );
	}
	const manifestPath = path.join( publicAssets, "npc", "manifest.json" );
	const previousManifest = readJsonOrNullSync( manifestPath );
	const previousGlbPaths = readPreviousResourceGlbPaths( manifestPath );
	const models = [];
	let builtResources = 0;
	let coveredModels = 0;
	let reusedModels = 0;
	/** bsrPath -> policy + baked result, so _CLON codenames reuse the base GLB. */
	const bakedByBsr = new Map();
	/** Browser output -> BSR identity, guarding every primary and ride write. */
	const outputOwners = new Map();
	/** Full BSR animation metadata stays outside the production-hot model manifest. */
	const retailAnimationResources = new Map();
	const retailAnimationModels = new Map();
	/** Secondary BSR -> the characterInfo records that require it. */
	const rideResources = new Map();
	/** characterInfo death BSR -> the codenames whose record names it. */
	const deathResources = new Map();
	for ( const rosterRef of roster ) {
		const { codename } = rosterRef;
		const model = resolveNpcModel( codename, rows );
		if ( !model ) {
			console.warn( `[npc] ${codename}: no characterdata row / bsr path` );
			models.push( { codename, error: "unresolved" } );
			continue;
		}
		// characterdata enables the small guard towers (STRUCTURE_SMALL_*_TOWER),
		// whose BSRs the v1.150 Data.pk2 does not hold: the client cannot draw
		// them, so they are not part of this client's world.
		if ( structureNames.has( codename ) && (await loadOptionalDataAsset( model.bsrPath )) === null ) {
			console.log( `[npc] SKIP ${codename}: v1.150 data ships no ${model.bsrPath}` );
			continue;
		}
		const isCos = cosNames.has( codename );
		const isMob = mobRosterByCodename.has( codename ) || isCos;
		const output = resourceGlbOutput( model.bsrPath, {
			namespace: "npc",
			publicAssetsRoot: publicAssets
		} );
		claimResourceOutput( outputOwners, model.bsrPath, output.publicPath );
		const publicPath = output.publicPath;
		const info = resolveCharacterInfo( codename );
		const soundProfileName = info?.soundProfileName;
		if ( !soundProfileName && isMob ) {
			throw new Error( `[npc] ${codename}: no skilleffect characterInfo ResourceTypeName` );
		}
		if ( !soundProfileName ) {
			console.warn(
				`[npc] ${codename}: no skilleffect characterInfo row; ` +
					"publishing the visual without an invented sound/action profile"
			);
		}
		const mediaRideModelPath = info?.rideModelPath ?? null;
		const serverRideModelPath = normalizeBsrPath( rosterRef?.rideModelPath ) ?? null;
		const mediaTransformMode = Number( info?.riderTransformMode ?? 0 );
		const serverTransformMode = Number( rosterRef?.riderTransformMode ?? 0 );
		if (
			mediaRideModelPath !== serverRideModelPath ||
			(mediaRideModelPath !== null && mediaTransformMode !== serverTransformMode)
		) {
			throw new Error(
				`[npc] ${codename}: server/media ride contract drift ` +
					`(server=${serverRideModelPath ?? "none"}/${serverTransformMode}, ` +
					`media=${mediaRideModelPath ?? "none"}/${mediaTransformMode})`
			);
		}
		if ( serverRideModelPath ) {
			const priorRide = rideResources.get( serverRideModelPath ) ?? {
				bsrPath: serverRideModelPath,
				requiredBy: [],
				transformModes: new Set()
			};
			priorRide.requiredBy.push( codename );
			priorRide.transformModes.add( serverTransformMode );
			rideResources.set( serverRideModelPath, priorRide );
		}
		const deathModelPath = info?.deathModelPath ?? null;
		if ( deathModelPath ) {
			const priorDeath = deathResources.get( deathModelPath ) ?? { bsrPath: deathModelPath, requiredBy: [] };
			priorDeath.requiredBy.push( codename );
			deathResources.set( deathModelPath, priorDeath );
		}
		const kind = isCos ? "cos" : isMob ? "monster" : structureNames.has( codename ) ? "structure" : "npc";
		const entry = {
			codename,
			refObjId: model.refObjId,
			eventRain: eventRain.get( codename ) ?? eventRain.get( model.baseCodename ) ?? false,
			kind,
			bsr: model.bsrPath,
			glb: publicPath,
			...(soundProfileName ? { soundProfileName } : {}),
			...(deathModelPath ? { deathModel: deathModelPath } : {})
		};
		if ( model.refObjId !== rosterRef.refObjId ) {
			throw new Error(
				`[npc] ${codename}: server/media RefObj identity drift ` +
					`(server=${rosterRef.refObjId}, media=${model.refObjId})`
			);
		}
		retailAnimationModels.set( codename, {
			codename,
			refObjId: model.refObjId,
			kind,
			bsr: model.bsrPath
		} );
		if ( isMob ) {
			// Scale percent from characterdata col[48] (tiger 100 / tiger_clon
			// 80). Data passthrough only - applying it is the spawn plane's leg.
			entry.scalePercent = model.scalePercent;
			entry.materialKind = model.materialKind;
			if ( model.baseCodename ) entry.baseCodename = model.baseCodename;
		}
		const prior = bakedByBsr.get( model.bsrPath );
		if ( prior ) {
			if ( prior.isMob !== isMob ) {
				throw new Error(
					`[npc] ${model.bsrPath}: shared by NPC and monster consumers with incompatible clip policies`
				);
			}
			Object.assign( entry, prior.baked );
			coveredModels += 1;
			reusedModels += 1;
			console.log( `[npc] OK   ${codename.padEnd( 20 )} -> ${publicPath} (shared bake)` );
			models.push( entry );
			continue;
		}
		try {
			const baked = await bakeCharacterResource( model.bsrPath, output, isMob );
			const { retailAnimationCatalog, ...missionBaked } = baked;
			Object.assign( entry, missionBaked );
			bakedByBsr.set( model.bsrPath, { isMob, baked: missionBaked } );
			retailAnimationResources.set( model.bsrPath, {
				bsr: model.bsrPath,
				glb: missionBaked.glb,
				animations: retailAnimationCatalog
			} );
			builtResources += 1;
			coveredModels += 1;
			console.log(
				`[npc] OK   ${codename.padEnd( 20 )} -> ${entry.glb} (${entry.bytes} B, clips=[${entry.clips}])`
			);
		} catch ( error ) {
			entry.error = String( error?.message ?? error );
			console.warn( `[npc] FAIL ${codename.padEnd( 20 )} ${entry.error}` );
		}
		models.push( entry );
	}

	// Secondary models are first-class resource entries keyed by normalized
	// BSR path. This mirrors ResourceManager lookup at action-effect +0x24 and
	// avoids inventing a second RefObj/codename identity for packetless rides.
	// CICharactor_Action_KnockdownDie (8E64F0) reloads a dying body from its
	// characterInfo death BSR the same way; death models bake with the monster
	// clip policy but require death 4 and deathLoop 36 instead of stand.
	const secondaryResources = [
		...[ ...rideResources.values() ].map( ride => ({
			bsrPath: ride.bsrPath,
			kind: "ride",
			isMob: true,
			requiredStates: BODY_REQUIRED_STATES,
			fields: {
				requiredBy: ride.requiredBy,
				riderTransformModes: [ ...ride.transformModes ].sort( ( a, b ) => a - b )
			}
		}) ),
		...[ ...deathResources.values() ].map( death => ({
			bsrPath: death.bsrPath,
			kind: "death",
			isMob: true,
			requiredStates: DEATH_REQUIRED_STATES,
			fields: { requiredBy: death.requiredBy }
		}) ),
		...loadBoothModelRoster( textdataDir )
	];
	const secondary = await bakeNpcSecondaryResources( {
		publicAssetsRoot: publicAssets,
		models,
		bakedByBsr,
		outputOwners,
		retailAnimationModels,
		retailAnimationResources,
		bake: bakeCharacterResource
	}, secondaryResources );
	builtResources += secondary.built;
	coveredModels += secondary.covered;
	reusedModels += secondary.reused;

	// CICATStruct_SetVisualStage (4F78A0) reloads the model from the stage's
	// atstructeffect BSR, falling back to the record's own; stage models are
	// path-keyed resources like the rides, baked with the static NPC policy.
	for ( const entry of models ) {
		const effects = entry.kind === "structure" && !entry.error ? structureEffects.get( entry.codename ) : undefined;
		if ( !effects ) continue;
		const stages = {};
		for ( const [stage, bsrPath] of Object.entries( effects.stages ) ) {
			const prior = bakedByBsr.get( bsrPath );
			if ( prior ) {
				if ( prior.isMob ) throw new Error( `[npc] ${bsrPath}: structure stage shared with a monster bake` );
				stages[stage] = { glb: prior.baked.glb, particleModifiers: prior.baked.particleModifiers };
				reusedModels += 1;
				continue;
			}
			const output = resourceGlbOutput( bsrPath, { namespace: "npc", publicAssetsRoot: publicAssets } );
			claimResourceOutput( outputOwners, bsrPath, output.publicPath );
			try {
				const { retailAnimationCatalog: _catalog, ...baked } = await bakeCharacterResource(
					bsrPath,
					output,
					false
				);
				bakedByBsr.set( bsrPath, { isMob: false, baked } );
				stages[stage] = { glb: baked.glb, particleModifiers: baked.particleModifiers };
				builtResources += 1;
				console.log( `[npc] OK   ${entry.codename} stage ${stage} -> ${baked.glb}` );
			} catch ( error ) {
				entry.error = `stage ${stage} ${bsrPath}: ${error?.message ?? error}`;
				console.warn( `[npc] FAIL ${entry.codename} ${entry.error}` );
			}
		}
		entry.structureStages = stages;
		entry.structureSounds = effects.sounds;
		entry.structureDamageEffects = effects.levels;
	}

	const preservedVat = preserveFreshNpcVatReferences( models, previousManifest, { publicAssetsRoot: publicAssets } );
	const { models: referenceRows, resources, boothModels } = splitNpcManifestModels( models );
	const manifest = {
		format: NPC_MANIFEST_FORMAT,
		version: NPC_MANIFEST_VERSION,
		source:
			"server NPC spawn roster + server-exported spawnable monster roster; PathCtl-owned in-place horizontal locomotion, complete native default CResAnimationStateTable event-map/time-warp payloads, BSR ModDataSound cursor tracks, and the unified skilleffect characterInfo sound + CICRide resource contract",
		count: models.length,
		builtCount: builtResources,
		coveredCount: coveredModels,
		reusedCount: reusedModels,
		models: referenceRows,
		resources,
		boothModels,
		...(preservedVat.contract ? { vat: preservedVat.contract } : {})
	};
	const failures = models.filter( ( model ) => model.error );
	if ( failures.length > 0 ) {
		throw new Error(
			`[npc] required model coverage incomplete; valid manifests were not replaced: ${
				failures
					.map( ( model ) => `${model.codename}: ${model.error}` )
					.join( "; " )
			}`
		);
	}
	fs.mkdirSync( path.dirname( manifestPath ), { recursive: true } );
	writeJsonIfChangedSync( manifestPath, manifest );
	console.log(
		`[npc] covered ${coveredModels}/${models.length} entries from ${builtResources} unique GLBs ` +
			`(${reusedModels} shared); manifest -> ${path.relative( gameRoot, manifestPath )}`
	);
	if ( preservedVat.preserved > 0 || preservedVat.stale > 0 ) {
		console.log(
			`[npc] preserved ${preservedVat.preserved} fresh VAT reference(s); ` +
				`dropped ${preservedVat.stale} stale reference(s)`
		);
	}
	const animationCatalog = {
		format: "sro-retail-character-animation-catalog",
		version: 2,
		source: "native BSR default animation-set state records joined to unique exported GLB clips and BAN loopType",
		modelCount: retailAnimationModels.size,
		resourceCount: retailAnimationResources.size,
		models: Object.fromEntries( retailAnimationModels ),
		resources: Object.fromEntries( retailAnimationResources )
	};
	const animationCatalogPath = path.join( publicAssets, "npc", "animation-catalog.json" );
	writeJsonIfChangedSync( animationCatalogPath, animationCatalog );
	await refreshPrecompressedSidecars( [ manifestPath, animationCatalogPath ], { onlyWhenStale: true } );
	console.log( `[npc] retail animation catalog -> ${path.relative( gameRoot, animationCatalogPath )}` );
	const removed = removeSupersededResourceOutputs( {
		previousPublicPaths: previousGlbPaths,
		currentPublicPaths: models.flatMap( (
			model
		) => [
			model.glb,
			...Object.values( model.materialVariants ?? {} ),
			...Object.values( model.structureStages ?? {} ).map( stage => stage.glb )
		] ).filter( Boolean ),
		namespace: "npc",
		publicAssetsRoot: publicAssets
	} );
	if ( removed.length > 0 ) {
		console.log( `[npc] removed ${removed.length} superseded GLB output(s)` );
	}
	return {
		built: builtResources,
		covered: coveredModels,
		reused: reusedModels,
		modelCount: models.length,
		manifestPath,
		animationCatalogPath
	};
}

if ( isMainScript( import.meta.url ) ) {
	const rootFlag = process.argv.find( arg => arg.startsWith( "--public-assets-root=" ) );
	await buildNpcModelAssets( {
		skipTextures: process.argv.includes( "--skip-textures" ),
		...(rootFlag ? { publicAssetsRoot: path.resolve( rootFlag.slice( "--public-assets-root=".length ) ) } : {})
	} );
}
