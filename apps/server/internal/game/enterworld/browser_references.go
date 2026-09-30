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
	// itemIDs are the published item rows; a login skips them.
	itemIDs map[uint32]bool
}

// maxPublicReferenceRows bounds each published table (the browser's
// REFERENCE_ROWS_LIMIT).
const maxPublicReferenceRows = 65536

// BrowserReferenceSources are the immutable tables one reference file holds.
type BrowserReferenceSources struct {
	Skills SkillDataSource
	// ItemCommands may be nil.
	ItemCommands interface{ ItemCommandReferences() []ItemCommandReference }
	// StaticItems are the item rows every viewer needs (StaticRefItemRows).
	StaticItems []RefItemRow
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
	data, err := json.Marshal(struct {
		ReferencesVersion     int                    `json:"referencesVersion"`
		SkillLifecycleVersion int                    `json:"skillLifecycleVersion"`
		RefSkillSnapshot      []SpawnSkillRow        `json:"refSkillSnapshot"`
		RefItemSnapshot       []RefItemRow           `json:"refItemSnapshot"`
		ItemCommandReferences []ItemCommandReference `json:"itemCommandReferences,omitempty"`
	}{releaseprotocol.ReferencesContract, 1, rows, itemRows, commandRows})
	if err != nil {
		return nil, err
	}
	if len(data) > 32<<20 {
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
		Path:     "/transport/references/" + hash + ".json",
		SHA256:   hash,
		Bytes:    len(data),
		gzipData: compressed.Bytes(),
		itemIDs:  itemIDs,
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
