/*
===========================================================================

spawn-fade.ts - the alpha ramp a monster enters the world with

CIDecoAppear_Initialize (8D4B60) starts alpha at 0 with a rate of
255 / 2.0 s (BCBEEC), and CIDecoAppear_SetAlpha (8D4C20) adds rate * frame
delta each frame, hands the render object the truncated byte, and at 255
writes 255 and retires. Only the monster spawn-packet path installs one:
CICMonster_DeserializeSpawnPacket (861B00) adds it as its last act, and a
monster's linked ride gets its own (861EE2).

The generic gate never fires for a visibility spawn: CICharactor_
OnSpawnInitialize (85FDB0) adds the deco only when the spawn-fade flag
(+0x74C) is set, and CIGIDObject_CreateByCodename - the factory behind
every CHAR_/MOB_/NPC_/COS_ spawn - clears it on the object it creates
(CICharactor_SetSpawnFadeEnabled(result, 0), call site 0x4CD624), as does
CICNpc_InitializeFromRecord (push 0 at 0x86EE81). Only the script-spawned
local player double enables it (Script_SpawnLocalPlayerDouble, 0x4EBBC3);
the port has no script doubles. Players, COS and NPCs therefore appear at
full alpha on the first frame they are drawn (#3217).

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";

// BCBEEC: the CIDecoAppear duration every spawn site passes.
export const SPAWN_FADE_SECONDS = 2;
// 8D4B60: 255 / SPAWN_FADE_SECONDS alpha units per second.
const ALPHA_RATE = 127.5;

/*
================
spawnFadeKind

Whether a freshly spawned entity of this kind enters with CIDecoAppear:
monsters only, per the native rule in the file banner. A player, COS or
NPC spawn is a factory creation whose fade flag is cleared, so it is
drawn opaque from its first frame.
================
*/
export function spawnFadeKind( kind: EntityState["kind"] ): boolean {
	return kind === "monster";
}

/*
================
spawnFadeAlpha

The opacity elapsed seconds into the ramp: the truncated alpha byte over
255, and exactly 1 once the ramp has reached 255.
================
*/
export function spawnFadeAlpha( elapsed: number ): number {
	if ( !(elapsed > 0) ) return 0;
	const alpha = Math.fround( ALPHA_RATE * elapsed );
	return alpha >= 255 ? 1 : Math.trunc( alpha ) / 255;
}
