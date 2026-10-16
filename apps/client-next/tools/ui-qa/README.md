# Local UI review workbench

**Work in progress: a prototype for human-driven UI QA.**

This developer-only page mounts the production UI, renderer, assets, browser input
bridge and report dialog in an isolated iframe. It has no session bootstrap,
game/API proxy, upload destination or release entry. It is port-only tooling;
it makes no new claim about native behavior.

From the repository root, with installed workspace dependencies:

```powershell
$env:SRO_GAME_ROOT = '<path-to-your-licensed-v1.150-client>'
node node_modules/vite/bin/vite.js --config apps/client-next/tools/ui-qa/vite.config.mjs
```

Open the loopback URL printed by Vite in an isolated Chrome profile. `settings.mjs`
owns `UI_QA_PORT` and `UI_QA_ORIGIN`, shared by the server and probes; the port is
separate from the operator console's default. Print the current URL with:

```powershell
node --input-type=module -e 'import { UI_QA_ORIGIN } from "./apps/client-next/tools/ui-qa/settings.mjs"; console.log(UI_QA_ORIGIN)'
```

The server binds only to loopback and refuses a busy port. The existing generated-root and native-root
resolvers own asset locations; no assets are copied or regenerated.

## Review workflow

1. Choose a journey. Read its seed and coverage limits before using the real game
   controls. Checkpoint buttons either send production UI events or make an
   explicitly named synthetic snapshot change.
2. Select viewport and backdrop. Either change resets the iframe; it does not
   scale the game. Reset destroys private owners/listeners and pending responses.
3. For Stall naming, use real input, then apply queued requests. Requests reduce
   through `stallRequest` in one publication; bounded responses use `stallFrame`.
   The cancel/reentry case deliberately coalesces the intermediate state.
4. Review visual, interaction and integration axes separately. Add notes with
   any native capture's version and provenance. Save in this browser, then export
   the local JSON ledger. It includes unreviewed and unsupported inventory too.

The catalog has 180 scenarios: 12 focused Guild/Stall/HUD journeys, all 32 production
panel entries and 136 additional recipes (including three service windows).
The specialized panel admission recipes and three service recipes
are shared with the existing runtime sweep in `tests/helpers/ui-entry-recipes.mjs`.
They publish explicit service/item prerequisites through real production owners.
Opening an entry does not establish complete state coverage.

Coverage categories keep their recipe list visible while inspecting a recipe.
Previous / Next preserve drafts without recording a review; Save & Next advances
only after the explicit save succeeds. The last recipe offers Save & finish list
and stays open with a completion message; it never marks the remaining category
reviewed. List filtering and scroll position survive recipe changes. Progress counts
saved judgments for the current viewport/backdrop, separately from drafts.
Blocked fixtures stay in the sequence and can receive an explicit unsupported or
failed observation with their actual error/trace evidence; saving does not admit them.
Registry and service evidence requires every target-specific control listed in
`tests/helpers/ui-entry-evidence.mjs`; companion Inventory and NPC menus do not
count as the requested window. Other contexts use named owner controls, DOM nodes
or rendered textures. These are presentation mounts. Direct NPC activation does
not establish that the visible menu exposes an enabled row. The
registry is not a complete inventory of browser UI. Additional layouts are mapped
in `prototype-scope.ts` and the Coverage tab. The older lightweight race recipe is
retired from navigation. Old direct fixture links resolve to `frontend:stage-race`,
which mounts the actual stage, figures and picking owner.
Generic HUD controls never count as admission for an unspecified target.

The inventory reconciles native/published resources and literal source references.
It preserves sections and repeated control occurrences. A reference is a lead,
not evidence that an entry point, dynamic child or handler works. Only explicit
resource-to-journey mappings claim partial scenario coverage.

Review records carry a source digest (including dirty fixture code), commit, resource,
define and font-metadata hashes, viewport, background, browser identity and trace.
Changed identity marks old records as needing recheck. The conservative digest
invalidates broadly, not by a speculative dependency graph. A save rechecks current
identity; reload after source changes. Font metadata does not hash every texture.

## What is intentionally bounded

- Guild snapshots keep party identity zero and guild-member/entity IDs distinct.
  Actual UI admission, text, selection, scrolling and confirmation are available.
  Guild server responses and candidate-election data are unsupported.
- Stall naming/creation/close and refusal response recipes use production reducers.
  Edit acknowledgements, successful purchase inventory settlement, worker
  publication transport and multi-client trading are unsupported. Refusal notices
  are recorded; the full inventory-owner notification pipeline is not mounted.
- HUD includes real tools and modal owners. Its FPS subtree is cloned from the
  production HTML without running startup scripts; diagnostics are enabled, so the developer `</>` control is
  also present. Telemetry and recording display are synthetic; capture/delivery
  and complete live world scenes are unsupported. A separate real actor/camera
  scenario projects name, guild and speech boards against an empty world.
- Clock advancement is explicit. Asynchronous asset loading uses the real worker
  and browser scheduling, so this is not a deterministic loader-latency simulator.
