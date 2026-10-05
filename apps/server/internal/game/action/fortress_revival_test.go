package action

import (
	"testing"

	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
TestFortressWarRevivesBySide

601700 during Jangan's war: the holder's guild revives at
GATE_JA_REVIVAL_GATE inside the fortress, everyone else at one of the
field gates that lead in; outside a war the appointed town stands. The
revival grace lasts 11 seconds in a siege world (4DF484).
================
*/
func TestFortressWarRevivesBySide(t *testing.T) {
	rt, c, _ := fortressFixtureWithClock(t, testFieldFortGate)
	rt.PushCharacterFrames, rt.PushDivisionPeerFrames = nil, nil
	enterFortress(t, rt, c)
	if _, ok := rt.fortressRevival(testDivision, c); ok {
		t.Fatal("a fortress revival outside the war")
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodWar, true)
	rt.RevivalRoll = func() (uint32, error) { return 4, nil }
	attacker, ok := rt.fortressRevival(testDivision, c)
	if !ok || attacker.world != instance.Pack(1, portalWorldLayer) {
		t.Fatalf("a guildless PC revived at %+v", attacker)
	}
	guild := int64(77)
	c.GuildID = &guild
	var jangan uint32
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	rt.Fortresses.Capture(testDivision, jangan, guild, 0)
	holder, ok := rt.fortressRevival(testDivision, c)
	if !ok || holder.spawn.RegionID != 17735 || holder.world != instance.Pack(2, portalWorldLayer) {
		t.Fatalf("the holder revived at %+v, want GATE_JA_REVIVAL_GATE", holder)
	}
	frames := rt.grantReviveUntouchable(testDivision, c, 1000)
	if len(frames) == 0 {
		t.Fatal("no revival grace")
	}
	if early := rt.bodyRestores.due(1000 + siegeReviveUntouchableMs - 1); len(early) != 0 {
		t.Fatal("the siege revival grace ended at the field's six seconds")
	}
	if ended := rt.bodyRestores.due(1000 + siegeReviveUntouchableMs); len(ended) != 1 {
		t.Fatal("the siege revival grace did not end at eleven seconds")
	}
}
