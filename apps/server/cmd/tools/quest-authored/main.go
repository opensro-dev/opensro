/*
===========================================================================

main.go - export the quest owner's registered, non-compiled codenames

The Python compiler consumes this derived snapshot rather than interpreting
Go source. No game data, runtime startup or research directory is required.

===========================================================================
*/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"opensro.online/server/internal/game/quest"
)

const authoredMetadataFileMode = 0o644

/*
================
writeMetadata
================
*/
func writeMetadata(output string) error {
	codes, err := quest.AuthoredQuestCodenames()
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(codes, "", "\t")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if output == "" {
		_, err = os.Stdout.Write(body)
		return err
	}
	return os.WriteFile(output, body, authoredMetadataFileMode)
}

/*
================
main
================
*/
func main() {
	flags := flag.NewFlagSet("quest-authored", flag.ContinueOnError)
	output := flags.String("output", "", "write the UTF-8 snapshot to this path instead of stdout")
	if err := flags.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := writeMetadata(*output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
