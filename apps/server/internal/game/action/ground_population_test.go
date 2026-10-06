package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

func TestGroundPopulationRequiresLiveAdmission(t *testing.T) {
	rt, c := newActiveEffectTestRuntime(t, staticSkillSource{})
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	main := rt.addCharacterGround(testDivision, c, grounditem.Item{})
	id := instance.Pack(10, 1)
	packed := uint32(id)
	c.World = &domain.CharacterWorld{PackedInstance: &packed}
	lease, _ := rt.Monsters.AllocatePopulation(testDivision, id)
	if item := rt.addCharacterGround(testDivision, c, grounditem.Item{}); item.Gid != 0 {
		t.Fatal("unadmitted producer")
	}
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	foreign := rt.addCharacterGround(testDivision, c, grounditem.Item{})
	if foreign.Gid == 0 {
		t.Fatal("admitted drop refused")
	}
	if _, ok := rt.characterGround(testDivision, c, main.Gid); ok {
		t.Fatal("main-world pickup leaked")
	}
	if items := rt.CharacterGroundItems(testDivision, c); len(items) != 1 || items[0].Gid != foreign.Gid {
		t.Fatal(items)
	}
	rt.Monsters.ReleasePopulation(testDivision, lease)
	rt.Monsters.AllocatePopulation(testDivision, id)
	if _, ok := rt.characterGround(testDivision, c, foreign.Gid); ok {
		t.Fatal("stale admission retrieved retired item")
	}
	if len(rt.CharacterGroundItems(testDivision, c)) != 0 {
		t.Fatal("stale bootstrap")
	}
}

func TestGroundPickupCannotUsePartyOwnershipAcrossWorlds(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, testItems())
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(nil, nil))
	drop := ownedGoldAtPlayer(t, rt, clock, c, 100099)
	rt.CanPickupOwnedDrop = func(string, string, uint32) bool { return true }
	packed := uint32(instance.Pack(10, 1))
	if c.World == nil {
		c.World = &domain.CharacterWorld{}
	}
	c.World.PackedInstance = &packed
	rt.Monsters.AllocatePopulation(testDivision, instance.ID(packed))
	if err := rt.AdmitCharacterSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	before := goldOf(c)
	result := rt.HandleTargetInteract(testDivision, c, wire.TargetInteract{Gid: drop.Gid}.Encode())
	assertOpcodes(t, result.Frames, wire.OpItemMoveResponse, wire.OpActionState)
	if goldOf(c) != before {
		t.Fatal("cross-world pickup credited gold")
	}
	if _, ok := rt.Ground.Get(testDivision, drop.Gid); !ok {
		t.Fatal("cross-world pickup removed item")
	}
}
