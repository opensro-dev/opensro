/*
===========================================================================
buildLocomotionBanAssets.mjs - publish native character motion clips and metadata.
BSR state tables own clip selection; the shared modifier selector owns sound
binding fallback. BAN bytes remain unchanged for the browser native loader.
===========================================================================
*/
import { loadAvatarVisualOverrides, avatarAnimationRequirements } from "./avatarVisualOverrides.mjs";
import { writeIntoPublicTreeSync } from "../shared/publicWrite.mjs";
import { loadEquipmentRecords } from "./equipmentVisualRecords.mjs";
import { pickAttachedMotionClips } from "./attachedMotionClips.mjs";
import { expandCharacterInfoCodenames } from "../shared/characterInfo.mjs";
// Animation Level B asset step: publish the RAW character .ban files used by
// the live motion plane (idle, locomotion, attack, pickup, hit, sit, and death) as
// browser-fetchable public assets, plus a manifest the WIP bridge uses to
// resolve modelCodename -> native clip path -> raw bytes.
//
// Boundary rationale: the decoded .bsr animation-set section is the native
// state-id -> clip-path authority. Locomotion resolves by exact default-set
// motion ids (0 stand, 1 walk, 7 run), never by filename. The raw .ban
// BYTES are shipped untouched - the browser parses them with the REAL WIP
// CResAnimationClip loader chain (sub_a6c500 -> sub_a6c2d0 -> sub_a673e0 ->
// sub_a6c150) over ArrayBuffer-backed CJArchiveFm ops.
//
// Output:
//   .generated/client-public/assets/anim/<game-relative path>.ban   (raw bytes)
//   .generated/client-public/assets/anim/manifest.json
//
// Manifest entry `path` is the native game-relative resource path exactly as
// the .bsr lists it (normalized) - the same string the native client would
// hand to CPFileManager_OpenArchive - and IS what the runtime stores in
// CResAnimationClip.resourceLoadRequest.

