import {pickAttachedMotionClips} from './attachedMotionClips.mjs';
// Assemble one SRO skinned resource (.bsr: mesh parts + skeleton + animation clip)
// into a single in-memory model with GLOBAL joint indices, ready for glTF emission.
// Build-time only. Works for player avatars, mounts (cos), NPCs, etc. — anything
// authored as JMXVRES 0109 + skinned JMXVBMS 0110.

import fs from "node:fs";
import { fileURLToPath } from "node:url";
import {
  findDefaultAnimationSet,
  pickAnimationSetStateClip,
  pickDefaultSetStateClip
} from "./animationUtils.mjs";
import { parseBan, parseBsk, parseCharacterBsr, parseSkinnedBms } from "./formats.mjs";
import { parseJmxResourceBsr } from "../world/objects/formats.mjs";
import { loadDataAsset, loadMaterialTextures } from "../shared/jmxAssetIO.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { quatRotateComponents } from "../shared/math3d.mjs";

export const NATIVE_CREATE_PREVIEW_ANIMATION_SET_NAMES = [
  // InitGlobalAnimationSetNameStrings + ItemTypeWord_ToAnimationSetName.
  // The create window updates the current weapon animation set from the equipped
  // item type, then plays state 0 through that set. Export the same state-0 clips
  // by set name instead of keeping a crossbow-only renderer shortcut.
  "sword",
  "spear",
  "bow",
  "onehand_staff",
  "onehand_sword",
  "twohand_sword",
  "dagger",
  "dual_axe",
  "harf",
  "twohand_staff"
];

export function previewAnimationRoleForSetName(setName) {
  return `preview-state0-${setName.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")}`;
}

/** Stable GLB role for a native weapon-set attack state. */
export function weaponAttackAnimationRoleForSetName(role, setName) {
  const normalizedSet = setName.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
  return `${role}-${normalizedSet}`;
}

export const NATIVE_CHARACTER_SELECT_STATE_IDS = [13, 14, 15];

// State-1 death enter in sub_8e64f0 uses this complete default-set trio:
// held corpse pose, ordinary death strike, and the quick-death one-shot.
// Keep the native state ids beside the exported roles so every generated
// player GLB exposes exactly the names the mission actor resolves.
export const NATIVE_DEATH_STATE_CLIPS = [
  { stateId: 0x24, role: "deathloop" },
  { stateId: 4, role: "death" },
  { stateId: 0x42, role: "deathquick" }
];

// Basic-attack action brackets enter these default-set states through
// CICharactor_PlayMotionEx. Keep all four authored variants in the mission
// character GLB; omitting them made the runtime correctly select attack1-4
// but then visibly fall back to the stand AnimationGroup.
export const NATIVE_ATTACK_STATE_CLIPS = [
  { stateId: 2, role: "attack1" },
  { stateId: 5, role: "attack2" },
  { stateId: 0x10, role: "attack3" },
  { stateId: 0x11, role: "attack4" }
];

// Complete default-set reaction family around the exact state-11/state-4
// selections. The ordinary/hard hits, knockdown, down-damage and wake-up are
// one-shots around the held downwait loop. Motion 0x42 remains owned by the
// death family above.
export const NATIVE_REACTION_STATE_CLIPS = [
  { stateId: 3, role: "hit1" },
  { stateId: 9, role: "hit2" },
  { stateId: 0x3e, role: "down" },
  { stateId: 0x3f, role: "downwait" },
  { stateId: 0x40, role: "downdamage" },
  { stateId: 0x41, role: "wakeup" }
];

// 8E5BB0 state-7 random idle, default animation set with 0x7a fallback.
export const NATIVE_IDLE_STATE_CLIPS = [
  { stateId: 0x7a, role: "idle122" },
  { stateId: 0x3d, role: "idle61" },
  { stateId: 0x51, role: "idle81" }
];

// 778190 -> state 13; 8E6310 selects action byte + 0x32.
export const NATIVE_EMOTE_STATE_CLIPS = Array.from({length:7},(_,action)=>({stateId:50+action,role:`emote${action}`}));

export function characterSelectAnimationRoleForStateId(stateId) {
  return `charselect-state${stateId}`;
}

