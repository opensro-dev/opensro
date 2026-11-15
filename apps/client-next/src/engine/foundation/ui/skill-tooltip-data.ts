export interface SkillTooltipRowView {
  id: number;
  groupId: number;
  basicLevel: number;
  basicActivity: number;
  masteryId: number;
  reqMasteryLevel: number;
  reqStr: number;
  reqInt: number;
  reqGroups: Array<{ groupId: number; level: number }>;
  reqLearnSp: number;
  nameSymbol: string;
  chainNextSkillId: number;
  requiredWeaponKinds: [number, number];
  requiredHp: number;
  requiredMp: number;
  requiredHpRatio: number;
  requiredMpRatio: number;
  tooltipDescriptionSymbol: string;
  studySymbol: string;
  directTooltipParams: {
    nativeParamBlocks: Array<{ offset: number; tag: number; values: number[] }>;
    durationMs: number | null;
    multiCount: number | null;
    downAttackRatio: number | null;
    criticalFlat: number;
    criticalRatio: number;
    tauntFlat: number;
    tauntRatio: number;
    knockout: { level: number; chance: number } | null;
    rangeIncreaseDeci: number | null;
    knockback: { chance: number; distance: number } | null;
    defense: { physical: number; magical: number; applyLimit: number } | null;
    recovery: { hpFlat: number; hpRatio: number; mpFlat: number; mpRatio: number } | null;
    setValues: Array<{ code: number; value: number }>;
    area: {
      gate: number;
      kind: number;
      radiusDeci: number;
      targetCount: number;
      pierceDecrease: number;
      reserved: number;
    } | null;
    areas: Array<{
      gate: number;
      kind: number;
      radiusDeci: number;
      targetCount: number;
      pierceDecrease: number;
      reserved: number;
    }>;
  };
  attack: {
    present: boolean;
    flags: number;
    percent: number;
    minimum: number;
    maximum: number;
    value5: number;
  };
}



// Map-like, not a Map: skill-tooltip-catalog.ts builds views on demand
// (SkillPane_CreateCatalog). structuredClone, postMessage and instanceof Map
// do not work on it; read it through get, has, keys and iteration only.
export type TooltipSkillCatalog=ReadonlyMap<number,SkillTooltipRowView>&{readonly groups:ReadonlyMap<string,SkillTooltipRowView>};
