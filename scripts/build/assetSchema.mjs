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

===========================================================================
*/

export const ASSET_SCHEMA = 3;
