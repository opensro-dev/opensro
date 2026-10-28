// Canonical environment.ifo track table - the SINGLE source of truth for parsing and
// naming every day/night environment curve. Mirrored (by id/offset) in the runtime at
// packages/runtime/src/world/environment/environmentTracks.ts.
//
// Each track is sampled per frame by the time-of-day and written into a MapRenderer env
// field. Order below is the FILE read order (matches the on-disk env-entry layout and the
// native sampling order in SWorld_UpdateEnvironmentLighting @ 0x008a7d10).
//
//   vec3 sampler EnvironmentVectorTrack_SampleAtTime, scalar sampler EnvironmentScalarTrack_SampleAtTime; field block MapRenderer+0x682f..+0x68cb.

// Scalar post-sample transforms (applied to the raw sampled value before the native store):
//   identity     : value as-is (consumer may clamp, e.g. star clamps to [-1,1])
//   halfBias      : (value + 1) * 0.5  -> remaps [-1,1] to [0,1]
//   invHalfBias   : 2 - (clamp(value,-1,1) + 1)  -> inverted half-bias of the [-1,1]-clamped
//                   sample (native clamp is in the COMMON path SWorld_UpdateEnvironmentLighting @008a806c, applies to all envs)
//   scaledCcbee8  : value * data_ccbee8 (runtime scale constant; consumer not yet wired)
export const ENV_SCALAR_TRANSFORM = {
  identity: "identity",
  halfBias: "halfBias",
  invHalfBias: "invHalfBias",
  scaledCcbee8: "scaledCcbee8"
};

