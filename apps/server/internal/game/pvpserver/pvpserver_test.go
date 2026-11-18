/*
===========================================================================

pvpserver_test.go - the PVP switch acts only on the pvp shard

===========================================================================
*/

package pvpserver

import "testing"

func TestEnabledOnlyOnThePvpShard(t *testing.T) {
	cases := []struct {
		shard, value string
		want         bool
		fails        bool
	}{
		{"pvp", "on", true, false},
		{"pvp", "true", true, false},
		{"pvp", "1", true, false},
		{"global-official", "on", false, false},
		{"pvp", "", false, false},
		{"pvp", "off", false, false},
		{"pvp", "maybe", false, true},
	}
	for _, c := range cases {
		getenv := func(key string) string {
			if key == EnvPvpServer {
				return c.value
			}
			return ""
		}
		got, err := Enabled(c.shard, getenv)
		if (err != nil) != c.fails {
			t.Fatalf("Enabled(%q, %q) error = %v, want error %v", c.shard, c.value, err, c.fails)
		}
		if got != c.want {
			t.Fatalf("Enabled(%q, %q) = %v, want %v", c.shard, c.value, got, c.want)
		}
	}
}

func TestEnabledFromEnvReadsTheShardID(t *testing.T) {
	t.Setenv(EnvPvpServer, "on")
	t.Setenv(envShardID, "pvp")
	if !EnabledFromEnv() {
		t.Fatal("EnabledFromEnv() = false on the pvp shard")
	}
	t.Setenv(envShardID, "global-official")
	if EnabledFromEnv() {
		t.Fatal("EnabledFromEnv() = true on another shard")
	}
}

func TestSamarkandSpawnIsTheGateArrival(t *testing.T) {
	a, b := SamarkandSpawn(), SamarkandSpawn()
	if *a.RegionID != 27244 || *a.X != 270 || *a.Y != 180 || *a.Z != 1421 {
		t.Fatalf("SamarkandSpawn = %d %v %v %v", *a.RegionID, *a.X, *a.Y, *a.Z)
	}
	if a.RegionID == b.RegionID || a.X == b.X {
		t.Fatal("SamarkandSpawn shares pointers between calls")
	}
}
