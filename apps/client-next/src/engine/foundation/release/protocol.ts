/*
===========================================================================

protocol.ts - the release identity this client build speaks

RELEASE_PROTOCOL names the complete set of browser-facing wire contracts
(the server owner is apps/server/internal/releaseprotocol). Every request
to the title and agent services declares it; a server speaking another
answers 426 Upgrade Required, and the page offers the newer release.

ASSET_SCHEMA is the format of the published asset data this client reads
(the pipeline owner is scripts/build/assetSchema.mjs). The release build
refuses to package data of another schema.

This file imports nothing, so Node tooling (probes, the release build and
checks) reads the same numbers the client ships.

===========================================================================
*/

// Protocol 7 restores authoritative skill cooldowns at world entry.
// 6 published every creatable monster's row in the reference file (#369).
export const RELEASE_PROTOCOL = 7;
// The public reference file's contract (releaseprotocol.ReferencesContract).
export const REFERENCES_CONTRACT = 3;
export const RELEASE_PROTOCOL_HEADER = "X-OpenSRO-Protocol";
// 426 Upgrade Required: the server speaks another release protocol.
export const RELEASE_OUTDATED_STATUS = 426;
export const ASSET_SCHEMA = 4;
