/*
===========================================================================

main.go - explicit offline authority upgrade (older schemas to current)

Converts schemas store.UpgradeFromVersion through the version before
store.CurrentVersion to the current one; the help text derives the range
from those constants so it cannot go stale. Defaults to validation. Stop
the game server before using -commit; the store lock enforces this
requirement. The upgrade preserves all existing records.

===========================================================================
*/
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"opensro.online/server/internal/data/store"
)

/*
================
main
================
*/
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

/*
================
run
================
*/
func run() error {
	dir := flag.String("authority-dir", "", fmt.Sprintf("existing authority directory (supported schemas %d through %d)",
		store.UpgradeFromVersion, store.CurrentVersion-1))
	commit := flag.Bool("commit", false, "retain a backup and commit the current authority schema")
	flag.Parse()
	if *dir == "" || flag.NArg() != 0 {
		return fmt.Errorf("usage: sro-authority-upgrade -authority-dir PATH [-commit]")
	}
	backup, err := store.UpgradeAuthority(*dir, *commit)
	if backup != "" {
		fmt.Println("Upgrade backup path:", backup)
	}
	if errors.Is(err, store.ErrAuthorityCurrent) {
		// A retried release: the upgrade already committed.
		fmt.Println("Authority already in the current format; nothing to upgrade.")
		return nil
	}
	if err != nil {
		return err
	}
	if *commit {
		fmt.Println("Authority upgraded to the current format and validated; existing records preserved.")
	} else {
		fmt.Println("Source authority validated; rerun with -commit to upgrade.")
	}
	return nil
}
