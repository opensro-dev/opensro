// Parser for the Silkroad region navmesh (JMXVNVM 1000, extracted/.../navmesh/nv_XXXX.nvm).
//
// Layout (verified against the client serializer navmeshbuilder.cpp NavMeshBuilder_WriteNVM at 0x45d499 and
// the SilkroadDoc JMXVNVM spec):
//
//   char[12] "JMXVNVM 1000"
//   u16 objectCount
//   object[objectCount]: { u32 assetId; f32 x,y,z; i16 type; f32 yaw; u16 localUid;
//                          u16 short0; u8 isBig; u8 isStruct; u16 regionId;
//                          u16 linkEdgeCount; linkEdge[linkEdgeCount] (6 bytes each) }
//     linkEdge row = { u16 neighborObjectIndex (into THIS region's object list,
//     0xffff = one-sided); u16 neighborOutlineEdgeIndex; u16 myOutlineEdgeIndex }.
//     Native: read into NavMeshInstance+0xa0 by CRTNavMeshTerrain_LoadFromNvm
//     (CRTNavMeshTerrain_Load); consumed by the CRTNavMeshTerrain_StepPlacedObject @0x0040429c edge-object
//     continuation (multi-object bridge chains). Reciprocal across the corpus.
//   u32 totalCellCount; u32 openCellCount;
//   cell[totalCellCount]: { f32 minX,minZ,maxX,maxZ; u8 objIdxCount; u16[objIdxCount] }
//   u32 globalEdgeCount;   globalEdge[..]  (27 bytes each)
//   u32 internalEdgeCount; internalEdge[..] (23 bytes each)
//   tile[96*96]: { u32 cellId; u16 flag; u16 textureId }   // flag bit0 = blocked
//   f32 heightMap[97*97]; u8 planeType[6*6]; f32 planeHeight[6*6]
//
// A region is 1920x1920 units; tiles are 20x20 units (96 tiles/axis).

import { BinaryReader } from "../../shared/jmxBinaryReader.mjs";

const SIGNATURE = "JMXVNVM 1000";
export const NAVMESH_TILES_PER_AXIS = 96;
export const NAVMESH_TILE_SIZE = 20;
export const NAVMESH_REGION_SIZE = NAVMESH_TILES_PER_AXIS * NAVMESH_TILE_SIZE; // 1920
export const NAVMESH_HEIGHT_AXIS_VERTICES = NAVMESH_TILES_PER_AXIS + 1;

