# Companion protocol and authority release order

Protocol 5 prepares the browser to consume native populated summoner items,
independent attack/pickup COS records, item state/time deltas and explicit
renewal/revival targets. It retains the existing empty summoner-item form.
The first protocol-5 server keeps authority schema 14 and its existing
gameplay implementation. Persistent companions follow in the schema-15 server.

These are two releases because coordinated publication promises that its
previous server can read every database written before a revert. A server
that only reads schema 14 cannot be that fallback after a schema-15 write.
Do not widen its declared schema support or bypass `admit_pair`.

1. Prepare and publish the protocol-5 client/server pair with schema 14.
   Run the coordinated live browser gate and confirm the pair. A failed gate
   can restore the protocol-4 pair without restoring the database.
2. Prepare the complete companion server with protocol 5 and schema 15.
   Publish it as a server-only forward release. The normal guarded authority
   upgrade stops the fleet, validates and backs up every enabled authority,
   preserves the schema-14 account tables, then deploys the new server.
3. Verify readiness, companion restoration and fresh login/reload. A later
   downgrade across schema 15 requires explicit database recovery; ordinary
   component rollback cannot perform it.

This sequence changes no release gate or compatibility admission policy.
Infrastructure targets, candidate identities and live results belong in the
operator's private operations repository. This document describes order,
not evidence that either release has been published.
