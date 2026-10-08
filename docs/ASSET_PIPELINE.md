# Asset pipeline

Turns a licensed, extracted Silkroad Online v1.150 client into the data the
browser client and server load. Nothing it produces is committed.

## Inputs and outputs

Inputs, in the game root (`SRO_GAME_ROOT`, else the folder containing the
checkout): `SRO_Client.exe`, `Particles.pk2` and `extracted/` (`Media_extracted`,
`Data_extracted`, `Map_extracted`, `Particles_extracted`, `Music_mp3`).
`pnpm assets prepare` (`scripts/prepare_client_resources.py`) produces
`extracted/` from the client's own PK2 archives, byte for byte, and converts
the ASCII-named `Music.pk2` tracks with `ffmpeg -codec:a libmp3lame -q:a 0`.
The CP949-named tracks are referenced only by the legacy `resinfo` sound table
the port does not read. `pnpm assets doctor` checks every input and tool; the
builds refuse to start while an input is missing
(`scripts/build/shared/clientInputs.mjs`). Python 3.12 with
`requirements-build.txt` is used for extraction, image and font conversion.

Lens mip resources are generated automatically before image conversion, and
by standalone world builds before copying sky assets. This uses Windows'
built-in 32-bit PowerShell/.NET and the 32-bit `d3dx9_39.dll` from
[Microsoft's DirectX End-User Runtime](https://www.microsoft.com/en-us/download/details.aspx?id=35).
No C++ compiler or DirectX SDK is needed. The fixed runtime and process bitness
preserve the existing compressed mip bytes. Missing-runtime errors include the
installation link. Outputs are rebuilt from `Map_extracted/sun/lens1..8.ddj`
into `.generated/intermediate/images/Map_extracted/sun/`.

Character base materials, environment reflections and equipment glows share
`scripts/build/shared/nativeCharacterTextures.mjs`. Before model publication,
the full build generates complete native mip resources using that same D3DX
loader. The source DDS blocks are copied verbatim; only missing mip levels come
from D3DX. Each cached resource is bound to both its source bytes and generator
code and checked against every authored level before use.

Asset schema **3** allows embedded `application/x-sro-texture` images in the
character model container. These carry BC1 (DXT1), BC2 (DXT3) or BC3 (DXT5)
mips. Uncompressed images, premultiplied DXT2 lightmaps and non-power-of-two
building images retain their existing PNG path. The worker transfers native
mip buffers once; the renderer keeps them for device restoration. A GPU without
BC support receives temporary RGBA uploads from the shared block decoder.
Terrain DDS fallback uses that decoder too.

For a standalone character publisher after changing retail texture inputs,
first run `node scripts/build/shared/nativeCharacterTextures.mjs`. Both this
prerequisite and the full build acquire the generated-asset lock. The cache
lives in `.generated/intermediate/images/native-character/`. A missing or
mismatched entry fails publication rather than silently switching formats.
An older client cannot consume schema 3; ship rebuilt data and the matching
client together as a data release.

| Output | Contents |
| --- | --- |
| `.generated/client-public/assets/` | Published browser assets, served at `/assets/...` |
| `.generated/client-public/assets/packs/` | `manifest.json` plus content-addressed `<group>-NNN-<hash>.bin` packs |
| `.generated/intermediate/` | Converted source images and their manifest, inputs to packing |
| `apps/server/.generated/game-data/1.150/server/` and sibling `server.srogz` | Verified server projection and its lossless archive, inside the Go module |

## Commands

| Command | Use |
| --- | --- |
| `pnpm assets prepare` | Extract the client archives into `extracted/` and convert the music; reruns repair |
| `pnpm assets doctor` | Read-only report of every input and tool, with the fix for each problem |
| `pnpm assets build full` | Full build: every outdoor region, then the browser projection (roughly 40 minutes) |
| `pnpm assets build` | The browser projection, reusing the outdoor world of the last full build |
| `pnpm assets publish [<family>]` | Standalone publishers and focused families the full build does not run yet (skill UI data, dungeon worlds, flares, party status icons, native window art, and others); run after `assets build` |
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

Focused families are rows of `scripts/build/families/looseFamilies.mjs`, run
by `scripts/refresh_asset_family.mjs <family>`. Every build process records
the public files it produced in the publication ledger, and the full build
reports packed files no current builder produced. Sizes, the delivery path
in the browser, the ledger and data releases are described in
[ASSET_DELIVERY.md](ASSET_DELIVERY.md).

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
