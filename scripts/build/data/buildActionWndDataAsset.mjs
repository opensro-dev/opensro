// Publish the actionwnddata.txt table as RAW record rows: the native
// CGlobalDataManager loads this file at client boot (loader tag 9 @0x723346
// in Client_LoadGameDataTables; producer CGlobalDataManager_LoadTextDataFile case 9) and parses each record line
// with CActionWndData_ParseTextRow / CActionWndData_ParseRecordBody into the action-record
// map at data_cec870+0x258 - the table CIFAction_OnCreate
// iterates to populate the Action pane's command-id -> slot map.
//
// NO field decoding happens here (the worldmap_localinfo raw-rows
// precedent): the browser bridge (bridge/data/actionRecordDataHost.ts) runs
// the REAL folded CActionWndData_ParseTextRow parser per row, so the column semantics live
// in exactly one place - the fold. This builder only frames records the way
// the native line reader does: drop // comment lines (the shipped file
// comments out row 1005) and blank lines, keep everything else byte-exact.
//
// Output: .generated/client-public/assets/data/actionwnddata.json
// Carried in the boot resource bundle (useSroResources) and seeded by
// CPSMission at scene mount, before the mission packet owner installs.

import fs from "node:fs";
import path from "node:path";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { readTextDataLinesSync } from "../shared/textDataIo.mjs";

import { extractedRoot, gameRoot, publicRoot } from "../world/paths.mjs";

const actionWndDataPath = path.join(
  extractedRoot,
  "Media_extracted",
  "server_dep",
  "silkroad",
  "textdata",
  "actionwnddata.txt"
);

export function buildActionWndDataAsset() {
  if (!fs.existsSync(actionWndDataPath)) {
    console.warn(`[actionwnddata] source missing (${actionWndDataPath}) - skipping`);
    return { written: false, records: 0 };
  }

  const rows = readTextDataLinesSync(actionWndDataPath);

  const catalog = {
    sourcePath: path.relative(gameRoot, actionWndDataPath).replaceAll("\\", "/"),
    format: "sro-actionwnddata",
    version: 1,
    rows
  };

  const { outPath } = exportDataAsset({ publicRoot, outputFileName: "actionwnddata.json", value: catalog });

  console.log(
    `[actionwnddata] wrote ${rows.length} raw record rows -> ${path.relative(publicRoot, outPath)}`
  );
  return { written: true, records: rows.length, outPath };
}

// Run directly (node scripts/build/data/buildActionWndDataAsset.mjs) or via
// the resource build aggregator.
if (isMainScript(import.meta.url)) {
  buildActionWndDataAsset();
}
