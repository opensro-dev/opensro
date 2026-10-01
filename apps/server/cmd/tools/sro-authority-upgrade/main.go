/*
===========================================================================

main.go - explicit offline Item Mall authority upgrade

Defaults to validation. Stop the game server before using -commit; the store
lock enforces this requirement. The upgrade preserves all existing records.

===========================================================================
*/
package main

import (
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
	dir := flag.String("authority-dir", "", "existing layout-4 authority directory")
	commit := flag.Bool("commit", false, "retain a backup and commit the layout-5 upgrade")
	flag.Parse()
	if *dir == "" || flag.NArg() != 0 {
		return fmt.Errorf("usage: sro-authority-upgrade -authority-dir PATH [-commit]")
	}
	backup, err := store.UpgradeMallAuthority(*dir, *commit)
	if backup != "" {
		fmt.Println("Upgrade backup path:", backup)
	}
	if err != nil {
		return err
	}
	if *commit {
		fmt.Println("Authority layout 5 committed and validated; existing records preserved.")
	} else {
		fmt.Println("Authority layout 4 validated; rerun with -commit to upgrade.")
	}
	return nil
}