export function parseNavmesh(buffer) {
  const sig = buffer.toString("latin1", 0, 12);
  if (sig !== SIGNATURE) {
    throw new Error(`navmesh: bad signature "${sig}" (expected "${SIGNATURE}")`);
  }

  const reader = new BinaryReader(buffer, "navmesh", 12);
  const u8 = () => reader.u8();
  const u16 = () => reader.u16();
  const i16 = () => reader.i16();
  const u32 = () => reader.u32();
  const f32 = () => reader.f32();

  const readEdgeBlock = (count, hasAssocRegions) => {
    const lines = new Float32Array(count * 4);
    const flags = new Uint8Array(count);
    const assocDirections = new Uint8Array(count * 2);
    const assocCells = new Uint16Array(count * 2);
    const assocRegions = hasAssocRegions ? new Uint16Array(count * 2) : undefined;

    for (let i = 0; i < count; i += 1) {
      const lineBase = i * 4;
      lines[lineBase] = f32();
      lines[lineBase + 1] = f32();
      lines[lineBase + 2] = f32();
      lines[lineBase + 3] = f32();
      flags[i] = u8();

      const assocBase = i * 2;
      assocDirections[assocBase] = u8();
      assocDirections[assocBase + 1] = u8();
      assocCells[assocBase] = u16();
      assocCells[assocBase + 1] = u16();
      if (assocRegions) {
        assocRegions[assocBase] = u16();
        assocRegions[assocBase + 1] = u16();
      }
    }

    return assocRegions
      ? { count, lines, flags, assocDirections, assocCells, assocRegions }
      : { count, lines, flags, assocDirections, assocCells };
  };

  const objectCount = u16();
  const objects = [];
  for (let i = 0; i < objectCount; i += 1) {
    const assetId = u32();
    const x = f32();
    const y = f32();
    const z = f32();
    const type = i16();
    const yaw = f32();
    const localUid = u16();
    const short0 = u16();
    const isBig = u8() !== 0;
    const isStruct = u8() !== 0;
    const regionId = u16();
    const linkEdgeCount = u16();
    const linkEdges = reader.bytes(linkEdgeCount * 6, "object link edges");
    const object = { assetId, x, y, z, type, yaw, localUid, short0, isBig, isStruct, regionId, linkEdgeCount };
    if (linkEdgeCount > 0) {
      object.linkEdges = Buffer.from(linkEdges).toString("base64");
    }
    objects.push(object);
  }

  const totalCellCount = u32();
  const openCellCount = u32();
  const cells = [];
  const openCells = [];
  for (let i = 0; i < totalCellCount; i += 1) {
    const minX = f32();
    const minZ = f32();
    const maxX = f32();
    const maxZ = f32();
    const objIdxCount = u8();
    const objectIndices = [];
    for (let j = 0; j < objIdxCount; j += 1) {
      objectIndices.push(u16());
    }
    const cell = { minX, minZ, maxX, maxZ, objectIndices };
    cells.push(cell);
    if (i < openCellCount) {
      openCells.push(cell);
    }
  }

  // Edges are written packed (no padding); strides verified to consume the file exactly:
  //   global   = NavLine(16) + Flag(1) + AssocDirection[2](2) + AssocCell[2](4) + AssocRegion[2](4) = 27
  //   internal = NavLine(16) + Flag(1) + AssocDirection[2](2) + AssocCell[2](4)                     = 23
  const globalEdgeCount = u32();
  const globalEdges = readEdgeBlock(globalEdgeCount, true);
  const internalEdgeCount = u32();
  const internalEdges = readEdgeBlock(internalEdgeCount, false);

  // TileMap 96*96: { u32 cellId; u16 flag; u16 textureId }
  const tileCount = NAVMESH_TILES_PER_AXIS * NAVMESH_TILES_PER_AXIS;
  const blockedTiles = new Uint8Array(tileCount);
  const tileCellIds = new Uint32Array(tileCount);
  const tileFlags = new Uint16Array(tileCount);
  const tileTextureIds = new Uint16Array(tileCount);
  for (let i = 0; i < tileCount; i += 1) {
    const cellId = u32();
    const flag = u16();
    const textureId = u16();
    tileCellIds[i] = cellId >>> 0;
    tileFlags[i] = flag;
    tileTextureIds[i] = textureId;
    blockedTiles[i] = (flag & 1) === 1 || cellId >= openCellCount ? 1 : 0;
  }

  const heightMap = new Float32Array(NAVMESH_HEIGHT_AXIS_VERTICES * NAVMESH_HEIGHT_AXIS_VERTICES);
  for (let i = 0; i < heightMap.length; i += 1) {
    heightMap[i] = f32();
  }

  const planeType = new Uint8Array(6 * 6);
  for (let i = 0; i < planeType.length; i += 1) {
    planeType[i] = u8();
  }

  const planeHeight = new Float32Array(6 * 6);
  for (let i = 0; i < planeHeight.length; i += 1) {
    planeHeight[i] = f32();
  }

  return {
    objectCount,
    objects,
    totalCellCount,
    openCellCount,
    cells,
    openCells,
    globalEdgeCount,
    globalEdges,
    internalEdgeCount,
    internalEdges,
    tileCellIds,
    tileFlags,
    tileTextureIds,
    blockedTiles,
    heightMap,
    planeType,
    planeHeight,
    bytesConsumed: reader.offset
  };
}
