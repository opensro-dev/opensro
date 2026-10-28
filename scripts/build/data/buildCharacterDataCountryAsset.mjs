// Publish the chardata COUNTRY catalog: model ref id -> the RefObjChar
// country byte, nothing more. The native CGlobalDataManager boot loader
// (Client_LoadGameDataTables at 0x723058) formats "%stextdata\characterdata.txt" and hands the manifest
// to the table dispatcher; each shard line is parsed by CCharacterData's
// CCharacterData_ParseTextRow -> CRefObjCommon_ParseTextRow (the table loader consumes column 0 "Service"
// before the parse runs, so parse token N is file column N+1):
//
//   col  1 -> payload+0x04 model ref id (the recordMap1f0 key CGlobalDataManager_GetCharacterDataById
//             lower-bounds; std_tree_node_to_value_808250 GetPayloadBody = record+4)
//   col 14 -> payload+0x9c country byte (parsed @0x00808c84, duplicated at
//             +0x9d; CCharacterData::Parse tests it against 3 @0x00808a30)
//
// The byte is the CharKindredIcon_GetPath kindred-art selector (0 -> com_kindred_china*,
// 1 -> com_kindred_europe*; ANY other value builds NO path - the fold's
// @0x0081d614/@0x0081d619 two-arm dispatch). The party pane's race-mark
// chain reads it through GlobalDataManager_GetCharCosDataById (@0x005b8533); the party-match race
// text (@0x0053054a) reads the same byte. Only country 0/1 rows ship -
// the 10k+ country-3 rows (mobs/NPCs) are outside the kindred fold's art
// domain, and a catalog miss decodes to the same "no art" result.
//
// Output: .generated/client-public/assets/data/characterDataCountry.json
// Fetched lazily by the party-pane plane (bridge/ui/panes/partyPanePlane.ts),
// which decodes the rows in exactly one place.

import fs from "node:fs";
import path from "node:path";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { readTextDataLinesSync, readTextDataRowsSync } from "../shared/textDataIo.mjs";

import { extractedRoot, gameRoot, publicRoot } from "../world/paths.mjs";

const textdataRoot = path.join(
  extractedRoot,
  "Media_extracted",
  "server_dep",
  "silkroad",
  "textdata"
);
const manifestPath = path.join(textdataRoot, "characterdata.txt");

// File column indices (see the contract above).
const COLUMN_ID = 1;
const COLUMN_COUNTRY = 14;
// The native CRefObjCommon_ParseTextRow/CCharacterData_ParseTextRow parse consumes 100+ tokens; short rows
// would underrun it. The country column is all this asset needs intact.
const MIN_COLUMN_COUNT = 15;

export function buildCharacterDataCountryAsset() {
  if (!fs.existsSync(manifestPath)) {
    console.warn(`[chardata-country] manifest missing (${manifestPath}) - skipping`);
    return { written: false, rows: 0 };
  }

  // The manifest lists the shard files exactly like the native loader
  // consumes them (CharacterData_5000.txt ... CharacterData_25000.txt).
  const shardPaths = readTextDataLinesSync(manifestPath)
    .map((name) => path.join(textdataRoot, name.trim().toLowerCase()))
    .filter((shardPath) => fs.existsSync(shardPath));

  const rows = [];
  for (const shardPath of shardPaths) {
    for (const cols of readTextDataRowsSync(shardPath)) {
      // The table loader keeps Service==1 rows (column 0).
      if (cols.length < MIN_COLUMN_COUNT || cols[0].trim() !== "1") {
        continue;
      }
      const country = cols[COLUMN_COUNTRY].trim();
      // Only the CharKindredIcon_GetPath art domain ships (0 = china, 1 = europe); the
      // country-3 rows decode to the same "no kindred art" a miss does.
      if (country !== "0" && country !== "1") {
        continue;
      }
      rows.push(`${cols[COLUMN_ID].trim()}\t${country}`);
    }
  }

  const catalog = {
    sourcePaths: [manifestPath, ...shardPaths].map((sourcePath) =>
      path.relative(gameRoot, sourcePath).replaceAll("\\", "/")
    ),
    format: "sro-chardata-country",
    version: 1,
    // Source column index per shipped tab-separated field (id, country byte;
    // decode semantics live in partyPanePlane.ts).
    columns: [COLUMN_ID, COLUMN_COUNTRY],
    rows
  };

  const { outPath } = exportDataAsset({
    publicRoot,
    outputFileName: "characterDataCountry.json",
    value: catalog
  });

  console.log(
    `[chardata-country] wrote ${rows.length} id->country rows (${shardPaths.length} shards) -> ${path.relative(publicRoot, outPath)}`
  );
  return {
    written: true,
    rows: rows.length,
    outPath
  };
}

// Run directly (node scripts/build/data/buildCharacterDataCountryAsset.mjs)
// or via the resource build aggregator.
if (isMainScript(import.meta.url)) {
  buildCharacterDataCountryAsset();
}
