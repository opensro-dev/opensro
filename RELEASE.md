# Release checklist

Source control carries source only. A licensed release assembly additionally
carries the locally generated compact client packs and the server archive, so
a shipped tree needs no parent checkout or original installation at runtime.

Before tagging a release:

1. `pnpm install --frozen-lockfile`
2. `pnpm check source` (includes the Go server gates).
3. On a licensed client extraction: `pnpm assets build`,
   `pnpm task build server-game-data`, then the full `pnpm check`. For a
   data release, build the packs against the live layout so unchanged packs
   keep the bytes and URLs players have cached (assetPackLayout.mjs):
   `curl -o live-manifest.json https://opensro.online/assets/packs/manifest.json`,
   then `SRO_ASSET_PACK_BASELINE=live-manifest.json pnpm assets build`.
   Then `pnpm assets ledger` must report no unclaimed assets and no missing
   owner (run `pnpm assets publish` and build again if it does); the beta
   packager refuses otherwise. [docs/ASSET_DELIVERY.md](docs/ASSET_DELIVERY.md)
   explains the ledger.
4. `git status --ignored` must list `.generated/`, `temp/` and `.state/` as
   ignored, and nothing generated as tracked.
5. `pnpm assets compact` and `pnpm assets check compact`. Publish the compact
   browser pack set from `.generated/client-public/assets/` and
   `apps/server/.generated/game-data/1.150/server.srogz`, not loose duplicates. The compact
   check enforces a combined client-plus-server ceiling of 80% of the original
   PK2 payload and verifies the archive is lossless and manifest-consistent.
6. With the loose server projection absent, `pnpm check server` proves
   archive-only materialization. Then `pnpm release` removes ignored build and
   profiling output.
7. `pnpm --filter @sro/client-next build:beta` packages the browser release;
   keep its `private/` source maps outside the deployable package.
8. No file selected by `git ls-files` may be 50 MiB or larger.
9. Review licensing and trademark language for the release.

Never force-add extracted PK2 data, generated media, asset packs,
precompressed sidecars, logs, caches or probe output.
