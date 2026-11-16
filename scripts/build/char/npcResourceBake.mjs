/*
===========================================================================

npcResourceBake.mjs - one NPC, monster or COS resource baked to GLB

The resource half of the NPC model bake (buildNpcModelAssets.mjs owns
the rosters, the publication and the manifests): bakeCharacterResource
turns one .bsr or .cpd into a GLB plus its material-set variants, and
npcResourceAction wraps that bake as a cached action (graph/action.mjs)
so a warm build lays the stored outputs down instead of baking again.

The tables a bake reads that no single model owns (the attached-motion
requests, the monster weapon-skill states, the tomb posture) are chosen
once outside every action (bakeTables) and keyed as its parameters; the
files it reads are discovered as it runs and checked on reuse.

===========================================================================
*/

import { writeIntoPublicTreeSync } from "../shared/publicWrite.mjs";
import { authoredAnimationBindings } from "./authoredAnimationBindings.mjs";
import {
	defaultAttachedMotionRequests,
	pickAttachedMotionClips,
	attachedMotionMetadata
} from "./attachedMotionClips.mjs";
import fs from "node:fs";
import { fileURLToPath } from "node:url";
import { characterMaterialVariants } from "./materialVariants.mjs";
import path from "node:path";
import {
	findAnimationSet,
	findDefaultAnimationSet,
	findDefaultAnimationState,
	pickAnimationSetSoundEvents,
	pickDefaultSetSoundEvents,
	pickAnimationStateTableMetadata,
	pickDefaultSetStateTableMetadata
} from "./animationUtils.mjs";
import { assembleAvatar, primSlot, NATIVE_IDLE_STATE_CLIPS, NATIVE_EMOTE_STATE_CLIPS } from "./buildAvatar.mjs";
import { assembleStaticBsrModel } from "./compileBsrVisual.mjs";
import { avatarToGlb } from "./exportGlb.mjs";
import { assembleCompoundCharacter } from "./itemAttachments.mjs";
import { parseSkillAniSet } from "./parseSkillEffect.mjs";
import { parseBan, parseCharacterBsr } from "./formats.mjs";
import { SKILL_EFFECT_ANIMATION_ID_BY_NAME } from "./native/skillEffectAnimationRegistry.ts";
import { dataAssetPath, dataRoot, loadDataAsset, loadMaterialTextures } from "../shared/jmxAssetIO.mjs";
import { runGraph } from "../graph/graph.mjs";
import { claimPublicFile } from "../shared/publicationLedger.mjs";
import { normalizeAssetPath } from "../shared/assetPaths.mjs";
import { loadOptionalDataAsset } from "../shared/optionalDataAsset.mjs";
import { qinshiTombCombatEnabled } from "../shared/qinshiTomb.mjs";
import { imageSourceRoot, retailTextdataRoot } from "../world/paths.mjs";

const MODULE_FILE = fileURLToPath( import.meta.url );
const textdataDir = retailTextdataRoot;
// The trees one resource's bake reads: extracted game data and converted
// images (the native texture cache lives under the latter).
const MODEL_READ_ROOTS = { data: dataRoot, images: imageSourceRoot };

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
	const defaultStates = defaultSet?.states ?? [];
	const normalizePath = normalizeAssetPath;

	return clips.map( ( { role, path: clipPath, clip } ) => {
		const normalizedClipPath = normalizePath( clipPath );
		const setMatch = /^native:([^:]+):\d+$/.exec( role );
		const setStates = setMatch ?
			(findAnimationSet( bsr, setMatch[1] )?.states ?? defaultStates) :
			defaultStates;
		return {
			role,
			durationMs: clip.durationMs,
			looping: clip.field2 === 1,
			stateIds: setStates
				.filter( ( state ) => normalizePath( state.animationPath ) === normalizedClipPath )
				.map( ( state ) => state.stateId )
		};
	} );
}

