// Publish the skill-effect DATA plane as a browser-fetchable asset: the
// f0902c GlobalEffectManager effect-record table (skilldata numeric id ->
// CIDecoSkillRecord), which the WIP skill folds read through
// GlobalEffectManager_FindSkillMotionMetadata GlobalEffectManager_FindEffectRecordBySkillId.
//
// Source join:
//   SkillData_*.txt col[1] numeric id + (col[5] base name OR exact col[3]) x
//   textdata/skilleffect.txt #section skillaniset{,2}
//   (authored name -> anim fields)
//   + data_ccdca8 built-in SYSTEM_*/STATUS_* name/id registrations
//   -> effectRecords.json { signed int32 id: record } for every authored
//      player, monster, pet, and built-in effect. Rows without a skillaniset
//      entry are absent.
//
// Output: .generated/client-public/assets/skill/effectRecords.json
// The WIP loader host (skillEffectLegHost) fetches it and fills
// g_effectRecordMap at mission-bridge init, retiring the registerRecord
// test stub for shipped skills.

import fs from "node:fs";
import path from "node:path";
import { isMainScript } from "../shared/fsUtils.mjs";
import { writeJsonIfChangedSync } from "../shared/jsonOut.mjs";
import { refreshPrecompressedSidecars } from "../generatedManifestSidecars.mjs";

import {
  clientV150ResinfoRoot,
  extractedRoot,
  publicRoot,
  retailTextdataRoot,
} from "../world/paths.mjs";
import { buildEffectRecordTable } from "./parseSkillEffect.mjs";

const textdataDir = retailTextdataRoot;
const skillEffectPath = path.join(textdataDir, "skilleffect.txt");
const clientSkillEffectPath = path.join(clientV150ResinfoRoot, "skilleffect.txt");
const primitiveSoundRoot = path.join(extractedRoot, "Data_extracted", "prim", "snd");

export async function buildSkillEffectRecordsAsset() {
  if (!fs.existsSync(textdataDir) || !fs.existsSync(skillEffectPath)) {
    console.warn(
      `[skill-effect-records] source missing (${textdataDir} / ${skillEffectPath}) - skipping`
    );
    return { written: false, records: 0 };
  }

  const {
    table, namedTable,
    skillDataRows,
    aniSets,
    effectSets,
    matched,
    builtinRegistered,
  } = buildEffectRecordTable(
    textdataDir,
    skillEffectPath,
    primitiveSoundRoot,
    clientSkillEffectPath,
  );
  const records = Object.keys(table).length;

  const outDir = path.join(publicRoot, "assets", "skill");
  fs.mkdirSync(outDir, { recursive: true });
  const outPath = path.join(outDir, "effectRecords.json");
  // The asset is a plain { id -> record } map. Positive keys are SkillData
  // ids; built-ins use the signed int32 representation read by GlobalEffectManager_FindSkillMotionMetadata.
  writeJsonIfChangedSync(outPath, table);
  const namedPath = path.join(outDir, "namedEffectRecords.json");
  writeJsonIfChangedSync(namedPath, namedTable);
  await refreshPrecompressedSidecars(
    [outPath, namedPath],
    { onlyWhenStale: true },
  );

  console.log(
    `[skill-effect-records] wrote ${records} records (${matched} SkillData joins + ${builtinRegistered} built-ins; ${skillDataRows} SkillData rows, ${aniSets} anim sets, ${effectSets} authored stage owners) -> ${path.relative(publicRoot, outPath)}`
  );
  return { written: true, records, outPath };
}

// Run directly (node scripts/build/char/buildSkillEffectRecords.mjs) or via
// the char build aggregator.
if (isMainScript(import.meta.url)) {
  await buildSkillEffectRecordsAsset();
}
