/*
===========================================================================

e2e_level_wire_test.go - progression through transport and persisted authority.

Three boots exercise disabled diagnostics, real level and stat transactions,
then restored state. Expected packets are independent byte literals.

===========================================================================
*/

package progression_test

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	wiretest "opensro.online/server/internal/game/internal"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/transport"
)

// Levelling lifecycle over the real wire and authority store.
const e2eLevelCharName = "e2eLvlTester"

/*
================
TestLevellingPathEndToEndOverWire

The real transport and store must agree on recovered gauges across restart.
================
*/
func TestLevellingPathEndToEndOverWire(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "authority")

	// Preflight: the scenario's three curve rows through the production
	// loader (a moved column fails HERE; the drift fixture pins the same
	// rows for the client half).
	textdata := shippedTextdataDir(t)
	levels := enterworld.NewTextdataLevels(textdata)
	for _, row := range []struct {
		level int64
		req   int64
	}{{1, 118}, {2, 470}, {3, 1058}} {
		req, ok := levels.ExpRequired(row.level)
		if !ok || req != row.req {
			t.Fatalf("shipped leveldata col 1: level %d requires %d/%v exp, want %d/true (column moved?)", row.level, req, ok, row.req)
		}
	}

	// The seed: a fresh-shaped level-1 character. StatPoints seeds at 1 -
	// deliberately not 0, 3 or 6 - so the S2 absolute tail (1 + 3x2 = 7)
	// cannot coincide with a per-level or per-grant value. The 500/400
	// maxima are the usual decoys: derived maxima must never echo them.
	seed := &enterworld.Character{
		Name:          e2eLevelCharName,
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     e2eInt64(enterworld.RaceChina),
		Gender:        e2eInt64(enterworld.GenderMale),
		Level:         e2eInt64(1),
		StatPoints:    e2eInt64(1),
		SkillPoints:   e2eInt64(0),
		GMPrivilege:   true,
		// The authority store supplies the racial base-skill seed required
		// by the current character schema.
		Skills: []uint32{},
	}

	// ---- boot A: the trigger gate (env off -> opcode never registered) ----
	t.Setenv(progression.EnvDevExpGrant, "0")
	gateServer := startProgressionServer(t, dir, seed)
	connA := dialProgressionWS(t, gateServer.srv)
	helloProgressionWS(t, connA)
	baselineA, _ := enterWorldAs(t, connA, e2eLevelCharName)
	if baselineA.Level != 1 || baselineA.Exp != 0 || baselineA.StatPoints != 1 {
		t.Fatalf("gate-boot baseline = L%d exp %d pts %d, want L1/0/1", baselineA.Level, baselineA.Exp, baselineA.StatPoints)
	}
	sendFrame(t, connA, progression.OpDevGrantExp, wire.NewWriter(8).U32(600).U32(0).Payload())
	// The transport FIFO barrier proves an accidentally registered trigger
	// did not put a 0x30D2 on the wire.
	wiretest.AssertQueueDrained(t, connA, "gate-boot")
	sendFrame(t, connA, transport.OpBye, []byte{transport.ByeReasonNormal})
	connA.Close()
	shutdownServer(t, gateServer.srv)
	gateServer.authority.Close()

	// ---- boot B: the live levelling script ----
	t.Setenv(progression.EnvDevExpGrant, "1")
	live := startProgressionServer(t, dir, nil)

	// The gid the 0x36B0 frame must carry (the store's own view).
	var playerGid uint32
	live.authority.ReadCharacters(e2eDivision, func(characters []*enterworld.Character) {
		if len(characters) != 1 {
			t.Fatalf("characters = %d, want 1", len(characters))
		}
		playerGid = enterworld.ObjectIDForCharacter(characters[0])
	})
	if playerGid == 0 {
		t.Fatal("playerGid = 0; ObjectIDForCharacter must map the seeded character")
	}

	conn := dialProgressionWS(t, live.srv)
	helloProgressionWS(t, conn)
	baseline, blobBefore := enterWorldAs(t, conn, e2eLevelCharName)
	if baseline.Level != 1 || baseline.Exp != 0 || baseline.SkillExp != 0 || baseline.StatPoints != 1 {
		t.Fatalf("baseline = L%d exp %d skillExp %d pts %d, want L1/0/0/1 (the gate boot must not have granted)",
			baseline.Level, baseline.Exp, baseline.SkillExp, baseline.StatPoints)
	}
	if blobBefore.Strength == nil || *blobBefore.Strength != enterworld.BaseStat {
		t.Fatalf("baseline blob strength = %v, want the creation base %d", blobBefore.Strength, enterworld.BaseStat)
	}

	// S1: +50 exp at L1 (50 < 118): exactly ONE 13-byte 0x30D2 - no
	// trailing u16, no companions (the next request's ack being the next
	// frame proves nothing else rode the burst). Expected bytes are
	// hand-written, NOT built with EncodeExpUpdate - the encoder is part
	// of what this test verifies.
	sendFrame(t, conn, progression.OpDevGrantExp, wire.NewWriter(8).U32(50).U32(0).Payload())
	assertBytes(t, expectFrame(t, conn, wire.OpExpUpdate, "S1 exp update"),
		[]byte{0, 0, 0, 0, 0x32, 0, 0, 0, 0, 0, 0, 0, 0}, "S1 0x30D2 (13 bytes, no crossing)")

	// S2: +550 exp: the full level-up burst in contract order.
	sendFrame(t, conn, progression.OpDevGrantExp, wire.NewWriter(8).U32(550).U32(0).Payload())
	wantGid := make([]byte, 4)
	binary.LittleEndian.PutUint32(wantGid, playerGid)
	assertBytes(t, expectFrame(t, conn, wire.OpLevelUpEffect, "S2 level-up effect"),
		wantGid, "S2 0x36B0 [playerGid]")
	// The user-visible payoff: levelling GREW the derived maxima through
	// the existing 0x343C path - 228 = trunc(1.02^(3-1) x 22 x 10) from
	// the auto +1/+1 growth, never the seeded 500/400 decoys.
	assertStatBlock(t, expectFrame(t, conn, wire.OpBaseStats, "S2 stat block"),
		uint16(enterworld.BaseStat)+2, uint16(enterworld.BaseStat)+2, 228, 228)
	wantVitals := append(append([]byte(nil), wantGid...), 0x80, 0, 3, 228, 0, 0, 0, 228, 0, 0, 0)
	assertBytes(t, expectFrame(t, conn, 0x33a6, "S2 recovered gauges"), wantVitals, "S2 HP/MP recovery")
	assertBytes(t, expectFrame(t, conn, wire.OpExpUpdate, "S2 exp update"),
		[]byte{0, 0, 0, 0, 0x26, 0x02, 0, 0, 0, 0, 0, 0, 0, 0x07, 0}, "S2 0x30D2 (15 bytes, absolute statPoints 7)")

	// S3: +STR spends one of the 7 granted points: ack, then a 0x343C
	// where maxHP 239 = trunc(1.02^2 x 230) DIFFERS from maxMP 228.
	sendFrame(t, conn, wire.OpAllocStrRequest, nil)
	assertBytes(t, expectFrame(t, conn, wire.OpAllocStrResponse, "S3 +STR ack"),
		[]byte{wire.ResultSuccess}, "S3 +STR ack")
	assertStatBlock(t, expectFrame(t, conn, wire.OpBaseStats, "S3 stat block"),
		uint16(enterworld.BaseStat)+3, uint16(enterworld.BaseStat)+2, 239, 228)

	// S4: +500 skill exp: the SP yield being ENABLED (COORD C5 ruling,
	// board seq 202) emits one 0x30B3 type 2 whose u32 is the ABSOLUTE
	// post-conversion SP (0 seeded + 500/400 = 1, notify=0), then the
	// 13-byte 0x30D2 (0x1f4 = 500). The mod-400 wrap persists as 100
	// (asserted after reboot, alongside the persisted SP).
	sendFrame(t, conn, progression.OpDevGrantExp, wire.NewWriter(8).U32(0).U32(500).Payload())
	assertBytes(t, expectFrame(t, conn, wire.OpPointsUpdate, "S4 SP yield"),
		[]byte{0x02, 0x01, 0, 0, 0, 0}, "S4 0x30B3 type 2 (absolute SP 1, notify=0)")
	assertBytes(t, expectFrame(t, conn, wire.OpExpUpdate, "S4 skill-exp update"),
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0xf4, 0x01, 0, 0, 0}, "S4 0x30D2 (skill exp only)")

	// Refusal probes, all SILENT (the dev trigger has no ack channel):
	// wrong length, then both-deltas-zero. The transport barrier proves
	// neither put anything on the wire.
	sendFrame(t, conn, progression.OpDevGrantExp, wire.NewWriter(4).U32(50).Payload())
	sendFrame(t, conn, progression.OpDevGrantExp, wire.NewWriter(8).U32(0).U32(0).Payload())
	wiretest.AssertQueueDrained(t, conn, "live-boot refusal probes")

	if dropped := live.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s)", dropped)
	}
	sendFrame(t, conn, transport.OpBye, []byte{transport.ByeReasonNormal})
	conn.Close()

	// ---- boot C: the reboot - the walk is PERSISTED state ----
	shutdownServer(t, live.srv)
	live.authority.Close()
	restartedServer := startProgressionServer(t, dir, nil)

	restartedServer.authority.ReadCharacters(e2eDivision, func(characters []*enterworld.Character) {
		if len(characters) != 1 {
			t.Fatalf("characters after reboot = %d, want 1", len(characters))
		}
		c := characters[0]
		if c.CurrentHP == nil || c.CurrentMP == nil || *c.CurrentHP != 228 || *c.CurrentMP != 228 {
			t.Errorf("restored gauges = %v/%v, want recovered 228/228", c.CurrentHP, c.CurrentMP)
		}
		if c.Level == nil || *c.Level != 3 {
			t.Errorf("restored level = %v, want 3", c.Level)
		}
		if c.MaxLevel == nil || *c.MaxLevel != 3 {
			t.Errorf("restored maxLevel watermark = %v, want 3", c.MaxLevel)
		}
		if c.Experience == nil || *c.Experience != 12 {
			t.Errorf("restored exp remainder = %v, want 12", c.Experience)
		}
		if c.SkillExp == nil || *c.SkillExp != 100 {
			t.Errorf("restored skillExp = %v, want the wrapped 100 (500 mod 400)", c.SkillExp)
		}
		if c.SkillPoints == nil || *c.SkillPoints != 1 {
			t.Errorf("restored skillPoints = %v, want the yielded 1 persisted", c.SkillPoints)
		}
		if got := enterworld.CharacterStrength(c); got != enterworld.BaseStat+3 {
			t.Errorf("restored strength = %d, want %d (+2 auto, +1 spent)", got, enterworld.BaseStat+3)
		}
		if got := enterworld.CharacterIntellect(c); got != enterworld.BaseStat+2 {
			t.Errorf("restored intellect = %d, want %d (+2 auto)", got, enterworld.BaseStat+2)
		}
		if c.StatPoints == nil || *c.StatPoints != 6 {
			t.Errorf("restored stat points = %v, want 6 (1 seed + 6 granted - 1 spent)", c.StatPoints)
		}
	})

	conn2 := dialProgressionWS(t, restartedServer.srv)
	helloProgressionWS(t, conn2)
	restored, blobAfter := enterWorldAs(t, conn2, e2eLevelCharName)
	if restored.Level != 3 || restored.MaxLevel != 3 {
		t.Fatalf("restored 0x32B3 level/maxLevel = %d/%d, want 3/3", restored.Level, restored.MaxLevel)
	}
	if restored.Exp != 12 || restored.SkillExp != 100 {
		t.Fatalf("restored 0x32B3 exp/skillExp = %d/%d, want 12/100", restored.Exp, restored.SkillExp)
	}
	if restored.StatPoints != 6 {
		t.Fatalf("restored 0x32B3 stat points = %d, want 6", restored.StatPoints)
	}
	if restored.SkillPoints != 1 {
		t.Fatalf("restored 0x32B3 skill points = %d, want the yielded 1", restored.SkillPoints)
	}
	if blobAfter.Strength == nil || *blobAfter.Strength != enterworld.BaseStat+3 {
		t.Fatalf("restored blob strength = %v, want %d", blobAfter.Strength, enterworld.BaseStat+3)
	}
	if blobAfter.Intellect == nil || *blobAfter.Intellect != enterworld.BaseStat+2 {
		t.Fatalf("restored blob intellect = %v, want %d", blobAfter.Intellect, enterworld.BaseStat+2)
	}

	// The reopened trigger continues from the PERSISTED remainder: +1 exp
	// (12+1 = 13 < 1058) is a plain 13-byte delta frame.
	sendFrame(t, conn2, progression.OpDevGrantExp, wire.NewWriter(8).U32(1).U32(0).Payload())
	assertBytes(t, expectFrame(t, conn2, wire.OpExpUpdate, "post-reboot exp update"),
		[]byte{0, 0, 0, 0, 0x01, 0, 0, 0, 0, 0, 0, 0, 0}, "post-reboot 0x30D2")

	if dropped := restartedServer.srv.Hub.Metrics().RateLimitedFrames; dropped != 0 {
		t.Fatalf("rate limiter clamped %d frame(s) on the reopened server", dropped)
	}
	sendFrame(t, conn2, transport.OpBye, []byte{transport.ByeReasonNormal})
}
