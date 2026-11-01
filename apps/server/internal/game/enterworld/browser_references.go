/*
===========================================================================

browser_references.go - the public reference file beside the transport

The skill catalogue, the item command table and the static item rows are
the same for every viewer, so they live in one immutable, content-addressed
file the browser caches, not in each EnterWorld result. A login names the
file; its own blob carries only what depends on the character and division.

===========================================================================
*/
package enterworld

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"opensro.online/server/internal/platform/jsonscratch"
	"opensro.online/server/internal/releaseprotocol"
)

/*
================
BrowserReferences

Immutable process-owned public data, compiled once from the same authority
used by combat. Content identity invalidates the cache when a row changes.
Never put character/session fields here.
================
*/
type BrowserReferences struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
	// Port-only, not native: a separately loaded, static beginner atlas.
	HuntingGuide *BrowserReferences `json:"huntingGuide,omitempty"`
	gzipData     []byte
	// itemIDs and objectIDs are the published item and object rows; a
	// login skips them.
	itemIDs   map[uint32]bool
	objectIDs map[uint32]bool
}

// The browser refuses a reference file past these bounds (http.ts
// REFERENCE_ROWS_LIMIT per table, REFERENCE_BYTES_LIMIT decoded), so the
// server refuses to publish one.
const (
	maxPublicReferenceRows  = 65536
	maxPublicReferenceBytes = 32 << 20
)

/*
================
BrowserReferenceSources

The immutable tables one reference file holds.
================
*/
type BrowserReferenceSources struct {
	Skills SkillDataSource
	// ItemCommands may be nil.
	ItemCommands interface{ ItemCommandReferences() []ItemCommandReference }
	// StaticItems are the item rows every viewer needs (StaticRefItemRows).
	StaticItems []RefItemRow
	// Monsters are the monster rows every viewer needs
	// (PublicMonsterRefObjRows).
	Monsters []RefObjRow
	// Port-only, not native. Read only when SRO_HUNTING_GUIDE is enabled.
	HuntingGuide HuntingGuideSource
	// ReverseMapPoints is the reverse return scroll's map table: empty
	// unless the port-only SRO_REVERSE_RETURN_MAP option is on.
	ReverseMapPoints []ReverseMapPoint
}

// maxReverseMapNameBytes bounds a published point name.
const maxReverseMapNameBytes = 128

/*
================
ReverseMapPoint

A reverse return map destination (port-only, not native). The browser paints
it from these coordinates; a use names only its ID, which the server
resolves (action/reversemap.go).
================
*/
type ReverseMapPoint struct {
	ID       uint32  `json:"id"`
	Name     string  `json:"name"`
	RegionID uint16  `json:"regionId"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
}

/*
================
NewBrowserReferences
================
*/
func NewBrowserReferences(sources BrowserReferenceSources) (*BrowserReferences, error) {
	rows := spawnSkillSnapshot(sources.Skills)
	if len(rows) == 0 || len(rows) > maxPublicReferenceRows {
		return nil, fmt.Errorf("invalid public skill catalogue size: %d", len(rows))
	}
	if len(sources.StaticItems) > maxPublicReferenceRows {
		return nil, fmt.Errorf("invalid public item catalogue size: %d", len(sources.StaticItems))
	}
	var commandRows []ItemCommandReference
	if sources.ItemCommands != nil {
		commandRows = sources.ItemCommands.ItemCommandReferences()
	}
	if len(commandRows) > maxPublicReferenceRows {
		return nil, fmt.Errorf("invalid public item command catalogue size: %d", len(commandRows))
	}
	itemRows := sources.StaticItems
	if itemRows == nil {
		itemRows = []RefItemRow{}
	}
	itemIDs := make(map[uint32]bool, len(itemRows))
	for _, row := range itemRows {
		if itemIDs[row.RefObjID] {
			return nil, fmt.Errorf("public item catalogue repeats %d", row.RefObjID)
		}
		itemIDs[row.RefObjID] = true
	}
	if len(sources.Monsters) > maxPublicReferenceRows {
		return nil, fmt.Errorf("invalid public monster catalogue size: %d", len(sources.Monsters))
	}
	objectRows := sources.Monsters
	if objectRows == nil {
		objectRows = []RefObjRow{}
	}
	objectIDs := make(map[uint32]bool, len(objectRows))
	for _, row := range objectRows {
		if objectIDs[row.RefObjID] {
			return nil, fmt.Errorf("public monster catalogue repeats %d", row.RefObjID)
		}
		objectIDs[row.RefObjID] = true
	}
	for index, point := range sources.ReverseMapPoints {
		if point.ID != uint32(index+1) || len(point.Name) > maxReverseMapNameBytes {
			return nil, fmt.Errorf("invalid reverse map point %d", point.ID)
		}
	}
	data, err := json.Marshal(struct {
		ReferencesVersion     int                    `json:"referencesVersion"`
		SkillLifecycleVersion int                    `json:"skillLifecycleVersion"`
		RefSkillSnapshot      []SpawnSkillRow        `json:"refSkillSnapshot"`
		RefItemSnapshot       []RefItemRow           `json:"refItemSnapshot"`
		RefObjSnapshot        []RefObjRow            `json:"refObjSnapshot"`
		ItemCommandReferences []ItemCommandReference `json:"itemCommandReferences,omitempty"`
		ReverseMapPoints      []ReverseMapPoint      `json:"reverseMapPoints,omitempty"`
	}{releaseprotocol.ReferencesContract, 1, rows, itemRows, objectRows, commandRows, sources.ReverseMapPoints})
	if err != nil {
		return nil, err
	}
	if len(data) > maxPublicReferenceBytes {
		return nil, fmt.Errorf("public references exceed decoded resource budget: %d", len(data))
	}
	result, err := newBrowserReferenceFile(data)
	if err != nil {
		return nil, err
	}
	result.itemIDs, result.objectIDs = itemIDs, objectIDs
	result.HuntingGuide, err = newHuntingGuideReference(sources.HuntingGuide)
	if err != nil {
		return nil, err
	}
	return result, nil
}

/*
================
newBrowserReferenceFile

One immutable HTTP resource owner for reference tables and the optional atlas.
================
*/
func newBrowserReferenceFile(data []byte) (*BrowserReferences, error) {
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return &BrowserReferences{Path: "/transport/references/" + hash + ".json", SHA256: hash,
		Bytes: len(data), gzipData: compressed.Bytes()}, nil
}

/*
================
ServeHTTP
================
*/

func (r *BrowserReferences) ServeHTTP(w http.ResponseWriter, q *http.Request) {
	if r.HuntingGuide != nil && q.URL.Path == r.HuntingGuide.Path {
		r.HuntingGuide.ServeHTTP(w, q)
		return
	}
	if q.URL.Path != r.Path {
		http.NotFound(w, q)
		return
	}
	if q.Method != http.MethodGet && q.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Vary", "Accept-Encoding")
	// Public reference tables contain no credentials or player state.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	compressed := false
	for _, encoding := range strings.Split(q.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(encoding) == "gzip" {
			compressed = true
			w.Header().Set("Content-Encoding", "gzip")
			break
		}
	}
	w.Header().Set("ETag", `"`+r.SHA256+`"`)
	if q.Header.Get("If-None-Match") == `"`+r.SHA256+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if q.Method == http.MethodGet {
		if compressed {
			_, _ = w.Write(r.gzipData)
			return
		}
		// Retain only the compressed immutable catalogue. Identity clients
		// stream through a bounded decoder instead of keeping a second copy.
		reader, err := gzip.NewReader(bytes.NewReader(r.gzipData))
		if err != nil {
			http.Error(w, "reference decoding failed", http.StatusInternalServerError)
			return
		}
		defer reader.Close()
		_, _ = io.Copy(w, reader)
	}
}

