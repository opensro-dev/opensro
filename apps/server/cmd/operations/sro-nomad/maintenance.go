/*
===========================================================================

maintenance.go - the server list the edge serves while the Agent is down

A release stops every job, the Agent included, so the title's server list
request fails during maintenance. The release deploy writes this list from
the host's own shard catalog through the Agent's own row mapping
(agentserver.OfflineServerList): every shard listed, none operating. The
HTTPS edge serves it for /api/title/servers when the Agent answers 502,
503 or 504, so the title shows each shard as native "Check".

===========================================================================
*/

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	agentserver "opensro.online/server/internal/agent/server"
	"opensro.online/server/internal/cluster/shard"
)

/*
================
runMaintenanceList
================
*/
func runMaintenanceList(arguments []string) error {
	flags := flag.NewFlagSet("maintenance-list", flag.ContinueOnError)
	catalogPath := flags.String("catalog", filepath.Join("config", "shards.json"), "shard catalog")
	output := flags.String("out", "", "file to write (replaced atomically)")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *output == "" {
		return fmt.Errorf("maintenance-list needs -out and no positional arguments")
	}
	catalog, err := shard.Load(*catalogPath)
	if err != nil {
		return err
	}
	body, err := agentserver.OfflineServerList(catalog, time.Now())
	if err != nil {
		return err
	}
	return writeFileAtomically(*output, append(body, '\n'), 0o644)
}

/*
================
writeFileAtomically

The edge may read the file at any moment: write beside it, then rename.
================
*/
func writeFileAtomically(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.incoming")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
