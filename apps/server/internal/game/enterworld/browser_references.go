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

	"opensro.online/server/internal/releaseprotocol"
)

// BrowserReferences is immutable process-owned public data, compiled once from
// the same authority used by combat. Never put character/session fields here.
// Content identity, rather than a manually maintained asset list or version,
// invalidates the browser cache when any source row or projection changes.
type BrowserReferences struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Bytes    int    `json:"bytes"`
	gzipData []byte
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

// BrowserReferenceSources are the immutable tables one reference file holds.
type BrowserReferenceSources struct {
	Skills SkillDataSource
	// ItemCommands may be nil.
	ItemCommands interface{ ItemCommandReferences() []ItemCommandReference }
	// StaticItems are the item rows every viewer needs (StaticRefItemRows).
	StaticItems []RefItemRow
	// Monsters are the monster rows every viewer needs
	// (PublicMonsterRefObjRows).
	Monsters []RefObjRow
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
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err = writer.Write(data); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return &BrowserReferences{
		Path:      "/transport/references/" + hash + ".json",
		SHA256:    hash,
		Bytes:     len(data),
		gzipData:  compressed.Bytes(),
		itemIDs:   itemIDs,
		objectIDs: objectIDs,
	}, nil
}

func (r *BrowserReferences) ServeHTTP(w http.ResponseWriter, q *http.Request) {
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

// The wire envelope carries only the immutable identity. The existing full
// BootstrapResult remains available for detached fixtures/legacy consumers.
func referenceEnterWorldBlob(result *BootstrapResult, refs *BrowserReferences) ([]byte, error) {
	copy := *result
	copy.RefSkillSnapshot = nil
	copy.Packets = nil
	blob, err := EnterWorldBlob(&copy)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Bootstrap map[string]json.RawMessage `json:"bootstrap"`
	}
	if err = json.Unmarshal(blob, &wrapper); err != nil {
		return nil, err
	}
	delete(wrapper.Bootstrap, "refSkillSnapshot")
	return json.Marshal(struct {
		V          int                        `json:"v"`
		Bootstrap  map[string]json.RawMessage `json:"bootstrap"`
		References *BrowserReferences         `json:"references"`
	}{2, wrapper.Bootstrap, refs})
}
