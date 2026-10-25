/*
===========================================================================

graphic-options.ts - the SWorld graphic options behind /wire../lod

SWorld owns one int per option at +0x2E50 + 4*mode: the getter
(SWorld_GetGraphicOption 883020) reads it, the setter
(SWorld_ApplyGraphicOption 8A54C0) stores it (behind the +0x2F40 gate)
and posts it on as app message 0x809. SWorld_Construct seeds wireframe
0 and the five others 1, so each console command's first press turns
wireframe on and everything else off. CGInterface_OnCommandMessage
(690C40) negates one option per command (ids 150-155, handlers
691686-6917A3) and prints nothing. The readers are render passes: the
fill mode in SWorld_Render (8B9C28), the terrain and ground-decal pass
(8ABFDE), the sky pass in CWorld_RenderScenePasses (8B32F9, 8B3489),
the model pass (8AA75D), the character shadow and dungeon passes
(8B130B, 8B1B4E) and the per-cell LOD choice in
SWorld_UpdateCameraFrustumAndVisibleCells (8B7F1D). Picking never reads
them.

===========================================================================
*/

export const GRAPHIC_OPTION_WIRE_FRAME = 0x12;
export const GRAPHIC_OPTION_GROUND = 0x13;
export const GRAPHIC_OPTION_SKY = 0x14;
export const GRAPHIC_OPTION_MAP_OBJECTS = 0x15;
export const GRAPHIC_OPTION_CHARACTERS = 0x16;
export const GRAPHIC_OPTION_LEVEL_OF_DETAIL = 0x17;
