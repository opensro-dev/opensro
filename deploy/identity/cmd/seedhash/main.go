/*
===========================================================================

seedhash - bcrypt one password from stdin, for the local smoke test

The upstream hash-agent-password refuses a non-terminal stdin, which is
right for a person and impossible for deploy/test/smoke.sh. This prints
the same kind of hash (bcrypt at the catalog's MaxBcryptCost) for a
password piped in. Never used on the server.

===========================================================================
*/
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"opensro.online/server/internal/security/auth"
)

/*
================
main
================
*/
func main() {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(os.Stderr, "seedhash: no password on stdin")
		os.Exit(1)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(strings.TrimRight(line, "\r\n")), auth.MaxBcryptCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seedhash: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(hash))
}