import fs from "node:fs";
import { NATIVE_IDLE_STATE_CLIPS, NATIVE_EMOTE_STATE_CLIPS } from "./buildAvatar.mjs";
import path from "node:path";
import {
	pickAnimationSetSoundEvents,
	pickAnimationStateTableMetadata,
	pickDefaultSetSoundEvents,
	pickDefaultSetStateClip,
	pickDefaultSetStateTableMetadata
} from "./animationUtils.mjs";
import { parseBan, parseCharacterBsr } from "./formats.mjs";
import { parseSkillAniSet } from "./parseSkillEffect.mjs";
import { SKILL_EFFECT_ANIMATION_NAME_ID_ROWS } from "./native/skillEffectAnimationRegistry.ts";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { resolveRoster } from "./resolveCharRoster.mjs";
import { dataAssetPath, loadDataAsset } from "../shared/jmxAssetIO.mjs";
import { writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { splitTextDataRow } from "../shared/textDataIo.mjs";
import { normalizeAssetPath, publicRoot, retailTextdataRoot, clientV150ResinfoRoot } from "../world/paths.mjs";

const animPublicRoot = path.join( publicRoot, "assets", "anim" );
const textdataDir = retailTextdataRoot;
const skillEffectPath = path.join( textdataDir, "skilleffect.txt" );

/*
================
collectRequiredSkillMotionIdsBySetName
================
*/
function collectRequiredSkillMotionIdsBySetName() {
	const animationIdByName = new Map( SKILL_EFFECT_ANIMATION_NAME_ID_ROWS );
	const requiredSkillMotionIdsBySetName = new Map();
	for ( const row of parseSkillAniSet( skillEffectPath ).values() ) {
		const setName = row.aniGroup.toLowerCase();
		let required = requiredSkillMotionIdsBySetName.get( setName );
		if ( !required ) {
			required = new Set();
			requiredSkillMotionIdsBySetName.set( setName, required );
		}
		for (
			const animationName of [
				...row.readyAnims,
				...row.waitAnims,
				...row.shotAnims,
				...row.actionReadyAnims,
				...row.actionWaitAnims,
				...row.actionShotAnims
			]
		) {
			const motionId = animationIdByName.get( animationName );
			if ( motionId === undefined ) {
				throw new Error(
					`[anim] skillaniset ${row.baseName}: ${animationName} is absent from native data_ccd620`
				);
			}
			required.add( motionId );
		}
	}
	return requiredSkillMotionIdsBySetName;
}

// Native sub_920020 / SkillEffectData_LoadAll selects #section
// `characterInfo`, then sub_91b830 / SkillEffectCharacterInfo_ParseRow
// interns column 2 (`ResourceTypeName`) into action-effect record +0x04.
// CICharactor_BindRecords copies that pointer to CICharactor+0x64c, where
// the CGEffSound profile handlers use it as the first lookup-key component.
/*
================
parseCharacterSoundProfiles
================
*/
function parseCharacterSoundProfiles( filePath ) {
	const text = fs.readFileSync( filePath, "utf16le" ).replace( /^\uFEFF/, "" );
	const profiles = new Map();
	let inCharacterInfo = false;
	for ( const line of text.split( /\r?\n/ ) ) {
		if ( line.startsWith( "#section" ) ) {
			inCharacterInfo = /^#section\s+characterInfo\b/i.test( line );
			continue;
		}
		if ( !inCharacterInfo || !line || line.startsWith( "//" ) ) continue;
		const [modelCodename, soundProfileName] = splitTextDataRow( line );
		if ( !modelCodename || !soundProfileName || soundProfileName === "none" ) {
			continue;
		}
		for ( const name of expandCharacterInfoCodenames( modelCodename ) ) profiles.set( name, soundProfileName );
	}
	return profiles;
}

/*
================
publishClip
================
*/
async function publishClip( gamePath, publishedByPath ) {
	const normalized = normalizeAssetPath( gamePath );
	const existing = publishedByPath.get( normalized );
	if ( existing ) return existing;

	const sourcePath = dataAssetPath( gamePath );
	const buffer = await loadDataAsset( gamePath );
	// Validate the raw payload with the proven parser before publishing; the
	// browser-side WIP loader will re-parse the same bytes for real.
	const ban = parseBan( buffer, sourcePath );

	const destPath = path.join( animPublicRoot, ...normalized.split( "/" ) );
	writeIntoPublicTreeSync( destPath, buffer );

	const entry = {
		path: normalized,
		url: `/assets/anim/${normalized}`,
		bytes: buffer.length,
		durationMs: ban.durationMs,
		frameCount: ban.frameCount,
		boneTracks: ban.animBoneCount,
		looping: ban.field2
	};
	publishedByPath.set( normalized, entry );
	return entry;
}

/*
================
buildLocomotionBanAssets
================
*/
export async function buildLocomotionBanAssets() {
	const requiredSkillMotionIdsBySetName = collectRequiredSkillMotionIdsBySetName();
	const avatarOverrides = loadAvatarVisualOverrides(
		path.join( clientV150ResinfoRoot, "avataritemdata.txt" ),
		loadEquipmentRecords( retailTextdataRoot )
	);
	const { resolved, missing } = resolveRoster( textdataDir );
	if ( missing.length ) throw new Error( `[anim] unresolved codenames: ${missing.join( ", " )}` );
	const soundProfileByModel = parseCharacterSoundProfiles( skillEffectPath );

	const publishedByPath = new Map();
	const models = {};
	let clipCount = 0;

	for ( const model of resolved ) {
		let bsr;
		try {
			bsr = parseCharacterBsr( await loadDataAsset( model.bsrPath ), model.bsrPath );
		} catch ( error ) {
			throw new Error( `[anim] ${model.codename}: cannot read ${model.bsrPath}: ${error?.message ?? error}`, {
				cause: error
			} );
		}

		const standPath = pickDefaultSetStateClip( bsr, 0 );
		const walkPath = pickDefaultSetStateClip( bsr, 1 );
		const runPath = pickDefaultSetStateClip( bsr, 7 );
		// ANI_ATTACK1..4 resolve through sub_8719d0 to motions 2/5/0x10/0x11.
		// SkillEffectObj_Init selects these authored names from skilleffect records,
		// then the state-2 motion bracket plays the resolved one-shot. Human-player
		// BSRs author SND_SWING1 in each corresponding ModDataSound state.
		const attack1Path = pickDefaultSetStateClip( bsr, 2 );
		const attack2Path = pickDefaultSetStateClip( bsr, 5 );
		const attack3Path = pickDefaultSetStateClip( bsr, 0x10 );
		const attack4Path = pickDefaultSetStateClip( bsr, 0x11 );
		// Motion 0x26 = the pickup scoop (ANI_PICK, state-10 enter sub_8e6150);
		// resolved from the .bsr default set state 0x26, not a filename heuristic.
		const pickPath = pickDefaultSetStateClip( bsr, 0x26 );
		// Motion 3 = the ordinary state-11 hit reaction (sub_8e6260). Every
		// published character BSR authors VOC_MOAN at cursor 0 on this state.
		const hitPath = pickDefaultSetStateClip( bsr, 3 );
		const hit2Path = pickDefaultSetStateClip( bsr, 9 );
		// State-4 knockdown enter plays 0x3e over the held 0x3f loop; state-11's
		// already-down branch plays 0x40, and 0x41 is the authored wake-up exit.
		const downPath = pickDefaultSetStateClip( bsr, 0x3e );
		const downWaitPath = pickDefaultSetStateClip( bsr, 0x3f );
		const downDamagePath = pickDefaultSetStateClip( bsr, 0x40 );
		const wakeupPath = pickDefaultSetStateClip( bsr, 0x41 );
		// Motions 0x0d/0x0e/0x0f = the sit trio (sit-down / sit / stand-up):
		// state-6 enter sub_8e6a90 plays 0x0e (quirk branch adds 0x0d) and the
		// Stall3 sit->stand enter sub_8e6980 plays 0x0f through vt+0xb8
		// PlayMotionBlend. Same default-set state-id resolution as the pickup -
		// these are the NATIVE_CHARACTER_SELECT_STATE_IDS trio the GLB pipeline
		// already picks for the char-select campfire.
		const sitDownPath = pickDefaultSetStateClip( bsr, 0x0d );
		const sitPath = pickDefaultSetStateClip( bsr, 0x0e );
		const standUpPath = pickDefaultSetStateClip( bsr, 0x0f );
		// State-1 death enter (sub_8e64f0) plays motion 0x24, optionally follows
		// with motion 4 when states 2/3 were active, and uses 0x42 for the native
		// quick-death branch. The authored SND_DEATH/VOC_DEATH ModDataSound tracks
		// live on motion 4, so publish the complete trio from their native state ids.
		const deathLoopPath = pickDefaultSetStateClip( bsr, 0x24 );
		const deathPath = pickDefaultSetStateClip( bsr, 4 );
		const deathQuickPath = pickDefaultSetStateClip( bsr, 0x42 );
		const soundProfileName = soundProfileByModel.get( model.codename );
		if ( !soundProfileName ) {
			throw new Error(
				`[anim] ${model.codename}: no characterInfo.ResourceTypeName in ${skillEffectPath}`
			);
		}
		const entry = { soundProfileName };
		// Motion 0 = the state-8 fallback/idle overlay (sub_8e5a60 plays it via
		// the REAL PlayMotionBlend chain at spawn and after every walk arrival).
		if ( standPath ) {
			entry.stand = {
				...(await publishClip( standPath, publishedByPath )),
				...pickDefaultSetStateTableMetadata( bsr, 0 )
			};
			clipCount += 1;
		}
		if ( walkPath ) {
			entry.walk = {
				...(await publishClip( walkPath, publishedByPath )),
				soundEvents: pickDefaultSetSoundEvents( bsr, 1, "snd_walk1" ),
				...pickDefaultSetStateTableMetadata( bsr, 1 )
			};
			clipCount += 1;
		}
		if ( runPath ) {
			entry.run = {
				...(await publishClip( runPath, publishedByPath )),
				soundEvents: pickDefaultSetSoundEvents( bsr, 7, "snd_run1" ),
				...pickDefaultSetStateTableMetadata( bsr, 7 )
			};
			clipCount += 1;
		}
		for (
			const [role, stateId, attackPath] of [
				[ "attack1", 2, attack1Path ],
				[ "attack2", 5, attack2Path ],
				[ "attack3", 0x10, attack3Path ],
				[ "attack4", 0x11, attack4Path ]
			]
		) {
			if ( !attackPath ) continue;
			entry[role] = {
				...(await publishClip( attackPath, publishedByPath )),
				soundEvents: pickDefaultSetSoundEvents( bsr, stateId, "snd_swing1" ),
				...pickDefaultSetStateTableMetadata( bsr, stateId )
			};
			clipCount += 1;
		}
		if ( pickPath ) {
			entry.pick = {
				...(await publishClip( pickPath, publishedByPath )),
				soundEvents: pickDefaultSetSoundEvents( bsr, 0x26, "snd_pickup" ),
				...pickDefaultSetStateTableMetadata( bsr, 0x26 )
			};
			clipCount += 1;
		}
		if ( hitPath ) {
			entry.hit = {
				...(await publishClip( hitPath, publishedByPath )),
				soundEvents: pickDefaultSetSoundEvents( bsr, 3, "voc_moan" ),
				...pickDefaultSetStateTableMetadata( bsr, 3 )
			};
			clipCount += 1;
		}
		for ( const { role, stateId } of [ ...NATIVE_IDLE_STATE_CLIPS, ...NATIVE_EMOTE_STATE_CLIPS ] ) {
			const idlePath = pickDefaultSetStateClip( bsr, stateId );
			if ( !idlePath ) continue;
			entry[role] = {
				...(await publishClip( idlePath, publishedByPath )),
				soundEvents: pickDefaultSetSoundEvents( bsr, stateId ),
				...pickDefaultSetStateTableMetadata( bsr, stateId )
			};
			clipCount++;
		}
		if ( hit2Path ) {
			entry.hit2 = {
				...(await publishClip( hit2Path, publishedByPath )),
				soundEvents: pickDefaultSetSoundEvents( bsr, 9, "voc_moan" ),
				...pickDefaultSetStateTableMetadata( bsr, 9 )
			};
			clipCount += 1;
		}
		for (
			const [role, stateId, reactionPath] of [
				[ "down", 0x3e, downPath ],
				[ "downwait", 0x3f, downWaitPath ],
				[ "downdamage", 0x40, downDamagePath ],
				[ "wakeup", 0x41, wakeupPath ]
			]
		) {
			if ( !reactionPath ) continue;
			entry[role] = {
				...(await publishClip( reactionPath, publishedByPath )),
				...pickDefaultSetStateTableMetadata( bsr, stateId )
			};
			clipCount += 1;
		}
		if ( sitDownPath ) {
			entry.sitdown = {
				...(await publishClip( sitDownPath, publishedByPath )),
				...pickDefaultSetStateTableMetadata( bsr, 0x0d )
			};
			clipCount += 1;
		}
		if ( sitPath ) {
			entry.sit = {
				...(await publishClip( sitPath, publishedByPath )),
				...pickDefaultSetStateTableMetadata( bsr, 0x0e )
			};
			clipCount += 1;
		}
		if ( standUpPath ) {
			entry.standup = {
				...(await publishClip( standUpPath, publishedByPath )),
				...pickDefaultSetStateTableMetadata( bsr, 0x0f )
			};
			clipCount += 1;
		}
		if ( deathLoopPath ) {
			entry.deathloop = {
				...(await publishClip( deathLoopPath, publishedByPath )),
				...pickDefaultSetStateTableMetadata( bsr, 0x24 )
			};
			clipCount += 1;
		}
		if ( deathPath ) {
			entry.death = {
				...(await publishClip( deathPath, publishedByPath )),
				soundEvents: pickDefaultSetSoundEvents( bsr, 4 ),
				...pickDefaultSetStateTableMetadata( bsr, 4 )
			};
			clipCount += 1;
		}
		if ( deathQuickPath ) {
			entry.deathquick = {
				...(await publishClip( deathQuickPath, publishedByPath )),
				...pickDefaultSetStateTableMetadata( bsr, 0x42 )
			};
			clipCount += 1;
		}

		// Complete live skill-animation closure. The native textdata
		// skillaniset/skillaniset2 rows
		// are the native consumers of these set/state pairs; every authored
		// Ready/Wait/Shot ANI_* token is resolved through native data_ccd620 (the
		// same map sub_91dae0 queries), then joined to the exact BSR set state. This replaces the old
		// runtime shortcut that collapsed every AniGroup to DEFAULT.
		for ( const motion of pickAttachedMotionClips( bsr ) ) {
			const clip = parseBan( await loadDataAsset( motion.path ), motion.path );
			entry[motion.role] = {
				...(await publishClip( motion.path, publishedByPath )),
				loop: clip.field2 !== 0,
				...pickAnimationStateTableMetadata( motion.state )
			};
		}
		entry.animationSets = {};
		// Every authored state of every set, not only the skill-required ones: the
		// native client plays the equipped weapon's set for all motions
		// (CCObjCharacter_ResolveWeaponAnimationPrefix 8E83F0 -> +0x114), so a
		// spear stands, walks and runs with its own two-handed clips.
		const requirements = avatarAnimationRequirements( bsr, avatarOverrides, requiredSkillMotionIdsBySetName );
		for ( const set of bsr.animationSets ?? [] ) {
			const name = set.name.toLowerCase(), ids = requirements.get( name ) ?? new Set();
			for ( const state of set.states ) if ( state.animationPath ) ids.add( state.stateId );
			requirements.set( name, ids );
		}
		for ( const [setName, requiredMotionIds] of requirements ) {
			const animationSet = bsr.animationSets?.find(
				( candidate ) => candidate.name.toLowerCase() === setName
			);
			if ( !animationSet ) {
				continue;
			}
			const states = {};
			for ( const motionId of [ ...requiredMotionIds ].sort( ( a, b ) => a - b ) ) {
				const state = animationSet.states.find(
					( candidate ) => candidate.stateId === motionId
				);
				if ( !state?.animationPath ) {
					continue;
				}
				states[String( motionId )] = {
					...(await publishClip( state.animationPath, publishedByPath )),
					soundEvents: pickAnimationSetSoundEvents( bsr, setName, motionId ),
					...pickAnimationStateTableMetadata( state )
				};
				clipCount += 1;
			}
			if ( Object.keys( states ).length > 0 ) {
				entry.animationSets[setName] = states;
			}
		}
		if (
			entry.stand ||
			entry.walk ||
			entry.run ||
			entry.attack1 ||
			entry.attack2 ||
			entry.attack3 ||
			entry.attack4 ||
			entry.pick ||
			entry.hit ||
			entry.hit2 ||
			entry.down ||
			entry.downwait ||
			entry.downdamage ||
			entry.wakeup ||
			entry.deathloop ||
			entry.death ||
			entry.deathquick
		) {
			models[model.codename] = entry;
		} else {
			throw new Error( `[anim] ${model.codename}: no live-plane clip in ${model.bsrPath}` );
		}
	}

	const manifest = {
		format: "sro-character-ban",
		version: 12,
		source:
			"characterdata .bsr animation lists plus enabled avataritemdata animation-set closure and complete native textdata/skilleffect skillaniset+skillaniset2 animation-group/state closure, complete retail CResAnimationStateTable event-map/time-warp payloads, BSR ModDataSound cursor tracks, native data_ccd620 skill-effect animation name ids, and skilleffect characterInfo sound-profile names; raw JMXVBAN 0102 bytes, WIP-loaded in browser",
		models
	};
	fs.mkdirSync( animPublicRoot, { recursive: true } );
	const manifestPath = path.join( animPublicRoot, "manifest.json" );
	writeJsonIfChangedSync( manifestPath, manifest );

	// The packs hold the manifest's .json.gz, so a sidecar left behind here would
	// pack the PREVIOUS manifest while the fresh bytes sit on disk. That is how
	// the motion-0x26 pick entries once stayed invisible for sixteen days.
	await refreshPrecompressedSidecars( [ manifestPath ] );

	console.log(
		`[anim] published ${publishedByPath.size} raw .ban file(s) (${clipCount} model-clip link(s)) for ${
			Object.keys( models ).length
		} model(s) -> ${path.relative( process.cwd(), animPublicRoot )}`
	);

	return {
		published: publishedByPath.size,
		clipCount,
		modelCount: Object.keys( models ).length,
		missing,
		manifestPath
	};
}

if ( isMainScript( import.meta.url ) ) {
	await buildLocomotionBanAssets();
}
