# Asset pipeline

Turns a licensed, extracted Silkroad Online v1.150 client into the data the
browser client and server load. Nothing it produces is committed.

## Inputs and outputs

Inputs, beside the checkout: `../SRO_Client.exe` and `../extracted/`
(`Data_extracted`, `Map_extracted`, `Media_extracted`, `Particles_extracted`,
and so on). Python 3.12 is used for image and font conversion.

Lens mip resources are generated automatically before image conversion, and
by standalone world builds before copying sky assets. This uses Windows'
built-in 32-bit PowerShell/.NET and the 32-bit `d3dx9_39.dll` from
[Microsoft's DirectX End-User Runtime](https://www.microsoft.com/en-us/download/details.aspx?id=35).
No C++ compiler or DirectX SDK is needed. The fixed runtime and process bitness
preserve the existing compressed mip bytes. Missing-runtime errors include the
installation link. Outputs are rebuilt from `Map_extracted/sun/lens1..8.ddj`
into `.generated/intermediate/images/Map_extracted/sun/`.

| Output | Contents |
| --- | --- |
| `.generated/client-public/assets/` | Published browser assets, served at `/assets/...` |
| `.generated/client-public/assets/packs/` | `manifest.json` plus content-addressed `<group>-NNN-<hash>.bin` packs |
| `.generated/intermediate/` | Converted source images and their manifest, inputs to packing |
| `.generated/game-data/1.150/server/` and `server.srogz` | Verified server projection and its lossless archive |

## Commands

| Command | Use |
| --- | --- |
| `pnpm assets build` | Full build of the browser projection (roughly 40 minutes) |
| `pnpm assets publish [<family>]` | Standalone publishers the full build does not run yet (skill UI data, dungeon worlds, flares, and others); run after `assets build` |
| `pnpm task build server-game-data` | Server game-data projection and `server.srogz` |
| `pnpm assets build world-outdoor -- --region=0x6a48 --force --jobs=1` | Rebuild one outdoor region |
| `pnpm assets refresh <family>` | Re-publish one family into an existing tree (`pnpm task list --kind assets` lists them) |
| `pnpm assets refresh delivery` | Regenerate the web manifest and stale precompressed sidecars |
| `pnpm assets repack` | Rebuild every pack from the published loose tree |
| `pnpm assets gc` | Report asset-pack outputs the published index no longer uses; `-- --apply` soft-archives them to `temp/archives/` (publishers also do this after every index publish) |
| `pnpm assets compact` | Release profile: keep compressed packs, drop loose duplicates |
| `pnpm assets check integrity` | Verify pack manifests and artifacts |
| `pnpm assets check compact` | Verify the compact release set is lossless |
| `pnpm assets lock` | Show which process holds the generated-asset lock |

The publishers live in `apps/client-next/tools/publish-*.mjs`; folding them
into `assets build` is outstanding work. Client source changes never need an
asset build. Builds take an exclusive lock
(`scripts/rebuildLock.mjs`), so concurrent builds refuse rather than corrupt
output.

## Packs

Small assets are delivered in binary packs (about 50 MiB each) instead of
thousands of loose files. `packs/manifest.json` maps every logical `/assets/...`
path to a pack, byte offset, length, MIME type and SHA-256. The build refuses to
write a manifest whose counts, sizes or ranges disagree, and the client verifies
each pack's length and hash before exposing any slice. Code keeps using the
logical `/assets/...` paths; only the loaders know about packs.

Tests that read published assets go through `scripts/lib/publishedAsset.mjs`,
which resolves a path through the installed packs and verifies it, so compacted
trees behave like the real product.

## Layout

| Folder | Contents |
| --- | --- |
| `scripts/build/char/` | Characters, NPCs, items, skill effects, animation (`native/` holds v1.150 data-format tables) |
| `scripts/build/world/` | Terrain, regions, objects, navmesh, environment |
| `scripts/build/effects/`, `data/`, `server/` | Effect programs, gameplay data tables, server projection |
| `scripts/build/shared/` | Cross-domain I/O, hashing, atomic publication, compression |
| `scripts/build/reference/` | Native reference data the builders consume |
| `scripts/checks/` | Delivery, integrity, compaction and precompressed-sidecar checks |
