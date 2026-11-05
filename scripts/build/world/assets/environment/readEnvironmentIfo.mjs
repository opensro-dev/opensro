import { readFileSync } from "node:fs";
import path from "node:path";
import { extractedRoot } from "../../paths.mjs";
import { decodeJmxText } from "../../../shared/jmxBinaryReader.mjs";
import {
  ENV_TIME_CYCLE,
  ENV_TRACKS,
  ENV_TRACKS_READ_ORDER
} from "./environmentTracks.mjs";

// environment.ifo (JMXVENVI "1003") day/night environment curve tables - GENERAL parser.
// Every track in the canonical table (environmentTracks.mjs) is parsed and carried, not just
// the sky-relevant ones, so any consumer (sky, cloud, stars, fog, sun/moon) can be wired
// without re-RE.
//   loader SEnvironment_ReadIfo @ 0x008a7870, samplers EnvironmentVectorTrack_SampleAtTime (vec3) / EnvironmentScalarTrack_SampleAtTime (scalar).

const ENVIRONMENT_IFO_PATH = path.join(extractedRoot, "Map_extracted", "environment.ifo");

export { ENV_TIME_CYCLE };

function createReader(buffer) {
  let pos = 0;
  return {
    get pos() {
      return pos;
    },
    u16() {
      const v = buffer.readUInt16LE(pos);
      pos += 2;
      return v;
    },
    u32() {
      const v = buffer.readUInt32LE(pos);
      pos += 4;
      return v;
    },
    f32() {
      const v = buffer.readFloatLE(pos);
      pos += 4;
      return v;
    },
    str() {
      const n = buffer.readUInt32LE(pos);
      pos += 4;
      const s = decodeJmxText(buffer.subarray(pos, pos + n));
      pos += n;
      return s;
    },
    bytes(n) {
      const slice = buffer.subarray(pos, pos + n);
      pos += n;
      return slice;
    }
  };
}

function readVec3Track(reader) {
  const count = reader.u32();
  const keyframes = [];
  for (let i = 0; i < count; i += 1) {
    const r = reader.f32();
    const g = reader.f32();
    const b = reader.f32();
    const t = reader.f32();
    keyframes.push({ t, r, g, b });
  }
  return keyframes;
}

function readScalarTrack(reader) {
  const count = reader.u32();
  const keyframes = [];
  for (let i = 0; i < count; i += 1) {
    const value = reader.f32();
    const t = reader.f32();
    keyframes.push({ t, value });
  }
  return keyframes;
}

// Decode CP949 / EUC-KR region names. Node's ICU build ships the euc-kr table.
const EUC_KR_DECODER = new TextDecoder("euc-kr");

// Second section (after the env entries .. EOF) is a region->env-key TREE. Nodes are
// length-prefixed (u32 len) strings; a u16 immediately after the name is that node's env key
// (0 = inherit/default for group + root nodes). Verified: env-tree node = 16-byte record
// [u32 namelen][name][u16 key][u16 nodeType(1=group,2=leaf)][flags] - NO sector bounds.
function readEnvironmentRegionTree(buffer, treeStart, maxEnvKey) {
  const regions = [];
  let pos = treeStart;
  while (pos + 4 < buffer.length) {
    const len = buffer.readUInt32LE(pos);
    if (len >= 1 && len <= 64 && pos + 4 + len + 2 <= buffer.length) {
      const raw = buffer.subarray(pos + 4, pos + 4 + len);
      if (isPrintableName(raw)) {
        const after = pos + 4 + len;
        const envKey = buffer.readUInt16LE(after);
        if (envKey <= maxEnvKey) {
          let name;
          try {
            name = EUC_KR_DECODER.decode(raw);
          } catch {
            name = raw.toString("latin1");
          }
          regions.push({ name, envKey, offset: pos });
          pos = after;
          continue;
        }
      }
    }
    pos += 1;
  }
  return regions;
}

function isPrintableName(raw) {
  for (const b of raw) {
    if (b < 0x20) {
      return false;
    }
  }
  return true;
}

export function readEnvironmentIfo(filePath = ENVIRONMENT_IFO_PATH) {
  const buffer = readFileSync(filePath);
  const signature = buffer.toString("latin1", 0, 8);
  if (signature !== "JMXVENVI") {
    throw new Error(`environment.ifo: unexpected signature ${JSON.stringify(signature)}`);
  }
  const version = buffer.toString("latin1", 8, 12);

  const reader = createReader(buffer);
  reader.bytes(12); // signature + version
  const entryCount = reader.u16();
  reader.str(); // global string (empty)

  const entries = [];
  for (let i = 0; i < entryCount; i += 1) {
    const key = reader.u16();
    const name = reader.str();
    reader.str();
    reader.str();
    // Parse every track in canonical file order, keyed by its framework id.
    const tracks = {};
    for (const track of ENV_TRACKS_READ_ORDER) {
      tracks[track.id] = track.kind === "vec3" ? readVec3Track(reader) : readScalarTrack(reader);
    }
    entries.push({ key, name, tracks });
  }

  const maxEnvKey = entries.reduce((max, entry) => Math.max(max, entry.key), 0);
  const regionTree = readEnvironmentRegionTree(buffer, reader.pos, maxEnvKey);

  return { version, entryCount, entries, regionTree };
}

