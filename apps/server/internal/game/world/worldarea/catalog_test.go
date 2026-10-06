package worldarea

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

func TestLiveAuthoredAreaCatalogPinsManyangLabContract(t *testing.T) {
	publicRoot, err := licensed.ClientPublicRoot()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(publicRoot, filepath.FromSlash(CatalogPublicPath))); err != nil {
		t.Skipf("client public assets unavailable: %v", err)
	}
	catalog, err := Load(publicRoot)
	if err != nil {
		t.Fatal(err)
	}
	area, ok := catalog.Resolve("MANYANG-LAB")
	if !ok || area.RegionID != 0x7e7e || area.Access != "gm" || len(area.Population) != 1 ||
		area.Population[0].Codename != "MOB_CH_MANGNYANG" {
		t.Fatalf("manyang-lab = %+v, %v", area, ok)
	}
	if catalog.CanEnterRegion(area.RegionID, false) || !catalog.CanEnterRegion(area.RegionID, true) {
		t.Fatal("GM access grade is not enforced")
	}
	if !catalog.CanEnterRegion(0x62a8, false) {
		t.Fatal("ordinary world region was captured by authored-area policy")
	}

	copy := catalog.Areas()
	copy[0].Population[0].Codename = "MUTATED"
	again, _ := catalog.Resolve("manyang-lab")
	if again.Population[0].Codename != "MOB_CH_MANGNYANG" {
		t.Fatal("catalog exposed mutable population backing storage")
	}
}

func TestLoadRejectsCatalogBundlePopulationDrift(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(root, "assets", "world", "test", "region.json")
	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o755); err != nil {
		t.Fatal(err)
	}
	area := Area{
		Slug: "test", RegionID: 0x1010, Access: "gm",
		Entry:                  Spawn{X: 10, Z: 10},
		Population:             []Population{{Codename: "MOB_A", X: 20, Z: 20, MaxCount: 1}},
		WorldRegionsPublicPath: "/assets/world/test/index.json",
		BundlePublicPath:       "/assets/world/test/region.json",
	}
	writeJSON := func(path string, value any) {
		contents, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	drift := area
	drift.Population = append([]Population(nil), area.Population...)
	drift.Population[0].MaxCount = 2
	writeJSON(bundlePath, bundleFile{AuthoredArea: drift})
	writeJSON(filepath.Join(root, CatalogPublicPath), catalogFile{
		Format: "sro-authored-world-area-catalog", Version: 1, Areas: []Area{area},
	})
	if _, err := Load(root); err == nil {
		t.Fatal("population drift was accepted")
	}
}

func TestValidPublicAssetPath(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"/assets/world/test/region.json":       true,
		"assets/world/test/region.json":        false,
		"/assets/world/":                       false,
		"/assets/world/../secret.json":         false,
		"/assets/world/test/../../secret.json": false,
		`/assets/world/test\..\secret.json`:    false,
		" /assets/world/test/region.json":      false,
		"/assets/world//test/region.json":      false,
	}
	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if got := validPublicAssetPath(value); got != want {
				t.Fatalf("validPublicAssetPath(%q) = %v, want %v", value, got, want)
			}
		})
	}
}
