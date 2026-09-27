// Command sro-provision-identity creates Agent's missing Ed25519 session
// signing key ring and account provisioning token without printing
// private material. Existing identity is
// validated and preserved.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"opensro.online/server/internal/cluster/provision"
)

func main() {
	stateDir := flag.String(
		"state-dir",
		filepath.Join(".state", "cluster"),
		"cluster identity state directory",
	)
	flag.Parse()
	if flag.NArg() != 0 {
		fatal("positional arguments are not accepted")
	}
	for _, ensure := range []func(string) (clusterprovision.FileResult, error){
		clusterprovision.EnsureIdentity,
		clusterprovision.EnsureProvisioningToken,
	} {
		result, err := ensure(*stateDir)
		if err != nil {
			fatal("%v", err)
		}
		if result.Created {
			fmt.Printf("created %s\n", result.Path)
		} else {
			fmt.Printf("preserved existing %s\n", result.Path)
		}
	}
}

func fatal(format string, arguments ...any) {
	fmt.Fprintf(
		os.Stderr,
		"sro-provision-identity: "+format+"\n",
		arguments...,
	)
	os.Exit(1)
}