const PREVIEW_WEAPON_SET_RULES = NATIVE_CREATE_PREVIEW_ANIMATION_SET_NAMES.map((setName) => ({
  role: previewAnimationRoleForSetName(setName),
  setName,
  stateId: 0
}));

export function pickPreviewWeaponClips(bsr) {
  const clips = [];
  const usedRoles = new Set();
  for (const rule of PREVIEW_WEAPON_SET_RULES) {
    const set = bsr.animationSets?.find((entry) => entry.name.toLowerCase() === rule.setName.toLowerCase());
    const state = set?.states.find((entry) => entry.stateId === rule.stateId);
    if (state?.animationPath && !usedRoles.has(rule.role)) {
      usedRoles.add(rule.role);
      clips.push({ role: rule.role, path: state.animationPath });
    }
  }
  return clips;
}

export function pickCharacterSelectStateClips(bsr) {
  const clips = [];
  const defaultSet = findDefaultAnimationSet(bsr);
  for (const stateId of NATIVE_CHARACTER_SELECT_STATE_IDS) {
    const state = defaultSet?.states.find((entry) => entry.stateId === stateId);
    if (state?.animationPath) {
      clips.push({
        role: characterSelectAnimationRoleForStateId(stateId),
        path: state.animationPath
      });
    }
  }
  return clips;
}

export function pickDeathStateClips(bsr) {
  const clips = [];
  const defaultSet = findDefaultAnimationSet(bsr);
  for (const { stateId, role } of NATIVE_DEATH_STATE_CLIPS) {
    const state = defaultSet?.states.find((entry) => entry.stateId === stateId);
    if (state?.animationPath) {
      clips.push({ role, path: state.animationPath });
    }
  }
  return clips;
}

export function pickReactionStateClips(bsr) {
  return pickStateClips(bsr,NATIVE_REACTION_STATE_CLIPS);
}

export function pickIdleStateClips(bsr) {
  return pickStateClips(bsr,NATIVE_IDLE_STATE_CLIPS);
}

function pickStateClips(bsr,rules) {
  const clips = [];
  const defaultSet = findDefaultAnimationSet(bsr);
  for (const { stateId, role } of rules) {
    const state = defaultSet?.states.find((entry) => entry.stateId === stateId);
    if (state?.animationPath) {
      clips.push({ role, path: state.animationPath });
    }
  }
  return clips;
}

export function pickWeaponAttackStateClips(bsr) {
  const clips = [];
  for (const setName of NATIVE_CREATE_PREVIEW_ANIMATION_SET_NAMES) {
    const set = bsr.animationSets?.find((entry) => entry.name.toLowerCase() === setName);
    if (!set) continue;
    for (const { stateId, role } of NATIVE_ATTACK_STATE_CLIPS) {
      const state = set.states.find((entry) => entry.stateId === stateId);
      if (state?.animationPath) {
        clips.push({
          role: weaponAttackAnimationRoleForSetName(role, setName),
          path: state.animationPath
        });
      }
    }
  }
  return clips;
}

export function pickAttackStateClips(bsr) {
  const clips = [];
  const defaultSet = findDefaultAnimationSet(bsr);
  for (const { stateId, role } of NATIVE_ATTACK_STATE_CLIPS) {
    const state = defaultSet?.states.find((entry) => entry.stateId === stateId);
    if (state?.animationPath) {
      clips.push({ role, path: state.animationPath });
    }
  }
  return clips;
}

/**
 * Pick the pickup/scoop clip: animation-set state 0x26 (native motion id
 * 0x26 = ANI_PICK, played by the state-10 pickup enter sub_8e6150 via
 * PlayMotionBlend). Resolved from the "default" set - the sub_8e7150
 * fallback key (0x00ccccc0) the runtime animation plane resolves through
 * (player .bsr files map every set's state 0x26 to the same
 * chinaman/chinawoman_pickup.ban).
 */
export function pickPickupClip(bsr) {
  const defaultSet = findDefaultAnimationSet(bsr);
  const state = defaultSet?.states.find((entry) => entry.stateId === 0x26);
  return state?.animationPath ?? null;
}

