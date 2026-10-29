/*
===========================================================================

itemAttachments.mjs - item attachment conversion onto a wearer skeleton

Converts item BSRs (armor pieces, weapons, shields, avatar parts, avatar
auxiliaries) into GLBs bound to a donor body skeleton of the item's own
race and sex. Natively all of these are the same CRes attachable on the
same CRTBranch mechanism; the runtime re-binds bones per wearer by name.
Each result records the native BSR per part for the material oracle.

The per-item catalog (buildEquipmentVisuals.mjs) and the default and
fortress wear use it; the roster owns only the body models. Keeping these
builders out of buildRoster.mjs also keeps the import graph acyclic: the
roster imports the equipment catalog, which imports this module.

===========================================================================
*/
import { writeIntoPublicTreeSync } from "../shared/publicWrite.mjs";
import fs from "node:fs";
import path from "node:path";
import { assembleAvatar, primSlot } from "./buildAvatar.mjs";
import { avatarToGlb } from "./exportGlb.mjs";
import { parseCompound } from "../world/objects/compound.mjs";
import { parseJmxResourceBsr } from "../world/objects/formats.mjs";
import { parseAttachPartLink, parseBsk, parseCharacterBsr } from "./formats.mjs";
import { characterMaterialVariants } from "./materialVariants.mjs";
import { loadDataAsset } from "../shared/jmxAssetIO.mjs";
import { loadOptionalDataAsset } from "../shared/optionalDataAsset.mjs";
import { qinshiTombBranchFallback } from "../shared/qinshiTomb.mjs";
import { AMBIENT_MODIFIER_KIND } from "./equipmentParticles.mjs";
import { publicAssetsRoot } from "../world/paths.mjs";

const publicAssets = publicAssetsRoot;

/*
================
bindWorldsMatch

Same-bone test across two skeleton files: the name alone is NOT identity (item-private
dangle bones reuse generic names like "Bone01"); the bind world pose must agree too.
Genuinely shared bones come from the same rig export, so the tolerance is loose only
against fp noise (0.1 SRO unit = 1cm).
================
*/
function bindWorldsMatch( a, b ) {
	if ( !a?.world || !b?.world ) return false;
	const at = a.world.t, bt = b.world.t;
	if ( Math.hypot( at[0] - bt[0], at[1] - bt[1], at[2] - bt[2] ) > 0.1 ) return false;
	const aq = a.world.q, bq = b.world.q;
	const dot = aq[0] * bq[0] + aq[1] * bq[1] + aq[2] * bq[2] + aq[3] * bq[3];
	return Math.abs( dot ) > 0.995; // q and -q are the same rotation
}

/*
================
createDonorPool

Skeleton donor per (race, gender). Bone NAMES are the only contract between an
item GLB and its wearers (the runtime re-binds indices per wearer by name -
bindDressMeshToWearerSkeleton, mirroring native CRTBranch_Build), but the donor
must still KNOW the item's bone names to keep their animation, and CH/EU
skeletons differ - so pick a donor of the item's own race.
================
*/
export function createDonorPool( resolvedRoster ) {
	const donorFor = ( race, gender ) =>
		resolvedRoster.find(
			( m ) => !m.isMount && m.codename.includes( `_${race}_${gender === "M" ? "MAN" : "WOMAN"}_` )
		) ?? resolvedRoster.find( ( m ) => !m.isMount && (gender === "M" ? /_MAN_/ : /_WOMAN_/).test( m.codename ) );
	const donors = new Map();
	const donorSkeletons = new Map(); // donor.bsrPath -> parsed BSK (byName)
	return async ( race, gender ) => {
		const donorKey = `${race}|${gender}`;
		if ( !donors.has( donorKey ) ) donors.set( donorKey, donorFor( race, gender ) );
		const donor = donors.get( donorKey );
		if ( !donor ) return null;
		let donorSkel = donorSkeletons.get( donor.bsrPath );
		if ( !donorSkel ) {
			const donorChar = parseCharacterBsr( await loadDataAsset( donor.bsrPath ), donor.bsrPath );
			donorSkel = parseBsk( await loadDataAsset( donorChar.skeletonPath ), donorChar.skeletonPath );
			donorSkeletons.set( donor.bsrPath, donorSkel );
		}
		return { donor, donorSkel };
	};
}

