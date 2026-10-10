/*
===========================================================================

public_api_test.go - the privacy write against a real authority store

===========================================================================
*/
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
)

/*
================
TestSetPublicHiddenChecksTheOwnerAndPersists

Only the owning account can hide a character, a character reserved for
deletion is refused, and the flag survives a reopen.
================
*/
func TestSetPublicHiddenChecksTheOwnerAndPersists(t *testing.T) {
	const shard = "test"
	dir := t.TempDir()
	// Creation requires racial base skills; any one id satisfies the store.
	options := store.Options{DefaultSkills: func(string, []uint32) ([]uint32, error) { return []uint32{1}, nil }}
	authority, err := store.Open(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	open := true
	t.Cleanup(func() {
		if open {
			authority.Close()
		}
	})
	kekw := &domain.Character{Name: "Kekw", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	gone := &domain.Character{Name: "Gone", ModelCodename: "CHAR_CH_MAN_ADVENTURER"}
	for _, c := range []*domain.Character{kekw, gone} {
		if err := authority.CreateCharacter(shard, "alpha", c); err != nil {
			t.Fatal(err)
		}
	}
	authority.MutateCharacter(gone, "test-delete", func() { gone.DeletePending = true })

	if err := setPublicHidden(authority, shard, "beta", "Kekw", true); !errors.Is(err, publicstats.ErrNotOwned) {
		t.Fatalf("another account's write = %v, want ErrNotOwned", err)
	}
	if err := setPublicHidden(authority, shard, "alpha", "Gone", true); !errors.Is(err, publicstats.ErrNotOwned) {
		t.Fatalf("deleted character's write = %v, want ErrNotOwned", err)
	}
	if err := setPublicHidden(authority, shard, "alpha", "nobody", true); !errors.Is(err, publicstats.ErrNotOwned) {
		t.Fatalf("unknown name = %v, want ErrNotOwned", err)
	}
	if err := setPublicHidden(authority, shard, "alpha", "kekw", true); err != nil {
		t.Fatal(err)
	}
	// Writing the value it already holds is still a success.
	if err := setPublicHidden(authority, shard, "alpha", "Kekw", true); err != nil {
		t.Fatal(err)
	}
	authority.Close()
	open = false

	reopened, err := store.Open(dir, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	hidden := false
	reopened.ReadCharacters(shard, func(characters []*domain.Character) {
		for _, c := range characters {
			if c.Name == "Kekw" {
				hidden = c.PublicHidden
			}
		}
	})
	if !hidden {
		t.Fatal("the privacy flag did not survive a reopen")
	}
}

/*
================
TestKillAfterCloseIsDroppedNotPanicking

A kill that reaches the hook after Close is dropped: the recorder never
sends on its closed queue, whatever order shutdown runs in.
================
*/
func TestKillAfterCloseIsDroppedNotPanicking(t *testing.T) {
	api := &publicAPI{kills: make(chan domain.UniqueKill, 1), done: make(chan struct{})}
	close(api.done)
	if err := api.Close(); err != nil {
		t.Fatal(err)
	}
	api.enqueue(domain.UniqueKill{RefObjID: 1954})
}

/*
================
TestABadTokenRenderLeavesTheWriteOff

A secret template that rendered its missing-key placeholder, or any short
token, disables the privacy write instead of guarding it with a guessable
value; a full-length token is read back trimmed.
================
*/
func TestABadTokenRenderLeavesTheWriteOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "public-api-token")
	t.Setenv(publicstats.EnvTokenPath, path)
	for _, payload := range []string{"<no value>\n", "", "short"} {
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		if token := readPublicWriteToken(); token != "" {
			t.Fatalf("payload %q gave token %q, want the write disabled", payload, token)
		}
	}
	full := strings.Repeat("c", publicstats.MinWriteTokenBytes)
	if err := os.WriteFile(path, []byte(full+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if token := readPublicWriteToken(); token != full {
		t.Fatalf("full token read as %q", token)
	}
}