/**
 * Slot label for a character's Nth prim. The native engine addresses skin prims purely
 * by INDEX into the .bsr mesh list - the part-link tail maps coverKey -> prim index
 * (see parseAttachPartLink in formats.mjs). Mirror that: no filename heuristics.
 */
export const primSlot = (primIndex) => `prim${primIndex}`;

/**
 * Pre-transform (in place) every vertex fully weighted to a "baked" local bone by that
 * bone's bake transform {q,t}. Used for item-private bone BRANCHES: natively the whole
 * branch is parented under a named wearer bone in attach mode (+0xa0=1; CRTBranch_
 * LinkToParentBranch sub_abc680) and the root's update (CRTSocket_UpdateMatrices-
 * AttachRoot sub_ab5870) composes with the attach bone's SKIN matrix (bind-relative
 * delta) re-translated to the attach bone's world position - the attach BIND rotation
 * is cancelled. With branch bones at bind pose the whole item reduces to
 *   v_world = attachDelta * (v_model + attachBindWorld.t)
 * and the wearer's VAT row IS attachDelta (world * invBind), so the exact bake is the
 * pure translation v' = v_model + attachBindWorld.t (identity q).
 */
function bakeMeshVertices(mesh, bakeByLocal, meshPath) {
  let mixed = 0;
  for (let i = 0; i < mesh.vertexCount; i += 1) {
    const influences = [];
    for (let s = 0; s < 2; s += 1) {
      if (mesh.boneWeights[i * 2 + s] > 0) influences.push(mesh.boneIndices[i * 2 + s]);
    }
    if (influences.length === 0) continue;
    const bakes = influences.map((li) => bakeByLocal.get(li));
    if (bakes.every((b) => b === undefined)) continue;
    if (bakes.some((b) => b === undefined)) mixed += 1; // partial: bake with bone0's transform
    const bake = bakes.find((b) => b !== undefined);
    const [px, py, pz] = quatRotateComponents(
      bake.q,
      mesh.positions[i * 3],
      mesh.positions[i * 3 + 1],
      mesh.positions[i * 3 + 2]
    );
    mesh.positions[i * 3] = px + bake.t[0];
    mesh.positions[i * 3 + 1] = py + bake.t[1];
    mesh.positions[i * 3 + 2] = pz + bake.t[2];
    const [nx, ny, nz] = quatRotateComponents(
      bake.q,
      mesh.normals[i * 3],
      mesh.normals[i * 3 + 1],
      mesh.normals[i * 3 + 2]
    );
    mesh.normals[i * 3] = nx;
    mesh.normals[i * 3 + 1] = ny;
    mesh.normals[i * 3 + 2] = nz;
  }
  if (mixed > 0) {
    console.warn(`[avatar] ${meshPath}: ${mixed} vert(s) blend a baked and an unbaked bone; baked whole vertex`);
  }
}

/**
 * Assemble a skinned model from a character .bsr. Options:
 * - meshPaths/materialSetPaths: override the BSR sections (used for ITEM dress sets: the
 *   item's skinned mesh rides a donor skeleton; the runtime re-binds per wearer by name).
 * - slotForMesh(meshPath, primIndex): group label per part ("prim4", "part:BA", ...).
 * - acceptBone(boneName, meshPath): veto a DIRECT name match (item-private bones with
 *   generic names like "Bone01" can collide with an unrelated donor bone; the caller
 *   compares bind poses). Vetoed bones go through resolveUnboundBone instead.
 * - resolveUnboundBone(boneName, meshPath): map a bone the skeleton doesn't know to a
 *   bone it does. Return a bone name (ride that row as-is: nearest-ancestor telescoping)
 *   or { bone, bake: {q,t} } (ride that row AND pre-transform the bone's vertices by
 *   bake - the attach-bone branch case). Return null to give up.
 * - skipUnboundMeshes: drop a mesh whose unknown bone stays unresolved (else throw).
 */
