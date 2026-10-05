/*
===========================================================================

maintenance_test.go - the maintenance list names every shard, none running

===========================================================================
*/

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestMaintenanceListShowsEveryShardChecked
================
*/
func TestMaintenanceListShowsEveryShardChecked(t *testing.T) {
	out := filepath.Join(t.TempDir(), "maintenance-servers.json")
	catalog := filepath.Join("..", "..", "..", "config", "shards.json")
	if err := runMaintenanceList([]string{"-catalog", catalog, "-out", out}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Operating     bool   `json:"operating"`
		OnlinePlayers int    `json:"onlinePlayers"`
		TransportURL  string `json:"transportUrl"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "global-official" || rows[0].TransportURL != "/shards/global-official" {
		t.Fatalf("rows = %+v", rows)
	}
	for _, row := range rows {
		if row.Operating || row.OnlinePlayers != 0 || row.Name == "" {
			t.Fatalf("maintenance row %+v must be named and not operating", row)
		}
	}
	if err := runMaintenanceList([]string{"-catalog", catalog}); err == nil {
		t.Fatal("a list without -out was accepted")
	}
}
