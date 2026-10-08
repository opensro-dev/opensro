// Thin facade: the former monolith was split verbatim into per-domain modules
// (2026-07-28). This file re-exports the public surface so the root facades
// (scripts/build/{cif,text,title,launcher,fonts,config,audio}.mjs) stay untouched.
export { buildAudioResources } from "./audioResources.mjs";
export { buildCifResources } from "./cifResources.mjs";
export { buildConfigResources } from "./configResources.mjs";
export { buildFontResources } from "./fontResources.mjs";
export { buildLauncherResources, copyLauncherAssets } from "./launcherResources.mjs";
export { buildTextResources } from "./textResources.mjs";
export { buildTitleResources, deriveTitleAreaFromIntroName } from "./titleResources.mjs";
