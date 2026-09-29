package enterworld

import (
	"errors"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/worldarea"
)

func shippedAuthoredAreaCatalog(t *testing.T) *worldarea.Catalog {
	t.Helper()
	path := filepath.Join(
		gamedatatest.Paths(t).BundleRoot,
		"world-authority",
		"areas",
		"catalog.json",
	)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("server authored-area projection is unavailable: %v", err)
		}
		t.Fatalf("stat server authored-area projection: %v", err)
	}
	catalog, err := worldarea.LoadAuthority(filepath.Join(gamedatatest.Paths(t).BundleRoot, "world-authority"))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func TestAppendAuthoredAreaPopulationUsesNormalTemplateWithoutForgingRetailEvidence(t *testing.T) {
	catalog := shippedAuthoredAreaCatalog(t)
	template := monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG", WalkSpeed: 8},
	}, nil)
	composed, err := appendAuthoredAreaPopulation(template, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(composed.Nests) != 1 {
		t.Fatalf("authored nests = %d, want 1", len(composed.Nests))
	}
	nest := composed.Nests[0]
	if nest.RegionID != 0x7e7e || nest.RefObjID != 1933 || nest.MaxCount != 1 ||
		!nest.PolicyPinned || nest.RetailEvidence {
		t.Fatalf("authored nest = %+v", nest)
	}
	registry := simulation.NewMonsterState(composed)
	registry.StartDivision("test")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("test", []uint16{0x7e7e})
	if len(instances) != 1 || instances[0].Ref.Codename != "MOB_CH_MANGNYANG" {
		t.Fatalf("normal registry instances = %+v", instances)
	}
}

func TestAppendAuthoredAreaPopulationRejectsMissingMonsterReference(t *testing.T) {
	catalog := shippedAuthoredAreaCatalog(t)
	if _, err := appendAuthoredAreaPopulation(monster.TemplateFromParts(nil, nil), catalog); err == nil {
		t.Fatal("unknown authored population codename was silently skipped")
	}
}
