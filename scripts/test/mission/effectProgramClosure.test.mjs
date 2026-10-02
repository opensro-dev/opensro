import assert from "node:assert/strict";
import path from "node:path";
import { test } from "node:test";
import {
  collectNativeExecutableEffectReferences,
  collectModelParticleReferences
} from "../../build/effects/buildEffectPrograms.mjs";
import {
  parseSkillAniSet,
  readSkillEffectSourceText
} from "../../build/char/parseSkillEffect.mjs";
import {
  gameRoot,
  retailTextdataRoot
} from "../../build/world/paths.mjs";
import { readPublishedAssetJson } from "../../lib/publishedAsset.mjs";

const EFFECT_RECORDS_PATH = "/assets/skill/effectRecords.json";
const EFFECT_PROGRAMS_PATH = "/assets/effects/programs.json";

function normalizeEffectPath(value) {
  return String(value ?? "").trim().replaceAll("\\", "/").replace(/^\/+/, "").toLowerCase();
}

function collectRecordEffectPaths(records) {
  const paths = new Set();
  const add = (value) => {
    const normalized = normalizeEffectPath(value);
    if (normalized.endsWith(".efp")) paths.add(normalized);
  };

  for (const record of Object.values(records)) {
    add(record.damageEffectPath);
    add(record.criticalDamageEffectPath);
    add(record.arrowTrailEffectPath);
    add(record.arrowForceEffectPath);
    for (const stage of record.authoredStages ?? []) {
      add(stage.objectResourcePath);
      add(stage.secondaryObjectPath);
    }
  }
  return [...paths].sort();
}

function catalogResolves(path, catalog) {
  if (catalog.effects[path]) return true;
  const basename = path.split("/").at(-1) ?? path;
  return catalog.effects[basename] !== undefined &&
    catalog.ambiguousEffectAliases?.[basename] === undefined;
}

test("every generated skill-effect EFP reaches the generated GPU program catalog", async () => {
  const [records, catalog] = await Promise.all([
    readPublishedAssetJson(EFFECT_RECORDS_PATH),
    readPublishedAssetJson(EFFECT_PROGRAMS_PATH),
  ]);
  const references = collectRecordEffectPaths(records);
  const unresolved = references.filter((path) => !catalogResolves(path, catalog));

  assert.ok(references.length > 600, "the complete textdata/skilleffect EFP closure was not scanned");
  assert.deepEqual(unresolved, []);
  assert.equal(catalog.version, 3);
  assert.match(catalog.source, /native reachable CIDecoSkillRecord EFP closure/);
  assert.ok(catalog.effects["system/system_levelup.efp"], "SYSTEM_LEVELUP has no GPU program");
});

test("every runtime-reachable EFP is mandatory source, never tolerated debt", async () => {
  const catalog = await readPublishedAssetJson(EFFECT_PROGRAMS_PATH);
  assert.deepEqual(catalog.missingEffects, []);
});

test("published stage models retain every native particle modifier and its EFP dependency", async () => {
  const [records, catalog, manifest] = await Promise.all([
    readPublishedAssetJson(EFFECT_RECORDS_PATH), readPublishedAssetJson(EFFECT_PROGRAMS_PATH),
    readPublishedAssetJson('/assets/skillfx/manifest.json')
  ]);
  const references = collectModelParticleReferences(records);
  assert.ok(references.length > 10);
  assert.deepEqual(catalog.reachability.modelParticleReferences, references);
  for (const { modelPath, effectPath } of references) {
    assert.ok(catalogResolves(effectPath, catalog), `${modelPath}: ${effectPath} missing from program catalog`);
    assert.ok(manifest.models[modelPath]?.particleModifiers.some(modifier =>
      modifier.entries.some(entry => normalizeEffectPath(entry.effectPath) === effectPath)),
    `${modelPath}: particle attachment lost during GLB export`);
  }
});

test("direct executable EFP literals are discovered from retail instead of hand-carried", async () => {
  const catalog = await readPublishedAssetJson(EFFECT_PROGRAMS_PATH);
  const executableReferences = collectNativeExecutableEffectReferences(
    path.join(gameRoot, "SRO_Client.exe")
  );
  assert.ok(executableReferences.length > 0);
  assert.deepEqual(
    catalog.reachability?.nativeExecutableReferences,
    executableReferences
  );
  for (const effectPath of executableReferences) {
    assert.ok(catalogResolves(effectPath, catalog), `${effectPath} executable literal is not compiled`);
  }
});

test("skillaniset2 Service=0 authoring rows never cross into runtime records", async () => {
  const skillEffectPath = path.join(retailTextdataRoot, "skilleffect.txt");
  const disabledNames = new Set();
  let inVersionedAnimationSection = false;
  for (const line of readSkillEffectSourceText(skillEffectPath).split(/\r?\n/)) {
    if (line.startsWith("#section")) {
      inVersionedAnimationSection = /^#section\s+skillaniset2\b/i.test(line);
      continue;
    }
    if (!inVersionedAnimationSection || !line || line.startsWith("//")) continue;
    const columns = line.split("\t");
    if (String(columns[0] ?? "").trim() !== "1" && columns[2]) {
      disabledNames.add(columns[2]);
    }
  }

  assert.ok(disabledNames.size > 0, "fixture has no disabled versioned animation rows");
  const activeAnimationSets = parseSkillAniSet(skillEffectPath);
  for (const name of disabledNames) {
    assert.equal(activeAnimationSets.has(name), false, `${name} ignored its native Service gate`);
  }

  const records = await readPublishedAssetJson(EFFECT_RECORDS_PATH);
  for (const record of Object.values(records)) {
    assert.equal(
      disabledNames.has(record.animBaseName),
      false,
      `${record.animBaseName} crossed from disabled authoring into the runtime table`
    );
  }
});

test("stationary skill objects authored as EFP resources reach the program catalog", async () => {
  const [catalog, manifest] = await Promise.all([
    readPublishedAssetJson(EFFECT_PROGRAMS_PATH),
    readPublishedAssetJson("/assets/skillfx/manifest.json")
  ]);
  const effects = [...new Set(Object.values(manifest.objects ?? {})
    .filter((resource) => resource.kind === "effect")
    .map((resource) => normalizeEffectPath(resource.path)))].sort();
  assert.ok(effects.includes("skill/europe/wizard_fire_trap_creation.efp"), "Fire Trap object resource not scanned");
  for (const effectPath of effects) {
    assert.ok(catalogResolves(effectPath, catalog), `${effectPath} skill object has no GPU program`);
  }
});