// The default-set motions bakeCharacterResource requires of a monster resource.
export const BODY_REQUIRED_STATES = [ 0 ];
export const DEATH_REQUIRED_STATES = [ 4, 36 ];
const nonDefaultSkillStatesByTomb = new Map();

/*
================
nonDefaultMonsterSkillStates

0x008e83f0 (CCObjCharacter_ResolveWeaponAnimationPrefix) keeps a monster or
COS compound's active weapon prefix at "default" (equipped item count +0x74 is
0), while CSkillEffectMgr / 8E7150 (skill-motion-resolve.ts) dispatches
skillaniset2's AniGroup (SWORD or SPEAR on Tomb .cpd skills) by (set, stateId).
Collect the exact (set -> Set<stateId>) pairs referenced by MSKILL_* rows so a
.cpd compound bakes only its reachable weapon-skill states rather than all 12
player weapon sets (~580 clips).
================
*/
function nonDefaultMonsterSkillStates() {
	const tombKey = qinshiTombCombatEnabled();
	let cached = nonDefaultSkillStatesByTomb.get( tombKey );
	if ( cached ) return cached;
	cached = new Map();
	const aniMap = parseSkillAniSet( path.join( textdataDir, "skilleffect.txt" ) );
	for ( const [name, row] of aniMap ) {
		if ( !/^MSKILL_/i.test( name ) ) continue;
		const set = String( row.aniGroup ?? "default" ).toLowerCase();
		if ( set === "default" ) continue;
		for ( const animName of [ ...row.readyAnims, ...row.waitAnims, ...row.shotAnims, ...row.actionWaitAnims ] ) {
			const id = SKILL_EFFECT_ANIMATION_ID_BY_NAME.get( animName );
			if ( id === undefined ) continue;
			let ids = cached.get( set );
			if ( !ids ) {
				ids = new Set();
				cached.set( set, ids );
			}
			ids.add( id );
		}
	}
	nonDefaultSkillStatesByTomb.set( tombKey, cached );
	return cached;
}

/*
================
bakeTables

The tables a resource bake reads that no single model owns: the
attached-motion requests and the monster weapon-skill states, both from
skilleffect.txt under the shard's tomb posture, and that posture itself.
They are read once, outside every cached bake, and handed to each one,
so they are part of its key (npcResourceAction) instead of an untracked
read inside it.
================
*/
export function bakeTables() {
	return {
		attachedMotionRequests: defaultAttachedMotionRequests(),
		skillStates: nonDefaultMonsterSkillStates(),
		tombCombat: qinshiTombCombatEnabled()
	};
}

/*
================
materialVariantPath

A body GLB's material-set variant beside it: x.glb -> x.material-<slot>.glb.
================
*/
function materialVariantPath( glbPath, slot ) {
	return glbPath.replace( /\.glb$/, `.material-${slot}.glb` );
}

/*
================
skillStateParam

tables.skillStates as plain sorted JSON for a key: [[set, [ids]], ...].
================
*/
function skillStateParam( skillStates ) {
	return [ ...skillStates ]
		.map( ( [set, ids] ) => [ set, [ ...ids ].sort( ( a, b ) => a - b ) ] )
		.sort( ( a, b ) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0) );
}

/*
================
materialVariantSlots

The material-set slots a resource's bake publishes a variant GLB for:
a monster-policy .bsr's non-default slots whose BMT the archive holds
(bakeCharacterResource's variant loop). An action declares its outputs
before it runs, so this is read ahead of the bake from the BSR's material
section, and the bake refuses to write any other set. A compound (.cpd)
has no variants.
================
*/
export async function materialVariantSlots( bsrPath, isMob ) {
	if ( !isMob || /\.cpd$/i.test( bsrPath ) ) return [];
	const source = await loadOptionalDataAsset( bsrPath );
	// No BSR: the bake fails on it with its own message.
	if ( source === null ) return [];
	const slots = [];
	for ( const [slot, materialPath] of characterMaterialVariants( source, bsrPath ) ) {
		if ( slot !== 0 && fs.existsSync( dataAssetPath( materialPath ) ) ) slots.push( slot );
	}
	return slots.sort( ( a, b ) => a - b );
}