/*
================
selectItemMaterialPaths

The material set a piece is converted with: all of the item's materials, or
the single authored set a caller names (fortress clothing picks one per team).
================
*/
export function selectItemMaterialPaths( itemBsr, materialSetId, itemBsrPath ) {
	if ( materialSetId === undefined ) return itemBsr.materialPaths;
	const selected = itemBsr.materialSection.paths.filter( ( _, i ) =>
		itemBsr.materialSection.setIds[i] === materialSetId
	);
	if ( selected.length !== 1 ) throw Error( `Missing/ambiguous material set ${materialSetId}: ${itemBsrPath}` );
	return selected;
}

/*
================
buildItemSetGlb

Build one item-set GLB (armor pieces OR weapon/shield parts - natively both are
the same CRes attachable on the same CRTBranch mechanism) on a donor body skeleton.
pieces: [{ part, itemBsrPath }]; returns a manifest entry { glb, parts, covers }
or null when nothing was bindable. `tag` is the log prefix ("dress"/"weapon").
write( diskPath, bytes, publicPath ) publishes the GLB (writeIntoPublicTreeSync
by default); a caller that post-processes the bytes holds them instead.
================
*/
export async function buildItemSetGlb(
	{ tag, key, donor, donorSkel, pieces, outSubdir, write = writeIntoPublicTreeSync }
) {
	// Resolve every piece's item .bsr -> skinned mesh paths + material set + cover keys.
	const meshPaths = [];
	const materialSetPaths = [];
	const partByMesh = new Map();
	const environmentByMesh = new Map();
	// Per-mesh view of the ITEM's OWN skeleton (.bsk): parent links for the ancestor
	// walk, full bones (incl. bind world) for bind-pose validation. Native item bone
	// hierarchies resolve inside the item's own CRTBranch only - a wearer bone with the
	// same generic name ("Bone01"...) is a different bone entirely, so a name match is
	// only trusted when the BIND POSES agree.
	const itemSkelByMesh = new Map();
	// Per-mesh wearer attach bone from the item .bsr skeleton section ("Bip01 Neck1"
	// on shoulder ornaments, "Bip01 R HandMid" on weapons, "Bip01 L Hand" on bows and
	// shields): the bone the item's whole private branch is parented to
	// (CRTBranch_LinkToParentBranch 0xABC680).
	const attachByMesh = new Map();
	const covers = {};
	// Native source per part, recorded for the material oracle.
	const sourceByPart = new Map();
	for ( const { part, itemBsrPath, materialSetId } of pieces ) {
		sourceByPart.set(
			part,
			materialSetId === undefined ? { bsr: itemBsrPath } : { bsr: itemBsrPath, materialSetId }
		);
		let itemBsr;
		let itemBuf;
		try {
			itemBuf = await loadDataAsset( itemBsrPath );
			itemBsr = parseJmxResourceBsr( itemBuf, itemBsrPath );
		} catch ( error ) {
			console.warn( `[${tag}] ${key} ${part}: ${error?.message ?? error}` );
			continue;
		}
		if ( itemBsr.meshPaths.length === 0 ) continue;
		let itemSkelInfo = null;
		let attachBone = "";
		try {
			const itemChar = parseCharacterBsr( itemBuf, itemBsrPath );
			attachBone = itemChar.skeletonAttachBone ?? "";
			if ( itemChar.skeletonPath ) {
				const itemSkel = parseBsk( await loadDataAsset( itemChar.skeletonPath ), itemChar.skeletonPath );
				itemSkelInfo = {
					parents: new Map( itemSkel.bones.map( ( b ) => [ b.name, b.parent ] ) ),
					byName: new Map( itemSkel.bones.map( ( b ) => [ b.name, b ] ) )
				};
			}
		} catch {
			itemSkelInfo = null; // item has no own skeleton; unbound bones stay unresolvable
		}
		for ( const meshPath of itemBsr.meshPaths ) {
			meshPaths.push( meshPath );
			partByMesh.set( meshPath, part );
			environmentByMesh.set( meshPath, itemBsr.modifiers?.environmentModifiers ?? [] );
			if ( itemSkelInfo ) itemSkelByMesh.set( meshPath, itemSkelInfo );
			if ( attachBone ) attachByMesh.set( meshPath, attachBone );
		}
		const selectedMaterials = selectItemMaterialPaths( itemBsr, materialSetId, itemBsrPath );
		for ( const mtl of selectedMaterials ) {
			if ( !materialSetPaths.includes( mtl ) ) materialSetPaths.push( mtl );
		}
		// Native cover keys from the item BSR tail. The apply gate is the THIRD header
		// dword (`c`, this[0xc5]) == 1 - CCompChar_ApplyItemCovers 0xA89BD0 checks
		// result_2[2], NOT the first dword. c is the cover MODE: 1 = replacement armor
		// (hide matching skin prims; bows are c=1 too), 2 = layered accessory (bracers/
		// shoulder pads/most weapons drawn OVER the skin - hide nothing). Gating on `a`
		// (always 1) hid the bare-hand skin prim under every c=2 bracer.
		const link = parseAttachPartLink( itemBuf );
		covers[part] = link && link.c === 1 ? link.pairs.map( ( p ) => p.key ) : [];
	}
	if ( meshPaths.length === 0 ) return null;

	const fileName = `${key.toLowerCase()}.glb`;
	const diskPath = path.join( publicAssets, "char", outSubdir, fileName );
	const publicPath = `/assets/char/${outSubdir}/${fileName}`;
	const avatar = await assembleAvatar( donor.bsrPath, {
		name: key,
		meshPaths,
		materialSetPaths,
		environmentModifiersForMesh: meshPath => environmentByMesh.get( meshPath ) ?? [],
		noClips: true,
		// A direct name match is only genuine if the item's own .bsk agrees with the
		// donor on that bone's BIND POSE (same rig family). Item-private bones with
		// generic names would otherwise teleport onto an unrelated wearer bone.
		acceptBone: ( bone, meshPath ) => {
			const info = itemSkelByMesh.get( meshPath );
			const itemBone = info?.byName.get( bone );
			if ( !itemBone ) return true; // not in the item's own skeleton: a wearer bone
			const donorBone = donorSkel.bones[donorSkel.byName.get( bone )];
			return bindWorldsMatch( itemBone, donorBone );
		},
		// Item-private bones, in native priority order:
		// 1) Nearest genuinely-shared ancestor (name AND bind pose match) by walking
		//    the ITEM's own .bsk hierarchy - trackless-bone telescoping (the skin
		//    matrix equals the nearest animated ancestor's; RTSkeleton.cpp
		//    CRTSocket_UpdateMatrices).
		// 2) The item's ATTACH bone (.bsr skeleton section, e.g. "Bip01 Neck1"):
		//    natively the whole private branch is parented under that wearer socket
		//    (CRTBranch_LinkToParentBranch 0xABC680) in a special mode (+0xa0=1).
		//    The root's update (CRTSocket_UpdateAttachRootMatrices 0xAB5870) does
		//    NOT use the attach bone's world matrix: it uses the attach bone's SKIN
		//    matrix (world*invBind = bind-relative delta) with its translation row
		//    replaced by the attach bone's absolute world POSITION. The attach bind
		//    ROTATION is cancelled by construction - the item keeps its authored
		//    model-space orientation. Net effect with branch bones at bind:
		//      v_world = attachDelta * (v_model + attachBindWorld.t)
		//    and since the VAT row IS attachDelta with translation
		//    t_cur - delta*t_bind, the exact bake is TRANSLATION ONLY:
		//      v' = v_model + attachBindWorld.t  (identity rotation!)
		//    (Baking the bind rotation too is what rotated the EU shoulder crystals
		//    90 degrees; the spherical CH orbs just hid the same error.)
		// 3) Donor ROOT row (documented deviation: native would hold bind pose +
		//    spring sim; VAT can't spring-simulate, but the piece follows the body).
		resolveUnboundBone: ( bone, meshPath ) => {
			const info = itemSkelByMesh.get( meshPath );
			let cur = bone;
			const seen = new Set();
			while ( info?.parents.has( cur ) && !seen.has( cur ) ) {
				seen.add( cur );
				cur = info.parents.get( cur );
				if ( !cur ) break;
				const di = donorSkel.byName.get( cur );
				if ( di !== undefined && bindWorldsMatch( info.byName.get( cur ), donorSkel.bones[di] ) ) return cur;
			}
			const attach = attachByMesh.get( meshPath );
			const ai = attach ? donorSkel.byName.get( attach ) : undefined;
			if ( ai !== undefined ) {
				const ab = donorSkel.bones[ai];
				return { bone: attach, bake: { q: [ 0, 0, 0, 1 ], t: ab.world.t } };
			}
			return donorSkel.bones[0].name;
		},
		slotForMesh: ( meshPath ) => `part:${partByMesh.get( meshPath )}`
	} );
	if ( avatar.parts.length === 0 ) {
		console.warn( `[${tag}] SKIP ${key.padEnd( 16 )} no bindable pieces` );
		return null;
	}
	const survivingParts = [ ...new Set( avatar.parts.map( ( p ) => partByMesh.get( p.meshPath ) ) ) ];
	// Equipment never widens a character's pick box (A9E310 skips the union
	// under a kind-0 body), so an item set carries none of its own; the donor
	// body's box would only mislead.
	avatar.aggregateBox = null;
	const glb = avatarToGlb( avatar );
	write( diskPath, glb, publicPath );

	// Keep cover keys only for parts that survived the export.
	const partCovers = {};
	for ( const part of survivingParts ) partCovers[part] = covers[part] ?? [];

	const dropped = avatar.skippedMeshes.map( ( s ) => `${partByMesh.get( s.meshPath )}(${s.bone})` );
	const remapped = avatar.remappedBones.map(
		( r ) => `${partByMesh.get( r.meshPath )}:${r.bone}${r.baked ? "=>" : "->"}${r.fallback}`
	);
	const coverNote = survivingParts
		.map( ( p ) => `${p}>{${(partCovers[p] ?? []).join( "," )}}` )
		.join( " " );
	console.log(
		`[${tag}] OK   ${key.padEnd( 16 )} -> ${publicPath} (${glb.length} B, parts=[${survivingParts}]` +
			`${dropped.length ? ` dropped=[${dropped}]` : ""}` +
			`${remapped.length ? ` remapped=[${remapped}]` : ""} covers{${coverNote}})`
	);
	const sources = Object.fromEntries( survivingParts.map( ( part ) => [ part, sourceByPart.get( part ) ] ) );
	return { glb: publicPath, parts: survivingParts, covers: partCovers, sources };
}

