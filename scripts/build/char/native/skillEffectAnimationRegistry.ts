// Owned by the asset pipeline: v1.150 data-format tables recovered from the
// native client. Keep behaviour identical to the published assets.
import { SCRIPT_OBJ_ANIMATION_NAME_ID_ROWS } from "./scriptObjAnimationRegistry.ts";

/**
 * Native v1.150 data_ccd620: the 121 {wstring pointer, int32 id} rows loaded
 * into GlobalEffectManager's animation-name map at 0xf090c8 by StaticInit_RegisterEffectAnimationNames.
 *
 * Rizin/PE census (2026-08-05): rows run from 0x00ccd620 through
 * 0x00ccd9e0, followed by the {nullptr,-1} sentinel at 0x00ccd9e8. The
 * accepted names are `none`, ANI_ATTACK1..9, ANI_READY01..04,
 * ANI_WAIT01..04, ANI_SKILL_1..100, ANI_HAMMER, ANI_HANDLOOF and ANI_TROW.
 * SkillAnimationSet_ParseRow calls WStringIdMap_Find over this table and only
 * increments a phase count on a hit. It does not use the larger 171-row
 * CICScriptObjManager registry.
 */
function belongsToSkillEffectAnimationTable(name: string): boolean {
  if (/^ANI_ATTACK[1-9]$/.test(name)) return true;
  if (/^ANI_READY0[1-4]$/.test(name)) return true;
  if (/^ANI_WAIT0[1-4]$/.test(name)) return true;
  const skill = /^ANI_SKILL_(\d+)$/.exec(name);
  if (skill) {
    const ordinal = Number(skill[1]);
    return ordinal >= 1 && ordinal <= 100;
  }
  return name === "ANI_HAMMER" || name === "ANI_HANDLOOF" || name === "ANI_TROW";
}

export const SKILL_EFFECT_ANIMATION_NAME_ID_ROWS = Object.freeze(
  SCRIPT_OBJ_ANIMATION_NAME_ID_ROWS.filter(([name]) =>
    belongsToSkillEffectAnimationTable(name)
  )
) as readonly (readonly [name: string, id: number])[];

/** `none` is the 121st native row and maps to the absent sentinel -1. */
export const SKILL_EFFECT_ANIMATION_NAME_ID_ROW_COUNT =
  SKILL_EFFECT_ANIMATION_NAME_ID_ROWS.length + 1;

if (SKILL_EFFECT_ANIMATION_NAME_ID_ROW_COUNT !== 121) {
  throw new Error(
    `data_ccd620 skill-effect animation registry drifted: expected 121 rows, got ${SKILL_EFFECT_ANIMATION_NAME_ID_ROW_COUNT}`
  );
}

export const SKILL_EFFECT_ANIMATION_ID_BY_NAME = new Map<string, number>(
  SKILL_EFFECT_ANIMATION_NAME_ID_ROWS
);
