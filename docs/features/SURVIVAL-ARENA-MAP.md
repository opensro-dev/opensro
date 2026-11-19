# Optional Colosseum asset import

The experimental So-ok event uses a private PvE instance of the Colosseum,
region25580 (0x63ec), centred on local X960/Z960. The official Survival Arena
is PvP; this optional event deliberately has different, owner-requested rules.
The [official operator guide](https://silkroad.gamegami.com/guide.php?catid=83)
provides the venue reference, not the gameplay specification for this feature.

## How the venue is built

The baseline v1.150 asset set lacks sector 236/99. Constantinople's older
colosseum world prop is not this arena. The venue comes from the pinned
**Silkroad Online v1.696** client the Alexandria variant build already reads
(`SRO_V1696_ROOT`, verified against `scripts/v1696_archive_pins.json` before
any archive is read).

`scripts/colosseum_import.py` is one later layer of that build
(`scripts/variant_later_layers.py`). It rides the arena's own switch,
`SRO_SOOK_SURVIVAL_ARENA`, so the build and the GameWorld always agree: a shard
with the arena on has its venue, and one with it off has none.

- The region is the one the v1.696 safezone teleport rows
  (`STRUCTURE_POS_COLOSSEUM_SAFEZONE_*`) stand in. It must be 25580, the region
  the server runs the arena in (`action/sookarena.go`), with at least eight
  starts; any other client is refused. The client authors no region-code row
  for it.
- The layer runs the later-world engine's map-only path
  (`later_world_import.add_map_world`), the same path as the Styria Clash arena:
  - the region's terrain, remapped onto the merged tile catalogue;
  - its navmesh and availability bit;
  - its minimap;
  - every Data file the tree lacks, and the particle closure of its models.
- It adds no monsters: the arena draws its waves from the shard's creatable
  monsters (`sookarena.BuildPool`).
- Its environment entry (55) is the VSRO `environment.ifo`'s, which the variant
  builder writes after every layer.
- Switching it off removes exactly what its marker (`.opensro-colosseum.json`)
  records, and reads no archive.

## Reproduce

Build the Alexandria variant as usual, with the v1.696 root set and the arena
on. The release build does this from the freeze's shard settings.

```powershell
$env:SRO_V1696_ROOT = 'C:/SRO-v1.696'
$env:SRO_SOOK_SURVIVAL_ARENA = 'on'
python scripts/build_alexandria_variant.py --vsro-root <vsro> --variant-root <variant> --imported-root <stage1>
```

The variant manifest's `colosseum` entry reports the region, the files added
and any particle gaps. `scripts/test/python/test_colosseum_import.py` pins the
region rule, and `test_later_world_import.py` pins the engine path.

Retail arena inputs are not included. Only the import code, the archive hashes
and original authored art are tracked.