/*
================
buildAuxiliarySet

One resource drawn as its own animated handle: its private skeleton and
BANs stay its own, attached under its .bsr skeleton socket. Rebinding these
bones into the body would lose their motion. The entry carries the
resource's ambient particle set, which rides the animated bones.
================
*/
export async function buildAuxiliarySet( bsrPath, glb, part ) {
	const buffer = await loadDataAsset( bsrPath ),
		bsr = parseCharacterBsr( buffer, bsrPath ),
		link = parseAttachPartLink( buffer );
	if ( !bsr.skeletonAttachBone ) throw Error( `Missing auxiliary socket ${bsrPath}` );
	const avatar = await assembleAvatar( bsrPath, { slotForMesh: () => `part:${part}` } );
	const clips = avatar.clips.map( c => c.role );
	if ( !clips.includes( "stand" ) ) throw Error( `Missing auxiliary initial track ${bsrPath}` );
	writeIntoPublicTreeSync( path.join( publicAssets, glb.slice( "/assets/".length ) ), avatarToGlb( avatar ) );
	const ambient = bsr.particleModifiers.filter( m => m.kind === AMBIENT_MODIFIER_KIND );
	return {
		glb,
		parts: [ part ],
		covers: { [part]: link?.c === 1 ? link.pairs.map( p => p.key ) : [] },
		sources: { [part]: { bsr: bsrPath } },
		bone: bsr.skeletonAttachBone,
		clips,
		environmentModifiers: bsr.environmentModifiers,
		...(ambient.length ? { particleModifiers: ambient } : {})
	};
}

