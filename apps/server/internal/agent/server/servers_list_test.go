/*
===========================================================================

servers_list_test.go - the public title server list

Pins what the title screen may offer: the shipped catalog's one public
shard under its catalog name, and no test shard - even from a catalog
that enables it.

===========================================================================
*/
package agentserver

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/platform/readiness"
)

/*
================
TestTitleServerListOffersOnlyThePublicShard

The shipped catalog with its test shard enabled: the shape a
host-owned catalog can carry (and the live list showed). The client's
list must hold exactly the public shard, named as the catalog names
it, operating on its fresh lease.
================
*/
func TestTitleServerListOffersOnlyThePublicShard(t *testing.T) {
	shipped, err := shard.Load(filepath.Join("..", "..", "..", "config", "shards.json"))
	if err != nil {
		t.Fatal(err)
	}
	definitions := shipped.Definitions()
	for index := range definitions {
		if definitions[index].Test {
			definitions[index].Enabled = true
		}
	}
	catalog, err := shard.NewCatalog(definitions)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := shard.NewDirectory(catalog, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	ready := readiness.NewGate()
	server, err := New(Config{
		Accounts:                loadTestAccounts(t),
		Catalog:                 catalog,
		Directory:               directory,
		SessionSigner:           testSessionSigner(t),
		ControlIdentityVerifier: testWorkloadVerifier{},
		ControlNamespace:        "sro",
		Now:                     func() time.Time { return now },
		Readiness:               ready,
		BcryptCost:              bcrypt.MinCost,
	})
	if err != nil {
		t.Fatal(err)
	}
	ready.Open()
	if err := directory.Publish(shard.Heartbeat{
		ShardID: "global-official", InstanceID: "worker-a", Sequence: 1,
	}, now); err != nil {
		t.Fatal(err)
	}

	response := performJSON(t, server.Handler(), http.MethodGet, "/title/servers", "", "")
	var servers []serverInfo
	if err := json.Unmarshal(response.Body.Bytes(), &servers); err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 {
		t.Fatalf("title server list = %+v, want exactly one row", servers)
	}
	row := servers[0]
	if row.ID != "global-official" || row.Name != "OpenSRO" || !row.Operating {
		t.Fatalf("title server row = %+v, want the operating OpenSRO shard", row)
	}
}