const RECONSTRUCTED_FROM = {
  file: "environment.ifo",
  loader: "sub_8a7870",
  sampler: "sub_4dc920/sub_4dcab0",
  evaluator: "sub_8a7d10",
  timeAdvance: "MapRenderer+0x26a3",
  regionTree: "sub_8a7870 second section (region->env-key tree)",
  doc: "reconstruct/verified_sub_8a7d10_EnvironmentTrackEvaluation.txt"
};

// The 3D login/title background renders the Constantinople city, which belongs to the
// "\uB3D9\uC720\uB7FD" (East Europe) zone. The dedicated "\uB85C\uADF8\uC778" (LOGIN) tree node
// resolves to env key 24 / entry "Env23" - a cyan, STAR-LESS night that does NOT match the
// original night login (violet sky + bright stars). The original instead uses the city's
// E.Europe environment ("Env8" family: violet starry night, yellow dawn, no midday stars).
const LOGIN_REGION_NAME = String.fromCodePoint(0xb85c, 0xadf8, 0xc778); // ·Î±×ÀÎ (unused authored LOGIN node)
const EAST_EUROPE_GROUP_NAME = String.fromCodePoint(0xb3d9, 0xc720, 0xb7fd); // µ¿À¯·´ (E.Europe zone)
const TOWN_REGION_NAME = String.fromCodePoint(0xb9c8, 0xc744); // ¸¶À» (town/village leaf)

// Locate the Constantinople login env node: the "¸¶À»" (town) leaf that follows the
// "µ¿À¯·´" (E.Europe) group node in the region tree (Env8 family, env key 33). The tree is
// stored in document order, so the town leaf under E.Europe is the first "¸¶À»" leaf whose
// byte offset is past the E.Europe group node.
function resolveLoginRegionNode(regionTree) {
  const group = regionTree.find((node) => node.name === EAST_EUROPE_GROUP_NAME);
  if (group) {
    const townLeaf = regionTree
      .filter((node) => node.name === TOWN_REGION_NAME && node.envKey !== 0 && node.offset > group.offset)
      .sort((a, b) => a.offset - b.offset)[0];
    if (townLeaf) {
      return townLeaf;
    }
  }
  return (
    regionTree.find((node) => node.name === LOGIN_REGION_NAME && node.envKey !== 0) ??
    regionTree.find((node) => node.name === LOGIN_REGION_NAME) ??
    null
  );
}

// Convert a parsed env entry into the runtime sky-environment shape. The
// canonical `tracks` table is the only curve authority.
function toSkyEnvironment(entry, version) {
  return {
    reconstructedFrom: RECONSTRUCTED_FROM,
    fileVersion: version,
    envKey: entry.key,
    envName: entry.name,
    startTimeOfDay: ENV_TIME_CYCLE.startTimeOfDay,
    cycleSeconds: ENV_TIME_CYCLE.cycleSeconds,
    ratePerSecond: ENV_TIME_CYCLE.ratePerSecond,
    // Full general track set (every environment.ifo curve), keyed by framework id.
    tracks: entry.tracks
  };
}

// Extract the sky-relevant day/night environment for a given env key.
export function resolveSkyEnvironment(envKey = 0, filePath = ENVIRONMENT_IFO_PATH) {
  const { version, entries } = readEnvironmentIfo(filePath);
  const entry = entries.find((candidate) => candidate.key === envKey) ?? entries[0];
  if (!entry) {
    return null;
  }
  return toSkyEnvironment(entry, version);
}

// Resolve the env key bound to a region/zone name via the environment.ifo tree.
export function resolveEnvKeyForRegion(regionName, filePath = ENVIRONMENT_IFO_PATH) {
  const { regionTree } = readEnvironmentIfo(filePath);
  const matches = regionTree.filter((node) => node.name === regionName);
  const leaf = matches.find((node) => node.envKey !== 0) ?? matches[0];
  return leaf ? leaf.envKey : 0;
}

// Resolve the login/title screen environment (Constantinople E.Europe town), derived from the
// tree rather than hardcoding a magic number.
export function resolveLoginSkyEnvironment(filePath = ENVIRONMENT_IFO_PATH) {
  const { version, entries, regionTree } = readEnvironmentIfo(filePath);
  const loginNode = resolveLoginRegionNode(regionTree);
  const envKey = loginNode ? loginNode.envKey : 0;
  const entry = entries.find((candidate) => candidate.key === envKey) ?? entries[0];
  if (!entry) {
    return null;
  }
  return { ...toSkyEnvironment(entry, version), regionName: loginNode?.name ?? null };
}

// Build the full shared sky-environment catalog: every env entry's full track set keyed by
// env key, plus the region->env-key map, the canonical track table, and the day/night cycle.
export function buildSkyEnvironmentCatalog(filePath = ENVIRONMENT_IFO_PATH) {
  const { version, entries, regionTree } = readEnvironmentIfo(filePath);
  const environments = {};
  for (const entry of entries) {
    environments[entry.key] = toSkyEnvironment(entry, version);
  }
  const loginNode = resolveLoginRegionNode(regionTree);
  return {
    reconstructedFrom: RECONSTRUCTED_FROM,
    fileVersion: version,
    timeCycle: { ...ENV_TIME_CYCLE },
    // Canonical track metadata (id/kind/fileOffset/fieldOffset/role/transform) so runtime
    // consumers and tooling share one description of the env curve set.
    trackTable: ENV_TRACKS,
    loginEnvKey: loginNode ? loginNode.envKey : 0,
    regions: regionTree.map((node) => ({ name: node.name, envKey: node.envKey })),
    environments
  };
}