export async function assembleAvatar(bsrAssetPath, options = {}) {
  const bsrBuffer = await loadDataAsset(bsrAssetPath);
  const bsr = parseCharacterBsr(bsrBuffer, bsrAssetPath);
  const bsrResource = parseJmxResourceBsr(bsrBuffer, bsrAssetPath);
  if (!bsr.skeletonPath) throw new Error(`${bsrAssetPath}: no skeleton`);

  const skeleton = parseBsk(await loadDataAsset(bsr.skeletonPath), bsr.skeletonPath);

  // Parse body-part meshes and remap each part's local bone list to global joints.
  const meshPaths = options.meshPaths ?? bsr.meshPaths;
  const parts = [];
  const skippedMeshes = [];
  const remappedBones = [];
  for (let primIndex = 0; primIndex < meshPaths.length; primIndex += 1) {
    const meshPath = meshPaths[primIndex];
    const mesh = parseSkinnedBms(await loadDataAsset(meshPath), meshPath);
    let unresolvedBone = null;
    let resolvedBoneCount = 0;
    const bakeByLocal = new Map(); // local bone index -> {q,t} vertex pre-transform
    const localToGlobal = mesh.boneNames.map((bn, localIndex) => {
      const gi = skeleton.byName.get(bn);
      // A direct name match can still be a COLLISION (item-private "Bone01" vs an
      // unrelated donor "Bone01"): natively item hierarchies resolve inside the item's
      // own CRTBranch, never against the wearer. Let the caller veto via bind compare.
      if (gi !== undefined && (!options.acceptBone || options.acceptBone(bn, meshPath))) {
        resolvedBoneCount += 1;
        return gi;
      }
      // Bone the export skeleton doesn't know (item dangle bones: capes "Bone05",
      // tassets...). Native never drops the mesh: the item gets its own CRTBranch
      // (RTSkeleton.cpp CRTBranch_Build sub_abe900) and a bone with no animation track
      // holds its bind-local pose under its animated ancestors, which makes its skin
      // matrix EQUAL to the nearest known ancestor's (world*invBind telescopes). Let the
      // caller resolve the bone via the item's OWN skeleton hierarchy - either to a
      // genuinely-shared ancestor, or to the item's ATTACH bone with a vertex bake.
      const fallback = options.resolveUnboundBone?.(bn, meshPath);
      const fbName = typeof fallback === "string" ? fallback : fallback?.bone;
      const fi = fbName !== null && fbName !== undefined ? skeleton.byName.get(fbName) : undefined;
      if (fi === undefined) {
        unresolvedBone = bn;
        return 0;
      }
      if (typeof fallback === "object" && fallback.bake) bakeByLocal.set(localIndex, fallback.bake);
      remappedBones.push({ meshPath, bone: bn, fallback: fbName, baked: typeof fallback === "object" && !!fallback.bake });
      return fi;
    });
    if (unresolvedBone !== null && options.rigidUnboundMeshes && resolvedBoneCount === 0) {
      // Native PrimGeometry_BakeSkinBlendWeights (sub_a85490) leaves names
      // that cannot bind to the compound skeleton at the 0xff rigid sentinel.
      // This is how authored model-local props such as the West-China smith
      // hammer remain drawable without becoming a build-fatal fake bone.
      mesh.rigid = true;
    } else if (unresolvedBone !== null) {
      if (options.skipUnboundMeshes) {
        skippedMeshes.push({ meshPath, bone: unresolvedBone });
        continue;
      }
      throw new Error(`${meshPath}: bone "${unresolvedBone}" not in skeleton`);
    }
    if (!mesh.rigid && bakeByLocal.size > 0) bakeMeshVertices(mesh, bakeByLocal, meshPath);
    const slot = options.slotForMesh ? options.slotForMesh(meshPath, primIndex) : null;
    const environmentModifiers=options.environmentModifiersForMesh?options.environmentModifiersForMesh(meshPath):bsrResource.modifiers?.environmentModifiers??[];
    parts.push({ meshPath, mesh, localToGlobal, slot, environmentModifiers, bsrModifiers:options.bsrModifiersForMesh?options.bsrModifiersForMesh(meshPath):options.meshPaths?undefined:{materialModifiers:bsrResource.modifiers?.materialModifiers??[],textureModifiers:bsrResource.modifiers?.textureModifiers??[]} });
  }

  // Default-set motion ids are the native authority: 0=stand, 1=walk, 7=run.
  // Filename matching was a reconstruction-era approximation and must never
  // choose a production clip now that the BSR animation-set table is decoded.
  const walkPath = options.noClips ? null : options.clipPath ?? pickDefaultSetStateClip(bsr, 1);
  const runPath = options.noClips ? null : pickDefaultSetStateClip(bsr, 7);
  const clip = walkPath ? parseBan(await loadDataAsset(walkPath), walkPath) : null;

  // Clips baked into the GLB so the runtime can pose per use: 'walk'/'run' for
  // PathCtl speed channels 0/1, 'stand' for idle (native ANI_STAND1 between move
  // commands), 'ride' (the exact "cart" animation-set state 0 selected by
  // Bionic_MountRider sub_871420), the four native basic-attack states, and the native
  // reaction/down family, and death/deathloop/deathquick state-1 trio.
  // Dress-item GLBs carry NO clips: their meshes reuse the WEARER's baked VAT matrices
  // (same skeleton file, same joint order), so embedded animation would be dead weight.
  const clips = [];
  if (clip) clips.push({ role: "walk", path: walkPath, clip });
  if (!options.noClips) {
    if (runPath && runPath !== walkPath) {
      clips.push({ role: "run", path: runPath, clip: parseBan(await loadDataAsset(runPath), runPath) });
    }
    const standPath = pickDefaultSetStateClip(bsr, 0);
    if (standPath && standPath !== walkPath) {
      clips.push({ role: "stand", path: standPath, clip: parseBan(await loadDataAsset(standPath), standPath) });
    }
    // InitGlobalAnimationSetNameStrings 0xbbdb70 proves data_cccd30 == "cart";
    // Bionic_MountRider calls PlayMotionEx(data_cccd30, 0, ...). Resolve that
    // authored set/state pair directly. Mount BSRs legitimately have no cart set.
    const ridePath = pickAnimationSetStateClip(bsr, "cart", 0);
    if (ridePath && ridePath !== walkPath) {
      clips.push({ role: "ride", path: ridePath, clip: parseBan(await loadDataAsset(ridePath), ridePath) });
    }
    // 'pick' = the ground-item pickup scoop (motion 0x26 / ANI_PICK, played
    // once by the state-10 enter when the server's 0x35C7 anim trigger lands).
    const pickPath = pickPickupClip(bsr);
    if (pickPath && pickPath !== walkPath) {
      clips.push({ role: "pick", path: pickPath, clip: parseBan(await loadDataAsset(pickPath), pickPath) });
    }
    for (const motion of pickAttachedMotionClips(bsr)) clips.push({role:motion.role,path:motion.path,clip:parseBan(await loadDataAsset(motion.path),motion.path)});
    for (const attackClip of pickAttackStateClips(bsr)) {
      clips.push({
        role: attackClip.role,
        path: attackClip.path,
        clip: parseBan(await loadDataAsset(attackClip.path), attackClip.path)
      });
    }
    for (const reactionClip of [...pickReactionStateClips(bsr), ...pickIdleStateClips(bsr), ...pickStateClips(bsr,NATIVE_EMOTE_STATE_CLIPS)]) {
      clips.push({
        role: reactionClip.role,
        path: reactionClip.path,
        clip: parseBan(await loadDataAsset(reactionClip.path), reactionClip.path)
      });
    }
    for (const deathClip of pickDeathStateClips(bsr)) {
      clips.push({
        role: deathClip.role,
        path: deathClip.path,
        clip: parseBan(await loadDataAsset(deathClip.path), deathClip.path)
      });
    }
    if (options.previewWeaponClips) {
      for (const previewClip of pickPreviewWeaponClips(bsr)) {
        clips.push({
          role: previewClip.role,
          path: previewClip.path,
          clip: parseBan(await loadDataAsset(previewClip.path), previewClip.path)
        });
      }
      for (const attackClip of pickWeaponAttackStateClips(bsr)) {
        clips.push({
          role: attackClip.role,
          path: attackClip.path,
          clip: parseBan(await loadDataAsset(attackClip.path), attackClip.path)
        });
      }
    }
    if (options.characterSelectStateClips) {
      for (const stateClip of pickCharacterSelectStateClips(bsr)) {
        clips.push({
          role: stateClip.role,
          path: stateClip.path,
          clip: parseBan(await loadDataAsset(stateClip.path), stateClip.path)
        });
      }
    }
  }

  // Materials come from the BSR header — same rule as static world objects.
  const materialSetPaths = options.materialSetPaths ?? bsrResource.materialPaths;
  const materials = await loadMaterialTextures(materialSetPaths, {
    onWarning: (msg) => console.warn(`[avatar] ${msg}`)
  });
  if (materials.size === 0 && materialSetPaths.length === 0) {
    console.warn(`[avatar] ${bsrAssetPath}: BSR lists no material sets; GLB will be untextured`);
  }

  return {
    bsrAssetPath,
    // CResObject box1 (+0x280) of the base resource: the compound's pick
    // box. CCompound_AttachResourceWithMaterialSet (A9E310) unions later
    // attachments only when part 1 is not kind 0, so a character body's box
    // stands alone under its equipment.
    aggregateBox: options.aggregateBox ?? bsrResource.aggregateBox,
    name: options.name ?? bsr.name,
    skeleton,
    parts,
    skippedMeshes,
    remappedBones,
    walkPath,
    runPath,
    clips,
    materials,
    animationPaths: bsr.animationPaths,
    // Native dress part-link tail ({coverKey -> prim index}; null for mounts/props).
    partLink: bsr.partLink
  };
}

