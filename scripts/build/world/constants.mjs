export const SIGNATURE_BYTES = 12;

export const MAPM_SIGNATURE = "JMXVMAPM1000";
export const MAPT_SIGNATURE = "JMXVMAPT1001";
export const MAPO_SIGNATURE = "JMXVMAPO1001";
export const VOBJI_SIGNATURE = "JMXVOBJI1000";
export const TILE2D_SIGNATURE = "JMXV2DTI1001";

export const MAP_BLOCKS_PER_AXIS = 6;
export const MAPM_VERTICES_PER_AXIS = 17;
export const MAPM_TILES_PER_AXIS = 16;
export const MAPM_BLOCK_BYTES = 2575;
export const MAPM_TEXTURE_ID_MASK = 0x03ff;
export const MAPM_TEXTURE_ATTRIBUTE_SHIFT = 10;
export const MAPT_NATIVE_LIGHT_BYTES =
  MAP_BLOCKS_PER_AXIS * MAP_BLOCKS_PER_AXIS * MAPM_TILES_PER_AXIS * MAPM_TILES_PER_AXIS;

export const WATER_NORMAL_FRAME_COUNT = 30;
export const WATER_NORMAL_FRAME_DURATION_MS = 100;

export const MAPO2_LOD_GROUPS = 4;
export const MAPO2_PLACEMENT_BYTES = 30;

export const REGION_SIZE = 1920;
export const OUTDOOR_WORLD_SHARED_RENDER_PUBLIC_PATH =
  "/assets/world/outdoor/shared-render-resources.json";
export const TITLE_TERRAIN_SECTOR_MARGIN = 1;

export const TITLE_REGION_694E = Object.freeze({
  area: "constantinople",
  sectorId: 0x694e,
  sectorX: 0x4e,
  sectorY: 0x69
});

// CPSCharacterSelect::OnCreate (CPSCharacterSelect_OnCreate @ 0x0073c8e8) switches the world to a
// dedicated stage region BEFORE placing the camera/pedestal/idols:
//   (uint16_t)region = 0x6951; (data_effe90 + 4)(region);  // SetWorldRegion
//   ref = (920, 0, 920);       (*data_effe90)(region, &ref) // stream reference
// 0x6951 packs to sector (x=0x51=81, y=0x69=105) - three sectors east of the
// title's 0x694e, i.e. the European port/wharf. The camera (60,-15,700) and the
// box/idol props (~155,-20,652) are all LOCAL to this region's SW corner.
export const CHARACTER_SELECT_REGION_6951 = Object.freeze({
  area: "constantinople",
  sectorId: 0x6951,
  sectorX: 0x51,
  sectorY: 0x69
});

// Native streams the 3x3 sector neighbourhood around the (920,0,920) reference,
// so the wharf geometry that spills into the neighbouring sectors stays loaded.
export const CHARACTER_SELECT_TERRAIN_SECTOR_MARGIN = 1;

// StaticInit_CharSelectCameraKeys (data_cc995c) - the verified char-select camera keyframe table, in
// the 0x6951 region-local frame. Kept here so the build manifest and the client
// stay in lock-step on the single source of native truth.
export const CHARACTER_SELECT_CAMERA_KEYFRAMES = Object.freeze([
  Object.freeze({ position: { x: 60, y: -15, z: 700 }, rotation: { x: 0.100000001, y: 3, z: 0 }, mode: 30 }),
  Object.freeze({ position: { x: 60, y: -15, z: 660 }, rotation: { x: 0.100000001, y: 3, z: 0 }, mode: 30 })
]);

// CPSCharacterSelect adds the two keys at t = keyIndex*2 (CPSCharacterSelect_BuildCameraPath) with a
// controller targetTime of 2.0s; the dolly is advanced by raw frame delta.
export const CHARACTER_SELECT_CAMERA_KEY_INTERVAL_SECONDS = 2;
export const CHARACTER_SELECT_REFERENCE_POSITION = Object.freeze({ x: 920, y: 0, z: 920 });

