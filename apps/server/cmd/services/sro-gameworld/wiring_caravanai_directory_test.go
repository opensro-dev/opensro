/*
===========================================================================

wiring_caravanai_directory_test.go - the caravan combat directory (slice 3c)

The adapter over a real caravan runtime: an agent's synthetic gid
resolves with its model radius and vitals, real damage lands through
RecordPlayerAttack, and a defeated agent stops resolving.

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravanai"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// caravanTestItems is the item source the directory test serves.
type caravanTestItems map[string]*enterworld.ItemRef

/*
==================
caravanTestItems.ItemRefByCodename
==================
*/
func (s caravanTestItems) ItemRefByCodename(codename string) (*enterworld.ItemRef, bool) {
	ref, ok := s[codename]
	return ref, ok
}

/*
==================
caravanTestItems.ItemRefByID
==================
*/
func (s caravanTestItems) ItemRefByID(id uint32) (*enterworld.ItemRef, bool) {
	for _, ref := range s {
		if ref.RefObjID == id {
			return ref, true
		}
	}
	return nil, false
}

/*
==================
TestCaravanAgentDirectory

Drives caravanAgentDirectory over the real roster: Jin resolves by
her synthetic gid as a trader with her model's radius, a scratch hit
wounds her, and a lethal one defeats her and ends resolution.
==================
*/
func TestCaravanAgentDirectory(t *testing.T) {
	rt := caravanai.NewRuntime(caravanai.Config{Enabled: true, BrainMode: caravanai.BrainHardcoded}, nil, nil, caravanai.WorldHooks{})
	const div = "directory-test"
	rt.EnsureDivisionRoster(div)
	game := &gameplayPlane{
		caravanAgents: rt,
		deps: &enterworld.Deps{
			Roster: &enterworld.Roster{Models: []enterworld.RosterModel{
				{Codename: "CHAR_CH_MAN_MERCHANT", RefObjID: 1908, BodyRadius: 4},
			}},
			Items: caravanTestItems{
				"ITEM_CH_SPEAR_07_C_RARE": {
					Codename: "ITEM_CH_SPEAR_07_C_RARE",
					Combat: &enterworld.ItemCombatRef{
						PhysicalAttack: enterworld.ItemAttackRange{
							Minimum: enterworld.ItemStatRange{Min: 100},
							Maximum: enterworld.ItemStatRange{Max: 140},
						},
					},
				},
			},
		},
	}
	dir := caravanAgentDirectory{game: game}
	gid := simulation.PlayerObjectID(caravanai.AgentCharacterIDBase)

	target, ok := dir.AgentByGID(div, gid)
	if !ok {
		t.Fatalf("Jin's gid %d did not resolve", gid)
	}
	if target.Name != "[AI]Trader_Jin" || target.JobType != domain.JobTrader {
		t.Fatalf("resolved target = %+v, want trader Jin", target)
	}
	if target.BodyRadius != 4 {
		t.Fatalf("BodyRadius = %v, want the model's 4", target.BodyRadius)
	}
	if target.HP != target.MaxHP || target.HP <= 0 || target.Level == 0 {
		t.Fatalf("vitals = %d/%d HP level %d, want full HP and a level", target.HP, target.MaxHP, target.Level)
	}
	if target.GID != gid || target.SkillID != 318 {
		t.Fatalf("identity = gid %d skill %d, want %d and Jin's strike skill 318", target.GID, target.SkillID, gid)
	}
	if target.PhysicalAttackMin != 100 || target.PhysicalAttackMax != 140 {
		t.Fatalf("weapon range = %v..%v, want the spear's 100..140", target.PhysicalAttackMin, target.PhysicalAttackMax)
	}

	if dir.ApplyAgentDamage(div, "[AI]Trader_Jin", "ViperPlayer", 100, 1_000) {
		t.Fatal("a 100-damage scratch defeated Jin")
	}
	wounded, ok := dir.AgentByGID(div, gid)
	if !ok || wounded.HP != target.HP-100 {
		t.Fatalf("after the scratch: ok=%v HP=%d, want %d", ok, wounded.HP, target.HP-100)
	}
	if !dir.ApplyAgentDamage(div, "[AI]Trader_Jin", "ViperPlayer", 999_999, 2_000) {
		t.Fatal("the lethal blow did not defeat Jin")
	}
	if _, ok := dir.AgentByGID(div, gid); ok {
		t.Fatal("the dead Jin still resolves")
	}
}
