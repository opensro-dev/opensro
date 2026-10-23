//go:build opensro_ops

// Command sro-account-owner previews or commits an offline, atomic transfer of
// every live character from one login account to another. It exists for
// explicit account administration; it never edits SQLite directly.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
)

func main() {
	authorityDir := flag.String("authority-dir", "", "authority store directory (default .state/shards/<shard>/authority)")
	shardID := flag.String("shard", "", "owning shard id (required)")
	fromAccount := flag.String("from", "", "current owner account id (required)")
	toAccount := flag.String("to", "", "new owner account id (required)")
	commit := flag.Bool("commit", false, "commit the transfer; without this flag the command only previews")
	flag.Parse()

	if flag.NArg() != 0 {
		fatal("unexpected positional arguments: %v", flag.Args())
	}
	if strings.TrimSpace(*shardID) == "" {
		fatal("-shard is required")
	}
	if strings.TrimSpace(*authorityDir) == "" {
		*authorityDir = store.DirForShardFromEnv(*shardID)
	}
	if !domain.AccountIDValid(*toAccount) {
		fatal("-to is required and must be an unpadded account id of at most %d bytes with no control text", domain.AccountIDMaxBytes)
	}
	if !domain.AccountIDValid(*fromAccount) {
		fatal("-from must be an unpadded account id of at most %d bytes with no control text", domain.AccountIDMaxBytes)
	}
	if *fromAccount == *toAccount {
		fatal("-from and -to must differ")
	}

	authority, err := store.Open(*authorityDir, store.Options{RequireStore: true})
	if err != nil {
		fatal("open authority store: %v\nStop the shard's GameWorld first; this command requires exclusive ownership of its authority store.", err)
	}
	defer authority.Close()
	if err := authority.ValidateShardState([]string{*shardID}); err != nil {
		fatal("authority does not exclusively own shard %q: %v", *shardID, err)
	}

	absoluteDir, err := filepath.Abs(*authorityDir)
	if err != nil {
		absoluteDir = *authorityDir
	}
	matches := ownedBy(authority.CharacterOwnerships(), *fromAccount)
	if len(matches) == 0 {
		fatal("no live characters in %s belong to %q", absoluteDir, *fromAccount)
	}

	fmt.Printf("Authority: %s\n", absoluteDir)
	fmt.Printf("Transfer:  %q -> %q\n", *fromAccount, *toAccount)
	printOwnerships(matches)
	if !*commit {
		fmt.Printf("\nDRY RUN ONLY: nothing changed.\n")
		fmt.Printf("Review the rows above, then repeat with -commit.\n")
		return
	}

	transferred, err := authority.TransferCharacterOwnership(*fromAccount, *toAccount)
	if err != nil {
		fatal("%v", err)
	}
	fmt.Printf("\nCOMMITTED: %d character(s) now belong to %q.\n", len(transferred), *toAccount)
	printOwnerships(transferred)
}

func ownedBy(ownerships []store.CharacterOwnership, accountID string) []store.CharacterOwnership {
	matches := make([]store.CharacterOwnership, 0)
	for _, ownership := range ownerships {
		if ownership.AccountID == accountID {
			matches = append(matches, ownership)
		}
	}
	return matches
}

func printOwnerships(ownerships []store.CharacterOwnership) {
	for _, ownership := range ownerships {
		fmt.Printf("  %s  id=%d  character=%q\n", ownership.DivisionID, ownership.CharacterID, ownership.CharacterName)
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "sro-account-owner: "+format+"\n", args...)
	os.Exit(1)
}
