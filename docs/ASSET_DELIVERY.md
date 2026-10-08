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
   publication ledger). The full build archives packed files no current
   builder produced; those are leftovers of older pipelines and must not
   ship.
5. One `pnpm assets build full` makes a complete tree, every focused family
   included, in about 6 minutes on 16 cores. Two fresh builds of the same commit are
   byte-identical, and an already built tree converges to the same bytes
   (see "Reuse is keyed by code").

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
| Focused families (code-selected images, rebuilt catalogs) | `pnpm assets refresh <family>` (`scripts/refresh_asset_family.mjs`) | `family-<family>` |
| Publishing families (skill UI, dungeon worlds, flares, ...) | `pnpm assets publish <family>` (`scripts/refresh_asset_family.mjs`) | `family-<family>` |
| World-map refresh (optional; files the resource build also writes) | `pnpm assets refresh world-map` | `world-map` |

A family's record is named after its task (`assets:refresh:<family>` or
`assets:publish:<family>`), not its pack folder, so the ledger can check the
task table for families that never ran.

`pnpm assets build full` runs the outdoor build, then the resource build,
which runs every family of `scripts/build/families/looseFamilies.mjs` after
its builders and before the image sweep and the pack tail. Each family still
records its own owner. `pnpm assets refresh <family>` and
`pnpm assets publish <family>` rerun one family into an existing tree while
you iterate on it; nothing needs them after a build.

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
(`scripts/build/shared/looseFamilyPublication.mjs`). That picks the group
of each file and republishes only those groups through
`scripts/build/shared/packGroupRefresh.mjs`, the one refresh sequence every
focused publisher shares: rebuild the touched groups from their loose files,
merge them into the index (refused across asset schemas), check every file
landed in its group, publish the index, soft-archive the packs those groups
no longer use, and refresh the web manifest and its sidecars. Unrelated pack
members are never touched. The native-font, title-crowd, outdoor and
world-map refreshes use the same sequence.

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
- Every file reaches the public tree through a writer that claims it:
  `copyIntoPublicTree`, `writeIntoPublicTree` and `writeIntoPublicTreeSync`
  (`shared/publicWrite.mjs`, for copies, models, VAT payloads, sounds),
  `publishFileFromTemp` / `publishBytesAtomically` (`shared/atomicPublish.mjs`),
  `writeJsonIfChanged` and its sync twin (`shared/jsonOut.mjs`),
  `publishConvertedImage` (`shared/convertedImages.mjs`), `writePublicFile`
  (`world/io.mjs`) and `publishBlockTextureFile`
  (`world/assets/blockTextures.mjs`). They claim a file whether they write it
  or find it already current. Never call `fs.writeFile` or `copyFile` on a
  public path directly.
- A builder that keeps an output without calling a writer (a fresh minimap
  tile, a fresh VAT, a reused outdoor region) claims it with
  `claimPublicFile` or `claimPublicPaths`. A step whose files another
  process writes claims them after that process ends (the cursors the
  Python extractor writes).
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

### The audit and what it moves

The full build's pack tail compares every file it is about to pack with the
records (`auditClaims`). Only the records of known owners count: both
builds, every family and publisher of the task table, and the optional
world-map refresh. Each file lands in one of three classes:

| Class | Meaning | What happens |
| --- | --- | --- |
| Claimed | a current step produced or kept it | packed |
| Local-only | a claimed manifest or catalog names it (the client will load it), but no current step produced it: it exists only because of an earlier run or a hand-placed file on this machine, and a fresh clone would not have it | the build **fails** and lists it |
| Unclaimed | nothing produced it and nothing names it: a leftover of an older pipeline | soft-archived and left out of the packs |

"Names it" means the file's `/assets/...` path appears in the text of a
claimed JSON output, followed transitively through the JSON files it names.

The two outcomes need every expected owner to have a record:

- **Every expected owner has a record.** Local-only files fail the build.
  Otherwise each unclaimed file is soft-archived with its whole compressed
  family (`x.json`, `x.json.gz`, `.br`, `.zst`, unless one of them is
  claimed), because a stale base left behind would have its `.gz`
  regenerated by the next build. Archiving moves files through
  `archiveGeneratedArtifact` to
  `<checkout>/temp/archives/generated-artifacts/<day>/unclaimed-public-asset/`
  and never deletes. Records of owners that no longer exist (a renamed or
  removed family) claim nothing and are archived too.
- **An expected owner has no record** (a tree whose outdoor world has not
  had a full build yet, or a build that stopped before its families). Nothing
  moves and nothing fails: the files of a family that never ran cannot be
  told from garbage. The summary names the missing owners.

The build summary prints the result, and `.generated/unclaimed-assets.json`
lists every unclaimed file with its group and size, totals per folder, the
missing owners and the retired ones:

