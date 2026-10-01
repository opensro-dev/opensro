import {parseModDataSection} from '../../shared/bsrModifiers.mjs';
import path from "node:path";
import { sha256Hex } from "../../shared/hash.mjs";
import { readCountedString } from "../../shared/jmxBinaryReader.mjs";
import { SIGNATURE_BYTES } from "../constants.mjs";
import { cleanFloat, ensureAvailable, readSignature } from "../jmx/common.mjs";
import { normalizeAssetPath } from "../paths.mjs";

export const BSR_SIGNATURE = "JMXVRES 0109";
export const BMS_SIGNATURE = "JMXVBMS 0110";
export const BMT_SIGNATURE = "JMXVBMT 0102";
export const BMS_STATIC_VERTEX_BYTES = 44;
export const BMS_STATIC_VERTEX_BYTES_WITH_SECONDARY_UV = 52;

const BMS_VERTEX_FLAG_SECONDARY_UV = 0x400;

const MAX_BSR_MATERIALS = 256;
const MAX_BSR_MESHES = 512;
const MAX_BMS_VERTICES = 1_000_000;
const MAX_BMS_TRIANGLES = 1_000_000;
const MAX_BMT_MATERIALS = 1024;

export function parseJmxResourceBsr(buffer, sourcePath = "<memory>") {
  const signature = readSignature(buffer, BSR_SIGNATURE, sourcePath);
  ensureAvailable(buffer, 0, 0x40, sourcePath);

  const headerOffsets = readUInt32Array(buffer, 0x0c, 13);
  const metadata = readBsrMetadata(buffer, sourcePath);
  const primaryMeshPath = readOptionalCountedPath(buffer, headerOffsets[7], sourcePath);
  const authoredBoxes = readBsrAuthoredBoxes(buffer, headerOffsets[7]);
  const materials = readBsrMaterialSection(buffer, headerOffsets[0], sourcePath);
  const renderMeshes = readBsrMeshSection(buffer, headerOffsets[1], sourcePath);
  const meshPaths = uniqueNormalizedPaths([primaryMeshPath, ...renderMeshes.paths].filter(Boolean));

  return {
    sourcePath: normalizeAssetPath(sourcePath),
    signature,
    byteLength: buffer.length,
    headerOffsets,
    metadata,
    modifiers:headerOffsets[6]?parseModDataSection(buffer,headerOffsets[6]):undefined,
    primaryMeshPath: primaryMeshPath ? normalizeAssetPath(primaryMeshPath) : null,
    // CResObject_LoadFromArchive (client A4FF00): box1 -> +0x280 is the box a
    // compound unions into its aggregate pick box; box2 -> +0x298.
    aggregateBox: authoredBoxes?.box1 ?? null,
    secondaryBox: authoredBoxes?.box2 ?? null,
    materialSection: {
      byteOffset: headerOffsets[0],
      count: materials.count,
      setIds: materials.setIds,
      paths: materials.paths.map(normalizeAssetPath),
      consumedBytes: materials.consumedBytes
    },
    renderMeshSection: {
      byteOffset: headerOffsets[1],
      count: renderMeshes.count,
      paths: renderMeshes.paths.map(normalizeAssetPath),
      entryFlags: renderMeshes.entryFlags,
      consumedBytes: renderMeshes.consumedBytes
    },
    meshPaths,
    materialPaths: materials.paths.map(normalizeAssetPath)
  };
}

