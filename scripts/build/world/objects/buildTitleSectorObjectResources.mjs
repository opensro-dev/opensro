/*
===========================================================================

buildTitleSectorObjectResources.mjs - the shared world object publisher

Parses one sector's BSR objects (materials, meshes, placements) and
publishes their textures. DXT power-of-two DDJ sources ship verbatim as
NTX1 .texture - the authored GPU blocks with a generated mip suffix -
everything else keeps the converted PNG path.

===========================================================================
*/
import { copyFile, mkdir, open, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { CPD_SIGNATURE, parseCompound } from "./compound.mjs";
import { exists } from "../io.mjs";
import { imageSourceRoot, normalizeAssetPath, publicRoot, toGameRelative } from "../paths.mjs";
import { runPython } from "../../shared/pythonRun.mjs";
import {
  BSR_SIGNATURE,
  parseJmxBmsStaticMesh,
  parseJmxBmtMaterialSet,
  parseJmxResourceBsr,
  resolveBmtTexturePath
} from "./formats.mjs";

const DDJ_HEADER_SIZE = 20;
const DDS_HEADER_BYTES = 148;
const DDS_HEIGHT_OFFSET = 32;
const DDS_WIDTH_OFFSET = 36;
const DDS_PIXEL_FORMAT_OFFSET = 100;
const DDPF_FOURCC = 0x4;
const BLOCK_FOURCCS = new Map([
  [0x31545844, "dxt1"],
  [0x33545844, "dxt3"],
  [0x35545844, "dxt5"]
]);
const ENCODER_PATH = fileURLToPath(new URL("../../native_texture_mips.py", import.meta.url));

export async function buildTitleSectorObjectResources(options) {
  const sourceExtractedRoot = options.extractedRoot;
  const sourceGameRoot = options.gameRoot;
  const area = options.area;
  const placementCounts =
    options.placementCountsByObjectId instanceof Map
      ? options.placementCountsByObjectId
      : countPlacementsByObjectId(options.placements ?? []);
  const missing = [];

  const bsrResources = [];
  const materialPathSet = new Set();
  const meshPathSet = new Set();

  async function loadBranch(sourcePath, definition) {
    const absolutePath = dataAssetPath(sourceExtractedRoot, sourcePath);
    if (!(await exists(absolutePath))) {
      missing.push({ type: "bsr", sourcePath });
      return null;
    }
    const buffer = await readFile(absolutePath);
    if (buffer.subarray(0, BSR_SIGNATURE.length).toString("latin1") !== BSR_SIGNATURE) {
      missing.push({ type: "unsupported-object-resource", sourcePath });
      return null;
    }
    const resource = parseJmxResourceBsr(buffer, sourcePath);
    for (const materialPath of resource.materialPaths) materialPathSet.add(materialPath);
    for (const meshPath of resource.meshPaths) meshPathSet.add(meshPath);
    return {
      ...resource,
      objectId: definition.objectId,
      objectFlags: definition.flags,
      placementCount: placementCounts.get(definition.objectId) ?? 0,
      sourceGamePath: toGameRelative(absolutePath, sourceGameRoot)
    };
  }

  for (const definition of options.objectDefinitions) {
    const sourcePath = normalizeAssetPath(definition.sourcePath);
    const absolutePath = dataAssetPath(sourceExtractedRoot, sourcePath);
    if (!(await exists(absolutePath))) {
      missing.push({ type: "bsr", sourcePath });
      continue;
    }
    const buffer = await readFile(absolutePath);
    if (buffer.subarray(0, 12).toString("latin1") === CPD_SIGNATURE) {
      const compound = parseCompound(buffer, sourcePath);
      const branches = [];
      for (const child of compound.branches) {
        const branch = await loadBranch(child, definition);
        if (branch) branches.push(branch);
      }
      // Retail clears the compound if any branch cannot be loaded (0xa9b3dc).
      if (branches.length !== compound.branches.length) {
        missing.push({ type: "incomplete-compound", sourcePath });
        continue;
      }
      bsrResources.push({ ...compound, objectId: definition.objectId,
        objectFlags: definition.flags, sourcePath,
        placementCount: placementCounts.get(definition.objectId) ?? 0,
        sourceGamePath: toGameRelative(absolutePath, sourceGameRoot), branches,
        materialPaths: [...new Set(branches.flatMap(branch => branch.materialPaths))],
        meshPaths: [...new Set(branches.flatMap(branch => branch.meshPaths))] });
    } else {
      const resource = await loadBranch(sourcePath, definition);
      if (resource) bsrResources.push(resource);
    }
  }

  const materialSets = [];
  const texturePathSet = new Set();
  /** textureSourcePath -> "dxt1" | "dxt3" | "dxt5" | null (null: stays PNG). */
  const blockFormats = new Map();
  const blockFormatFor = async (textureSourcePath) => {
    if (!blockFormats.has(textureSourcePath)) {
      blockFormats.set(
        textureSourcePath,
        await probeBlockTexture(dataAssetPath(sourceExtractedRoot, textureSourcePath))
      );
    }
    return blockFormats.get(textureSourcePath);
  };
  for (const materialPath of [...materialPathSet].sort()) {
    const absolutePath = dataAssetPath(sourceExtractedRoot, materialPath);
    if (!(await exists(absolutePath))) {
      missing.push({ type: "bmt", sourcePath: materialPath });
      continue;
    }

    const materialSet = parseJmxBmtMaterialSet(await readFile(absolutePath), materialPath);
    const materials = [];
    for (const material of materialSet.materials) {
      const textureSourcePath = resolveBmtTexturePath(materialPath, material.textureName);
      const blockFormat = textureSourcePath ? await blockFormatFor(textureSourcePath) : null;
      if (textureSourcePath) {
        texturePathSet.add(textureSourcePath);
      }

      materials.push({
        ...material,
        textureSourcePath,
        texturePublicPath: textureSourcePath ?
          objectTexturePublicPath(area, textureSourcePath, blockFormat ? ".texture" : ".png") :
          null
      });
    }

    materialSets.push({
      sourcePath: materialPath,
      sourceGamePath: toGameRelative(absolutePath, sourceGameRoot),
      signature: materialSet.signature,
      byteLength: materialSet.byteLength,
      materialCount: materialSet.materialCount,
      materials
    });
  }

  const meshes = [];
  for (const meshPath of [...meshPathSet].sort()) {
    const absolutePath = dataAssetPath(sourceExtractedRoot, meshPath);
    if (!(await exists(absolutePath))) {
      missing.push({ type: "bms", sourcePath: meshPath });
      continue;
    }

    const mesh = parseJmxBmsStaticMesh(await readFile(absolutePath), meshPath);
    meshes.push({
      sourcePath: meshPath,
      sourceGamePath: toGameRelative(absolutePath, sourceGameRoot),
      signature: mesh.signature,
      byteLength: mesh.byteLength,
      headerOffsets: mesh.headerOffsets,
      metadata: mesh.metadata,
      vertexLayout: mesh.vertexLayout,
      vertexCount: mesh.vertexCount,
      triangleCount: mesh.triangleCount,
      positions: mesh.positions,
      normals: mesh.normals,
      uvs: mesh.uvs,
      indices: mesh.indices,
      bounds: mesh.bounds,
      ...(mesh.nativePayloads?.length ? { nativePayloads: mesh.nativePayloads } : {})
    });
  }

  const textures = await copyObjectMaterialTextures(
    [...texturePathSet].sort(),
    area,
    missing,
    blockFormats,
    sourceExtractedRoot
  );

  return {
    format: "sro-title-sector-object-resources",
    version: 1,
    reconstructionSources: [
      "sub_4413b0_MapLoader_CreateObjectInstanceFromPlacement",
      "sub_443840_MapLoader_ResolveObjectResource",
      "JMXVRES0109_BSR_layout",
      "JMXVBMS0110_BMS_static_mesh_layout",
      "JMXVBMT0102_BMT_material_layout"
    ],
    bsrCount: bsrResources.length,
    materialSetCount: materialSets.length,
    meshCount: meshes.length,
    textureCount: textures.length,
    missingCount: missing.length,
    missing,
    bsr: bsrResources,
    materialSets,
    meshes,
    textures
  };
}

/*
================
copyObjectMaterialTextures

Publish each material texture: block sources (probeBlockTexture admitted)
as one batched NTX1 encode of the authored DDJ, the rest as converted PNG
copies. The PNG of a block source is NOT published - the .texture is the
only shipped artifact for it.
================
*/
async function copyObjectMaterialTextures(texturePaths, area, missing, blockFormats, sourceExtractedRoot) {
  const copied = [];
  const encodeJobs = [];

  for (const textureSourcePath of texturePaths) {
    const blockFormat = blockFormats.get(textureSourcePath) ?? null;
    if (blockFormat) {
      const publicPath = objectTexturePublicPath(area, textureSourcePath, ".texture");
      const source = dataAssetPath(sourceExtractedRoot, textureSourcePath);
      encodeJobs.push({
        source,
        target: path.join(publicRoot, publicPath.replace(/^\/+/, ""))
      });
      copied.push({
        sourcePath: textureSourcePath,
        imageSourcePath: toGameRelative(source),
        imagePublicPath: publicPath,
        blockFormat
      });
      continue;
    }
    const imageRelativePath = objectTextureImageRelativePath(textureSourcePath, ".png");
    const pngSource = path.join(imageSourceRoot, "Data_extracted", ...imageRelativePath.split("/"));
    const publicPath = objectTexturePublicPath(area, textureSourcePath, ".png");
    const target = path.join(publicRoot, publicPath.replace(/^\/+/, ""));

    if (!(await exists(pngSource))) {
      missing.push({ type: "ddj-image", sourcePath: textureSourcePath, imageSourcePath: pngSource });
      continue;
    }

    await mkdir(path.dirname(target), { recursive: true });
    await copyFile(pngSource, target);

    copied.push({
      sourcePath: textureSourcePath,
      imageSourcePath: toGameRelative(pngSource),
      imagePublicPath: publicPath
    });
  }

  if (encodeJobs.length) {
    await runBlockTextureEncode(encodeJobs);
  }

  return copied;
}

/*
================
runBlockTextureEncode

One python invocation encodes every admitted DDJ into its NTX1 container
(native_texture_mips.py: authored levels verbatim, box-filtered mip suffix).
================
*/
async function runBlockTextureEncode(jobs) {
  const manifestPath = path.join(publicRoot, "..", "object-texture-encode-jobs.json");
  await mkdir(path.dirname(manifestPath), { recursive: true });
  await writeFile(manifestPath, JSON.stringify(jobs), "utf8");
  try {
    await runPython([ENCODER_PATH, "-Manifest", manifestPath], {
      task: "Encode world object textures to NTX1 block containers",
      context: ["Sources are authored DXT power-of-two DDJ files; targets sit in the published asset tree."]
    });
  } finally {
    await rm(manifestPath, { force: true });
  }
}

/*
================
probeBlockTexture

Read the DDJ wrapper's DDS header and report the block format when the
source is a power-of-two DXT texture the client's NTX1 route admits
(native-texture.ts rejects non-PoT dimensions). Everything else returns
null and stays on the converted PNG path.
================
*/
async function probeBlockTexture(absolutePath) {
  if (!(await exists(absolutePath))) return null;
  const handle = await open(absolutePath, "r");
  try {
    const header = Buffer.alloc(DDS_HEADER_BYTES);
    const { bytesRead } = await handle.read(header, 0, DDS_HEADER_BYTES, 0);
    if (bytesRead < DDS_HEADER_BYTES) return null;
    if (header.toString("latin1", 0, 8) !== "JMXVDDJ ") return null;
    if (header.toString("latin1", DDJ_HEADER_SIZE, DDJ_HEADER_SIZE + 4) !== "DDS ") return null;
    const height = header.readUInt32LE(DDS_HEIGHT_OFFSET);
    const width = header.readUInt32LE(DDS_WIDTH_OFFSET);
    if (width < 1 || height < 1 || width & (width - 1) || height & (height - 1)) return null;
    // dwMipMapCount 0 means one authored level; the encoder clamps the same way.
    const flags = header.readUInt32LE(DDS_PIXEL_FORMAT_OFFSET);
    if ((flags & DDPF_FOURCC) === 0) return null;
    return BLOCK_FOURCCS.get(header.readUInt32LE(DDS_PIXEL_FORMAT_OFFSET + 4)) ?? null;
  } finally {
    await handle.close();
  }
}

function objectTexturePublicPath(area, textureSourcePath, extension) {
  return `/assets/world/${area}/object-textures/${objectTextureImageRelativePath(textureSourcePath, extension)}`;
}

function objectTextureImageRelativePath(textureSourcePath, extension) {
  return normalizeAssetPath(textureSourcePath).replace(/\.[^.]+$/, "") + extension;
}

function dataAssetPath(sourceExtractedRoot, sourcePath) {
  return path.join(sourceExtractedRoot, "Data_extracted", ...normalizeAssetPath(sourcePath).split("/"));
}

function countPlacementsByObjectId(placements) {
  const counts = new Map();
  for (const placement of placements) {
    counts.set(placement.objectId, (counts.get(placement.objectId) ?? 0) + 1);
  }
  return counts;
}
