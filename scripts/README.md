# scripts

The asset pipeline, repository checks and the task runner. Run everything from
the repository root through the task CLI: `pnpm task list` shows every task,
`pnpm task explain <task>` its command and prerequisites. Task definitions live
in [tasks/](tasks/README.md). The pipeline itself is described in
[docs/ASSET_PIPELINE.md](../docs/ASSET_PIPELINE.md).

## Entry points

| Script | Task |
| --- | --- |
| `build_sro_resources.mjs` | `pnpm assets build` |
| `build_outdoor_world_resources.mjs` | `pnpm assets build world-outdoor` |
| `refresh_asset_family.mjs <family>` | `pnpm assets refresh <family>` for the focused families in `build/families/looseFamilies.mjs` |
| `refresh_{native_font,title_crowd,outdoor,world_map}_asset_packs.mjs` | `pnpm assets refresh fonts`, `title-crowd`, `outdoor`, `world-map` |
| `../apps/client-next/tools/publish-<family>.mjs` | `pnpm assets publish [<family>]` |
| `refresh_asset_delivery.mjs` | `pnpm assets refresh delivery` |
| `rebuild_asset_packs_from_public.mjs` | `pnpm assets repack` |
| `compact_sro_assets.mjs` | `pnpm assets compact` |
| `rebuildLock.mjs`, `rebuild_lock.py` | The generated-asset lock shared by the JS and Python steps; `pnpm assets lock` |
| `convert_images.py` | Spawned by the build: DDJ/DDS to browser images |
| `extract_client_cursors.py` | Spawned by the build: client cursors |
| `clean_release_workspace.mjs` | `pnpm release` |
| `setup_git_hooks.mjs` | `pnpm task hooks install` |
| `task.mjs` | The task CLI |

## Folders

| Folder | Contents |
| --- | --- |
| `build/` | Pipeline modules by domain: `char/` (`native/` holds v1.150 data-format tables), `world/`, `effects/`, `data/`, `server/`, `shared/`, `artifacts/`, `reference/` |
| `checks/` | Asset delivery, pack integrity, compaction, precompressed sidecars, Go server gates, source size (`size-baseline.txt`), source encoding, shared native fixtures, formatting ratchet (`format-baseline.txt`) |
| `lib/` | Published-asset access, probe endpoints and sessions, the Chrome launcher for browser tests |
| `analysis/` | `project_hive_caps.py` (generates the server's hive-cap table), `verify_particle_archive.py` |
| `tools/` | Localization gap data, native window image refresh |
| `test/` | Pipeline tests by suite (`assets`, `cif`, `region`, `world`; see `test/suites/`) |
| `tasks/` | Task definitions and check pipelines |

Output goes to the ignored `temp/` or `.generated/`, never into `scripts/`.
