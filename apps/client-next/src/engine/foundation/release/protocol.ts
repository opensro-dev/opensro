/*
===========================================================================

protocol.ts - the release protocol this client build speaks

A release protocol names the complete set of browser-facing wire contracts
(the server owner is apps/server/internal/releaseprotocol). Every request
to the title and agent services declares it; a server speaking another
answers 426 Upgrade Required, and the page offers the newer release.

This file imports nothing, so Node tooling (probes, release checks) reads
the same number the client ships.

===========================================================================
*/

export const RELEASE_PROTOCOL = 3;
export const RELEASE_PROTOCOL_HEADER = "X-OpenSRO-Protocol";
// 426 Upgrade Required: the server speaks another release protocol.
export const RELEASE_OUTDATED_STATUS = 426;
