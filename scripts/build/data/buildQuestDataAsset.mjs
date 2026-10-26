// Publish the questdata table as PROJECTED raw record rows plus the quest
// text entries the rows reference: the native CGlobalDataManager boot loader
// formats "%stextdata\questdata.txt" (fmt @0xc051c0) and hands the manifest
// to the table dispatcher sub_7f22d0(0xcec870, ..., kind=0x19, 0)
// (@0x00723ac2); each line is parsed by CQuestData's sub_810620 into the
// record (GetData() = sub_8105e0 = record+0x04) and inserted into the
// data_cec870+0x3f0 by-id map (@0x007f458f) - gated on the payload+0x20
// level byte <= 0x5a (@0x007f4552; a higher level deletes the record).
//
// The CIFQuestSlotMain live-row draw (sub_5c3f90) consumes exactly two
// payload fields: the +0x24 title codename resolved through the 0xcec800
// localized-text manager (sub_796330 @0x005c40aa - seeded natively from
// "%stextdata\textquest.txt" via sub_797240 @0x00726de2) for the
// "%s (%d/%d)" title, and the +0x20 level byte (sub_5c3840's content-button
// level-delta art pick against g_pLocalCICPlayer+0x820). Values stay RAW
// strings (tab-joined, in source order); the browser bridge
// (bridge/ui/panes/questPlane.ts) decodes them in exactly one place, the
// buildSkillDataAsset.mjs precedent.
//
// VERSION 2 (the CIFQuestSlotSub detail expansion): the expanded sub-row
// line (sub_5c4870 @0x005c4941/0x005c48d2) resolves the WIRE-carried
// SQuestContents+0x08 symbol (the 0x31ED flags&0x10 entries, sub_785c60)
// through the SAME 0xcec800 manager - the server may reference ANY
// textquest key, so the current v5 asset contains the FULL textquest table
// (native loads the whole file at media boot, sub_797240 @0x00726de2).
//
// VERSION 3 (the CIFQuestReward content-view popup, pinned 2026-07-28):
// the quest row's content button (id 13) routes its msgmap handler
// sub_5c26e0 (registered @0x00bb4125) into the CIFQuestReward window
// (interface registry id 0x26, created @0x0069b134 from the 0xce9c90
// factory), and the populate sub_5c1fa0 consumes TWO more questdata
// payload symbols through the 0xcec800 manager: payload+0x40 (source col
// 6, the SN_PAY_* popup TITLE, @0x005c1ff3 onto child 0x11) and
// payload+0x5c (source col 8, the SN_PAYCON_* popup BODY, @0x005c2026
// onto the child 0x12 CIFPML). The current projection includes [6, 8].
//
// The populate also caches payload+0xb1 (@0x005c20a2 -> window+0x378):
// NOT a questdata column - the media-boot loader joins
// textdata\questcontentsdata.txt onto the SAME records by codename
// (sub_7f22d0 case 0x1a @0x007f4599: codename map find @0x007f463f,
// record body parse sub_810f10 @0x007f4660; the +0xb1 byte parse
// @0x008113d9 reads contents column 4). The give-up button (sub_5c2440
// @0x005c247e) picks the confirm body by it: 0 -> "type" 0x64
// (UIIT_MSG_QUEST_GIVEUP_WINDOW_1), nonzero -> 0x65 (..._WINDOW_2).
// Ships as `giveupWarnBytes`, PARALLEL to `rows` (unjoined rows keep the
// sub_810a10 ctor zero).
//
// VERSION 4 (the CIFQuestInfo level-up notify window, pinned 2026-07-28):
// the guide-icon chain (sub_69cca0 @0x0069ccdb -> sub_7e5fa0 walk ->
// sub_69cdf0 window -> sub_62de20 rebuild -> sub_62d8d0 row ->
// sub_62e240 slot populate) consumes TWO more questdata payload symbols
// through the 0xcec800 manager: payload+0x78 (source col 9, the SN_NN_*
// notify-NPC symbol, slot child id 4) and payload+0x94 (source col 10,
// the SN_NC_* notify-condition symbol, slot child id 8). The current
// projection includes [9, 10].
//
// The walk (sub_7e5fa0) also gates on payload+0xb0 - NOT a questdata
// column: the questcontentsdata.txt join (sub_7f22d0 case 0x1a) parses
// contents column 2 into record+0xb4 = payload+0xb0 (@0x00810fdb, the
// int parse right after the col-1 string skip; col 4 -> payload+0xb1
// @0x008113d9 is the v3 giveupWarnBytes sibling). The byte is the
// COUNTRY gate: 3 = both, else it must equal the local player's
// refobj +0x9c country byte (@0x007e604e/@0x007e6061) - the check is
// skipped entirely when playerLevel > 0x28 (@0x007e603e). Ships as
// `countryBytes`, PARALLEL to `rows` (unjoined rows keep the ctor zero).
//
// VERSION 5 (the enter-quest emission wave, 2026-07-29): the sub_788210
// progress-sentinel fixup reads payload+0xc0 (record+0xc4) - the
// sub_810f10 @0x811439 CRT_Wtol byte parse of the token PAST the 8 name
// columns (questcontentsdata "column 13"). The shipped v1.150 table
// carries exactly 13 columns (0..12), so the trailing parse consumes no
// authored token and EVERY row evaluates to 0 - shipped honestly as
// `progressClearBytes` (parallel to `rows`) so the client's predicate
// (questPlane QuestPlane_AccountClearsProgressSentinel) is a real data
// lookup, not a hardcoded false. Computed from cols[13] when a future
// table authors it.
//
// PINNED COLUMN MISS (the expansion audit, 2026-07-28): questdata.txt
// has NO objective/description column the EXPANSION consumes -
// sub_5c44c0's text arrives on the wire (SQuestContents+0x08).
//
// Pinned column contract (source column index -> native parse offset; the
// table loader consumes column 0 "Service" before sub_810620 runs):
//   col  1 -> +0x04 quest id     (sub_9fbec0 @0x810680; the parse return and
//                                 the +0x3f0 map key)
//   col  3 -> +0x24 level byte   (sub_9fbec0 @0x8106c9: payload+0x20, the
//                                 <= 0x5a keep gate and the sub_5c3840 pick)
//   col  5 -> +0x28 title symbol (sub_811950 @0x8106f7: payload+0x24, the
//                                 SN_ codename sub_796330 resolves; col 4 is
//                                 the loader-discarded Korean debug name)
//   col  6 -> +0x44 pay title    (sub_811950 @0x810725: payload+0x40, the
//                                 SN_PAY_* CIFQuestReward title symbol)
//   col  8 -> +0x60 pay body     (sub_811950 @0x810764: payload+0x5c, the
//                                 SN_PAYCON_* CIFQuestReward body symbol;
//                                 col 7 is loader-discarded)
//   col  9 -> +0x7c notify npc   (sub_811950: payload+0x78, the SN_NN_*
//                                 CIFQuestInfo slot child-4 symbol)
//   col 10 -> +0x98 notify cond  (sub_811950: payload+0x94, the SN_NC_*
//                                 CIFQuestInfo slot child-8 symbol)
//
// Output: .generated/client-public/assets/data/questData.json
// Fetched lazily by the quest plane when the mission scene mounts.

