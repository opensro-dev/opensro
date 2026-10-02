/*
===========================================================================

pet-mini-info.ts - what the attack pet's mini window shows

CIFPetMiniInfo is the player mini window's child 100 (GDR_PMI_PET_MINI_INFO).
CIFCOSManager_AddCompanion shows it, bound to the companion, when the new
companion is an attack pet (CIFQuickState_SetPetInfoVisible); removing the
companion hides it. CIFPetMiniInfo_SetCos (6B3AD0) writes the portrait, the
name (the pet's own, else UIIT_STT_COSNEWUI_TITLE) and the level, and
CIFPetMiniInfo_OnUpdate (6B34C0) drives the two gauges and the HP caution.

===========================================================================
*/
import type { CosRecord } from "@/engine/contracts/gameplay";
import { COS_CLASS_ATTACK, cosClass, type CosReference } from "./cos-command";

// 6B34C0 divides the satiety word by 10000.0 for the HGP gauge.
const SATIETY_FULL = 10000;
// 6B34C0 blinks the HP caution while 0 < HP ratio <= 0.300000012f, the
// float32 nearest 0.3 (written out so the constant stays a literal).
const HP_CAUTION_RATIO = 0.30000001192092896;

/*
================
PetMiniInfo
================
*/
export interface PetMiniInfo {
	readonly gid: number;
	readonly name: string | undefined;
	readonly level: number;
	readonly icon: string | undefined;
	// Gauge fills in 0..1; null when the authority to compute them is absent.
	readonly hp: number | null;
	readonly hgp: number;
	readonly caution: boolean;
}

/*
================
petMiniInfo

The attack pet the window is bound to, or null when none is summoned.
================
*/
export function petMiniInfo(
	records: readonly CosRecord[] | undefined,
	references: ReadonlyMap<number, CosReference> | undefined
): PetMiniInfo | null {
	const record = records?.find( row => cosClass( row.band ) === COS_CLASS_ATTACK );
	if ( !record ) return null;
	const reference = references?.get( record.refObjId );
	// 6B34C0 stores the HP ratio through a float: compare in float32.
	const hp = reference && reference.maxHp > 0 ? Math.min( 1, Math.fround( record.hp / reference.maxHp ) ) : null;
	return {
		gid: record.gid,
		name: record.name,
		level: record.level ?? 0,
		icon: reference?.icon,
		hp,
		hgp: Math.min( 1, Math.fround( (record.satiety ?? 0) / SATIETY_FULL ) ),
		caution: hp !== null && hp > 0 && hp <= HP_CAUTION_RATIO
	};
}
