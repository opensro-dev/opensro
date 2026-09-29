package action

import (
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

func TestDevelopmentFixtureUsesAuthoredDelayedSummonAndCleansFamily(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	refs := monster.LoadMonsterRefs(dir)
	skills := enterworld.NewTextdataSkills(dir)
	var nests []monster.NestRow
	for _, ref := range refs {
		if ref.MonsterType == 3 {
			nests = append(nests, monster.NestRow{SpawnPoint: monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: 0x62aa, X: 960, Z: 960}})
		}
	}
	state := simulation.NewMonsterState(monster.TemplateFromParts(refs, nests))
	state.SetRandomSource(func() float64 { return 0 })
	rt := NewRuntime(&enterworld.Deps{Skills: skills}, state)
	rt.Now = func() time.Time { return time.UnixMilli(100000) }
	var frames []wire.Frame
	rt.PushDivisionPeerFrames = func(_, _ string, got []wire.Frame) { frames = append(frames, got...) }
	ref, err := rt.DevelopmentSummonReference()
	if err != nil {
		t.Fatal(err)
	}
	parent, err := state.DevelopmentCreateLeader("fixture", ref.RefObjID, monster.Pose{RegionID: 0x62aa, X: 960, Z: 960}, 200000)
	if err != nil {
		t.Fatal(err)
	}
	skillID, err := rt.DevelopmentStartSummon("fixture", parent.Gid)
	if err != nil {
		t.Fatal(err)
	}
	skill, _ := skills.SkillByID(skillID)
	if len(frames) == 0 || frames[0].Opcode != 0xb245 {
		t.Fatal("real cast encoder not used")
	}
	if skill.ActionCastingTimeMs > 0 {
		state.AdvanceSummons(100000 + int64(skill.ActionCastingTimeMs) - 1)
		if len(state.MaterializedInstances("fixture")) != 1 {
			t.Fatal("wave released before authored casting boundary")
		}
	}
	state.AdvanceSummons(100000 + int64(skill.ActionCastingTimeMs))
	family := state.DevelopmentFollowSnapshot("fixture", parent.Gid, 100000+int64(skill.ActionCastingTimeMs))
	if len(family) < 2 {
		t.Fatal("authored wave has no children")
	}
	for _, child := range family {
		if child.GID != parent.Gid && child.Summoner != parent.Gid {
			t.Fatal("lost production relationship")
		}
	}
	state.DevelopmentRemoveFamily("fixture", parent.Gid)
	state.AdvanceSummons(300000)
	if len(state.MaterializedInstances("fixture")) != 0 {
		t.Fatal("fixture family leaked or respawned")
	}
	t.Logf("reference=%s skill=%d actors=%d casting=%d", ref.Codename, skillID, len(family), skill.ActionCastingTimeMs)
}
