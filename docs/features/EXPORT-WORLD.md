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
- **Deliberate exclusion.** Only character rows whose codename opens
  with a native kind (MOB, COS, NPC, MOV, STRUCTURE, CHAR) are kept;
  another server's own authored rows carry its prefix instead, are
  dropped at pack time and counted in the report; they are not
  later-client content. The export's remaining unique rows ride the
  shard whole - what an area imports is its own
  PR's decision.
- **Deterministic.** Sorted entries at a fixed timestamp: the same
  export and areas file re-pack to the same sha256.

## The importer (this slice, #3329)

`scripts/export_world_import.py` is the optional layer beside the
others (`variant_later_layers.py`, last of the world layers). While
`SRO_EXPORT_WORLD` is on and `SRO_CONTENT_ZIP` names an archive that
verifies against the pin, every packed area merges into the variant:
sector files into `Map_extracted`, navmesh into
`Data_extracted/navmesh`, map tiles into `Media_extracted`, the
sectors' availability bits set, and the area's region rows (from the
archive's `tables/regioninfo.txt`) join the tree's `regioninfo.txt`
under a section of its own. The marker (`.opensro-export-world.json`)
enrols every file and row, so a switch off - or the archive gone -
reaps the tree back byte for byte. Off, unnamed or unpinned, nothing
is written: the world is unchanged, which is every checkout's state
until a real pack is pinned.

The build also writes the server's admission index,
`export-world.json` beside the character tables: each area's region
ids and the character kinds it admits (empty until its own import
names them). The server reads it only while the flag is on
(`internal/game/world/exportworld`), and an area's population - a
Version-2 nest file, `export-world-field.txt` - joins only when
every nest stands in an admitted region and names an admitted kind
(`monster.AppendExportWorld`); anything else refuses startup. With
no population file yet, the flag on boots the native population.

Character kinds convert into the identity band **190000**, reserved
here: no other layer may take it. The archive's character shard and
`tables/dungeoninfo.txt` ride along unread - conversions, spawns,
NPCs, teleports and dungeon rows are each area's own import PR
(authors its areas-file entry, its population rows and its admitted
kinds against this road). The inventory
(`research/investigations/later-sources-3247/v2355-inventory.md`)
prices every area: no area is asset-blocked.

Ops companion owed: the shard job gains an `export_world` variable
before a release build can carry the switch (the muhan/hunting2
order); until then the flag is set by hand.

## Claims

No native behaviour is claimed: the content postdates the v1.150
client. The export's file layout (numeric sector dirs, the navmesh
name rule, the character shard at the root) is read from the export's
own tables, the same layout the v1.696 importers already rely on.
