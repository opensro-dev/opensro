import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import {
  applyCifPreprocessor,
  loadCifDefines,
  parseCifDefines
} from "../../build/shared/cifPreprocessor.mjs";
import { discoverCifLayouts } from "../../build/shared/cifLayoutCatalog.mjs";
import { parseCifLayout } from "../../build/shared/cifResources.mjs";
import { publicRoot, readText, resinfoDir } from "../../build/shared/resourceIo.mjs";

const layoutOutputDir = path.join(publicRoot, "assets", "cif", "layouts");

test("CIF define registry follows active config/define.txt lines", async () => {
  const defines = await loadCifDefines();

  assert.equal(defines.EUROPE_SYSTEM, true);
  assert.equal(defines.RENEWAL_SHOP_SYSTEM, true);
  assert.equal(defines.APPLY_MENTOR_SYSTEM, true);
  assert.equal(defines.APPLY_AVATAR_SYSTEM, true);
  assert.equal(defines.APPLY_SIEGE_FORTRESS_SYSTEM, true);
  assert.equal(defines.APPLY_UI_4TH, true);
  assert.equal(defines.HISTORY_FOR_BUYING_SILK, undefined);
  assert.equal(parseCifDefines("//DISABLED\n ENABLED // comment\n").ENABLED, true);
} );

test("CIF preprocessor preserves lines and rejects malformed directive ownership", () => {
  const source = "first\n#ifdef LIVE\nactive\n#else\ninactive\n#endif\nlast";
  assert.equal(
    applyCifPreprocessor(source, { LIVE: true }).split("\n").length,
    source.split("\n").length
  );
  assert.match(applyCifPreprocessor(source, { LIVE: true }), /active/);
  assert.doesNotMatch(applyCifPreprocessor(source, { LIVE: true }), /\ninactive\n/);
  assert.throws(() => applyCifPreprocessor("#else", {}), /without #ifdef/);
  assert.throws(() => applyCifPreprocessor("#ifdef OPEN", {}), /unterminated/);
} );

test("every shipped resinfo source has a published layout", async () => {
  const [sourceFiles, publishedFiles] = await Promise.all([
    discoverCifLayouts(resinfoDir),
    readdir(layoutOutputDir)
  ]);
  const jsonFiles = publishedFiles
    .filter((name) => name.endsWith(".json"))
    .sort((left, right) => left.localeCompare(right));
  const expectedJsonFiles = sourceFiles.map((name) => name.replace(/\.txt$/i, ".json"));

  assert.equal(sourceFiles.length, 231);
  assert.deepEqual(jsonFiles, expectedJsonFiles);
} );

// cif-class-catalog.json was a one-off reverse-engineering export; no build
// step produces it, so the layout is checked against a fresh parse instead.
test("the generated layout parses the same live macro branch as its source", async () => {
  const [raw, defines, generatedSource] = await Promise.all([
    readText(path.join(resinfoDir, "ginterface.txt")),
    loadCifDefines(),
    readFile(path.join(layoutOutputDir, "ginterface.json"), "utf8")
  ]);
  const parsed = parseCifLayout(
    applyCifPreprocessor(raw, defines),
    path.join(resinfoDir, "ginterface.txt")
  );
  const generated = JSON.parse(generatedSource.replace(/^\uFEFF/, ""));

  assert.deepEqual(
    generated.sections.map((section) => section.name),
    parsed.sections.map((section) => section.name)
  );
} );
