# The export world (SRO_EXPORT_WORLD)

Port-only, not native (owner ask #3263, route #3254). A later client's
export holds new areas beyond every baseline the port imports from.
They reach the build only through one pinned archive,
`opensro_content.zip`, packed from the export and never committed;
with the archive absent or the flag off, the world is unchanged.

## The archive route (this slice)

| Piece | What it is |
| --- | --- |
| `scripts/pack_content.py` | The packer. Reads the extracted export (data only, nothing in it is run) and an areas file, writes the archive outside the repository, prints its sha256 pin. |
| `scripts/content_archive_pins.json` | The pinned sha256 (`opensro-content-archive-pin-v1`), committed once a real pack exists. No pin ships before then. |
| `scripts/content_archive.py` | The format (`opensro-content-archive-v1`) and the reader: verifies the pin before any entry is read, then serves one entry at a time. |
| areas file | `{"format": "opensro-content-areas-v1", "areas": {"<name>": {"regions": [[x, y], ...]}}}` - each area's sector keys, authored by its import PR from the export's `regioninfo.txt`. |

Inside the archive: `manifest.json` (the format, each packed area's
regions and file count, the shared tables), `tables/` (`regioninfo.txt`,
the export's one character shard, `dungeon/dungeoninfo.txt`), and
`areas/<name>/` with the export-relative paths of the area's closure:
sector terrain (`<sectorY>/<sectorX>.t/.m/.o/.o2`), navmesh
(`navmesh/nv_<region>.nvm`) and map tiles.

## Rules the packer keeps

- **Whole or not at all.** A sector missing its terrain mesh, its
  placements or its navmesh excludes its area; the report names every
  gap and no half area is written. Packing no complete area refuses.
- **Deliberate exclusion.** Character rows naming a third-party server
  brand (CREDDY, ORIGIN) are dropped at pack time and counted in the
  report; they are not later-client content. The export's remaining
  unique rows ride the shard whole - what an area imports is its own
  PR's decision.
- **Deterministic.** Sorted entries at a fixed timestamp: the same
  export and areas file re-pack to the same sha256.

## What is left (one PR per area)

The `SRO_EXPORT_WORLD` importer that reads the archive into the
variant build beside the other optional layers, authors each area's
areas-file entry, and carries its regions, spawns, NPCs and teleports;
the server admits the area's regions and kinds only while the flag is
on. The inventory (`research/investigations/later-sources-3247/v2355-inventory.md`)
prices every area: no area is asset-blocked.

## Claims

No native behaviour is claimed: the content postdates the v1.150
client. The export's file layout (numeric sector dirs, the navmesh
name rule, the character shard at the root) is read from the export's
own tables, the same layout the v1.696 importers already rely on.
