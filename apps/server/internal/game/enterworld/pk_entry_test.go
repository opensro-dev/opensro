package enterworld

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"
)

func TestLocalEntryProjectsCriminalRecordAndEventTeam(t *testing.T) {
	c := chinaSpearman()
	c.PK = &domain.PKRecord{DailyCount: 3, TotalCount: 5, Penalty: 3600}
	c.EventMembership = &domain.EventMembership{ID: 7, Team: 0}
	entry := ResolveLocalPlayerEntry(c, testRoster())
	if entry.PVPState != 2 || entry.ArenaTeam != 0 {
		t.Fatalf("bootstrap state disagrees with authority: %+v", entry)
	}
	p := BuildLocalPlayerEntryPayload(c, &entry, 0, nil)
	// Client 863880's fixed base record ends with visual flags, daily PK u8,
	// total PK u16 and penalty u32, before the variable inventory block.
	const dailyOffset = 47
	if len(p) < dailyOffset+7 || p[dailyOffset] != 3 || binary.LittleEndian.Uint16(p[dailyOffset+1:]) != 5 || binary.LittleEndian.Uint32(p[dailyOffset+3:]) != 3600 {
		t.Fatalf("native criminal record differs from bootstrap: %x", p)
	}
	c.PK = nil
	c.EventMembership = nil
	entry = ResolveLocalPlayerEntry(c, testRoster())
	if entry.PVPState != 0 || entry.ArenaTeam != 255 {
		t.Fatal("absent authority acquired a synthetic active team")
	}
}