/*
================
npcResourceAction

One character resource bake as a cached action (graph/action.mjs). Its
key is this module's code closure and the request: the BSR, its clip
policy and required states, the published path, the variant slots, and
the tables chosen outside it (bakeTables): the weapon-skill states only
where the bake reads them (a monster policy or a compound), the tomb
posture only for a compound, whose missing degree-10 branches it
substitutes. The files it reads (the BSR and everything it names, the
converted textures) are discovered as it runs and checked on reuse.
================
*/
export function npcResourceAction( request, tables = bakeTables() ) {
	const { bsrPath, output, isMob, publicAssetsRoot } = request;
	const requiredStates = request.requiredStates ?? BODY_REQUIRED_STATES;
	const variantSlots = request.variantSlots ?? [];
	const isCpd = /\.cpd$/i.test( bsrPath );
	const files = [ output.diskPath, ...variantSlots.map( slot => materialVariantPath( output.diskPath, slot ) ) ];
	return {
		name: `npc:${bsrPath}`,
		outputRoot: publicAssetsRoot,
		outputs: files.map( file => path.relative( publicAssetsRoot, file ).split( path.sep ).join( "/" ) ),
		code: [ MODULE_FILE ],
		params: {
			bsrPath,
			isMob,
			requiredStates,
			publicPath: output.publicPath,
			variantSlots,
			attachedMotionRequests: tables.attachedMotionRequests,
			...(isMob || isCpd ? { skillStates: skillStateParam( tables.skillStates ) } : {}),
			...(isCpd ? { tombCombat: tables.tombCombat } : {})
		},
		discoverRoots: MODEL_READ_ROOTS,
		run: async () => {
			const baked = await bakeCharacterResource( bsrPath, output, isMob, requiredStates, tables );
			const written = Object.keys( baked.materialVariants ?? {} ).map( Number ).sort( ( a, b ) => a - b );
			if ( written.join( "," ) !== variantSlots.join( "," ) ) {
				throw new Error( `${bsrPath}: baked material variants [${written}], declared [${variantSlots}]` );
			}
			return baked;
		}
	};
}

