import {authoredAnimationBindings} from './authoredAnimationBindings.mjs';
// Common compiler for generic CResource BSR visuals. This lane intentionally
// covers generic static BSRs reached from drop, skill-stage, and structure
// RefObj rows. Character/NPC skeleton animation and animated world objects keep
// their explicit domain policies; a static structure does not become a fake
// character merely because its RefObj shares the client NPC class band.

import { assembleAvatar, primSlot } from "./buildAvatar.mjs";
import { avatarToGlb } from "./exportGlb.mjs";
import { parseBan, parseCharacterBsr } from "./formats.mjs";
import { loadDataAsset, loadMaterialTextures } from "../shared/jmxAssetIO.mjs";
import { parseJmxBmsStaticMesh, parseJmxResourceBsr } from "../world/objects/formats.mjs";

/**
 * Assemble a static BSR with a one-joint stand-in rig so the normal avatar GLB
 * exporter can preserve its mesh/material/Z-flip contract.
 */
export async function assembleStaticBsrModel(bsrAssetPath) {
  const bsr = parseJmxResourceBsr(await loadDataAsset(bsrAssetPath), bsrAssetPath);
  if (bsr.meshPaths.length === 0) {
    throw new Error(`${bsrAssetPath}: no meshes`);
  }
  const materials = await loadMaterialTextures(bsr.materialPaths, {
    // Preserve the historical generic-compiler warning tag for log parity.
    onWarning: (message) => console.warn(`[itemdrop] ${message}`)
  });
  const parts = [];
  for (const [index, meshPath] of bsr.meshPaths.entries()) {
    const mesh = parseJmxBmsStaticMesh(await loadDataAsset(meshPath), meshPath);
    const boneIndices = new Uint16Array(mesh.vertexCount * 2);
    const boneWeights = new Float32Array(mesh.vertexCount * 2);
    for (let vertexIndex = 0; vertexIndex < mesh.vertexCount; vertexIndex += 1) {
      boneWeights[vertexIndex * 2] = 1;
    }
    parts.push({
      slot: primSlot(index),
      bsrModifiers:{materialModifiers:bsr.modifiers?.materialModifiers??[],textureModifiers:bsr.modifiers?.textureModifiers??[]},
      localToGlobal: [0],
      mesh: {
        meshName: mesh.metadata.meshName,
        materialName: mesh.metadata.materialName || "default",
        vertexCount: mesh.vertexCount,
        triangleCount: mesh.triangleCount,
        positions: mesh.positions,
        normals: mesh.normals,
        uvs: mesh.uvs,
        indices: mesh.indices,
        boneIndices,
        boneWeights
      }
    });
  }
  const rootBone = {
    index: 0,
    type: 0,
    name: "root",
    parent: "",
    local: { q: [0, 0, 0, 1], t: [0, 0, 0] },
    world: { q: [0, 0, 0, 1], t: [0, 0, 0] },
    invWorld: { q: [0, 0, 0, 1], t: [0, 0, 0] },
    parentIndex: -1
  };
  return {
    aggregateBox: bsr.aggregateBox,
    skeleton: { boneCount: 1, bones: [rootBone], byName: new Map([["root", 0]]) },
    parts,
    materials,
    animationPaths: []
  };
}

/** Compile one generic authored BSR visual, with an optional state-zero clip. */
export async function compileBsrVisualToGlb(
  bsrPath,
  { includeStateZeroClip = true, stateIds = [],allStates=false } = {}
) {
  let avatar;
  try {
    avatar = await assembleAvatar(bsrPath, {
      noClips: true,
      slotForMesh: (_meshPath, index) => primSlot(index)
    });
  } catch (error) {
    if (!String(error?.message ?? error).includes("no skeleton")) throw error;
    avatar = await assembleStaticBsrModel(bsrPath);
  }

  const clips = [];
  let clipLoop = false;
  const bsr = parseCharacterBsr(await loadDataAsset(bsrPath), bsrPath);
  if (includeStateZeroClip) {
    const idlePath = resolveStateZeroClipPath(bsr);
    if (idlePath) {
      const clip = parseBan(await loadDataAsset(idlePath), idlePath);
      clipLoop = (clip.field2 ?? 0) === 1;
      clips.push({ role: "stand", path: idlePath, clip });
    }
  }
  const states = {};
  const set = bsr.animationSets?.find(set => set.name?.toLowerCase() === 'default') ?? bsr.animationSets?.[0];
  for (const id of stateIds) {
    const state = set?.states.find(state => state.stateId === id);
    if (!state?.animationPath) throw Error(`${bsrPath}: missing required animation state ${id}`);
    const clip = parseBan(await loadDataAsset(state.animationPath), state.animationPath), role = `state-${id}`;
    clips.push({role, path:state.animationPath, clip});
    states[id] = {clip:role, durationMs:clip.durationMs, loop:clip.field2===1, trackEvents:state.trackEvents};
  }
  const animationBindings=allStates?await authoredAnimationBindings(bsr,clips,loadDataAsset):undefined;
  return {
    animationBindings,glb: avatarToGlb({ ...avatar, clips }),
    clips,
    clipLoop,
    states,
    modifierSets:bsr.modifierSets,particleModifiers: bsr.particleModifiers,
    materialModifiers:bsr.materialModifiers,textureModifiers:bsr.textureModifiers
  };
}

function resolveStateZeroClipPath(bsr) {
  const sets = bsr.animationSets ?? [];
  const defaultSet = sets.find((set) => set.name?.toLowerCase() === "default") ?? sets[0];
  const stateZero = defaultSet?.states?.find((state) => state.stateId === 0);
  return stateZero?.animationPath ?? bsr.animationPaths?.[0] ?? null;
}
