// Publish the siegefortress.txt table as DECODED emblem rows: the native
// CGlobalDataManager parses this file into the CSiegeFortressData record map
// (GlobalDataManager map488 off 0xcec870, stride 0xb8), and the fortress
// emblem builder CSkillEffectManager_InitializeDamageGlyphsAndEffects walks those records, DDJ_SpriteCreate's each
// record's CrestPath128 (record+0x9c, rooted under the native icon\ base -
// the DDJ ships at icon/etc/fort_jangan.ddj) and inserts the sprite into the
// fortress-id -> emblem map at 0xf0935c keyed by record+0x00.
//
// Unlike the actionwnddata/skillMasteryData RAW-rows precedents, this table
// is decoded HERE: the consumer needs fortress id -> PUBLIC emblem image and
// the DDJ -> public-path mapping must live in exactly one place -
// cifResources.mjs imagePublicPath, which a browser-side fold cannot reach.
//
// Column layout of the shipped siegefortress.txt (UTF-16 LE, one tab-
// separated record per line; observed against this Media's single row
// "1 1 FORTRESS_JANGAN <kr> SN_FORTRESS_JANGAN GATE_CH 0 300 30 25 63
// 5000000 etc\fort_jangan.ddj NPC_CH_FORTRESS_OFFICIAL"):
//   0  Service        1 = live row (disabled rows are skipped, the
//                     regioncode/messagetip builder convention)
//   1  ID             fortress id - record+0x00, the CSkillEffectManager_InitializeDamageGlyphsAndEffects map key
//   2  CodeName128    "FORTRESS_JANGAN"
//   3  ObjName128     Korean display name
//   4  NameStrID128   "SN_FORTRESS_JANGAN" (resolves via textdataname.en.json)
//   5  GateCodeName   "GATE_CH"
//   6..11             six numerics (0 300 30 25 63 5000000) - siege economy/
//                     guard parameters the emblem draw does not need
//   12 CrestPath128   "etc\fort_jangan.ddj" - record+0x9c, icon\-rooted
//   13 NPCCodeName    "NPC_CH_FORTRESS_OFFICIAL"
//
// Output: .generated/client-public/assets/data/siegeFortressData.json
// Rows carry { fortressId, codeName, nameStrId, crestPath, image } where
// image is the resolved public PNG path; rowsById indexes them by fortress
// id for the emblem draw's map-keyed lookup.

import fs from "node:fs";
import path from "node:path";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { imagePublicPath } from "../shared/cifResources.mjs";
import { readTextDataRowsSync } from "../shared/textDataIo.mjs";

import { extractedRoot, gameRoot, publicRoot } from "../world/paths.mjs";

const siegeFortressDataPath = path.join(
  extractedRoot,
  "Media_extracted",
  "server_dep",
  "silkroad",
  "textdata",
  "siegefortress.txt"
);

export function buildSiegeFortressDataAsset() {
  if (!fs.existsSync(siegeFortressDataPath)) {
    console.warn(`[siegefortress] source missing (${siegeFortressDataPath}) - skipping`);
    return { written: false, rows: 0 };
  }

  const rows = [];
  const rowsById = {};
  for (const rawColumns of readTextDataRowsSync(siegeFortressDataPath)) {
    const columns = rawColumns.map((column) => column.trim());
    if (columns.length < 13 || columns[0] !== "1" || !/^\d+$/.test(columns[1])) {
      continue;
    }
    const crestPath = columns[12];
    const row = {
      fortressId: Number(columns[1]),
      codeName: columns[2],
      nameStrId: columns[4],
      crestPath,
      image: imagePublicPath(`icon/${crestPath}`)
    };
    rows.push(row);
    rowsById[String(row.fortressId)] = row;
  }

  const catalog = {
    sourcePath: path.relative(gameRoot, siegeFortressDataPath).replaceAll("\\", "/"),
    format: "sro-siegefortressdata",
    version: 1,
    rows,
    rowsById
  };

  const { outPath } = exportDataAsset({
    publicRoot,
    outputFileName: "siegeFortressData.json",
    value: catalog
  });

  console.log(
    `[siegefortress] wrote ${rows.length} fortress emblem row(s) -> ${path.relative(publicRoot, outPath)}`
  );
  return { written: true, rows: rows.length, outPath };
}

// Run directly (node scripts/build/data/buildSiegeFortressDataAsset.mjs) or
// via the resource build aggregator.
if (isMainScript(import.meta.url)) {
  buildSiegeFortressDataAsset();
}