// CRITICAL: CPSCharacterCreate*_OnCreate streams a race-specific customize
// backdrop, not the character-select wharf and not a single shared Europe scene.
//   China  CPSCharacterCreateChina_OnCreate: region 0x62a8, camera/world anchor (960.418884, 50, 458.259766)
//   Europe CPSCharacterCreateEurope_OnCreate: region 0x694f, camera/world anchor (335, 140, 1460)
// The CInterfaceModel_SetPreviewTransform(3,0.5,0,0) that follows is local to the
// CIF character model control; do not reinterpret it as terrain/world placement.
export const CHARACTER_CREATE_REGIONS = Object.freeze({
  europe: Object.freeze({
    area: "constantinople",
    sectorId: 0x694f,
    sectorX: 0x4f,
    sectorY: 0x69,
    sourcePath: "SRO_Client.exe:sub_730e40 (CPSCharacterCreateEurope_OnCreate)",
    cameraAnchor: Object.freeze({ x: 335, y: 140, z: 1460 })
  }),
  china: Object.freeze({
    area: "china",
    sectorId: 0x62a8,
    sectorX: 0xa8,
    sectorY: 0x62,
    sourcePath: "SRO_Client.exe:sub_72d070 (CPSCharacterCreateChina_OnCreate)",
    cameraAnchor: Object.freeze({ x: 960.418884, y: 50, z: 458.259766 })
  })
});

// Native streams the neighbourhood around the preview reference, same shape as the
// character-select/title CPS stages.
export const CHARACTER_CREATE_TERRAIN_SECTOR_MARGIN = 1;

// StaticInit_CharCreateCameraKeys (data_cc999c table) - the verified CREATE sub-mode camera keyframes, in the same
// 0x6951 region-local frame. CPSCharacterSelect::StartCreateCamera (CPSCharacterSelect_EnterCreateView @ 0x00739b30)
// inserts these 4 keys via CCameraPath_Evaluate at t = index * (5.0/3.0) and sets the controller targetTime
// to 5.0s. Key 0 == the select-mode resting pose (60,-15,660), so the fly is continuous; the
// camera swings right/down toward the creation pedestal. Values are verbatim native floats.
export const CHARACTER_SELECT_CREATE_CAMERA_KEYFRAMES = Object.freeze([
  Object.freeze({ position: { x: 60, y: -15, z: 660 }, rotation: { x: 0.100000001, y: 3, z: 0 }, mode: 30 }),
  Object.freeze({ position: { x: 100, y: 0, z: 625 }, rotation: { x: -0.100000001, y: 2.20000005, z: 0 }, mode: 30 }),
  Object.freeze({ position: { x: 150, y: -10, z: 642 }, rotation: { x: -0.00999999978, y: 1.79999995, z: 0 }, mode: 30 }),
  Object.freeze({ position: { x: 165.199997, y: -33.9000015, z: 640.299988 }, rotation: { x: 0.620000005, y: 2.52999997, z: 0 }, mode: 30 })
]);

// StartCreateCamera inserts key i at t = i * (5/3); controller targetTime = 5.0s, raw frame delta.
export const CHARACTER_SELECT_CREATE_CAMERA_KEY_INTERVAL_SECONDS = 5 / 3;

// CPSCharacterSelect::OnCreate (CPSCharacterSelect_OnCreate at 0x73c930..0x73cb6a) loads the foreground
// interface props as CInterfaceModel instances (NOT SWorld objects):
//   CCObjAnimation_LoadModel(model, "res\interface\<bsr>", 1) // CInterfaceModel::Load
//   CCObjCharacter_ForwardVSlot28(model, &pos)                      // SetPosition (region-local)
//   CCObjCharacter_ForwardVSlot30(model, yaw)                       // SetYaw (radians, about +Y)
//   (*model + 0xc)(0,0,0,0, 1.0, 1.0)            // SetScale
// The pedestal lives at self+0x1f0; the three race idols are a 3-entry array at
// self+0x160 (stride 0xc). Positions/yaws are verbatim from the native floats.
export const CHARACTER_SELECT_INTERFACE_MODELS = Object.freeze([
  Object.freeze({
    name: "pedestal",
    bsrPath: "res\\interface\\box.bsr",
    position: Object.freeze({ x: 155, y: -20, z: 652 }),
    yaw: 3,
    scale: 1
  }),
  Object.freeze({
    name: "idol_europe",
    bsrPath: "res\\interface\\interface_idol_europe.bsr",
    position: Object.freeze({ x: 157, y: -20, z: 654.400024 }),
    yaw: 3.30999994,
    scale: 1
  }),
  Object.freeze({
    name: "idol_china",
    bsrPath: "res\\interface\\interface_idol_china.bsr",
    position: Object.freeze({ x: 156.199997, y: -20, z: 651.599976 }),
    yaw: 3.02999997,
    scale: 1
  }),
  Object.freeze({
    name: "idol_lizard",
    bsrPath: "res\\interface\\interface_lizard.bsr",
    position: Object.freeze({ x: 155.600006, y: -20, z: 651.599976 }),
    yaw: 3,
    scale: 1
  })
]);
