/*
===========================================================================

blob_encode_test.go - the enter-world blobs are Marshal's bytes

EnterWorldBlob and referenceEnterWorldBlob encode through a pooled
scratch (marshalBlob) instead of json.Marshal. The blob is the
browser's bootstrap contract, so the swap must not move a byte: these
tests compare both encoders against plain json.Marshal of the same
views, byte for byte. TestEnterWorldBlobSinglePassEquivalence covers
the semantic equivalence against the legacy projections.

===========================================================================
*/

package enterworld

import (
	"bytes"
	"encoding/json"
	"testing"

	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

/*
================
TestBlobEncodersMatchMarshal
================
*/
func TestBlobEncodersMatchMarshal(t *testing.T) {
	character := chinaSpearman()
	character.ID = 7
	deps := testDeps(character)
	deps.ResolveDivisionID = DevResolveDivisionIDFromCatalog(deps.Characters)
	outcome := HandleEnterWorld(deps, transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, "0", "asd2")))
	if !outcome.OK {
		t.Fatalf("HandleEnterWorld failed: %+v", outcome.Result)
	}

	gotV1, err := EnterWorldBlob(outcome.Result)
	if err != nil {
		t.Fatalf("EnterWorldBlob: %v", err)
	}
	wantV1, err := json.Marshal(enterWorldBlob{
		V:         EnterWorldBlobVersion,
		Bootstrap: outcome.Result.blobView(),
	})
	if err != nil {
		t.Fatalf("Marshal v1 view: %v", err)
	}
	if !bytes.Equal(gotV1, wantV1) {
		t.Fatalf("EnterWorldBlob differs from Marshal:\ngot:  %s\nwant: %s", gotV1, wantV1)
	}

	refs := &BrowserReferences{
		Path:   "/transport/references/test.json",
		SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Bytes:  1024,
	}
	gotV2, err := referenceEnterWorldBlob(outcome.Result, refs)
	if err != nil {
		t.Fatalf("referenceEnterWorldBlob: %v", err)
	}
	wantV2, err := json.Marshal(struct {
		V          int                `json:"v"`
		Bootstrap  any                `json:"bootstrap"`
		References *BrowserReferences `json:"references"`
	}{2, outcome.Result.referenceBlobView(), refs})
	if err != nil {
		t.Fatalf("Marshal v2 view: %v", err)
	}
	if !bytes.Equal(gotV2, wantV2) {
		t.Fatalf("referenceEnterWorldBlob differs from Marshal:\ngot:  %s\nwant: %s", gotV2, wantV2)
	}

	// The borrowed hot path: the envelope browserEntryPayload returns
	// must carry Marshal's blob bytes for both view shapes.
	for _, tc := range []struct {
		name string
		refs *BrowserReferences
		want []byte
	}{
		{"references", refs, wantV2},
		{"plain", nil, wantV1},
	} {
		payload, err := browserEntryPayload(outcome.Result, tc.refs)
		if err != nil {
			t.Fatalf("browserEntryPayload %s: %v", tc.name, err)
		}
		wantPayload := transport.EncodeEnterWorldResult(transport.EnterWorldResult{OK: true, Blob: tc.want})
		if !bytes.Equal(payload, wantPayload) {
			t.Fatalf("browserEntryPayload %s differs from the Marshal envelope", tc.name)
		}
	}
}

/*
================
BenchmarkBrowserEntryPayload

The per-entry hot path after the scratch swap: pooled encode plus the
envelope's one copy. The Marshal form it replaces measured the blob
encode (BenchmarkEnterWorldBlob/V2SinglePass: 8,331 B/op) plus the
same envelope copy on top.
================
*/
func BenchmarkBrowserEntryPayload(b *testing.B) {
	character := chinaSpearman()
	character.ID = 7
	deps := testDeps(character)
	deps.ResolveDivisionID = DevResolveDivisionIDFromCatalog(deps.Characters)
	outcome := HandleEnterWorld(deps, transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(b, "0", "asd2")))
	if !outcome.OK {
		b.Fatalf("HandleEnterWorld failed: %+v", outcome.Result)
	}
	refs := &BrowserReferences{
		Path:   "/transport/references/test.json",
		SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Bytes:  1024,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := browserEntryPayload(outcome.Result, refs); err != nil {
			b.Fatal(err)
		}
	}
}