export function parseJmxBmsStaticMesh(buffer, sourcePath = "<memory>") {
  const signature = readSignature(buffer, BMS_SIGNATURE, sourcePath);
  ensureAvailable(buffer, 0, 0x3c, sourcePath);

  const headerOffsets = readUInt32Array(buffer, 0x0c, 12);
  const metadata = readBmsMetadata(buffer, sourcePath);
  const vertices = readBmsStaticVertices(buffer, headerOffsets[0], headerOffsets[1], metadata, sourcePath);
  const triangles = readBmsTriangleIndices(buffer, headerOffsets[2], headerOffsets[3], sourcePath);
  // headerOffsets[4] is a 4-byte record (observed always 0); the 24-byte AABB lives at
  // headerOffsets[5]. Reading at [4] shifts the box by one float and yields invalid boxes
  // (max < min), which broke the native visibility radius (sub_8c4c00 parity).
  const bounds = readBmsBounds(buffer, headerOffsets[5], sourcePath);
  const nativePayloads = readBmsNativePayloads(buffer, headerOffsets, sourcePath);

  return {
    sourcePath: normalizeAssetPath(sourcePath),
    signature,
    byteLength: buffer.length,
    headerOffsets,
    metadata,
    vertexLayout: {
      byteStride: vertices.byteStride,
      attributes: vertices.attributes
    },
    vertexCount: vertices.vertexCount,
    triangleCount: triangles.triangleCount,
    positions: vertices.positions,
    normals: vertices.normals,
    uvs: vertices.uvs,
    vertexUnknowns: vertices.unknowns,
    indices: triangles.indices,
    bounds,
    ...(nativePayloads.length > 0 ? { nativePayloads } : {})
  };
}

export function parseJmxBmtMaterialSet(buffer, sourcePath = "<memory>") {
  const signature = readSignature(buffer, BMT_SIGNATURE, sourcePath);
  ensureAvailable(buffer, SIGNATURE_BYTES, 4, sourcePath);

  let offset = SIGNATURE_BYTES;
  const materialCount = buffer.readUInt32LE(offset);
  offset += 4;

  if (materialCount > MAX_BMT_MATERIALS) {
    throw new Error(`${sourcePath}: suspicious BMT material count ${materialCount}`);
  }

  const materials = [];
  for (let materialIndex = 0; materialIndex < materialCount; materialIndex += 1) {
    const byteOffset = offset;
    const name = readCountedString(buffer, offset, sourcePath);
    offset = name.nextOffset;

    const colorGroups = [];
    for (let groupIndex = 0; groupIndex < 4; groupIndex += 1) {
      ensureAvailable(buffer, offset, 16, sourcePath);
      colorGroups.push([
        cleanFloat(buffer.readFloatLE(offset)),
        cleanFloat(buffer.readFloatLE(offset + 4)),
        cleanFloat(buffer.readFloatLE(offset + 8)),
        cleanFloat(buffer.readFloatLE(offset + 12))
      ]);
      offset += 16;
    }

    ensureAvailable(buffer, offset, 8, sourcePath);
    // Native CPrimMtrl serializer (sub_a62d90 @00a62e94) writes this slot as a FLOAT
    // (struct +0x50, init default 0.5 in sub_a63350): the material alpha reference.
    const field0 = buffer.readUInt32LE(offset);
    const alphaRef = cleanFloat(buffer.readFloatLE(offset));
    offset += 4;
    const flags = buffer.readUInt32LE(offset);
    offset += 4;

    const textureName = readCountedString(buffer, offset, sourcePath);
    offset = textureName.nextOffset;

    ensureAvailable(buffer, offset, 7, sourcePath);
    const textureScale = cleanFloat(buffer.readFloatLE(offset));
    offset += 4;
    const renderStateTail = [buffer[offset], buffer[offset + 1], buffer[offset + 2]];
    offset += 3;

    materials.push({
      materialIndex,
      byteOffset,
      name: name.value,
      colors: {
        ambient: colorGroups[0],
        diffuse: colorGroups[1],
        specular: colorGroups[2],
        emissive: colorGroups[3]
      },
      field0,
      alphaRef,
      flags,
      textureName: normalizeAssetPath(textureName.value),
      textureScale,
      renderStateTailHex: renderStateTail.map((value) => value.toString(16).padStart(2, "0")).join(" ")
    });
  }

  if (offset !== buffer.length) {
    throw new Error(`${sourcePath}: consumed ${offset} bytes but file has ${buffer.length}`);
  }

  return {
    sourcePath: normalizeAssetPath(sourcePath),
    signature,
    byteLength: buffer.length,
    materialCount,
    materials
  };
}

