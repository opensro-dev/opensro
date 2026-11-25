/*
===========================================================================

sessionconstants.go - the session-constants digest (#4421)

The entry answer's constant sections (the reference snapshot, the
magic-option snapshot and the commerce seed) travel under one digest
of their own bytes. A client that took a full answer states the
digest on re-entry; a session whose statement matches keeps its
tables and the server omits the sections. The digest owns no state:
it is recomputed from the same inputs the answers are built from.

===========================================================================
*/
package enterworld

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

/*
================
snapshotRowsDigest

The sha256 hex of one snapshot section's wire JSON. The sections are
static per catalogue generation, so the snapshot's digest rides its
cache (filteredRefObjSnapshot) instead of re-marshaling 700 KB per
entry; the magic section is small and hashed per entry.
================
*/
func snapshotRowsDigest(rows any) string {
	raw, err := json.Marshal(rows)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

/*
================
sessionConstantsDigest

The one digest a session's constants travel under (#4421): the
reference file's digest, the filtered RefObj snapshot, the magic
option snapshot and the commerce seed, hashed together. The
bootstrap names it in every answer; a client that holds it states
it on re-entry, and a match omits the constant sections (Build) and
the game-ready seed (RegisterGameReady). A data change anywhere in
the four inputs changes the digest, so a stale statement simply
earns a full answer. Empty means the inputs could not be hashed;
nothing is ever omitted for it.
================
*/
func sessionConstantsDigest(deps *Deps, refObjRows []RefObjRow, magicRows []MagicOptionRow) string {
	snapshotDigest := ""
	if cache := deps.refObjCache; cache != nil {
		cache.mu.Lock()
		snapshotDigest = cache.digest
		cache.mu.Unlock()
	}
	if snapshotDigest == "" {
		// No cache holder (fixtures): hash the rows this call built.
		snapshotDigest = snapshotRowsDigest(refObjRows)
	}
	fileDigest := ""
	if deps.BrowserReferences != nil {
		fileDigest = deps.BrowserReferences.SHA256
	}
	sum := sha256.Sum256([]byte(fileDigest + "|" + snapshotDigest + "|" +
		snapshotRowsDigest(magicRows) + "|" + deps.SeedDigest))
	return hex.EncodeToString(sum[:])
}
