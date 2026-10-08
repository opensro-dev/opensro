/*
===========================================================================

buildRoster.mjs - character catalog: models, item attachments, manifest

Converts every CharacterData model the client can show into a skinned GLB,
then the Hwan hair and the per-item equipment catalog. The native
client dresses the dock, the creation preview, transforms, the title crowd
and the world through one routine (CCObjCharacter_SetEquipSlotVisual, fed by
RefItemIDs or by GlobalDataManager_GetItemRecordByCodeName), so there is one
per-item catalog (dress.equipment, see buildEquipmentVisuals.mjs) and no
crowd-only tables. Every attachment records the native BSR it was converted
from; the material oracle reads that provenance instead of re-deriving it.

Output mirrors the native res/ layout:
  res/char/<region>/<name>.bsr -> assets/char/<region>/<name>.glb
  res/cos/<name>.bsr           -> assets/cos/<name>.glb
plus assets/char/roster.json.

===========================================================================
*/
import { writeIntoPublicTreeSync } from "../shared/publicWrite.mjs";
import fs from "node:fs";
import path from "node:path";
import {
	NATIVE_ATTACK_STATE_CLIPS,
	NATIVE_CHARACTER_SELECT_STATE_IDS,
	NATIVE_CREATE_PREVIEW_ANIMATION_SET_NAMES,
	NATIVE_DEATH_STATE_CLIPS,
	NATIVE_REACTION_STATE_CLIPS,
	NATIVE_IDLE_STATE_CLIPS,
	NATIVE_EMOTE_STATE_CLIPS,
	assembleAvatar,
	characterSelectAnimationRoleForStateId,
	previewAnimationRoleForSetName,
	weaponAttackAnimationRoleForSetName,
	primSlot
} from "./buildAvatar.mjs";
import { avatarToGlb } from "./exportGlb.mjs";
import { buildEquipmentVisuals } from "./buildEquipmentVisuals.mjs";
import { resolveRoster } from "./resolveCharRoster.mjs";
import { parseAttachPartLink } from "./formats.mjs";
import { loadDataAsset } from "../shared/jmxAssetIO.mjs";
import { writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { convertTextureTrees } from "../shared/convertImagesRunner.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { quatMultiply, quatRotateVector } from "../shared/math3d.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";
import { gameRoot, publicAssetsRoot, retailTextdataRoot } from "../world/paths.mjs";

const textdataDir = retailTextdataRoot;
const publicAssets = publicAssetsRoot;
// "pick" = the ground-item pickup scoop (motion 0x26 / ANI_PICK): the mission
// local player renders from this GLB, and the 0x35C7 -> state-10 chain plays
// the scoop once with a 200ms blend.
// The charselect-state13/14/15 roles are the .bsr default-set sit trio
// (sit-down / sit / stand-up - the same states the char-select campfire
// uses): the mission 0x7017/0x3122 motion-state round trip plays them via
// state-6 (CICharactor_Action_SitDown 0x8E6A90) and Stall3 (CICharactor_Action_StandUp 0x8E6980), so the mission GLB must
// carry the groups. The attack1-4 roles are native default-set states
// 2/5/0x10/0x11; death/deathloop/deathquick are the state-1
// motion 4/0x24/0x42 trio. The mission actor needs both families.
const WEAPON_ATTACK_CLIP_ROLES = NATIVE_CREATE_PREVIEW_ANIMATION_SET_NAMES.flatMap( ( setName ) =>
	NATIVE_ATTACK_STATE_CLIPS.map( ( { role } ) => weaponAttackAnimationRoleForSetName( role, setName ) )
);
const CROWD_CLIP_ROLES = new Set( [
	// Mission inventory borrows this model and plays native weapon-set state 0.
	...NATIVE_CREATE_PREVIEW_ANIMATION_SET_NAMES.map( previewAnimationRoleForSetName ),
	...NATIVE_EMOTE_STATE_CLIPS.map( ( { role } ) => role ),
	...NATIVE_IDLE_STATE_CLIPS.map( ( { role } ) => role ),
	"walk",
	"run",
	"stand",
	"ride",
	"pick",
	...NATIVE_ATTACK_STATE_CLIPS.map( ( { role } ) => role ),
	...NATIVE_REACTION_STATE_CLIPS.map( ( { role } ) => role ),
	...WEAPON_ATTACK_CLIP_ROLES,
	...NATIVE_DEATH_STATE_CLIPS.map( ( { role } ) => role ),
	...NATIVE_CHARACTER_SELECT_STATE_IDS.map( characterSelectAnimationRoleForStateId )
] );
const CREATE_PREVIEW_CLIP_ROLES = new Set( [
	"stand",
	...NATIVE_ATTACK_STATE_CLIPS.map( ( { role } ) => role ),
	...NATIVE_REACTION_STATE_CLIPS.map( ( { role } ) => role ),
	...NATIVE_DEATH_STATE_CLIPS.map( ( { role } ) => role ),
	...NATIVE_CREATE_PREVIEW_ANIMATION_SET_NAMES.map( previewAnimationRoleForSetName ),
	...WEAPON_ATTACK_CLIP_ROLES,
	...NATIVE_CHARACTER_SELECT_STATE_IDS.map( characterSelectAnimationRoleForStateId )
] );

// Public namespaces the roster publishes GLBs into; it is their only GLB writer
// (char/vat holds the crowd VAT's .bin/.json, never a GLB).
const ROSTER_NAMESPACES = [ "char", "cos" ];
// Precompressed transport copies a GLB may carry beside it.
const GLB_SIDECAR_SUFFIXES = [ ".br", ".gz", ".zst" ];

// Rider seat dummy bone present in every trade-transport skeleton (Bionic_MountRider).
const SEAT_BONE = "saddle";

/*
================
seatOffset

Absolute bind position (SRO model space, Y-up) of the seat bone via FK over the
skeleton's parent-local transforms - the same space the GLB exporter bakes vertices in
(the runtime flips Z to match the right-handed GLB). Returns null if no seat bone.
================
*/
function seatOffset( skeleton ) {
	const idx = skeleton.byName.get( SEAT_BONE );
	if ( idx === undefined ) return null;
	const worldQ = new Array( skeleton.bones.length );
	const worldT = new Array( skeleton.bones.length );
	for ( const b of skeleton.bones ) {
		if ( b.parentIndex < 0 ) {
			worldQ[b.index] = b.local.q.slice();
			worldT[b.index] = b.local.t.slice();
		} else {
			const pq = worldQ[b.parentIndex];
			const pp = worldT[b.parentIndex];
			const rt = quatRotateVector( pq, b.local.t );
			worldT[b.index] = [ pp[0] + rt[0], pp[1] + rt[1], pp[2] + rt[2] ];
			worldQ[b.index] = quatMultiply( pq, b.local.q );
		}
	}
	const t = worldT[idx];
	return [ Number( t[0].toFixed( 3 ) ), Number( t[1].toFixed( 3 ) ), Number( t[2].toFixed( 3 ) ) ];
}

/*
================
glbOutput

Public output path (and on-disk path) for a resolved model, mirroring native res/.
================
*/
function glbOutput( model ) {
	// model.bsrPath = "res/char/europe/foo.bsr" | "res/cos/t_horse1.bsr"
	const rel = model.bsrPath.replace( /^res\//i, "" ).replace( /\.bsr$/i, ".glb" );
	return { publicPath: `/assets/${rel}`, diskPath: path.join( publicAssets, ...rel.split( "/" ) ) };
}

/*
================
previewGlbOutput

Create-screen avatars use the same default live stand state as CIFCharacterWnd.
================
*/
function previewGlbOutput( model ) {
	const rel = model.bsrPath.replace( /^res\/char\//i, "" ).replace( /\.bsr$/i, ".glb" );
	return {
		publicPath: `/assets/char/preview/${rel}`,
		diskPath: path.join( publicAssets, "char", "preview", ...rel.split( "/" ) )
	};
}

/*
================
buildHwanHairSets

Retail8E9060 attaches these two race-specific resources on body mode1.
================
*/
export async function buildHwanHairSets() {
	const entries = {};
	for ( const [gender, name] of [ [ "M", "chinaman" ], [ "W", "chinawoman" ] ] ) {
		const key = `CH_${gender}`,
			bsrPath = `res/char/china/${name}_hwan_hair.bsr`,
			buffer = await loadDataAsset( bsrPath ),
			link = parseAttachPartLink( buffer );
		// Preserve the private hair skeleton and authored default/state0 BAN.
		const avatar = await assembleAvatar( bsrPath, { slotForMesh: () => "part:HWAN_HAIR" } );
		if ( !avatar.clips.some( c => c.role === "stand" ) ) throw new Error( `Missing Hwan animation ${gender}` );
		const publicPath = `/assets/char/hwan/${key.toLowerCase()}.glb`,
			diskPath = path.join( publicAssets, publicPath.slice( "/assets/".length ) );
		writeIntoPublicTreeSync( diskPath, avatarToGlb( avatar ) );
		entries[key] = {
			glb: publicPath,
			parts: [ "HWAN_HAIR" ],
			covers: { HWAN_HAIR: link?.c === 1 ? link.pairs.map( p => p.key ) : [] },
			sources: { HWAN_HAIR: { bsr: bsrPath } },
			clip: "stand",
			bone: "Bip01 Head"
		};
	}
	return entries;
}

/*
================
rosterGlbPaths

Every GLB a catalog names, found by walking the whole document: the roster
owns each of them, whatever table of whichever format version holds it.
================
*/
function rosterGlbPaths( roster ) {
	const paths = [];
	const pending = [ roster ];
	while ( pending.length ) {
		const node = pending.pop();
		if ( !node || typeof node !== "object" ) continue;
		for ( const [key, value] of Object.entries( node ) ) {
			if ( typeof value === "string" && value.endsWith( ".glb" ) && (key === "glb" || key === "previewGlb") ) {
				paths.push( value );
			} else if ( typeof value === "object" ) {
				pending.push( value );
			}
		}
	}
	return paths;
}

/*
================
retireUnpublishedRosterOutputs

The roster is the only writer of GLBs under its namespaces, so after a
build every GLB there that the new catalog does not name is dead output,
whichever build left it: delete it with its transport sidecars, then any
directory left empty. Returns the retired public paths.
================
*/
function retireUnpublishedRosterOutputs( roster ) {
	const published = new Set( rosterGlbPaths( roster ).map( ( p ) => p.toLowerCase() ) );
	const removed = [];
	function sweep( directory ) {
		for ( const entry of fs.readdirSync( directory, { withFileTypes: true } ) ) {
			const filePath = path.join( directory, entry.name );
			if ( entry.isDirectory() ) {
				sweep( filePath );
				if ( fs.readdirSync( filePath ).length === 0 ) fs.rmdirSync( filePath );
				continue;
			}
			if ( !entry.name.toLowerCase().endsWith( ".glb" ) ) continue;
			const publicPath = "/assets/" + path.relative( publicAssets, filePath ).replaceAll( "\\", "/" );
			if ( published.has( publicPath.toLowerCase() ) ) continue;
			for ( const suffix of [ "", ...GLB_SIDECAR_SUFFIXES ] ) fs.rmSync( filePath + suffix, { force: true } );
			removed.push( publicPath );
		}
	}
	for ( const namespace of ROSTER_NAMESPACES ) {
		const root = path.join( publicAssets, namespace );
		if ( fs.existsSync( root ) ) sweep( root );
	}
	return removed;
}

/*
================
buildRoster
================
*/
export async function buildRoster( { skipTextures = false } = {} ) {
	if ( !skipTextures ) await convertTextureTrees( "roster", [ "prim/mtrl" ] );

	const manifestPath = path.join( publicAssets, "char", "roster.json" );
	const { resolved, missing } = resolveRoster( textdataDir );
	if ( missing.length ) throw new Error( `[roster] unresolved codenames: ${missing.join( ", " )}` );

	const models = [];
	let built = 0;
	for ( const model of resolved ) {
		const { publicPath, diskPath } = glbOutput( model );
		const entry = {
			codename: model.codename,
			refObjId: model.refObjId,
			region: model.region,
			isMount: model.isMount,
			walkSpeed: model.walkSpeed,
			runSpeed: model.runSpeed,
			glb: publicPath,
			// The whole model is one native source; previewGlb shares it.
			source: model.bsrPath
		};
		try {
			// Slot names are prim indices ("prim0".."prim8") - the same addressing the native
			// part-link tables use, so the runtime can hide exactly the prims an item covers.
			const avatar = await assembleAvatar( model.bsrPath, {
				slotForMesh: ( _mp, i ) => primSlot( i ),
				previewWeaponClips: !model.isMount,
				characterSelectStateClips: !model.isMount
			} );
			// Both the title crowd simulation and Mission PathCtl advance their
			// actor holders. Their shared runtime GLB therefore owns pose only;
			// authored horizontal walk/run travel would otherwise be applied once
			// by the holder and a second time by the root bone.
			const runtimeAvatar = {
				...avatar,
				clips: model.isMount ?
					avatar.clips :
					avatar.clips.filter( (
						clip
					) => (CROWD_CLIP_ROLES.has( clip.role ) || clip.role.startsWith( "attached-" )) ),
				inPlaceHorizontalRootMotionRoles: [ "walk", "run" ]
			};
			const glb = avatarToGlb( runtimeAvatar );
			writeIntoPublicTreeSync( diskPath, glb );
			entry.bytes = glb.length;
			entry.bones = avatar.skeleton.boneCount;
			entry.hasClip = runtimeAvatar.clips.length > 0;
			entry.clips = runtimeAvatar.clips.map( ( c ) => c.role );
			const requiredCrowdRoles = model.isMount ? [ "stand", "walk" ] : [ "stand", "walk", "ride" ];
			for ( const role of requiredCrowdRoles ) {
				if ( !entry.clips.includes( role ) ) {
					throw new Error( `${model.codename} lacks required native crowd clip role ${role}` );
				}
			}
			entry.materials = avatar.materials.size;
			if ( model.isMount ) {
				const seat = seatOffset( avatar.skeleton );
				if ( !seat ) throw new Error( `mount ${model.codename} has no required ${SEAT_BONE} bone` );
				entry.seat = seat;
			}
			if ( !model.isMount ) {
				const previewOut = previewGlbOutput( model );
				const previewClips = avatar.clips.filter( ( clip ) => CREATE_PREVIEW_CLIP_ROLES.has( clip.role ) );
				if ( previewClips.length === 0 ) throw new Error( `${model.codename} has no required preview clips` );
				const previewAvatar = { ...avatar, clips: previewClips };
				const previewGlb = avatarToGlb( previewAvatar );
				writeIntoPublicTreeSync( previewOut.diskPath, previewGlb );
				entry.previewGlb = previewOut.publicPath;
				entry.previewBytes = previewGlb.length;
				entry.previewClips = (previewAvatar.clips ?? []).map( ( c ) => c.role );
			}
			// Native {coverKey -> prim index} map from the char BSR tail; worn items hide
			// the prims whose keys they cover (CCompChar_ApplyItemCovers 0xA89BD0). Mounts have none.
			if ( avatar.partLink && avatar.partLink.pairs.length > 0 ) {
				entry.cover = Object.fromEntries( avatar.partLink.pairs.map( ( p ) => [ p.key, p.value ] ) );
			}
			built += 1;
			const seatNote = entry.seat ? ` seat=[${entry.seat}]` : "";
			const previewNote = entry.previewGlb ? ` preview=[${entry.previewClips}]` : "";
			console.log(
				`[roster] OK   ${
					model.codename.padEnd( 28 )
				} -> ${publicPath} (${glb.length} B, clips=[${entry.clips}]${seatNote}${previewNote})`
			);
		} catch ( error ) {
			throw new Error( `[roster] FAIL ${model.codename}: ${error?.message ?? error}`, { cause: error } );
		}
		models.push( entry );
	}

	const manifest = {
		format: "sro-character-catalog",
		version: 3,
		source: "CharacterData models; equipment by RefItemID (CCObjCharacter_SetEquipSlotVisual)",
		// Rider seat dummy referenced by CICharactor_MountRider (0x871420, name @0xcccd30):
		// every trade-transport skeleton carries a "saddle" bone; per-mount bind offset is
		// stored as models[].seat (SRO model space, Y-up) for runtime rider placement.
		mountSeatBone: SEAT_BONE,
		count: models.length,
		builtCount: built,
		models,
		dress: {
			hwan: await buildHwanHairSets()
		}
	};
	manifest.dress.equipment = await buildEquipmentVisuals( manifest.dress );
	fs.mkdirSync( path.dirname( manifestPath ), { recursive: true } );
	writeJsonIfChangedSync( manifestPath, manifest );
	await refreshPrecompressedSidecars( [ manifestPath ], { onlyWhenStale: true } );
	const removed = retireUnpublishedRosterOutputs( manifest );
	if ( removed.length ) console.log( `[roster] retired ${removed.length} unpublished GLB output(s)` );
	console.log(
		`\n[roster] built ${built}/${models.length} GLBs; manifest -> ${path.relative( gameRoot, manifestPath )}`
	);
	return { built, modelCount: models.length, missing, manifestPath, removed: removed.length };
}

if ( isMainScript( import.meta.url ) ) {
	const args = process.argv.slice( 2 );
	await buildRoster( {
		skipTextures: args.includes( "--skip-textures" )
	} );
}