export function resolveBmtTexturePath(materialSetPath, textureName) {
  const normalizedTexture = normalizeAssetPath(textureName);
  if (!normalizedTexture) {
    return null;
  }

  if (normalizedTexture.includes("/")) {
    return normalizedTexture;
  }

  return normalizeAssetPath(path.posix.join(path.posix.dirname(normalizeAssetPath(materialSetPath)), normalizedTexture));
}

function readBsrMetadata(buffer, sourcePath) {
  ensureAvailable(buffer, 0x40, 8, sourcePath);
  const kind0 = buffer.readUInt16LE(0x40);
  const kind1 = buffer.readUInt16LE(0x42);
  const name = readCountedString(buffer, 0x44, sourcePath);

  return {
    kind0,
    kind1,
    name: name.value
  };
}

function readBmsMetadata(buffer, sourcePath) {
  ensureAvailable(buffer, 0x3c, 12, sourcePath);
  let offset = 0x3c;
  const unknown0 = buffer.readUInt32LE(offset);
  offset += 4;
  const unknown1 = buffer.readUInt32LE(offset);
  offset += 4;
  const unknown2 = buffer.readUInt32LE(offset);
  offset += 4;

  const meshName = readCountedString(buffer, offset, sourcePath);
  offset = meshName.nextOffset;
  const materialName = readCountedString(buffer, offset, sourcePath);
  offset = materialName.nextOffset;

  return {
    unknown0,
    unknown1,
    unknown2,
    meshName: meshName.value,
    materialName: materialName.value,
    consumedBytes: offset
  };
}

// Section @ptr[0]: u32 count, then count x { u32 setId, string bmtPath }.
// The per-entry LEADING u32 is a material-set id, not a section-level field:
// the old {count, unknown0, strings...} read treated entry 1's id as
// "unknown0" (so entry 1 parsed by luck) and then consumed every later
// entry's id as a tiny junk string - the long-standing benign-looking
// "could not load material set '\x00'" bake warnings. Files with 3+ sets
// (chakji/bandit/whitetiger/gyo/waterghost/movoi .bsr, MONSTER-LIVE bake
// wave) walked into string bytes and threw. Geometry verified byte-exact:
// tiger.bsr 2 entries end exactly at ptr[1]=265, chakji.bsr 3 entries
// (base/clone/champ) end exactly at ptr[1]=312.
function readBsrMaterialSection(buffer, offset, sourcePath) {
  ensureAvailable(buffer, offset, 4, sourcePath);
  const count = buffer.readUInt32LE(offset);
  offset += 4;

  if (count > MAX_BSR_MATERIALS) {
    throw new Error(`${sourcePath}: suspicious BSR material count ${count}`);
  }

  const paths = [];
  const setIds = [];
  for (let index = 0; index < count; index += 1) {
    ensureAvailable(buffer, offset, 4, sourcePath);
    setIds.push(buffer.readUInt32LE(offset));
    offset += 4;
    const item = readCountedString(buffer, offset, sourcePath);
    paths.push(item.value);
    offset = item.nextOffset;
  }

  return { count, setIds, paths, consumedBytes: offset };
}

function readBsrMeshSection(buffer, offset, sourcePath) {
  ensureAvailable(buffer, offset, 4, sourcePath);
  const count = buffer.readUInt32LE(offset);
  offset += 4;

  if (count > MAX_BSR_MESHES) {
    throw new Error(`${sourcePath}: suspicious BSR mesh count ${count}`);
  }

  const paths = [];
  const entryFlags = [];
  for (let index = 0; index < count; index += 1) {
    const item = readBsrMeshEntry(buffer, offset, sourcePath);
    paths.push(item.value);
    entryFlags.push(item.entryFlag);
    offset = item.nextOffset;
  }

  return { count, paths, entryFlags, consumedBytes: offset };
}