```
Publication ledger: <n> file(s), <MiB> MiB, claimed by no build owner; soft-archived to temp/archives and left out of the packs. Report: .generated/unclaimed-assets.json
  /assets/images/Map_extracted/tile2d: <n> file(s), <MiB> MiB
```

The release packager (`apps/client-next/tools/beta/build.mjs`) is the hard
gate: it refuses to package unless every expected owner has a record and
every asset in the pack index is claimed, with no local-only file
(`verifyIndexClaims`).

`pnpm assets ledger` (`scripts/report_publication_ledger.mjs`) prints the
same check read-only for the installed pack index (`-- --json` for the full
list) and exits 1 unless it is empty. Run it before packaging, and after a
pull to see what the next build will archive.

The first enforcing build on the shared tree (2026-10-08) archived 1,572
files, 260.7 MiB: 1,430 PNGs and 47 DDS lightmaps whose `.texture`
replacements are claimed (all 426 tile2d tiles; the title-scene, dungeon and
character-select object textures; the title lightmaps), and 95 outdoor mesh
files the object index no longer names (their bases and sidecars followed
in the next build, once archiving took the whole compressed family).

Reading the report: a local-only file is needed but produced by no current
step; find which builder should write it and route the write through
`shared/publicWrite.mjs` (or claim the kept output). An unclaimed folder of
files whose format moved (PNG next to `.texture`) is real garbage, and
archiving it is the point. To restore a wrongly archived file, copy it back
from the archive folder; the path under the reason folder mirrors
`client-public/`.

## Build parallelism

One setting sizes every parallel stage of the asset build:
`SRO_BUILD_JOBS`, a positive integer, defaulting to every core but one
(`scripts/build/shared/buildParallelism.mjs`). It sets:

| Stage | Before | Now |
| --- | --- | --- |
| Packs built at once (each holds its buffer, about 100 MiB) | 3 | `SRO_BUILD_JOBS` |
| libuv thread pool (runs the gzip compression the packer awaits) | 4 | `SRO_BUILD_JOBS` (at least 4) |
| JSON sidecar compression workers | `min(cores - 2, 4)` | `SRO_BUILD_JOBS` (at most the core count) |
| Outdoor region builders (`--jobs=N` still overrides) | 2 | `SRO_BUILD_JOBS` |
| Resource-build lanes running side by side | 2 | `SRO_BUILD_JOBS` |
| Image conversion (`convert_images.py`) | 1 process | a pool of `SRO_BUILD_JOBS` processes |

Outputs never depend on the setting: every stage writes the same bytes
whatever its concurrency (`test_image_conversion_jobs.py` checks the image
pool), so it is not part of the build fingerprint. Lower it when the machine
must stay responsive, for example `SRO_BUILD_JOBS=4 pnpm assets build`.
Entry points import `buildParallelism.mjs` first, because libuv reads
`UV_THREADPOOL_SIZE` once, when the pool first starts.

Measured on 16 cores (`SRO_BUILD_JOBS` 15), 2026-10-08: a fresh full build
of an empty generated tree takes 352 s in one command; a rebuild with
nothing changed takes 3 s (the fingerprint). Image conversion went from
202 s to 42 s and uncached packs from 507 s to 163 s when the stages went
parallel.

### Compression the build does and does not do

The build writes identity packs, gzip transports for large members and a
`.gz` sidecar for each published JSON manifest
(`PUBLISHED_SIDECAR_SUFFIXES` in `shared/compressionUtils.mjs`). It writes
no Brotli or zstd sidecars: no host serves them. `pnpm assets compact` alone
makes the zstd copies of the packs it keeps (`ensurePackZstdCopies`),
because only the compact release profile drops the identity copies.

### Reuse is keyed by code

The build reuses output already on disk wherever it can. Reuse is safe only
when the code that wrote the output is the code running now, so every reuse
cache is keyed by code, never by a file merely existing:

| Cache | Key |
| --- | --- |
| The whole build | the resource-build fingerprint: inputs, outputs, `scripts/build/`, every file the entries can run outside it (the converter, `sro_paths.py`, `scripts/lib/`), and the `SRO_ASSET_PACK_BASELINE` file |
| Outdoor region bundles and shared indexes | the `outdoor-world` code stamp |
| Converted PNGs (`convert_images.py`) | the `image-conversion` code stamp, then the PNG's mtime against its source |
| Block textures (`.texture`) | the encoder's sha256 plus the source's |
| Crowd VAT bakes | `CROWD_VAT_COMPILER_VERSION` |

A code stamp (`scripts/build/shared/codeStamp.mjs`) is the sha256 of a
builder's source closure: the entry module, every module it imports through
relative paths, the Python helpers they name, and the modules beside those
helpers that they import. It lives in `.generated/build-stamps/`. When it
does not match, the cache is rebuilt in full, and only a complete run writes
the new stamp. Before stamps, a builder fix never reached a tree that was
already built: the main tree kept 2,126 outdoor regions an older builder had
written, while two fresh clones agreed with each other.

