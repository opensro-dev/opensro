/*
===========================================================================

automob.ts - the anti-bot monster's disguise (MOB_AUTOMOB, #4530)

The anti-bot stone (ITEM_ETC_AUTOMOB) calls MOB_AUTOMOB beside its user.
Each viewer's client draws that monster as the last COS (pet, horse or
transport) the viewer targeted, so it looks different to every viewer,
and removes it without the despawn fade:

  - CICMonster_DeserializeSpawnPacket 0x861b12..0x861b4a: when the
    spawn's reference is MOB_AUTOMOB (resolved by codename at login,
    AutoMob_ResolveRecords 0x728ab0), the monster takes the reference id
    in CGInterface+0x4e0 (CGInterface_GetLastTargetedCosRefId 0x67b210)
    as its own (+0x214) and as its data record (+0x648), and sets +0x774.
  - CIFTargetWindow_SetTarget 0x581851 writes that id when the granted
    target is a CICCos (0x58183e) and not a CICMonster (0x581626).
  - CIGIDObject_Despawn 0x852848 fades over 0 s, not 1.5 s, for an exact
    CICMonster with +0x774 set.

Native defect, not copied: before any COS was targeted the id is 0, the
lookup of reference 0 misses and returns the pointer 4
(GlobalDataManager_GetCharCosDataById 0x7efeb0), and the next read of
+0x260 faults. The port keeps MOB_AUTOMOB's own look then.

The class stays the monster's: the spawn body is decoded as a monster
and the TID checks read the original record (+0x218), so only the
reference id and the name the board shows follow the disguise.

===========================================================================
*/

// AUTOMOB_CODENAME is the anti-bot monster the stone calls.
export const AUTOMOB_CODENAME = "MOB_AUTOMOB";

/*
================
AutoMobLook

The reference a viewer last targeted as a COS, and its name.
================
*/
export interface AutoMobLook {
	readonly refObjId: number;
	readonly name?: string;
}

/*
================
autoMobSpawn

The MOB_AUTOMOB spawn as this viewer sees it: drawn as look when one is
known, and always removed without a fade (vanishes).
================
*/
export function autoMobSpawn<T extends { readonly refObjId: number; readonly name: string; }>(
	entity: T,
	look: AutoMobLook | undefined
): T & { readonly vanishes: true; } {
	if ( !look || !look.refObjId ) return { ...entity, vanishes: true };
	return { ...entity, refObjId: look.refObjId, name: look.name || entity.name, vanishes: true };
}