function readBsrMeshEntry(buffer, offset, sourcePath) {
  const direct = readCountedString(buffer, offset, sourcePath);
  if (isBmsPath(direct.value)) {
    return { ...direct, entryFlag: null };
  }

  // Some China JMXVRES 0109 resources keep a small per-mesh flag/index before
  // every render-mesh path after entry 0 (for example cj_pub01.bsr). The title
  // subset did not exercise this, so fall back only when the direct counted
  // string is not a mesh path.
  ensureAvailable(buffer, offset, 8, sourcePath);
  const entryFlag = buffer.readUInt32LE(offset);
  const flagged = readCountedString(buffer, offset + 4, sourcePath);
  if (!isBmsPath(flagged.value)) {
    throw new Error(
      `${sourcePath}: BSR mesh entry at ${offset} is neither a direct nor flagged BMS path ` +
        `(direct=${JSON.stringify(direct.value)}, flag=${entryFlag}, flagged=${JSON.stringify(flagged.value)})`
    );
  }

  return { ...flagged, entryFlag };
}

function isBmsPath(value) {
  return /(?:^|[\\/])prim[\\/]mesh[\\/].+\.bms$/i.test(normalizeAssetPath(value));
}

function readBmsStaticVertices(buffer, vertexOffset, nextOffset, metadata, sourcePath) {
  ensureAvailable(buffer, vertexOffset, 4, sourcePath);
  const vertexCount = buffer.readUInt32LE(vertexOffset);
  if (vertexCount > MAX_BMS_VERTICES) {
    throw new Error(`${sourcePath}: suspicious BMS vertex count ${vertexCount}`);
  }

  const layout = selectBmsVertexLayout(metadata);
  const verticesOffset = vertexOffset + 4;
  const vertexEnd = verticesOffset + vertexCount * layout.byteStride;
  ensureAvailable(buffer, verticesOffset, vertexCount * layout.byteStride, sourcePath);
  if (nextOffset && vertexEnd > nextOffset) {
    throw new Error(`${sourcePath}: BMS vertex section ends at ${vertexEnd}, past next header offset ${nextOffset}`);
  }

  const positions = [];
  const normals = [];
  const uvs = [];
  const unknowns = [];

  for (let vertexIndex = 0; vertexIndex < vertexCount; vertexIndex += 1) {
    const offset = verticesOffset + vertexIndex * layout.byteStride;
    positions.push(
      cleanFloat(buffer.readFloatLE(offset)),
      cleanFloat(buffer.readFloatLE(offset + 4)),
      cleanFloat(buffer.readFloatLE(offset + 8))
    );
    normals.push(
      cleanFloat(buffer.readFloatLE(offset + 12)),
      cleanFloat(buffer.readFloatLE(offset + 16)),
      cleanFloat(buffer.readFloatLE(offset + 20))
    );
    uvs.push(cleanFloat(buffer.readFloatLE(offset + 24)), cleanFloat(buffer.readFloatLE(offset + 28)));
    const unknownOffset = offset + layout.unknownOffset;
    unknowns.push(
      buffer.readUInt32LE(unknownOffset),
      buffer.readUInt32LE(unknownOffset + 4),
      buffer.readUInt32LE(unknownOffset + 8)
    );
  }

  return {
    vertexCount,
    byteStride: layout.byteStride,
    attributes: layout.attributes,
    positions,
    normals,
    uvs,
    unknowns
  };
}

function selectBmsVertexLayout(metadata) {
  if (metadata.unknown1 & BMS_VERTEX_FLAG_SECONDARY_UV) {
    // CRITICAL: China/Jangan BMS meshes use flag 0x400 for a 52-byte static
    // vertex: pos3 + normal3 + uv0 + uv1 + u32x3. The title-sector-only
    // 44-byte assumption desynchronizes after vertex 0 and renders UVs/0xffffffff
    // as positions, producing the large "exploding mesh" sheets in China create.
    return {
      byteStride: BMS_STATIC_VERTEX_BYTES_WITH_SECONDARY_UV,
      unknownOffset: 40,
      attributes: ["position:f32x3", "normal:f32x3", "uv0:f32x2", "uv1:f32x2", "unknown:u32x3"]
    };
  }

  return {
    byteStride: BMS_STATIC_VERTEX_BYTES,
    unknownOffset: 32,
    attributes: ["position:f32x3", "normal:f32x3", "uv:f32x2", "unknown:u32x3"]
  };
}

