package agentapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
================
uploadedCrestStub

One uploaded crest behind a native name, as the game world's crest
store would resolve it.
================
*/
type uploadedCrestStub struct {
	kind     string
	id       int64
	revision uint32
	payload  []byte
	hash     string
}

/*
================
ServeCrest
================
*/
func (stub uploadedCrestStub) ServeCrest(kind string, id int64, revision uint32) ([]byte, string, bool) {
	if kind != stub.kind || id != stub.id || revision != stub.revision {
		return nil, "", false
	}
	return stub.payload, stub.hash, true
}

/*
================
TestGuildMarkServesUploadedCrestByRevision

An uploaded crest (#2282) serves under its standing revision's
native name as the store's exact bytes, with the hash as its cache
validator; a replaced revision, a wrong owner, a foreign kind and a
traversal all still answer 404.
================
*/
func TestGuildMarkServesUploadedCrestByRevision(t *testing.T) {
	api, _ := newTestAPI(t)
	payload := bytes.Repeat([]byte{0x2A}, 256)
	api.crests = uploadedCrestStub{kind: "guild", id: 3, revision: 7, payload: payload,
		hash: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"}
	get := func(path, ifNoneMatch string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if ifNoneMatch != "" {
			request.Header.Set("If-None-Match", ifNoneMatch)
		}
		result := httptest.NewRecorder()
		api.Handler().ServeHTTP(result, request)
		return result
	}

	// The client's name carries its session crest prefix (9 here);
	// the per-shard store keys on the guild and revision alone.
	result := get("/marks/G9_3_7.crb", "")
	if result.Code != http.StatusOK || !bytes.Equal(result.Body.Bytes(), payload) {
		t.Fatalf("uploaded crest = %d %x, want the store's bytes", result.Code, result.Body.Bytes())
	}
	if got := result.Header().Get("ETag"); got != `"`+"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"+`"` {
		t.Fatalf("ETag = %q, want the payload hash", got)
	}
	if got := result.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("content-type-options = %q, want nosniff", got)
	}
	if got := result.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("cache-control = %q, want the immutable crest policy", got)
	}
	etag := result.Header().Get("ETag")
	if cached := get("/marks/G9_3_7.crb", etag); cached.Code != http.StatusNotModified {
		t.Fatalf("validator answer = %d, want 304", cached.Code)
	}
	for _, path := range []string{
		"/marks/G9_3_8.crb", // a revision that is not standing
		"/marks/G9_4_7.crb", // another guild
		"/marks/A9_3_7.crb", // the union kind
		"/marks/G9_3.crb",   // a truncated name
	} {
		if code := get(path, "").Code; code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", path, code)
		}
	}
}