// id        : stable framework id (semantic where the consumer is confirmed, else color/scalar+offset)
// kind      : "vec3" | "scalar"
// fileOffset: offset of the track within the 0x398-byte env entry (parse order key)
// fieldOffset: destination MapRenderer field (first float; vec3 uses 3 consecutive floats)
// role      : human description / consumer
// transform : (scalars only) post-sample transform id
export const ENV_TRACKS = [
  { id: "color0x88", kind: "vec3", fileOffset: 0x88, fieldOffset: 0x682f, role: "SUN DISC tint -> D3DRS_TEXTUREFACTOR = pack255(this) (sub_8ac9c0 @008acc1a). Sun billboard stage0 COLOROP=SELECTARG1(TFACTOR) so RGB = FLAT this tint (texture RGB ignored), ALPHAOP=SELECTARG1(TEXTURE) = lens2.ddj alpha; NOT a texture*TFACTOR modulate. Also sub_8cad50 arg5 (sky-dome dirty-check input only, never blended into a vertex). Verified: verified_sub_8ac9c0_8acf80_SunMoonLensFlare.txt" },
  { id: "zenith", kind: "vec3", fileOffset: 0xbc, fieldOffset: 0x683b, role: "sky dome zenith (sub_8cad50 arg3). Also packed into scene D3DMATERIAL9.Emissive (sub_8a7d10 @008a96e1, note 222) but that material is overwritten by ResetPerFrameDeviceState before lit draws - dome colour is the visible consumer" },
  { id: "color0xf0", kind: "vec3", fileOffset: 0xf0, fieldOffset: 0x6847, role: "OBJECT LIGHT+MATERIAL diffuse (MapRenderer+0x6847). CONFIRMED dynamic (runtime --pid): scene D3DLIGHT9 Diffuse = this * 0.6 -> data_f0ff28; D3DMATERIAL9.Diffuse = this (unscaled). sub_8a7d10 @008a93fb" },
  { id: "color0x124", kind: "vec3", fileOffset: 0x124, fieldOffset: 0x6853, role: "OBJECT LIGHT+MATERIAL ambient (MapRenderer+0x6853). CONFIRMED dynamic: scene D3DLIGHT9 Ambient = this * 1.0 (UNSCALED) -> data_f0ff38; D3DMATERIAL9.Ambient = this. sub_8a7d10 @008a9457" },
  { id: "scatter", kind: "vec3", fileOffset: 0x158, fieldOffset: 0x685f, role: "sky scatter / horizon glow (sub_8cad50 arg6)" },
  { id: "color0x18c", kind: "vec3", fileOffset: 0x18c, fieldOffset: 0x686b, role: "INERT / no visible consumer (MapRenderer+0x686b). Read by sub_8b3180 @008b34b7 into a D3DMATERIAL9.Emissive via SetMaterial (dev vtable+0xc4), but RUNTIME-CONFIRMED it never reaches geometry: it is ALWAYS overwritten first. The overwriter is MapRenderer_ResetPerFrameDeviceState (sub_887240) - a full memset'd 0x44 D3DMATERIAL9 SetMaterial @008872ac - which OutdoorWorldGeometryPass (sub_8b1050) calls @008b10ae/008b1262 BEFORE any object Render (vtbl+0x28 @008b13b2/15ae/1669). No draw occurs between 008b34eb and that overwrite (only sky-matrix SetTransform, ZENABLE/FOGENABLE, fog-colour pack, GetDeviceFogHelper config). The cloud draw @008b359c is BEFORE lighting is enabled @008b35cd, so fixed-function ignores the material there anyway. Confirmed by a live hardware break on IDirect3DDevice9::SetMaterial (title scene): the stable per-frame sequence is color0x18c -> ResetPerFrameDeviceState x2 -> object renderers (008b3a06/008b3a36 are not reached). NOTE: unrelated to the cloud 'scatter' tint (sky-object cache sky+0x18c, a coincidental offset collision). See revtool notes 368/369." },
  { id: "color0x1c0", kind: "vec3", fileOffset: 0x1c0, fieldOffset: 0x6883, role: "TERRAIN LIGHTMAP shadow-floor lift (RESOLVED + runtime-confirmed 2026-06-10). SetTerrainLightmapMultiplyPassState (sub_8a4d40) packs *255 -> D3DRS_TEXTUREFACTOR; pass (sub_8abfd0 gate +0x2f30, draw @008ac433, live at title) multiplies the terrain framebuffer by saturate(.t JMXVMAPT1001 512x512 lightmap + this): SRCBLEND=ZERO/DESTBLEND=SRCCOLOR, stage0 COLOROP=ADD(TEXTURE,TFACTOR). NOT scene fog (world fog = color0x2b4 -> D3DRS_FOGCOLOR 0x22, verified live). Title live value ~(0.041,0.041,0.054). Rebuild consumer: TerrainLightmapOverlay. See revtool notes 373/374." },
  { id: "scalar0x25c", kind: "scalar", fileOffset: 0x25c, fieldOffset: 0x689b, role: "sky dome scatter-glow RADIUS source (MapRenderer+0x689b -> sub_8cad50 arg8; glowR = max(1, halfBias(this)*80000)). Login env 33 = -1 all day -> glow OFF", transform: ENV_SCALAR_TRANSFORM.halfBias },
  { id: "scalar0x288", kind: "scalar", fileOffset: 0x288, fieldOffset: 0x689f, role: "sky dome VERTICAL-gradient falloff rate (MapRenderer+0x689f -> sub_8cad50 arg7, halved; h = clamp((vertexY/5000)*this*0.5,0,1), base = lerp(horizon,zenith,h)). transform invHalfBias clamps raw to [-1,1] in the common path (sub_8a7d10 @008a806c)", transform: ENV_SCALAR_TRANSFORM.invHalfBias },
  { id: "color0x2b4", kind: "vec3", fileOffset: 0x2b4, fieldOffset: 0x68a3, role: "SCENE FOG COLOUR -> D3DRS_FOGCOLOR (MapRenderer+0x68a3). Browser/terrain tint = linear clamp01(this) (sub_8b1050 +0x67e2 override); native world pass packs sqrt(clamp(this))*255 as the gamma fog tint +0x67e6 (sub_8b3180 @008b3783). CONFIRMED static + live (runtime --pid night login). consumer: applyNativeSceneFog/NativeViewZFogPlugin" },
  { id: "scalar0x2e8", kind: "scalar", fileOffset: 0x2e8, fieldOffset: 0x68af, role: "unmapped (MapRenderer+0x68af, likely fog)", transform: ENV_SCALAR_TRANSFORM.scaledCcbee8 },
  { id: "scalar0x314", kind: "scalar", fileOffset: 0x314, fieldOffset: 0x68b3, role: "unmapped (MapRenderer+0x68b3, likely fog)", transform: ENV_SCALAR_TRANSFORM.scaledCcbee8 },
  { id: "cloudAlpha", kind: "scalar", fileOffset: 0x340, fieldOffset: 0x68c7, role: "cloud layer alpha (sub_8a5350 -> sub_8cb770)", transform: ENV_SCALAR_TRANSFORM.halfBias },
  { id: "horizon", kind: "vec3", fileOffset: 0x1f4, fieldOffset: 0x6877, role: "sky dome horizon (sub_8cad50 arg2)" },
  { id: "color0x228", kind: "vec3", fileOffset: 0x228, fieldOffset: 0x688f, role: "unmapped (MapRenderer+0x688f, likely ambient)" },
  { id: "starAlpha", kind: "scalar", fileOffset: 0x36c, fieldOffset: 0x68cb, role: "star-field day/night alpha (sub_8cb380; clamp[-1,1])", transform: ENV_SCALAR_TRANSFORM.identity }
];

// Tracks in file read order (parser consumes them in exactly this sequence).
export const ENV_TRACKS_READ_ORDER = ENV_TRACKS;

// Lookup helpers.
export const ENV_TRACK_BY_ID = Object.freeze(
  Object.fromEntries(ENV_TRACKS.map((track) => [track.id, track]))
);

export const ENV_VEC3_TRACK_IDS = ENV_TRACKS.filter((t) => t.kind === "vec3").map((t) => t.id);
export const ENV_SCALAR_TRACK_IDS = ENV_TRACKS.filter((t) => t.kind === "scalar").map((t) => t.id);

// Native day/night time cycle (MapRenderer+0x26a3): start 0.5, advance += dt * rate, wrap [0,1].
export const ENV_TIME_CYCLE = {
  startTimeOfDay: 0.5,
  ratePerSecond: 0.0005000000237487257, // data_c14590 = 0x3f40624de0000000
  cycleSeconds: 1 / 0.0005000000237487257
};