/*
================
blobScratchPool, marshalBlob

Enter-world renders its blob per entry (355 MB of Marshal output in a
60-second production heap profile). Entry is concurrent across
sessions and no single owner spans it, so the encode buffer's owner
is a pool checkout; a scratch that grew past maxBlobScratch is dropped
rather than held. marshalBlob is the owned-bytes form for callers
that keep the blob (fixtures, the failure outcome): pooled encode,
then one clone at final size. The per-entry hot path
(browserEntryPayload) borrows the pooled bytes instead and lets the
envelope's copy be the only one.

The blob bytes are identical to json.Marshal's (jsonscratch pins the
equivalence).
================
*/
var blobScratchPool = sync.Pool{New: func() any { return new(jsonscratch.Scratch) }}

const maxBlobScratch = 1 << 20

func marshalBlob(v any) ([]byte, error) {
	scratch := blobScratchPool.Get().(*jsonscratch.Scratch)
	raw, err := scratch.Marshal(v)
	if err != nil {
		releaseBlobScratch(scratch)
		return nil, err
	}
	// raw aliases the scratch: clone it before the scratch goes back,
	// or a concurrent entry can encode over it mid-clone and hand this
	// caller another player's blob.
	owned := bytes.Clone(raw)
	releaseBlobScratch(scratch)
	return owned, nil
}

/*
================
releaseBlobScratch

Returns a scratch to the pool once nothing reads its bytes any more,
dropping one that grew past maxBlobScratch.
================
*/
func releaseBlobScratch(scratch *jsonscratch.Scratch) {
	if scratch.Cap() <= maxBlobScratch {
		blobScratchPool.Put(scratch)
	}
}

/*
================
referenceEnterWorldBlob

The wire envelope carries only the immutable identity. The full
BootstrapResult remains available for detached fixtures/legacy consumers.
================
*/
func referenceEnterWorldBlob(result *BootstrapResult, refs *BrowserReferences) ([]byte, error) {
	return marshalBlob(referenceBlobValue(result, refs))
}

/*
================
referenceBlobValue

The reference blob's one shape, shared by the owned encoder above and
the borrowed per-entry path (browserEntryPayload), so the two can never
drift apart.
================
*/
func referenceBlobValue(result *BootstrapResult, refs *BrowserReferences) any {
	return struct {
		V          int                `json:"v"`
		Bootstrap  any                `json:"bootstrap"`
		References *BrowserReferences `json:"references"`
	}{2, result.referenceBlobView(), refs}
}