function bbox(avatar) {
  const min = [Infinity, Infinity, Infinity];
  const max = [-Infinity, -Infinity, -Infinity];
  let verts = 0, tris = 0;
  for (const { mesh } of avatar.parts) {
    verts += mesh.vertexCount; tris += mesh.triangleCount;
    for (let i = 0; i < mesh.vertexCount; i += 1) {
      for (let a = 0; a < 3; a += 1) {
        const v = mesh.positions[i * 3 + a];
        if (v < min[a]) min[a] = v;
        if (v > max[a]) max[a] = v;
      }
    }
  }
  return { min, max, verts, tris };
}

// ---- CLI sanity dump
if (isMainScript(import.meta.url)) {
  const target = process.argv[2] ?? "res/char/europe/europeman_adventurer.bsr";
  const avatar = await assembleAvatar(target);
  const bb = bbox(avatar);
  console.log(`avatar "${avatar.name}"  (${avatar.bsrAssetPath})`);
  console.log(`  skeleton: ${avatar.skeleton.boneCount} bones; root="${avatar.skeleton.bones[0].name}"`);
  console.log(`  parts: ${avatar.parts.length}`);
  for (const p of avatar.parts) {
    console.log(`    - ${p.mesh.meshName} v=${p.mesh.vertexCount} t=${p.mesh.triangleCount} mat="${p.mesh.materialName}" bones=[${p.mesh.boneNames.length}] -> global[${p.localToGlobal.join(",")}]`);
  }
  console.log(`  geometry: ${bb.verts} verts, ${bb.tris} tris`);
  console.log(`  bbox min=[${bb.min.map((v) => v.toFixed(2))}] max=[${bb.max.map((v) => v.toFixed(2))}]  height=${(bb.max[1] - bb.min[1]).toFixed(2)}`);
  const walkClip = avatar.clips.find((entry) => entry.role === "walk")?.clip;
  if (walkClip) {
    const c = walkClip;
    console.log(`  walk clip: "${c.name}" (${avatar.walkPath})`);
    console.log(`    durationMs=${c.durationMs} frames=${c.frameCount} animBones=${c.animBoneCount}`);
    const allInSkel = c.bones.every((b) => avatar.skeleton.byName.has(b.name));
    console.log(`    all anim bones present in skeleton: ${allInSkel}`);
  } else {
    console.log("  walk clip: NONE FOUND");
  }
  console.log(`  run clip: ${avatar.runPath ?? "NONE FOUND"}`);
  // weight sanity
  let bad = 0;
  for (const { mesh } of avatar.parts) {
    for (let i = 0; i < mesh.vertexCount; i += 1) {
      const s = mesh.boneWeights[i * 2] + mesh.boneWeights[i * 2 + 1];
      if (Math.abs(s - 1) > 1e-3) bad += 1;
    }
  }
  console.log(`  vertices with non-normalized weights: ${bad}`);
}