/*
================
buildAuxiliaryAvatarSets

8E9DD0 installs the override's auxiliary resource as its second handle.
================
*/
export async function buildAuxiliaryAvatarSets( overrides ) {
	const entries = {};
	for ( const [id, row] of Object.entries( overrides ) ) {
		if ( !row.additionalBsr ) continue;
		entries[id] = await buildAuxiliarySet(
			row.additionalBsr,
			`/assets/char/equipment/avatar_aux_${id}.glb`,
			"AVATAR_AUX"
		);
	}
	return entries;
}

/*
================
resolveCompoundBranchBsr

0x00a9b280 (CCompoundObj_vf12C) opens each branch BSR through
CResourceMgr_LoadSimpleResourceFromArchive (0x00a58370). When any branch
returns 0 (0x00a9b3aa), 0x00a9b280 aborts and destroys the compound.
Port-only, not native: shinmu.cpd (MOB_TQ_FLAMEMASTER / MOB_QT_TQ_FLAMEMASTER)
names degree-10 armor/weapon BSRs (heavy_10_*.bsr, spear_10.bsr) that v1.150
Data.pk2 does not ship because v1.150 caps at degree 9. When SRO_QINSHI_TOMB
is on and a _10 branch is absent, substitute the matching _09 BSR from the
same directory so the Floor 5 unique boss can assemble.
================
*/
async function resolveCompoundBranchBsr( branchPath ) {
	const direct = await loadOptionalDataAsset( branchPath );
	if ( direct !== null ) {
		return { bsrPath: branchPath, buffer: direct };
	}
	const fallbackPath = qinshiTombBranchFallback( branchPath );
	if ( fallbackPath !== null ) {
		const fallback = await loadOptionalDataAsset( fallbackPath );
		if ( fallback !== null ) {
			return { bsrPath: fallbackPath, buffer: fallback };
		}
	}
	throw new Error( `Compound branch BSR absent from archive: ${branchPath}` );
}

