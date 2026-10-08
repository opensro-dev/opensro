# Asset delivery

How game data travels from the licensed client on a developer's disk to a
player's browser, what each byte costs on the way, how the build keeps old
output from shipping, and how a data release is staged.
[ASSET_PIPELINE.md](ASSET_PIPELINE.md) covers the build commands;
[RELEASE.md](../RELEASE.md) is the release checklist. This document explains
why things are the way they are, so read it before changing a builder, a
pack format, the asset worker or the release tools.

## The short version

1. `pnpm assets build` turns `extracted/` into the public tree
   `.generated/client-public/assets/` and packs it into about 190 binary
   packs (`assets/packs/*.bin`) listed in `assets/packs/manifest.json`.
2. The browser's asset worker reads every file through that manifest:
   a whole small pack, a byte range of a large pack, or a gzip "transport"
   copy of a large member. Every file is verified against its SHA-256
   before anything uses it, then kept in the browser's Cache Storage.
3. A data release uploads only the bytes the live site does not already
   have, and pack layout is planned against the live manifest so unchanged
   packs keep their URLs and players' caches stay valid.
4. Every build process records which public files it produced (the
   publication ledger). The full build reports packed files no current
   builder produced; those are leftovers of older pipelines and must not
   ship.

## Where the bytes come from

| Stage | Location | Owner |
| --- | --- | --- |
| Licensed client | `<game root>/*.pk2`, `SRO_Client.exe` | the player's own install, never committed |
| Extraction | `<game root>/extracted/` | `pnpm assets prepare` (`scripts/prepare_client_resources.py`) |
| Converted sources | `.generated/intermediate/` | image conversion, native texture mips |
| Public tree | `.generated/client-public/assets/` | the build steps (`scripts/build/`) |
| Packs | `.generated/client-public/assets/packs/` | the pack tail (`scripts/build/packPublicTree.mjs`) |
| Publication ledger | `.generated/publication-ledger/` | `scripts/build/shared/publicationLedger.mjs` |
| Release package | `apps/client-next/temp/artifacts/beta/<stamp>/package/` | `apps/client-next/tools/beta/build.mjs` |

