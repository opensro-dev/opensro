// Native CMapLoader_LoadMapAndO2ForNavMeshBuilder at 0x440b64 MapLoader runtime-metadata corner read order (must be
// preserved by callers). This fold does not choose the SWorld near-terrain
// diffuse association; SWorldCell_BuildTerrainMeshLOD owns that draw path.
//   [0] = vertex(tileZ,     tileX)     @ [edi - 0x77]  (0x00440b64)
//   [1] = vertex(tileZ + 1, tileX)     @ [edi]         (0x00440ba5)
//   [2] = vertex(tileZ + 1, tileX + 1) @ [edi + 0x07]  (0x00440bee)
//   [3] = vertex(tileZ,     tileX + 1) @ [edi - 0x70]  (0x00440c38)
export function selectTerrainTileTextureId(cornerTextureIds) {
  for (let leftIndex = 0; leftIndex < cornerTextureIds.length - 1; leftIndex += 1) {
    for (let rightIndex = leftIndex + 1; rightIndex < cornerTextureIds.length; rightIndex += 1) {
      if (cornerTextureIds[leftIndex] === cornerTextureIds[rightIndex]) {
        return cornerTextureIds[leftIndex];
      }
    }
  }

  // Native all-distinct fallback, verified at instruction level: 0x00440c91 presets
  // ecx to corner [0] before any pair test, and the no-pair exit (jne 0x00440cbd ->
  // 0x00440cc7) never writes ecx again, so the FIRST corner wins when all four differ.
  return cornerTextureIds[0];
}
