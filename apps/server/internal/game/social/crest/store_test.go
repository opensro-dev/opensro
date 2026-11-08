/*
===========================================================================

store_test.go - the crest store: content-addressed payloads, the
standing-crest index, revisions that are never reused, and the rate
limit

#2282 (register half): only re-encoded bytes land in the store, named
by their hash; the index remembers which hash a guild's or union's
standing revision holds and the highest revision ever issued for it,
so a register after a delete is never named for art a browser may
hold cached under the same immutable URL.

===========================================================================
*/
package crest

import (
	"bytes"
	"os"
	"testing"
	"time"
)

/*
================
storePayload

One distinct 256-byte payload per seed.
================
*/
func storePayload(seed byte) []byte {
	payload := make([]byte, CrestPayloadBytes)
	for i := range payload {
		payload[i] = seed
	}
	return payload
}

/*
================
TestStoreSaveServeRoundTrip

A saved payload resolves only under its standing revision, and
nothing else does - a replaced or never-issued revision is not
hosted, and a hash that names no stored payload serves nothing.
================
*/
func TestStoreSaveServeRoundTrip(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := storePayload(3)
	hash, err := store.Save(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 64 {
		t.Fatalf("hash = %q, want a SHA-256 hex name", hash)
	}
	if _, _, ok := store.ServeCrest(KindGuild, 12, 1); ok {
		t.Fatal("an unindexed payload served")
	}
	if err := store.SetCurrent(KindGuild, 12, Ref{Revision: 1, Hash: hash}); err != nil {
		t.Fatal(err)
	}
	payload, gotHash, ok := store.ServeCrest(KindGuild, 12, 1)
	if !ok || gotHash != hash || !bytes.Equal(payload, first) {
		t.Fatalf("serve = %v, %q, %t; want the saved payload", payload, gotHash, ok)
	}
	if _, _, ok := store.ServeCrest(KindGuild, 12, 2); ok {
		t.Fatal("an unissued revision served")
	}
	if _, _, ok := store.ServeCrest(KindUnion, 12, 1); ok {
		t.Fatal("the guild's crest served as a union's")
	}
	if _, ok := store.Payload("zz"); ok {
		t.Fatal("a non-hash name served")
	}
	if _, ok := store.Payload(hash); !ok {
		t.Fatal("a stored payload did not read back")
	}
}

/*
================
TestStoreRevisionsAreNeverReused

After a delete the wire revision returns to 0, but the next register
takes one past the highest ever issued - and the high-water mark
survives a reopen.
================
*/
func TestStoreRevisionsAreNeverReused(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := store.Save(storePayload(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := store.NextRevision(KindGuild, 9, 0); got != 1 {
		t.Fatalf("first revision = %d, want 1", got)
	}
	if err := store.SetCurrent(KindGuild, 9, Ref{Revision: 1, Hash: hash}); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveCurrent(KindGuild, 9); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Current(KindGuild, 9); ok {
		t.Fatal("a removed crest still stands")
	}
	if got := store.NextRevision(KindGuild, 9, 0); got != 2 {
		t.Fatalf("revision after a delete = %d, want 2", got)
	}
	// Host art the index never saw counts too: the record's own
	// standing revision is a floor.
	if got := store.NextRevision(KindUnion, 9, 41); got != 42 {
		t.Fatalf("revision past a standing record = %d, want 42", got)
	}

	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.NextRevision(KindGuild, 9, 0); got != 2 {
		t.Fatalf("revision after a reopen = %d, want 2", got)
	}
}

/*
================
TestStoreIndexSurvivesReopen

The standing crest (revision and hash) reads back from a fresh store
over the same directory, and its payload still serves.
================
*/
func TestStoreIndexSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	payload := storePayload(7)
	hash, err := store.Save(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrent(KindUnion, 4, Ref{Revision: 3, Hash: hash}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := reopened.Current(KindUnion, 4)
	if !ok || ref != (Ref{Revision: 3, Hash: hash}) {
		t.Fatalf("standing crest = %+v (%t), want revision 3 at %q", ref, ok, hash)
	}
	served, _, ok := reopened.ServeCrest(KindUnion, 4, 3)
	if !ok || !bytes.Equal(served, payload) {
		t.Fatal("the reopened store did not serve the standing payload")
	}
	// Only the payload file and the index live in the directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{hash + ".crb": true, "crest-index.json": true}
	for _, entry := range entries {
		if !want[entry.Name()] {
			t.Fatalf("unexpected store file %q", entry.Name())
		}
	}
}

/*
================
TestStoreRateLimit

One change per cooldown per crest: a change lands, the next inside
the window refuses, one after it passes - and another crest's clock
is its own.
================
*/
func TestStoreRateLimit(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	if !store.AllowChange(KindGuild, 1, now) {
		t.Fatal("a first change was refused")
	}
	store.MarkChange(KindGuild, 1, now)
	if store.AllowChange(KindGuild, 1, now.Add(ChangeCooldown-time.Second)) {
		t.Fatal("a change inside the cooldown was allowed")
	}
	if !store.AllowChange(KindGuild, 1, now.Add(ChangeCooldown)) {
		t.Fatal("a change at the cooldown was refused")
	}
	if !store.AllowChange(KindUnion, 1, now) {
		t.Fatal("the union's clock ran on the guild's change")
	}
}