function readBmsTriangleIndices(buffer, indexOffset, nextOffset, sourcePath) {
  ensureAvailable(buffer, indexOffset, 4, sourcePath);
  const triangleCount = buffer.readUInt32LE(indexOffset);
  if (triangleCount > MAX_BMS_TRIANGLES) {
    throw new Error(`${sourcePath}: suspicious BMS triangle count ${triangleCount}`);
  }

  const indexCount = triangleCount * 3;
  const indicesOffset = indexOffset + 4;
  const indexEnd = indicesOffset + indexCount * 2;
  ensureAvailable(buffer, indicesOffset, indexCount * 2, sourcePath);
  if (nextOffset && indexEnd > nextOffset) {
    throw new Error(`${sourcePath}: BMS index section ends at ${indexEnd}, past next header offset ${nextOffset}`);
  }

  const indices = [];
  for (let index = 0; index < indexCount; index += 1) {
    indices.push(buffer.readUInt16LE(indicesOffset + index * 2));
  }

  return { triangleCount, indices };
}

function readBmsBounds(buffer, boundsOffset, sourcePath) {
  if (!boundsOffset || boundsOffset + 24 > buffer.length) {
    return null;
  }

  ensureAvailable(buffer, boundsOffset, 24, sourcePath);
  return {
    min: [
      cleanFloat(buffer.readFloatLE(boundsOffset)),
      cleanFloat(buffer.readFloatLE(boundsOffset + 4)),
      cleanFloat(buffer.readFloatLE(boundsOffset + 8))
    ],
    max: [
      cleanFloat(buffer.readFloatLE(boundsOffset + 12)),
      cleanFloat(buffer.readFloatLE(boundsOffset + 16)),
      cleanFloat(buffer.readFloatLE(boundsOffset + 20))
    ]
  };
}

function readBmsNativePayloads(buffer, headerOffsets, sourcePath) {
  const offset = headerOffsets[7];
  if (!offset || offset >= buffer.length) {
    return [];
  }

  ensureAvailable(buffer, offset, buffer.length - offset, sourcePath);
  const raw = buffer.subarray(offset);
  return [
    {
      kind: "bms-offset7-post-payload-tail",
      headerOffsetIndex: 7,
      byteOffset: offset,
      byteLength: raw.length,
      countHint: raw.length >= 4 ? buffer.readUInt32LE(offset) : null,
      sha256: sha256Hex(raw),
      rawBase64: raw.toString("base64"),
      evidenceSource: "JMXVBMS0110_BMS_static_mesh_layout"
    }
  ];
}

// A4FF00 seeks to header offset[7], reads the counted primary-mesh path,
// then two boxes of six floats (min xyz, max xyz).
function readBsrAuthoredBoxes(buffer, offset) {
  if (!offset || offset + 4 > buffer.length) {
    return null;
  }
  const start = offset + 4 + buffer.readUInt32LE(offset);
  if (start + 48 > buffer.length) {
    return null;
  }
  const box = at => Array.from({ length: 6 }, (_, i) => cleanFloat(buffer.readFloatLE(at + i * 4)));
  return { box1: box(start), box2: box(start + 24) };
}

function readOptionalCountedPath(buffer, offset, sourcePath) {
  if (!offset || offset + 4 > buffer.length) {
    return null;
  }

  const item = readCountedString(buffer, offset, sourcePath);
  if (item.nextOffset > buffer.length || !item.value) {
    return null;
  }

  return item.value;
}


function readUInt32Array(buffer, offset, count) {
  return Array.from({ length: count }, (_, index) => buffer.readUInt32LE(offset + index * 4));
}

function uniqueNormalizedPaths(paths) {
  return [...new Set(paths.map(normalizeAssetPath))];
}