- Frontend title controls, the character-creation draft owner and character
  delete/restore owner are real. Controls operate on their production state, and
  outgoing name checks/deletions stay visibly pending or unsupported. Stage/camera
  navigation and 3D portraits remain unmounted in those lightweight contexts.
  The `frontend:stage-*` recipes mount the actual title/dock/creation flow, stage
  loader, camera, renderer, character assembly and production readiness. Empty,
  populated, selected, delete/restore, both races and creation confirmation are
  available. Picking uses the renderer. Inventory, Skin and Mall also have real
  actor previews. Advance or play the explicit clock to animate transitions;
  authentication and character-operation settlement remain unsupported.
- DOM loading/update/report surfaces clone authored production HTML or use the
  report owner. Transfer metrics and archive rows are synthetic; archive export,
  deletion and report delivery are unsupported. The real Refresh control reloads
  only the fixture document when a reviewer explicitly uses it.
- This milestone exports notes/trace; screenshots are captured by the browser
  probe or the reviewer. It does not import ledgers, file issues or certify native
  parity. Notes and axis selections persist as unreviewed local drafts across
  scenario changes; only an explicit save creates a review record.

## Ownership and verification

The Coverage tab groups all registered recipes by owner family, identifies source
paths and lists outstanding transitions. Its counts measure registered recipes,
not completed state coverage. The exported ledger includes those mappings and
human review status separately. Fingerprinted `extended/filtered.json` probe
results record actual mount evidence, errors and control identities.

The finite checklist separates A (unique implemented inaccessible layouts), B
(essential local operation) and C (optional exhaustive permutations/integration).
Current main fixes the former specialty transport duplicate-close blocker.
The actual `breadth:specialty-buy` recipe mounts the production quantity dialog,
Shop and COS inventory with distinct close identities; closing the transport bag
ends the local trade presentation. A has no identified outstanding unique layout.
B has no identified outstanding harness operation. C does not
block a usable human-review prototype. These inventory conclusions remain subject
to new source evidence; a native resource entry does not prove an implemented UI.
Review records never become approved merely because an admission probe passed.

The `breadth:` recipes add populated restoration, merchant transactions, Academy
permissions, party registration, union exit, job services, fortress employment,
COS tabs/target confirmations and premium uses. They check observed enabled
controls before activating them. Sale quotes remain unacknowledged; no commerce
settlement is faked. Premium uses and specialty goods use synthetic references;
the package icon is explicitly a placeholder. Restoration ranks/costs come from
published data through the production decoder. Specialty transport trading now
mounts with the upstream Shop/COS close fixes. The recipe directly seeds a
merchant and transport publication, then activates the actual shop offer. It does
not verify the preceding visible NPC menu/request/reply flow or live settlement.
Duplicate-control assertions stay enabled.

The `reach:` recipes add full Party leader/member permissions, incoming approval
and timeout, Guild war agreement/result, soldier rows, warehouse and compensation,
quest objectives/reward/abandon, tax questions, structure removal/denial, job outcome
and NPC portal/recall. Publications and unsupported replies remain explicit. The
Coverage tab contains the residual transitions; mounted states are never marked
as human reviewed automatically. `probe-reachability.mjs` exercises real input for
permissions, commands, cancellation, timeout and 3D picking.

`frame.ts` owns one fixture document, animation callback, message listener and
disposal sequence. `workbench.ts` owns iframe replacement, local review storage and
export. `catalog.mjs` is a read-only server inventory. These are developer entry
points under `tools`, not additions to the production `src` ownership graph. The
normal game build has no import edge to this directory. CSP and absent proxies
prevent the fixture from connecting to a game/report service.

```powershell
pnpm --filter @sro/client-next verify:quick
pnpm --filter @sro/client-next verify:test-types
node --test apps/client-next/tests/architecture/ui-qa-probe-support.test.mjs
node apps/client-next/tools/ui-qa/probe.mjs
node apps/client-next/tools/ui-qa/probe.mjs --interactions
node apps/client-next/tools/ui-qa/probe-boundaries.mjs
node apps/client-next/tools/ui-qa/probe-registry.mjs
node apps/client-next/tools/ui-qa/probe-registry.mjs --extended
node apps/client-next/tools/ui-qa/probe-specialty.mjs
node --test apps/client-next/tests/runtime/ui-panel-sweep.test.mjs
```

The Chrome probe targets the independently running loopback preview, uses the
repository's isolated system-Chrome launcher, never rewrites served source, and
closes its owned browser. Evidence goes to `temp/artifacts/ui-qa` under the
repository root, even when a probe starts from another working directory. The
workbench TypeScript is included in the existing client `typecheck`; the shared
JavaScript helpers and their unit coverage run in the existing test/type gates.
Browser probes remain manual because they require prepared assets and Chrome.
They install the shared browser-context request guard before navigation. This
blocks external HTTP requests from the page, iframe and workers. Empty external
lists mean no external request attempts were observed in that scenario; they do
not alone prove request prevention. `probe-specialty.mjs` additionally attempts
a different loopback port and checks `ERR_BLOCKED_BY_CLIENT`, recording that
deliberate challenge separately from the scenario's zero external requests.

The main probe exercises
admission, real Guild transfer input, Stall coalesced naming, reset, review
persistence/export and an unsupported inventory entry. Browser captures establish
browser behavior only; native screenshots and server semantics remain separate.