/*
================
cachedBake

bakeCharacterResource's signature over the action store: each call runs
one npcResourceAction, claims its outputs for the publication (a hit
lays them down without going through publicWrite.mjs) and returns the
bake's result. The result goes through JSON either way, so a miss and a
hit hand the manifest the same values. stats.stored counts the hits.
================
*/
export function cachedBake( { store, publicAssetsRoot, tables, stats } ) {
	return async ( bsrPath, output, isMob, requiredStates = BODY_REQUIRED_STATES ) => {
		const variantSlots = await materialVariantSlots( bsrPath, isMob );
		const request = { bsrPath, output, isMob, requiredStates, variantSlots, publicAssetsRoot };
		const action = npcResourceAction( request, tables );
		const outcome = (await runGraph( [ action ], { store } ))[action.name];
		for ( const name of action.outputs ) claimPublicFile( path.join( publicAssetsRoot, ...name.split( "/" ) ) );
		if ( outcome.hit ) stats.stored += 1;
		return JSON.parse( JSON.stringify( outcome.result ) );
	};
}

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
export async function bakeCharacterResource(
	bsrPath,
	output,
	isMob,
	requiredStates = BODY_REQUIRED_STATES,
	tables = bakeTables()
) {
	const { publicPath, diskPath } = output;
	const isCpd = /\.cpd$/i.test( bsrPath );
	const source = await loadDataAsset( bsrPath );
	let bsr;
	let materialSets;
	let avatar;
	if ( isCpd ) {
		const assembled = await assembleCompoundCharacter( bsrPath, source, { tombCombat: tables.tombCombat } );
		const skillStatesBySet = tables.skillStates;
		const scopedSets = [];
		for ( const set of assembled.bsr.animationSets ?? [] ) {
			const setKey = set.name.toLowerCase();
			if ( setKey === "default" ) {
				scopedSets.push( set );
				continue;
			}
			const wantedIds = skillStatesBySet.get( setKey );
			if ( !wantedIds ) continue;
			const states = set.states.filter( ( state ) => wantedIds.has( state.stateId ) );
			if ( states.length > 0 ) {
				scopedSets.push( { ...set, states } );
			}
		}
		bsr = { ...assembled.bsr, animationSets: scopedSets };
		materialSets = assembled.materialSets;
		avatar = assembled.avatar;
	} else {
		bsr = parseCharacterBsr( source, bsrPath );
		materialSets = characterMaterialVariants( source, bsrPath );
		avatar = bsr.skeletonPath ?
			await assembleAvatar( bsrPath, {
				noClips: true,
				materialSetPaths: materialSets.has( 0 ) ? [ materialSets.get( 0 ) ] : [],
				rigidUnboundMeshes: true,
				slotForMesh: ( _mp, i ) => primSlot( i )
			} ) :
			await assembleStaticBsrModel( bsrPath );
	}
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
	for ( const motion of pickAttachedMotionClips( bsr, tables.attachedMotionRequests ) ) {
		const clip = parseBan( await loadDataAsset( motion.path ), motion.path );
		clips.push( { role: motion.role, path: motion.path, clip } );
		animationStates[motion.role] = {
			stateId: motion.id,
			durationMs: clip.durationMs,
			loop: clip.field2 !== 0,
			...attachedMotionMetadata( bsr, motion )
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
		for ( const set of bsr.animationSets ?? [] ) {
			const setKey = set.name.toLowerCase();
			if ( setKey === "default" ) continue;
			const wantedIds = tables.skillStates.get( setKey );
			if ( !wantedIds ) continue;
			for ( const state of set.states ) {
				if ( !state.animationPath || !wantedIds.has( state.stateId ) ) continue;
				const role = `native:${setKey}:${state.stateId}`;
				if ( animationStates[role] ) continue;
				const shared = clips.find( ( clip ) => clip.path === state.animationPath );
				const clip = shared?.clip ??
					parseBan( await loadDataAsset( state.animationPath ), state.animationPath );
				clips.push( { role, path: state.animationPath, clip } );
				animationStates[role] = {
					stateId: state.stateId,
					durationMs: clip.durationMs,
					soundEvents: pickAnimationSetSoundEvents( bsr, setKey, state.stateId ),
					...pickAnimationStateTableMetadata( state )
				};
			}
		}
	}
	const allowedClips = Object.keys( animationStates );

	if ( isMob && !isCpd ) {
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
			// BAN lazily (CResChar_ForEachLinkedItem_Helper -> CResAnimationClip_LoadBan). An archive miss returns 0 for
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
			// A variant slot whose BMT the archive does not hold publishes no
			// variant (v1.696's mujigi_koold.bsr names a _clon set it never
			// shipped). INFERENCE: the native loader opens a material set
			// only when a model asks for that slot and draws its default set
			// when the file is absent, as it does for a missing BAN; the
			// client already falls back to the body's GLB for a slot with
			// no variant (appearance-lookup.ts resourceFor).
			if ( (await loadOptionalDataAsset( materialPath )) === null ) {
				console.warn( `[npc] optional material set absent in archive: ${materialPath} (model ${bsrPath})` );
				missingOptionalAnimationPaths.push( materialPath );
				continue;
			}
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
			writeIntoPublicTreeSync( materialVariantPath( diskPath, slot ), variant );
			materialVariants[slot] = materialVariantPath( publicPath, slot );
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
