/*
===========================================================================

caravan_entities_test.go - the agents' appearance data is shipped data

Slice 3a (#3704) shows each AI agent as a peer wearing a character
model, a job suit and a weapon named by codename on its roster
record. If any of those codenames stopped resolving against the
shipped data the agent would silently never appear, so this test
resolves all of them against the real character-authority catalogue
and textdata, the same sources the gameworld wiring reads.

===========================================================================
*/
package movement

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
==================
TestCaravanAgentAppearanceResolves

Every roster agent's model codename is in the shipped character
catalogue, and its suit and weapon codenames are shipped items whose
type ids pack into a spawn-row equipment entry.
==================
*/
func TestCaravanAgentAppearanceResolves(t *testing.T) {
	paths := gamedatatest.Paths(t)
	roster, err := enterworld.LoadRoster(filepath.Join(paths.CharacterAuthorityDir, "catalog.json"))
	if err != nil {
		t.Fatalf("load character catalogue: %v", err)
	}
	items := enterworld.NewTextdataItems(paths.TextdataDir)

	rt := caravanai.NewRuntime(
		caravanai.Config{Enabled: true, BrainMode: caravanai.BrainHardcoded},
		nil, caravanai.WorldHooks{},
	)
	entities := rt.AgentEntities("caravan-appearance")
	if len(entities) == 0 {
		t.Fatal("no agent entities")
	}
	for _, entity := range entities {
		model := roster.ModelByCodename(entity.ModelCode)
		if model == nil || model.RefObjID == 0 {
			t.Errorf("%s model %q not in the shipped character catalogue", entity.Name, entity.ModelCode)
		}
		for _, gear := range []struct{ label, code string }{
			{"suit", entity.SuitCode},
			{"weapon", entity.WeaponCode},
		} {
			ref, ok := items.ItemRefByCodename(gear.code)
			if !ok || ref.RefObjID == 0 {
				t.Errorf("%s %s %q is not a shipped item", entity.Name, gear.label, gear.code)
				continue
			}
			if ref.Codename != gear.code {
				t.Errorf("%s %s resolved to %q, want %q", entity.Name, gear.label, ref.Codename, gear.code)
			}
			if gear.label == "suit" {
				flags := wire.PackTypeFlags(
					uint8(ref.TypeIDs[0]), uint8(ref.TypeIDs[1]),
					uint8(ref.TypeIDs[2]), uint8(ref.TypeIDs[3]))
				if job := enterworld.JobSuitJob(flags); job != entity.JobType {
					t.Errorf("%s suit %q dresses job %d, want the agent's job %d",
						entity.Name, gear.code, job, entity.JobType)
				}
			}
		}
	}
}
