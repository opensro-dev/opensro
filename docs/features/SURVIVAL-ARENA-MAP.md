# Optional Colosseum asset import

The experimental So-ok event uses a private PvE instance of the Colosseum,
region25580 (0x63ec), centred on local X960/Z960. The official Survival Arena
is PvP; this optional event deliberately has different, owner-requested rules.
The [official operator guide](https://silkroad.gamegami.com/guide.php?catid=83)
provides the venue reference, not the gameplay specification for this feature.

## Required assets

The baseline v1.150 asset set lacks sector236/99. Constantinople's older
colosseum world prop is not this arena. Do not enable a teleport into a missing
sector. The missing Colosseum inputs are included under
`docs/features/assets/survival/colosseum/`. The default importer uses those
inputs plus the original v1.150 extraction, with no newer PK2 or download.
It never runs a retail executable or edits the original archives. The asset
manifest pins each included file; `scripts/survival_archive_pins.json` records
its source provenance. An explicit `--client-root` still supports the original
pinned-archive import for reproduction.

`scripts/import_survival_colosseum.py` creates a separate variant root using
the existing guarded Alexandria import helpers. It imports the sector terrain,
180 object placements, nine BSR models and their BMS/BMT/DDJ dependencies,
navigation, referenced particle resources, minimap236x99 and environment55.
The newer safezone teleport rows identify the region; the port keeps its own
private leases instead of importing the newer game's event implementation.

Conflicting native model/texture paths are rejected. Terrain tile IDs use the
existing remapper; unrelated lighting bindings remain unchanged. A resumable
manifest records source identities, output hashes and completion. Incomplete
or mismatched inputs cannot silently replace a completed import.

## Reproduce

Use the documented Python asset-tool environment and FFmpeg. Start with a
built native baseline. All roots below are explicit operator-provided paths.

```powershell
$env:SRO_SURVIVAL_COLOSSEUM = 'on'
$env:SRO_SOOK_SURVIVAL_ARENA = 'on'
python scripts/import_survival_colosseum.py --native-root <owned-v150-root> --variant-root <separate-output-root>
$env:SRO_GAME_ROOT = '<separate-output-root>'
$env:SRO_GENERATED_ROOT = '<absolute-generated-root-for-this-variant>'
pnpm assets build world-outdoor -- --region=0x63ec --force --jobs=1
pnpm assets refresh outdoor
```

Build and publish the client reference/image closure and server game-data
bundle from the same variant using the normal asset pipeline. Configure the
server's game-data root and matching manifest digest, not a loose mixture of
native and imported tables. The importer adds owned level variants needed by
the event's native-model fallbacks; the published client and server tables
must agree on their identities.

Focused outdoor refresh owns `/assets/world/outdoor`, not shared terrain
images. Publish new `/assets/images/Map_extracted/tile2d` files through
`refreshOwnedPackFiles`, using `imagePackGroup` as the default owner, under the
generated-assets lock. A loose image alone is insufficient: its pack index
and manifest sidecars must be refreshed. Do not mutate pack ownership by hand.

## Enabling and verification

Enable only the GameWorld's `SRO_SOOK_SURVIVAL_ARENA=on` after installing the
matched data. The flag is off by default and missing venue/navigation data
refuses admission safely. The asset import itself is an explicit opt-in tool,
not an automatic modification of players' client files.

Importer unit tests cover safezone identification and conflicting object
references. The feature's actor, private-world and pickup tests cover server
isolation. Earlier local engine tests demonstrated imported geometry and
world entry; they do not replace a fresh full-stage regression of each merged
build. See [implementation handoff](SOOK-SURVIVAL-ARENA-CLAUDE-HANDOFF.md).

Generated client/server bundles, PK2 files, local databases, executable builds
and recordings remain untracked. Only the missing arena build inputs are
included; native v1.150 assets and complete archives are not copied into Git.
