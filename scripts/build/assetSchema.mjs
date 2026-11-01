/*
===========================================================================

assetSchema.mjs - the format version of the published asset data

A published asset tree is readable only by clients built for its formats.
The pack index (assets/packs/manifest.json) records the schema it was built
with; the client build refuses data of any other schema, and release
admission compares both with compatibility.json. A partial publish never
merges into an index of another schema: it would mix formats.

History:
  1  crowd dress, weapon and cosmetic tables (roster catalog version 2)
  2  one per-item catalog keyed by RefItemID with codes (catalog version 3)
  3  character GLBs can embed original BC1/BC2/BC3 mip resources
  4  SROPACK2 packs: members stored gzip-compressed when that saves a tenth;
     no gzip transports or delivery.json (pack index version 2)
     Later additive, without a bump: the lazy texture-variants-astc group
     (ASTC re-encodings at /assets/texture-variants/astc/, NTX code "AS44").
     Older clients never ask for those paths; newer ones read the original
     when a variant is absent. The lazy ui-atlas group is the same kind of
     addition (the icon family's sheet and manifest at /assets/ui-atlas/):
     older clients keep reading the per-icon files beside it.

===========================================================================
*/

export const ASSET_SCHEMA = 4;