/*
================
compoundBodyBsr

A compound's body: branch 0, the kind-0 character BSR. A compound row's
manifest modifiers are this branch's (assembleCompoundCharacter returns its
BSR), so every reader that refreshes them reads the same branch.
================
*/
export async function compoundBodyBsr( cpdPath ) {
	const cpd = parseCompound( await loadDataAsset( cpdPath ), cpdPath );
	if ( cpd.branches.length === 0 ) {
		throw new Error( `${cpdPath}: compound has no branches` );
	}
	return resolveCompoundBranchBsr( cpd.branches[0] );
}

/*
================
assembleCompoundCharacter

0x00a577b0 (CResourceMgr_LoadCompoundObjectFromArchive) -> 0x00a9b280
(CCompoundObj_vf12C) -> 0x00a89ed0 (CCompChar_AttachItemAndApplyCovers):
branch 0 is the kind-0 character body BSR (skeleton, aggregateBox, animation
sets); branches 1..N are kind-5 item/hair/weapon BSRs attached through
CCompound_AttachResourceWithMaterialSet (0x00a9e310) whose c == 1 cover keys
hide matching body prims via CCompChar_ApplyItemCovers (0x00a89bd0).
================
*/
export async function assembleCompoundCharacter( cpdPath, source ) {
	const cpdBuffer = source ?? (await loadDataAsset( cpdPath ));
	const cpd = parseCompound( cpdBuffer, cpdPath );
	if ( cpd.branches.length === 0 ) {
		throw new Error( `${cpdPath}: compound has no branches` );
	}
	const baseBranch = await resolveCompoundBranchBsr( cpd.branches[0] );
	const baseBsrPath = baseBranch.bsrPath;
	const baseBuf = baseBranch.buffer;
	const baseBsr = parseCharacterBsr( baseBuf, baseBsrPath );
	const donorSkel = parseBsk( await loadDataAsset( baseBsr.skeletonPath ), baseBsr.skeletonPath );
	const coveredPrimIndices = new Set();
	const itemMeshes = [];
	const itemMaterials = [];
	const environmentByMesh = new Map();
	const itemSkelByMesh = new Map();
	const attachByMesh = new Map();
	const slotByMesh = new Map();

	for ( let branchIndex = 1; branchIndex < cpd.branches.length; branchIndex++ ) {
		const resolved = await resolveCompoundBranchBsr( cpd.branches[branchIndex] );
		const itemBsrPath = resolved.bsrPath;
		const itemBuf = resolved.buffer;
		const itemBsr = parseJmxResourceBsr( itemBuf, itemBsrPath );
		const link = parseAttachPartLink( itemBuf );
		if ( link && link.c === 1 ) {
			const coveredKeys = new Set( link.pairs.map( ( pair ) => pair.key ) );
			for ( const bodyPair of baseBsr.partLink?.pairs ?? [] ) {
				if ( coveredKeys.has( bodyPair.key ) ) {
					coveredPrimIndices.add( bodyPair.value );
				}
			}
		}
		let itemSkelInfo = null;
		let attachBone = "";
		try {
			const itemChar = parseCharacterBsr( itemBuf, itemBsrPath );
			attachBone = itemChar.skeletonAttachBone ?? "";
			if ( itemChar.skeletonPath ) {
				const itemSkel = parseBsk( await loadDataAsset( itemChar.skeletonPath ), itemChar.skeletonPath );
				itemSkelInfo = {
					parents: new Map( itemSkel.bones.map( ( b ) => [ b.name, b.parent ] ) ),
					byName: new Map( itemSkel.bones.map( ( b ) => [ b.name, b ] ) )
				};
			}
		} catch {
			itemSkelInfo = null;
		}
		for ( const [meshIndex, meshPath] of itemBsr.meshPaths.entries() ) {
			itemMeshes.push( meshPath );
			slotByMesh.set( meshPath, `cpd:${branchIndex}:${meshIndex}` );
			environmentByMesh.set( meshPath, itemBsr.modifiers?.environmentModifiers ?? [] );
			if ( itemSkelInfo ) itemSkelByMesh.set( meshPath, itemSkelInfo );
			if ( attachBone ) attachByMesh.set( meshPath, attachBone );
		}
		for ( const mtl of itemBsr.materialPaths ) {
			if ( !itemMaterials.includes( mtl ) ) itemMaterials.push( mtl );
		}
	}

	const meshPaths = [];
	for ( const [primIndex, meshPath] of baseBsr.meshPaths.entries() ) {
		if ( coveredPrimIndices.has( primIndex ) ) continue;
		meshPaths.push( meshPath );
		slotByMesh.set( meshPath, primSlot( primIndex ) );
		environmentByMesh.set( meshPath, baseBsr.environmentModifiers ?? [] );
	}
	meshPaths.push( ...itemMeshes );

	const baseMaterialSets = characterMaterialVariants( baseBuf, baseBsrPath );
	const baseMaterials = baseMaterialSets.has( 0 ) ? [ baseMaterialSets.get( 0 ) ] : baseBsr.materialPaths;
	const materialSetPaths = [ ...baseMaterials ];
	for ( const mtl of itemMaterials ) {
		if ( !materialSetPaths.includes( mtl ) ) materialSetPaths.push( mtl );
	}

	const avatar = await assembleAvatar( baseBsrPath, {
		name: path.basename( cpdPath, ".cpd" ),
		meshPaths,
		materialSetPaths,
		environmentModifiersForMesh: ( meshPath ) => environmentByMesh.get( meshPath ) ?? [],
		noClips: true,
		rigidUnboundMeshes: true,
		acceptBone: ( bone, meshPath ) => {
			const info = itemSkelByMesh.get( meshPath );
			const itemBone = info?.byName.get( bone );
			if ( !itemBone ) return true;
			const donorBone = donorSkel.bones[donorSkel.byName.get( bone )];
			return bindWorldsMatch( itemBone, donorBone );
		},
		resolveUnboundBone: ( bone, meshPath ) => {
			const info = itemSkelByMesh.get( meshPath );
			let cur = bone;
			const seen = new Set();
			while ( info?.parents.has( cur ) && !seen.has( cur ) ) {
				seen.add( cur );
				cur = info.parents.get( cur );
				if ( !cur ) break;
				const di = donorSkel.byName.get( cur );
				if ( di !== undefined && bindWorldsMatch( info.byName.get( cur ), donorSkel.bones[di] ) ) return cur;
			}
			const attach = attachByMesh.get( meshPath );
			const ai = attach ? donorSkel.byName.get( attach ) : undefined;
			if ( ai !== undefined ) {
				const ab = donorSkel.bones[ai];
				return { bone: attach, bake: { q: [ 0, 0, 0, 1 ], t: ab.world.t } };
			}
			return donorSkel.bones[0].name;
		},
		slotForMesh: ( meshPath, index ) => slotByMesh.get( meshPath ) ?? primSlot( index )
	} );
	// A compound's item branches carry their own materials; baseBsr's material
	// variants apply only to the body branch, so compounds publish a single set.
	return { bsr: baseBsr, materialSets: new Map(), avatar };
}