import fs from "node:fs";
import path from "node:path";
import { exportDataAsset } from "../shared/dataAssetExport.mjs";
import { isMainScript } from "../shared/fsUtils.mjs";
import { readTextDataRowsSync, readLocalizedTextDataRowsSync } from "../shared/textDataIo.mjs";
import { questGuideRecords } from '../shared/questGuideRecords.mjs';
import { completeGuideTitles } from '../shared/textResources.mjs';
import { completedEnglish, isPlaceholderText } from '../shared/englishCompletions.mjs';

import { extractedRoot, gameRoot, publicRoot } from "../world/paths.mjs";

const textdataRoot = path.join(
  extractedRoot,
  "Media_extracted",
  "server_dep",
  "silkroad",
  "textdata"
);
const questDataPath = path.join(textdataRoot, "questdata.txt");
const questContentsDataPath = path.join(textdataRoot, "questcontentsdata.txt");
const textQuestPath = path.join(textdataRoot, "textquest.txt");
const textHelpPath = path.join(textdataRoot,'texthelp.txt');
const guideDataPath = path.join(textdataRoot,'gameguidedata.txt');

// The projected source columns, in shipped order (see the contract above).
const PROJECTED_COLUMNS = [1, 3, 5, 6, 8, 9, 10];
// questdata.txt carries 11 columns; sub_810620 reads through column 10.
const MIN_COLUMN_COUNT = 11;
// questdata.txt column 2: the codename the questcontentsdata join keys on
// (the sub_7f22d0 case-0x1a codename-map find @0x007f463f).
const QUEST_CODENAME_COLUMN = 2;
// questcontentsdata.txt column 0 is the codename (no Service prefix; the
// case-0x1a walker reads it @0x007f45d0); column 4 is the +0xb1 byte parse
// (@0x008113d9) - the give-up confirm variant flag.
const CONTENTS_CODENAME_COLUMN = 0;
const CONTENTS_GIVEUP_WARN_COLUMN = 4;
// questcontentsdata.txt column 2: the int parse right after the col-1
// string skip (sub_810f10 @0x00810fdb) -> record+0xb4 = payload+0xb0, the
// sub_7e5fa0 country gate byte (0/1/3 in the retail table).
const CONTENTS_COUNTRY_COLUMN = 2;
// questcontentsdata "column 13": the trailing byte parse (sub_810f10
// @0x811439 CRT_Wtol) -> record+0xc4 = payload+0xc0, the sub_788210
// progress-sentinel clear byte. The shipped table stops at column 12, so
// every retail row parses 0 (see the VERSION 5 banner note).
const CONTENTS_PROGRESS_CLEAR_COLUMN = 13;

