/*
===========================================================================

page-globals.d.ts - names that exist only inside the browser page

Browser tests run code in the page through page.evaluate. That code reads
probes and fixtures the tests install on globalThis (a test's init script
or an earlier evaluate sets them), and imports modules by their Vite dev
server path (/src/...). None of that exists in the Node program the type
checker sees, so this file declares it. The probes are deliberately `any`:
they are ad hoc views of runtime state, not a contract.

===========================================================================
*/

declare module "/src/*";

declare var __arrowActors: any;
declare var __castActors: any;
declare var __castRenderer: any;
declare var __cerberusEnvironment: any;
declare var __championAggro: any;
declare var __commerceHelpHover: any;
declare var __commerceHelpScene: any;
declare var __companionRuntime: any;
declare var __deathActors: any;
declare var __deathRenderer: any;
declare var __deathSamples: any;
declare var __dropActors: any;
declare var __dropRenderer: any;
declare var __equipmentActors: any;
declare var __equipmentError: any;
declare var __equipmentGlowWrites: any;
declare var __followObservedGids: any;
declare var __followVisible: any;
declare var __fontAudit: any;
declare var __fontAuditTexture: any;
declare var __fortActors: any;
declare var __fortError: any;
declare var __gmItemProbe: any;
declare var __hitActors: any;
declare var __hitAudioStarts: any;
declare var __hitRenderer: any;
declare var __hitSounds: any;
declare var __iceCells: any;
declare var __iceDecal: any;
declare var __iceRenderer: any;
declare var __menus: any;
declare var __minimap: any;
declare var __music: any;
declare var __musicBlobs: any;
declare var __npcWindowProbe: any;
declare var __overlayOwner: any;
declare var __overlayScene: any;
declare var __overlaySemantics: any;
declare var __playableRuntime: any;
declare var __portal: any;
declare var __portalMatrix: any;
declare var __portalOrigin: any;
declare var __potionUi: any;
declare var __pursuit: any;
declare var __rebirthAnimation: any;
declare var __rebirthCommands: any;
declare var __rebirthPick: any;
declare var __refreshActors: any;
declare var __refreshError: any;
declare var __refreshFeet: any;
declare var __reportingHud: any;
declare var __returnSemantics: any;
declare var __returnView: any;
declare var __scrollActors: any;
declare var __scrollCharacters: any;
declare var __scrollDebug: any;
declare var __shadowEvidence: any;
declare var __shadowRenderer: any;
declare var __shopEntities: any;
declare var __shopReadyCount: any;
declare var __sightAggro: any;
declare var __skillActors: any;
declare var __skillRenderer: any;
declare var __snap: any;
declare var __snapActors: any;
declare var __snapRenderer: any;
declare var __speedScrollProbe: any;
declare var __speedView: any;
declare var __spread: any;
declare var __stairs: any;
declare var __toeGpuUploads: any;
declare var __tooltipHover: any;
declare var __tooltipOwner: any;
declare var __tooltipScene: any;
declare var __tooltipSemantics: any;
declare var __uiRetail: any;
declare var __uiRetailScene: any;
declare var __uiRetailSemantics: any;
declare var chromeFixture: any;
declare var fixture: any;
declare var flagFixture: any;
declare var groundInputProbe: any;
declare var inventoryFixture: any;
declare var matchingFixture: any;
declare var questProbe: any;
declare var quickbarFixture: any;
declare var quickslotFixture: any;
declare var serviceFixture: any;
declare var sessionProbeRuntime: any;
declare var special: any;
