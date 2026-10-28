// Publish the skillmasterydata.txt + skillgroup.txt tables as RAW record
// rows: the native CGlobalDataManager loads both at client boot into the
// mastery table at data_cec870+0x1c0 and the per-mastery group vectors at
// +0x184, the sources CIFSkill's mastery-tab builder (CIFSkill_RebuildMasteryPages), the
// board header refresh (CIFSkillBoard_RefreshMasteryHeaderAndTrainButton: name "%s %s", level "Lv %d", icon) and
// the group-row fill (CIFSkillMasteryRow_CreateSkillCells: skillgroup ICON column + "_focus.ddj",
// group name via textdataname) read through SkillMasteryRecord_GetPayload / CGlobalData_FindMasterySkillRow /
// CGlobalData_GetMasterySkillRowPayload.
//
// NO field decoding happens here (the actionwnddata raw-rows precedent):
// the browser bridge (bridge/ui/panes/skillPanePlane.ts) decodes the columns it
// consumes, so the column semantics live in exactly one place. This builder
// only frames records the way the native line reader does: drop // comment
// lines and blank lines, keep everything else byte-exact.
//
// Output: .generated/client-public/assets/data/skillMasteryData.json
// Fetched lazily by the skill-pane plane when the Skill tab first renders.

import fs from "node:fs";
import path from "node:path";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { readTextDataLinesSync } from "../shared/textDataIo.mjs";

import { extractedRoot, gameRoot, publicRoot } from "../world/paths.mjs";

const textdataRoot = path.join(
  extractedRoot,
  "Media_extracted",
  "server_dep",
  "silkroad",
  "textdata"
);
const masteryDataPath = path.join(textdataRoot, "skillmasterydata.txt");
const skillGroupPath = path.join(textdataRoot, "skillgroup.txt");

export function buildSkillMasteryDataAsset() {
  if (!fs.existsSync(masteryDataPath) || !fs.existsSync(skillGroupPath)) {
    console.warn(
      `[skillmasterydata] source missing (${masteryDataPath} / ${skillGroupPath}) - skipping`
    );
    return { written: false, masteryRows: 0, groupRows: 0 };
  }

  const masteryRows = readTextDataLinesSync(masteryDataPath);
  const groupRows = readTextDataLinesSync(skillGroupPath);

  const catalog = {
    sourcePaths: [
      path.relative(gameRoot, masteryDataPath).replaceAll("\\", "/"),
      path.relative(gameRoot, skillGroupPath).replaceAll("\\", "/")
    ],
    format: "sro-skillmasterydata",
    version: 1,
    masteryRows,
    groupRows
  };

  const { outPath } = exportDataAsset({
    publicRoot,
    outputFileName: "skillMasteryData.json",
    value: catalog
  });

  console.log(
    `[skillmasterydata] wrote ${masteryRows.length} mastery + ${groupRows.length} group raw rows -> ${path.relative(publicRoot, outPath)}`
  );
  return {
    written: true,
    masteryRows: masteryRows.length,
    groupRows: groupRows.length,
    outPath
  };
}

// Run directly (node scripts/build/data/buildSkillMasteryDataAsset.mjs) or
// via the resource build aggregator.
if (isMainScript(import.meta.url)) {
  buildSkillMasteryDataAsset();
}
