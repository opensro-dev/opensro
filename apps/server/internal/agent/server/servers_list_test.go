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

/*
================
TestTitleServerListComingSoonShards

Port-only, not native (#3925): an announced shard the operator has not
opened (a disabled catalog entry) is invisible with
SRO_COMING_SOON_SHARDS off - the native list - and lists with it on,
marked coming soon, carrying no transport URL, and still refused at
login. A test shard stays unlisted either way.
================
*/
func TestTitleServerListComingSoonShards(t *testing.T) {
	catalog, err := shard.NewCatalog([]shard.Definition{
		{
			ID: "open", Name: "Open", NativeServerID: 1, Capacity: 10, Default: true, Enabled: true,
			ControlURL: "http://127.0.0.1:9001", TransportURL: "https://127.0.0.1:9002",
		},
		{
			ID: "announced", Name: "Announced", NativeServerID: 2, Capacity: 10, Enabled: false,
			ControlURL: "http://127.0.0.1:9011", TransportURL: "https://127.0.0.1:9012",
		},
		{
			ID: "fixture", Name: "Fixture", NativeServerID: 3, Capacity: 10, Enabled: true, Test: true,
			ControlURL: "http://127.0.0.1:9021", TransportURL: "https://127.0.0.1:9022",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	listServer := func(t *testing.T, comingSoon bool) *Server {
		t.Helper()
		directory, err := shard.NewDirectory(catalog, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
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
			ComingSoonShards:        comingSoon,
		})
		if err != nil {
			t.Fatal(err)
		}
		ready.Open()
		return server
	}
	listRows := func(t *testing.T, server *Server) []serverInfo {
		t.Helper()
		response := performJSON(t, server.Handler(), http.MethodGet, "/title/servers", "", "")
		var rows []serverInfo
		if err := json.Unmarshal(response.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}

	off := listRows(t, listServer(t, false))
	if len(off) != 1 || off[0].ID != "open" || off[0].ComingSoon {
		t.Fatalf("flag off rows = %+v, want only the open shard, unmarked", off)
	}

	onServer := listServer(t, true)
	on := listRows(t, onServer)
	if len(on) != 2 {
		t.Fatalf("flag on rows = %+v, want the open and announced shards", on)
	}
	soon := on[1]
	if soon.ID != "announced" || !soon.ComingSoon || soon.Operating || soon.TransportURL != "" {
		t.Fatalf("coming soon row = %+v, want announced, marked, not operating, no transport URL", soon)
	}

	login := performJSON(t, onServer.Handler(), http.MethodPost, "/title/login",
		`{"id":"tester","password":"123123","serverId":"announced"}`, "")
	body := decodeObject(t, login)
	if body["ok"] != false || body["code"] != "SHARD_OFFLINE" {
		t.Fatalf("coming soon login = %#v, want SHARD_OFFLINE", body)
	}
}
