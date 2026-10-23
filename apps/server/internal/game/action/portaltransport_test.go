/*
===========================================================================

portaltransport_test.go - the server's transport restriction per link

===========================================================================
*/
package action

import (
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
writePortalTransport
================
*/
func writePortalTransport(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(PortalTransportFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

/*
================
sourceDataPortalTransport

The operator's table laid out as the projection's derived data would hold
it (skips without it).
================
*/
func sourceDataPortalTransport(t *testing.T) string {
	t.Helper()
	return writePortalTransport(t, licensed.SourceDataFile(t, "server/"+PortalTransportFile))
}

/*
================
TestPortalTransportTableOverridesTheClientKind

A link the table has takes the server's answer either way; a link it lacks,
or every link without a table, keeps the client's kind.
================
*/
func TestPortalTransportTableOverridesTheClientKind(t *testing.T) {
	dir := writePortalTransport(t, []byte(`{"format":"sro-portal-transport-v1","links":[["GATE_A","GATE_B",true],["GATE_B","GATE_A",false]]}`))
	table, err := loadPortalTransport(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source, target string
		client, want   bool
	}{
		{"GATE_A", "GATE_B", false, true},
		{"GATE_B", "GATE_A", true, false},
		{"GATE_A", "GATE_C", true, true},
		{"GATE_A", "GATE_C", false, false},
	} {
		if got := table.refuses(tc.source, tc.target, tc.client); got != tc.want {
			t.Fatalf("%s->%s (client %v) refuses %v, want %v", tc.source, tc.target, tc.client, got, tc.want)
		}
	}
	var none portalTransport
	if !none.refuses("GATE_A", "GATE_B", true) || none.refuses("GATE_A", "GATE_B", false) {
		t.Fatal("no table did not keep the client's kind")
	}
	if absent, err := loadPortalTransport(t.TempDir()); absent != nil || err != nil {
		t.Fatalf("absent table: %v, %v", absent, err)
	}
}

/*
================
TestPortalTransportRefusesBadTables
================
*/
func TestPortalTransportRefusesBadTables(t *testing.T) {
	for _, bad := range []string{
		`{"format":"other","links":[]}`,
		`{"format":"sro-portal-transport-v1","links":[["GATE_A","",true]]}`,
		`{"format":"sro-portal-transport-v1","links":[["GATE_A","GATE_B","yes"]]}`,
		`{"format":"sro-portal-transport-v1","links":[["GATE_A","GATE_B",true],["GATE_A","GATE_B",false]]}`,
	} {
		if _, err := loadPortalTransport(writePortalTransport(t, []byte(bad))); err == nil {
			t.Fatalf("admitted %s", bad)
		}
	}
}