Packed outputs carry no build time. A packed manifest stamped with
`generatedAt` changes bytes on every run, which changes its pack's hash and
URL and makes players download it again for nothing; add a version field
when a reader needs to tell formats apart. The top-level indexes
(`packs/manifest.json`, the web manifest) are not packed and keep their
previous `generatedAt` unless their content changed.

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

The verified-file store (`packs/persistent.ts`) may hold half the origin's
quota, at least 512 MiB and at most 4 GiB (`budgetFromQuota`). Browsers give
an origin far more than the game needs:

| Browser | Per-origin limit |
| --- | --- |
| Chrome, Edge | 60% of the disk |
| Firefox, best effort | the smaller of 10% of the disk and 10 GiB |
| Firefox, persistent | 50% of the disk |
| Safari (browser app) | about 60% of the disk |

So on most machines the whole game stays local and a player who explores
never downloads an area twice. Beyond the budget, files leave least
recently used first, except the files of the startup groups (`native-ui`,
`game-images`, `game-data`, whose packs the manifest marks `load: startup`):
those are pinned with a response header and never evicted, so the next
start never waits for them. The budget used and the pinned count are in the
store's `stats()`.

When the device runs low on disk, browsers evict whole origins, least
recently used first, and never evict an origin marked persistent
([MDN: storage quotas and eviction](https://developer.mozilla.org/docs/Web/API/Storage_API/Storage_quotas_and_eviction_criteria)).
At world entry the client calls `navigator.storage.persist()`
(`foundation/assets/persistent-storage.ts`): Chrome, Edge and Safari decide
silently from engagement. Firefox would answer with a permission prompt in
the middle of play, so it is not asked; its best-effort quota already holds
the game. The Chrome storage team recommends the Cache API for large binary
resources ([Chrome: cache models](https://developer.chrome.com/docs/ai/cache-models)),
which is what the store uses.

## Releasing data

A release that changes any public file is a **data release**: only the
machine with the licensed build can produce it. The steps, from a worktree
at the commit being released:

1. Fetch the live manifest:
   `curl -o live-manifest.json https://<origin>/assets/packs/manifest.json`.
2. Build against it:
   `SRO_ASSET_PACK_BASELINE=live-manifest.json pnpm assets build`.
3. Run `pnpm assets ledger`. It must report no unclaimed asset and no
   missing owner; the packager refuses otherwise. If the build summary says
   it archived files, check they are leftovers (see the audit section).
   `pnpm assets compact` runs after this, never before: it drops loose files
   on purpose.
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
- A family or publisher changed: the same `pnpm assets build` reruns it.
- A new `ASSET_SCHEMA`: run `pnpm assets build full`.
- Then run `pnpm assets ledger`. The full build has already archived what
  no current builder produces; files it still lists mean the build did not
  complete, so run `pnpm assets build full`.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| "SRO resources up to date" but you expected a rebuild | Only pipeline code, inputs and outputs are fingerprinted. Set `SRO_FORCE_RESOURCE_BUILD=1`. |
| `Asset absent from published manifest` in the client | The file was never packed: run the family or publisher that owns it (`pnpm task list --kind assets`). |
| `Pack header disagrees with manifest` | A pack was rewritten outside the pack owner. Rebuild with `pnpm assets build`. |
| An upload far larger than the change | The build was not planned against the live manifest (step 2 above), or a builder rewrote many files with new bytes. Compare the per-group totals of the two manifests before staging. |
| The build fails with "a fresh clone would not have them" | A manifest names files no current step wrote. Route that builder's write through `shared/publicWrite.mjs`, or claim the output its skip path keeps. |
| A live folder was archived | Its builder writes or skips without claiming and nothing names the files. Fix the builder as above and restore the files from `temp/archives/generated-artifacts/<day>/unclaimed-public-asset/`. |
| The packager says "files the current pipeline did not produce" | Run `pnpm assets build full`, then `pnpm assets ledger`. |
| Another build "holds the generated-assets lock" | `pnpm assets lock` shows the holder. Builds refuse rather than corrupt the shared tree. |

## Planned work

Measured on the 2026-10-08 build. Each item ships with tests and a
data release.

| Change | Measured effect |
| --- | --- |
| Members stored zstd-compressed inside packs (per member; 1 MiB blocks measured no better), retiring the gzip transports | region JSON 18.1% → 9.6% of raw; models about 50% → 23-38% |
| Original DXT blocks (`.texture`) instead of PNG for minimap, outdoor object textures and tile2d | minimap about 269 → 70 MiB; outdoor textures about 213 → 85 MiB; 4-8× less GPU memory |
| Size gate (`check_compact_assets.mjs`) measures the bytes actually served | today it measures offline zstd copies nobody downloads |

Target: a full download of about 1.1 GiB instead of 2.04 GiB, with no
visual change.
