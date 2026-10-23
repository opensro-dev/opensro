//go:build opensro_ops

// Command sro-archive-character performs an explicit offline soft deletion.
// The final character record is preserved in deleted_characters; social edges
// and mail are healed through the same store-owned archive transaction.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
)

func main() {
	authorityDir := flag.String(
		"authority-dir",
		"",
		"stopped authority store directory (default .state/shards/<shard>/authority)",
	)
	shardID := flag.String("shard", "", "owning shard id")
	name := flag.String("character", "", "exact character name")
	commit := flag.Bool(
		"commit",
		false,
		"archive the character; without this flag only preview",
	)
	flag.Parse()
	if flag.NArg() != 0 || strings.TrimSpace(*shardID) == "" ||
		strings.TrimSpace(*name) == "" {
		fatal("-shard and -character are required; positional arguments are not accepted")
	}
	if strings.TrimSpace(*authorityDir) == "" {
		*authorityDir = store.DirForShardFromEnv(*shardID)
	}

	authority, err := store.Open(
		*authorityDir,
		store.Options{RequireStore: true},
	)
	if err != nil {
		fatal("open authority: %v", err)
	}
	defer authority.Close()

	var selected *domain.Character
	for _, character := range authority.Characters().CharactersForDivision(*shardID) {
		if character != nil && character.Name == *name {
			selected = character
			break
		}
	}
	if selected == nil {
		fatal("character %q does not exist on shard %q", *name, *shardID)
	}
	fmt.Printf(
		"Archive %s/%s (id=%d owner=%s)\n",
		*shardID,
		selected.Name,
		selected.ID,
		selected.AccountID,
	)
	if !*commit {
		fmt.Println("DRY RUN ONLY: repeat with -commit after reviewing this identity.")
		return
	}

	matured := time.Now().
		Add(-store.DeleteReservationWindow - time.Second).
		UTC().
		Format(time.RFC3339)
	if !authority.ReserveCharacterDeletion(selected, matured) {
		fatal("archive reservation refused; the character may belong to a guild/training camp or already be pending")
	}
	archived := authority.ReapMaturedDeletions()
	wanted := *shardID + "/" + selected.Name
	for _, identity := range archived {
		if identity == wanted {
			fmt.Printf("ARCHIVED: %s\n", wanted)
			return
		}
	}
	fatal("archive transaction did not include %s", wanted)
}

func fatal(format string, arguments ...any) {
	fmt.Fprintf(
		os.Stderr,
		"sro-archive-character: "+format+"\n",
		arguments...,
	)
	os.Exit(1)
}