// The guide publishes quests up to the shard's configured cap
// (SRO_LEVEL_CAP, read with the server's own vocabulary -
// progression.LevelCapFromEnv): unset keeps the native 90, so the
// v1.150 build's output is unchanged; 110 admits the expansion's rows
// when a variant build supplies them (#1749).
function configuredGuideLevelCap() {
  const value = (process.env.SRO_LEVEL_CAP ?? "").trim().toLowerCase();
  if (value === "" || value === "0" || value === "off" || value === "false" || value === "90") return 90;
  if (value === "110") return 110;
  throw new Error(`[questdata] SRO_LEVEL_CAP must be off, 90 or 110, not ${JSON.stringify(value)}`);
}

export function buildQuestDataAsset() {
  for (const requiredPath of [questDataPath, questContentsDataPath, textQuestPath,textHelpPath,guideDataPath]) {
    if (!fs.existsSync(requiredPath)) {
      throw new Error(`[questdata] required source missing: ${requiredPath}`);
    }
  }

  // The questcontentsdata join table (codename -> the column-4 byte the
  // sub_810f10 parse stores at payload+0xb1 @0x008113d9, plus the column-2
  // byte it stores at payload+0xb0 @0x00810fdb). Duplicate codenames keep
  // the FIRST row (std::map insert semantics).
  const giveupWarnByCodename = new Map();
  const countryByCodename = new Map();
  const progressClearByCodename = new Map();
  for (const cols of readTextDataRowsSync(questContentsDataPath)) {
    const codename = cols[CONTENTS_CODENAME_COLUMN]?.trim();
    if (!codename || giveupWarnByCodename.has(codename)) {
      continue;
    }
    giveupWarnByCodename.set(
      codename,
      Number.parseInt(cols[CONTENTS_GIVEUP_WARN_COLUMN] ?? "0", 10) & 0xff || 0
    );
    countryByCodename.set(
      codename,
      Number.parseInt(cols[CONTENTS_COUNTRY_COLUMN] ?? "0", 10) & 0xff || 0
    );
    progressClearByCodename.set(
      codename,
      Number.parseInt(cols[CONTENTS_PROGRESS_CLEAR_COLUMN] ?? "0", 10) & 0xff || 0
    );
  }

  const rows = [];
  const giveupWarnBytes = [];
  const countryBytes = [];
  const progressClearBytes = [];
  for (const cols of readTextDataRowsSync(questDataPath)) {
    // The table loader keeps Service==1 rows (sub_9fbec0 @0x7f44fd); short
    // rows would underrun the native parse.
    if (cols.length < MIN_COLUMN_COUNT || cols[0].trim() !== "1") {
      continue;
    }
    rows.push(PROJECTED_COLUMNS.map((index) => cols[index]).join("\t"));
    // Unjoined rows keep the sub_810a10 ctor zeroes at payload+0xb1/+0xb0/+0xc0.
    const codename = cols[QUEST_CODENAME_COLUMN]?.trim();
    giveupWarnBytes.push(giveupWarnByCodename.get(codename) ?? 0);
    countryBytes.push(countryByCodename.get(codename) ?? 0);
    progressClearBytes.push(progressClearByCodename.get(codename) ?? 0);
  }

  // The FULL textquest table (version 2): key column 1, English column 8
  // (the buildTextCatalog column semantics - Korean defaults fill
  // authored gaps). The title chain resolves the questdata payload+0x24
  // codenames; the CIFQuestSlotSub expansion resolves the WIRE-carried
  // SQuestContents+0x08 symbols (sub_5c4870) - both against this one
  // manager slice, exactly the native sub_797240 wholesale boot load.
  const textEntries = {};
  for (const cols of readLocalizedTextDataRowsSync(textQuestPath)) {
    const key = cols[1]?.trim();
    // Duplicate keys keep the FIRST row (the manager's std::map
    // insert-if-absent).
    if (!key || key in textEntries) {
      continue;
    }
    // Retail English, else the authored product completion
    // (englishCompletions.mjs; coverage-gated for every active row). Korean and
    // translator-note columns are never shown as English.
    const english = completedEnglish("textquest.txt", cols);
    textEntries[key] = (isPlaceholderText(english) ? "" : english).replaceAll("\\n", "\n");
  }

  // Native English/English lookup (797240/791F50): keep authored empty and
  // literal "0" cells. Column 3 contains translator notes, never UI fallback.
  const guideKeys=new Set(readTextDataRowsSync(guideDataPath).filter(r=>r[0]==='1'&&Number(r[1])>=100000).flatMap(r=>[r[5],r[6]]));
  const guideTextEntries={};
  for(const file of [textQuestPath,textHelpPath])for(const fields of readLocalizedTextDataRowsSync(file)){
    const key=fields[1];if(fields[0]!=='1'||!guideKeys.has(key)||key in guideTextEntries)continue;
    guideTextEntries[key]=completedEnglish(path.basename(file),fields).replaceAll('\\n','\n');
  }
  // Untranslated region-group titles ship as literal "0"/empty English cells
  // (verified in raw texthelp.txt); completed from the shared attested map.
  completeGuideTitles(guideTextEntries);
  const catalog = {
    sourcePaths: [questDataPath, questContentsDataPath, textQuestPath,textHelpPath,guideDataPath].map((sourcePath) =>
      path.relative(gameRoot, sourcePath).replaceAll("\\", "/")
    ),
    format: "sro-questdata",
    version: 5,
    // Source column index per shipped tab-separated field (the contract in
    // the banner above; decode semantics live in questPlane.ts).
    columns: PROJECTED_COLUMNS,
    rows,
    // PARALLEL to `rows`: the questcontentsdata column-4 byte joined by
    // codename (payload+0xb1, the sub_5c2440 give-up confirm variant).
    giveupWarnBytes,
    // PARALLEL to `rows`: the questcontentsdata column-2 byte joined by
    // codename (payload+0xb0, the sub_7e5fa0 country gate - 3 = both).
    countryBytes,
    // PARALLEL to `rows`: the questcontentsdata trailing byte joined by
    // codename (payload+0xc0, the sub_788210 progress-sentinel clear -
    // all zeros in the shipped table, see the VERSION 5 banner note).
    progressClearBytes,
    // Additive guide projection: preserves the existing quest-journal schema.
    guideRecords: questGuideRecords(readTextDataRowsSync(questDataPath),readTextDataRowsSync(questContentsDataPath),configuredGuideLevelCap()),
    guideTextEntries,
    textEntries
  };

  const { outPath } = exportDataAsset({ publicRoot, outputFileName: "questData.json", value: catalog });

  console.log(
    `[questdata] wrote ${rows.length} projected rows (${Object.keys(textEntries).length} text entries) -> ${path.relative(publicRoot, outPath)}`
  );
  return {
    written: true,
    rows: rows.length,
    textEntries: Object.keys(textEntries).length,
    outPath
  };
}

// Run directly (node scripts/build/data/buildQuestDataAsset.mjs) or via the
// resource build aggregator.
if (isMainScript(import.meta.url)) {
  buildQuestDataAsset();
}