`.generated/` is resolved by one owner (`scripts/lib/generatedRoot.mjs`).
Worktrees point `SRO_GENERATED_ROOT` at the main checkout's `.generated`
(see [AGENTS.md](../AGENTS.md#worktrees)), so the public tree, the packs and
the ledger are shared by every worktree on a machine.

## Sizes, measured (2026-10-08 build)

The original install is 2.36 GB of PK2 archives: Data 1.3 GB, Map 594 MB,
Media 276 MB, Particles 65 MB, Music 34 MB. "The game is 1.3 GB" usually
means Data.pk2 alone.

| Group | Load | Raw in packs | On the wire today |
| --- | --- | --- | --- |
| outdoor-world | manual (streamed by region) | 893 MiB | 648 MiB |
| game-models | lazy | 520 MiB | 273 MiB |
| game-images | startup | 337 MiB | 314 MiB |
| equipment-models | lazy | 271 MiB | 159 MiB |
| mission-minimap | lazy | 269 MiB | 269 MiB |
| mission-npc-vat | lazy | 226 MiB | 114 MiB |
| game-audio | manual | 130 MiB | 123 MiB |
| mission-cos-models | lazy | 86 MiB | 46 MiB |
| game-data | startup | 61 MiB | 59 MiB |
| native-ui | startup | 25 MiB | 25 MiB |
| title-crowd-vat | lazy | 18 MiB | 9 MiB |
| **Total** | | **2.84 GiB** | **2.04 GiB** |

"On the wire" counts a member's gzip transport when it has one, else its
raw bytes. A player never downloads all of it at once: the client requests
files on demand, so the first world entry costs a few hundred MiB and the
world streams as the player moves. "Load" is the group's intent label in
the manifest; the client does not preload a group because of it.

Why the raw total exceeds the original:

- **Images that were DXT in the original are shipped as PNG.** A PNG of a
  decoded DXT1 texture is 1.7 to 2.6 times the original block data
  (minimap tiles: 268 MiB of PNG from 104 MiB of DXT1; outdoor object
  textures: 213 MiB from 125 MiB). Images whose original was uncompressed
  RGBA (icons, most UI) are smaller as PNG.
- **Outdoor region records are JSON**, gzip-compressed inside the packs
  (about 18% of the JSON). The original binary map files are smaller.
- **Leftovers of older builds were still packed** (for example tile2d
  PNGs next to the `.texture` files that replaced them). The publication
  ledger exists to stop this; see below.
- **The host serves packs uncompressed.** A ranged read cannot use HTTP
  `Content-Encoding` (the range would apply to the compressed bytes), so
  compression has to be part of our own delivery format.

[Planned work](#planned-work) lists the measured fixes.

## The public tree and its owners

Every file under `client-public/assets/` is produced by exactly one owner:

| Owner | Entry point | Ledger record |
| --- | --- | --- |
| Outdoor world (every region) | `pnpm assets build world-outdoor` (`scripts/build_outdoor_world_resources.mjs`) | `outdoor-world` |
| Resource build (everything else the full build makes) | `pnpm assets build` (`scripts/build_sro_resources.mjs`) | `resource-build` |
| Focused families (code-selected images, rebuilt catalogs) | `pnpm assets refresh <family>` (`scripts/refresh_asset_family.mjs`) | `family-<pack folder>` |
| Standalone publishers (skill UI, dungeon worlds, flares, ...) | `pnpm assets publish [<family>]` (`apps/client-next/tools/publish-*.mjs`) | `family-<name>` |

`pnpm assets build full` runs the outdoor build, then the resource build.
`pnpm assets publish` with no family runs every publisher and every focused
family; a fresh tree needs it after the full build, because the full build
does not produce what those families publish.

### Focused families

A focused family is a handful of files the full build does not reach:
images the executable selects by literal path rather than through a
resinfo layout, and catalogs one build step rewrites. Each family is one
row of `scripts/build/families/looseFamilies.mjs`:

```js
"quickslot": {
	label: "native quickslot textures",   // log text
	packFolder: "quickslots",             // assets/packs/incremental/<packFolder>/
	defaultGroup: "native-ui",            // group for a file no group owns yet
	async produce( flags ) {              // writes the files, returns their public paths
		return { files: await publishImages( [ ... ] ) };
	}
}
```

`scripts/refresh_asset_family.mjs <family>` takes the generated-assets lock,
runs `produce`, and hands the files to `publishLooseFamily`
(`scripts/build/shared/looseFamilyPublication.mjs`), which patches only the
pack groups that hold them, merges the manifest and refreshes the web
manifest. Unrelated pack members are never touched.

To add a family: add a row, add its name to `REFRESH_FAMILIES` in
`scripts/tasks/assets.mjs` (`assetFamilyTasks.test.mjs` keeps the two equal),
and keep the native provenance comment that explains why the build does not
reach those files. Never add a new `refresh_*` script for a family.

Shared helpers in the family module: `publishImages` (converted images for a
list of DDJ references), `buttonStates` (a CIFButton's state DDJs),
`registerSprites` (keeps code-selected art in the CIF sprite catalog and
refreshes its sidecars) and `packedJson` (refreshes stale JSON sidecars and
returns the `.json.gz` members).

## The publication ledger

### The problem it solves

The build is incremental: it reuses whatever output is already on disk, and
the pack tail collects groups by sweeping the public tree by extension
(`scripts/build/assetPackGroups.mjs`). So when a builder changes what it
writes, the file the old code wrote stays on disk and keeps being packed and
uploaded. Pulling new code does trigger a rebuild (the resource-build
fingerprint covers `scripts/build/`), but a rebuild only adds; it never
learns that an old file is no longer anyone's output.

Real example: terrain ground tiles moved from PNG to native `.texture`
containers. The tiles' PNGs stayed in `assets/images/Map_extracted/tile2d/`
and shipped in a data release next to their replacements.

### How it works

- An owner opens a publication when it starts real work
  (`beginPublication( owner )`) and commits it when it finishes
  (`commitPublication()`). A failed run throws before the commit, so a good
  record is never replaced by a partial one.
- The shared writers claim each file they write **or find already
  current**: `publishFileFromTemp` / `publishBytesAtomically`
  (`shared/atomicPublish.mjs`), `writeJsonIfChanged` and its sync twin
  (`shared/jsonOut.mjs`), `publishConvertedImage`
  (`shared/convertedImages.mjs`), `writePublicFile` (`world/io.mjs`) and
  `publishBlockTextureFile` (`world/assets/blockTextures.mjs`). A builder
  that writes through these needs no ledger code.
- A builder that skips work because its output is current, without calling
  a shared writer, must claim that output itself with `claimPublicFile` or
  `claimPublicPaths`. Forgetting this is the one way to get a live file
  reported as unclaimed.
- A complete run replaces its owner's record. A partial run (one outdoor
  region with `--region=`) merges into it.
- `publishLooseFamily` records its family's files itself, so every focused
  family and standalone publisher is covered without code in each.
- Precompressed sidecars (`.gz`, `.br`, `.zst`) belong to whoever claimed
  their base, and a base belongs to whoever claimed its packed `.json.gz`:
  the JSON compressor derives one from the other.
- Files under `assets/packs/` and `assets/manifest.json` belong to the pack
  tail and are never claimed.

Records are plain JSON, one per owner, in `.generated/publication-ledger/`.
They describe the shared tree, which is why they live beside it rather than
in a worktree's `.state/`.

### The audit

The full build's pack tail compares every file it is about to pack with the
union of all records and writes `.generated/unclaimed-assets.json`: each
unclaimed file with its group and size, totals per folder, and the owners
it read. The build summary prints the totals:

```
Publication ledger: <files> packed file(s), <MiB> MiB, claimed by no build owner (report only; ...)
  /assets/images/Map_extracted/tile2d: <files> file(s), <MiB> MiB
  ...
```

**Today the audit only reports.** Nothing is moved until the report lists
nothing but genuine leftovers. The next step turns an unclaimed file into a
soft-archived one (moved through `archiveGeneratedArtifact` to
`temp/archives/generated-artifacts/`, never deleted) and makes the release
packager refuse a tree whose report is not empty.

Reading a report: a folder full of files you know are live means its
builder has a skip path that does not claim; fix the builder. A folder of
files whose format moved (PNG next to `.texture`, `.json` next to a newer
name) is real garbage.

## Packs

### Layout

A pack is `SROPACK1` (8 bytes), a little-endian u32 header length, a JSON
header listing its members (`path`, `offset`, `length`, `mime`, `sha256`),
then the members' bytes back to back. Its file name is
`<group>-<slot>-<first 12 hex of its sha256>.bin`, so a pack's URL changes
exactly when its bytes do. `assets/packs/manifest.json` lists every group,
pack and member; the client admits it only if counts, ranges and digests
agree (`apps/client-next/src/engine/runtime/assets/worker/packs/index/index.ts`).

### Stable layout across builds

Cutting each group's sorted files into fixed-size chunks would move every
later file into a different pack whenever one file is inserted, and every
player would download those packs again. `scripts/build/assetPackLayout.mjs`
plans the layout against a **baseline index**: a baseline pack whose members
are all present and unchanged keeps its members, order and slot, so it
rebuilds to the same bytes and URL. New and changed files, and the
survivors of a pack that lost a member, go into fresh slots. A group whose
pack count drifts more than `MAX_PACK_SLACK` (3) past its ideal is repacked
whole.

The baseline is the previous local build, or the live site's manifest when
`SRO_ASSET_PACK_BASELINE` points at it. **Always build a release against the
live manifest**; otherwise the layout follows your local history instead of
what players have cached.

### Gzip transports

For a member of at least 64 KiB, or any GLB, where gzip saves at least 10%,
`scripts/build/assetDelivery.mjs` writes a gzip copy at
`/assets/packs/transport/<sha256>.gz` and records it on the member. The
client fetches that copy instead of the member's range, so large models and
records travel compressed. On the host this is a second copy of the bytes;
on the wire a player gets one or the other, never both.

## How the browser reads a file

All reads happen in the asset worker
(`apps/client-next/src/engine/runtime/assets/worker/`). `readVerified` in
`packs/packs.ts` resolves the path through the manifest (a request for
`x.json` also finds `x.json.gz`) and tries, in order:

1. **Cache Storage** (`packs/persistent.ts`, cache `sro-next-verified-v1`),
   keyed by the member's digest. A hit is re-hashed; a mismatch is removed.
2. **A resident pack** already in memory (up to 128 MiB of whole packs).
3. **The member's gzip transport**, decoded with `DecompressionStream`.
4. **The pack**:
   - a pack of at most 4 MiB is fetched whole, verified and kept;
   - a larger pack is read by HTTP Range. `packs/blocks.ts` groups adjacent
     members into reads of at most 1 MiB, never splitting a member, with at
     most four reads in flight and a 64 MiB block cache. The header is read
     first (also by range) and must agree with the manifest.
5. **A loose file**, for development trees that are not packed.

Every result is checked against the manifest's length and SHA-256 before
any caller sees it, then queued for Cache Storage. Ranged responses are
fetched with `cache: "no-store"`, because different ranges share one URL;
Cache Storage, keyed by content digest, is the durable cache.

### Cache Storage budget

The verified-file cache is limited to `min(512 MiB, quota / 4)` and evicts
least recently used files beyond that. That is far less than browsers allow
an origin:

| Browser | Per-origin limit |
| --- | --- |
| Chrome, Edge | 60% of the disk |
| Firefox, best effort | the smaller of 10% of the disk and 10 GiB |
| Firefox, persistent | 50% of the disk |
| Safari (browser app) | about 60% of the disk |

When the device runs low, browsers evict whole origins, least recently used
first, and never evict an origin marked persistent
([MDN: storage quotas and eviction](https://developer.mozilla.org/docs/Web/API/Storage_API/Storage_quotas_and_eviction_criteria)).
`navigator.storage.persist()` is granted silently by Chrome and Safari based
on engagement, and shows a prompt in Firefox. The Chrome storage team
recommends the Cache API for large binary resources
([Chrome: cache models](https://developer.chrome.com/docs/ai/cache-models)).
Raising the budget and asking for persistence is [planned](#planned-work).

## Releasing data

A release that changes any public file is a **data release**: only the
machine with the licensed build can produce it. The steps, from a worktree
at the commit being released:

1. Fetch the live manifest:
   `curl -o live-manifest.json https://<origin>/assets/packs/manifest.json`.
2. Build against it:
   `SRO_ASSET_PACK_BASELINE=live-manifest.json pnpm assets build`
   (and `pnpm assets publish` if a family or publisher changed).
3. Read the publication ledger line in the build summary. Investigate any
   unclaimed files before going further.
4. Package: `node apps/client-next/tools/beta/build.mjs` (from
   `apps/client-next`). It writes
   `temp/artifacts/beta/<stamp>/package/` and verifies every pack and
   transport against the manifest.
5. Stage: `python apps/server/ops/release/data_release.py <package> <output>
   --origin https://<origin> --ssh-target <stage user>@<host> --identity
   <stage key>`. It reads the live release, uploads in batches only the
   content the live site lacks (keyed by sha256), and prints the
   `candidate` and `commit` for approval. Interrupted uploads resume.
6. Approve the candidate in the operator's release workflow.
   [apps/server/ops/release/README.md](../apps/server/ops/release/README.md)
   describes what the host checks and how rollback works.

Bumping `ASSET_SCHEMA` (`scripts/build/assetSchema.mjs`) makes old clients
refuse the new data; ship the rebuilt data and the matching client
together.

## After you pull

- Builder code changed: run `pnpm assets build`. The fingerprint notices
  changed pipeline code and rebuilds; with no change it prints "up to date"
  in seconds.
- A family or publisher changed: run `pnpm assets publish <family>`, or all
  of them with no family.
- A new `ASSET_SCHEMA`: run `pnpm assets build full` followed by
  `pnpm assets publish`.
- Then read the ledger line. Unclaimed files after a pull usually mean a
  builder changed its output format; they are not yours to keep.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| "SRO resources up to date" but you expected a rebuild | Only pipeline code, inputs and outputs are fingerprinted. Set `SRO_FORCE_RESOURCE_BUILD=1`. |
| `Asset absent from published manifest` in the client | The file was never packed: run the family or publisher that owns it (`pnpm task list --kind assets`). |
| `Pack header disagrees with manifest` | A pack was rewritten outside the pack owner. Rebuild with `pnpm assets build`. |
| An upload far larger than the change | The build was not planned against the live manifest (step 2 above), or a builder rewrote many files with new bytes. Compare the per-group totals of the two manifests before staging. |
| The ledger reports a folder you know is live | Its builder skips work without claiming. Add `claimPublicFile` to the skip path. |
| Another build "holds the generated-assets lock" | `pnpm assets lock` shows the holder. Builds refuse rather than corrupt the shared tree. |

## Planned work

Measured on the 2026-10-08 build. Each item ships with tests and a
data release.

| Change | Measured effect |
| --- | --- |
| Members stored zstd-compressed inside packs (per member; 1 MiB blocks measured no better), retiring the gzip transports | region JSON 18.1% → 9.6% of raw; models about 50% → 23-38% |
| Original DXT blocks (`.texture`) instead of PNG for minimap, outdoor object textures and tile2d | minimap about 269 → 70 MiB; outdoor textures about 213 → 85 MiB; 4-8× less GPU memory |
| Unclaimed files soft-archived; packaging refuses a tree with unclaimed files | removes leftovers such as the tile2d PNG twins |
| Size gate (`check_compact_assets.mjs`) measures the bytes actually served | today it measures offline zstd copies nobody downloads |
| Cache budget `clamp(quota × 0.5, 512 MiB, 4 GiB)`, startup groups never evicted, eviction by region group, `persist()` after world entry (Firefox: from a setting) | explorers stop re-downloading visited areas |

Target: a full download of about 1.1 GiB instead of 2.04 GiB, with no
visual change.
