import path from "node:path";
import {
  CHARACTER_SELECT_CAMERA_KEYFRAMES,
  CHARACTER_SELECT_CAMERA_KEY_INTERVAL_SECONDS,
  CHARACTER_SELECT_CREATE_CAMERA_KEYFRAMES,
  CHARACTER_SELECT_CREATE_CAMERA_KEY_INTERVAL_SECONDS,
  CHARACTER_SELECT_REGION_6951,
  CHARACTER_SELECT_TERRAIN_SECTOR_MARGIN
} from "./constants.mjs";
import { writeJson } from "./io.mjs";
import { publicRoot } from "./paths.mjs";
import { buildWorldRegionResources } from "./buildWorldRegionResources.mjs";

// CPSCharacterSelect (CPSCharacterSelect_OnCreate .. CPSCharacterSelect_Update) is a CPSOuterInterface subclass
// just like CPSTitle, but it streams its OWN dedicated stage region (0x6951, the
// European wharf) instead of reusing the title's Constantinople city (0x694e).
//
// This emits two artifacts that mirror the title pipeline:
//   * assets/world/<area>/region-6951.json  - the self-contained SWorld bundle.
//   * assets/character-select/world-manifest.json - the CPS world descriptor
//     (mapSector + camera keyframes + region bundle path) the client loads.
export async function buildCharacterSelectWorldRegionResources() {
  const resources = await buildWorldRegionResources({
    area: CHARACTER_SELECT_REGION_6951.area,
    sectorId: CHARACTER_SELECT_REGION_6951.sectorId,
    sectorX: CHARACTER_SELECT_REGION_6951.sectorX,
    sectorY: CHARACTER_SELECT_REGION_6951.sectorY,
    terrainSectorMargin: CHARACTER_SELECT_TERRAIN_SECTOR_MARGIN
  });

  const manifest = buildCharacterSelectWorldManifest(
    resources.bundle,
    resources.regionBundlePublicPath,
    resources.worldRegionsPublicPath
  );
  const manifestPublicPath = "/assets/character-select/world-manifest.json";
  const manifestPath = path.join(publicRoot, "assets", "character-select", "world-manifest.json");
  await writeJson(manifestPath, manifest);

  return {
    ...resources,
    manifestPublicPath,
    regionBundlePublicPath: resources.regionBundlePublicPath
  };
}

function buildCharacterSelectWorldManifest(bundle, regionBundlePublicPath, worldRegionsPublicPath) {
  const sectorX = bundle.source.sectorX;
  const sectorY = bundle.source.sectorY;
  // bundle.source.sectorId is the "0x6951" hex string; its digits name the navmesh.
  const sectorIdHexDigits = bundle.source.sectorId.replace(/^0x/i, "");

  // The keyframes are LOCAL to the 0x6951 region: CCameraPath_Evaluate (non sector-adjusted
  // AddKey) inserts them verbatim, so sectorX/sectorY point at the stage region
  // and position stays in that region's local frame.
  const toKey = (key, timeSeconds) => ({
    timeSeconds,
    sourceTimeSeconds: timeSeconds,
    sectorX,
    sectorY,
    position: { x: key.position.x, y: key.position.y, z: key.position.z },
    rotation: { x: key.rotation.x, y: key.rotation.y, z: key.rotation.z },
    mode: key.mode
  });

  const camera = CHARACTER_SELECT_CAMERA_KEYFRAMES.map((key, index) =>
    toKey(key, index * CHARACTER_SELECT_CAMERA_KEY_INTERVAL_SECONDS)
  );

  // CREATE sub-mode fly (CPSCharacterSelect_EnterCreateView): 4 keys at t = i * (5/3), controller targetTime 5.0s.
  const createCamera = CHARACTER_SELECT_CREATE_CAMERA_KEYFRAMES.map((key, index) =>
    toKey(key, index * CHARACTER_SELECT_CREATE_CAMERA_KEY_INTERVAL_SECONDS)
  );

  const objects2 = bundle.source.objects2;
  return {
    sourcePath: "SRO_Client.exe:sub_73bf00 (CPSCharacterSelect::OnCreate)",
    introName: "characterselect",
    area: bundle.source.area,
    camera,
    cameraControllerTargetTimeSeconds: camera.at(-1)?.timeSeconds ?? 0,
    createCamera,
    createCameraControllerTargetTimeSeconds: createCamera.at(-1)?.timeSeconds ?? 0,
    mapSector: {
      sectorX,
      sectorY,
      mapFiles: {
        terrain: bundle.source.terrain,
        texture: bundle.source.terrainTexture,
        objects: objects2.replace(/\.o2$/i, ".o"),
        objects2
      },
      navmesh: `extracted/Data_extracted/navmesh/nv_${sectorIdHexDigits}.nvm`
    },
    regionBundlePublicPath,
    worldRegionsPublicPath,
    seedRegionId: bundle.source.sectorId,
    seedResources: []
  };
}
