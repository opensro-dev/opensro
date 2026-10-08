/*
===========================================================================

copyTerrainLightmaps.mjs - the MAPT lightmap publisher

Each MAPT terrain sector embeds one DDS lightmap. Block sources (the
measured corpus is uniformly DXT1 512x512) ship as NTX1 .texture containers
holding the authored levels only, uploaded by the client as GPU bc1 instead
of a CPU decode to expanded RGBA8. A non-block surface keeps the raw .dds
and the client's legacy decode route.
===========================================================================
*/
import { publicPathToFile } from "../../shared/assetPaths.mjs";
import { writePublicFile } from "../io.mjs";
import { publicRoot } from "../paths.mjs";
import { probeBlockDdsPayload, writeAuthoredBlockContainer } from "./blockTextures.mjs";

/*
================
terrainLightmapPublicPath
================
*/
export function terrainLightmapPublicPath( area, sectorX, sectorY, block ) {
	return `/assets/world/${area}/terrain-lightmaps/${sectorY}-${sectorX}.${block ? "texture" : "dds"}`;
}

/*
================
publishTerrainLightmap

Publish the embedded payload and return its public path. Retain an earlier
representation: neighboring region bundles can share this sector while a
scoped rebuild updates only one bundle's references.
================
*/
export async function publishTerrainLightmap( area, sectorX, sectorY, payload ) {
	const block = probeBlockDdsPayload( payload );
	const publicPath = terrainLightmapPublicPath( area, sectorX, sectorY, block );
	const target = publicPathToFile( publicPath, publicRoot );
	if ( block ) {
		// Authored levels only: retail sampled this DDS with its own level
		// count (0x9f8ea0 passes the file's count to D3DX), so the container
		// is a byte remap and no generator runs.
		await writeAuthoredBlockContainer( payload, publicPath, target );
	} else {
		await writePublicFile( publicPath, payload );
	}
	return publicPath;
}
